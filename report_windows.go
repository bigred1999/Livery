//go:build windows

package main

// One-click support report: a zip on the Desktop with everything needed to
// troubleshoot Livery on someone else's PC. It only reads: devices are listed,
// never opened for writing. Secrets (Tuya keys and secret, the cloud project
// ID) are left out, and the Windows user name is masked in paths and logs.

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// redactor hides the Windows user name and profile path.
func redactor() func(string) string {
	var pairs []string
	if p := os.Getenv("USERPROFILE"); p != "" {
		pairs = append(pairs, p, `%USERPROFILE%`, strings.ReplaceAll(p, `\`, `\\`), `%USERPROFILE%`)
	}
	if u := os.Getenv("USERNAME"); len(u) > 2 {
		pairs = append(pairs, u, "<user>")
	}
	r := strings.NewReplacer(pairs...)
	return r.Replace
}

func windowsVersion() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE)
	if err != nil {
		return "unknown"
	}
	defer k.Close()
	name, _, _ := k.GetStringValue("ProductName")
	disp, _, _ := k.GetStringValue("DisplayVersion")
	build, _, _ := k.GetStringValue("CurrentBuildNumber")
	return strings.TrimSpace(fmt.Sprintf("%s %s (build %s)", name, disp, build))
}

// writeReport writes the human-readable part of the report.
func writeReport(w io.Writer) {
	p := func(f string, a ...any) { fmt.Fprintf(w, f+"\n", a...) }
	cfg := loadConfig()
	p("Livery support report")
	p("Livery %s, %s, %s/%s", version, time.Now().Format("2006-01-02 15:04:05 MST"), runtime.GOOS, runtime.GOARCH)
	p("Windows: %s", windowsVersion())
	exe, _ := os.Executable()
	p("Livery location: %s", exe)
	p("Background mode running: %v   Start at sign-in: %v", panelUp(), autostartEnabled())

	p("\n== Status (from the running Livery) ==")
	if st, ok := fetchRunningState(); ok {
		b, _ := json.MarshalIndent(st, "", "  ")
		p("%s", b)
	} else {
		p("(background mode not running)")
	}

	p("\n== Settings ==")
	s := cfg.Settings
	p("theme %s, brightness %d%%, effect %s, speed %d", s.ThemeID, s.Brightness, s.Effect, s.Speed)
	p("custom liveries: %d", len(cfg.Custom))
	if cfg.Screen != nil {
		p("keyboard screen: %s body %s accent %s glow %s", cfg.Screen.Model, cfg.Screen.Body, cfg.Screen.Accent, cfg.Screen.Glow)
	}
	p("smart bulbs: cloud project set=%v region=%q bulbs=%d", cfg.Tuya.AccessID != "", cfg.Tuya.Region, len(cfg.Tuya.Bulbs))
	for _, b := range cfg.Tuya.Bulbs {
		p("  %-24q category=%-5s enabled=%-5v role=%-6s ip=%-15s protocol=%s layout=%s", b.Name, b.Category, b.Enabled, b.Role, b.IP, b.Version, b.Layout)
	}

	p("\n== OpenRGB ==")
	p("installed at: %q   server reachable: %v   process running: %v", findOpenRGB(), openRGBServerUp(), processRunning("OpenRGB.exe"))
	if o, err := ConnectOpenRGB(); err == nil {
		ctrls, err := o.Controllers()
		o.Close()
		if err != nil {
			p("controller list failed: %v", err)
		}
		for _, c := range ctrls {
			names := c.LEDNames
			if len(names) > 6 {
				names = append(append([]string{}, names[:6]...), "...")
			}
			skip := ""
			if handledNatively(c.Location) {
				skip = "  (driven by Livery itself)"
			}
			p("  [%d] %q type=%d vendor=%q location=%q zones=%d leds=%d %v%s", c.Index, c.Name, c.Type, c.Vendor, c.Location, len(c.Zones), len(c.LEDNames), names, skip)
		}
	}

	p("\n== USB / HID devices (read only) ==")
	all, err := EnumerateHID()
	if err != nil {
		p("enumerate failed: %v", err)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].VID != all[j].VID {
			return all[i].VID < all[j].VID
		}
		if all[i].PID != all[j].PID {
			return all[i].PID < all[j].PID
		}
		return all[i].UsagePage < all[j].UsagePage
	})
	for _, h := range all {
		tag := ""
		switch {
		case detectAfxVersion(h) == 4:
			tag = "  <- Alienware AW-ELC"
		case detectAfxVersion(h) == 5:
			tag = "  <- Alienware per-key keyboard"
		case isLogiReceiver(h):
			tag = "  <- Logitech receiver"
		case isGlorious(h):
			tag = "  <- Glorious"
		case isAK820Pro(h):
			tag = "  <- Ajazz AK820 Pro"
		}
		rw := "rw"
		if !h.Writable {
			rw = "ro"
		}
		p("%s %s%s", rw, h.Short(), tag)
		if h.UsagePage >= 0xFF00 || tag != "" {
			for _, r := range h.Reports {
				p("      %-7s report id 0x%02X  %d bits", r.Type, r.ID, r.Bits)
			}
		}
	}
}

func panelClient() *http.Client { return &http.Client{Timeout: 3 * time.Second} }

// fetchRunningState asks the background process for its live status.
func fetchRunningState() (any, bool) {
	resp, err := panelClient().Get(panelURL + "api/state")
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	var st struct {
		Version string       `json:"version"`
		Status  DeviceStatus `json:"status"`
		Bulbs   tuyaView     `json:"bulbs"`
		Update  updateInfo   `json:"update"`
	}
	if json.NewDecoder(resp.Body).Decode(&st) != nil {
		return nil, false
	}
	st.Bulbs.AccessID = "" // keep the cloud project ID out of the report
	return st, true
}

// CreateSupportReport writes Livery-report-<time>.zip to the Desktop (or the
// Downloads folder, or next to Livery) and returns its path.
func CreateSupportReport() (string, error) {
	dir := ""
	for _, id := range []*windows.KNOWNFOLDERID{windows.FOLDERID_Desktop, windows.FOLDERID_Downloads} {
		if d, err := windows.KnownFolderPath(id, 0); err == nil {
			if st, err := os.Stat(d); err == nil && st.IsDir() {
				dir = d
				break
			}
		}
	}
	if dir == "" {
		dir = exeDir()
	}
	path := filepath.Join(dir, "Livery-report-"+time.Now().Format("2006-01-02-1504")+".zip")
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	z := zip.NewWriter(f)
	red := redactor()

	var sb strings.Builder
	writeReport(&sb)
	if w, err := z.Create("report.txt"); err == nil {
		io.WriteString(w, red(sb.String()))
	}
	// Recent logs (last 256 KB of each), with the user name masked.
	logs, _ := filepath.Glob(filepath.Join(exeDir(), "logs", "*.txt"))
	for _, lp := range logs {
		b, err := os.ReadFile(lp)
		if err != nil {
			continue
		}
		if len(b) > 256<<10 {
			b = b[len(b)-256<<10:]
		}
		if w, err := z.Create("logs/" + filepath.Base(lp)); err == nil {
			io.WriteString(w, red(string(b)))
		}
	}
	if err := z.Close(); err != nil {
		return "", err
	}
	logf("support report saved to %s", red(path))
	return path, nil
}

// showReportInExplorer opens the folder with the report selected.
func showReportInExplorer(path string) {
	exec.Command("explorer.exe", "/select,", path).Start()
}

// runReport is the "livery.exe report" command (Start menu shortcut).
func runReport() {
	path, err := CreateSupportReport()
	title, _ := windows.UTF16PtrFromString("Livery")
	if err != nil {
		msg, _ := windows.UTF16PtrFromString("Couldn't create the support report:\n" + err.Error())
		windows.MessageBox(0, msg, title, windows.MB_OK|windows.MB_ICONWARNING)
		return
	}
	showReportInExplorer(path)
	msg, _ := windows.UTF16PtrFromString("Saved " + filepath.Base(path) + " to " + filepath.Dir(path) + ".\n\nSend this file to whoever is helping you, or attach it to an issue at github.com/bigred1999/Livery/issues. Passwords and keys are not included.")
	windows.MessageBox(0, msg, title, windows.MB_OK|windows.MB_ICONINFORMATION)
}
