//go:build windows

package main

import (
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const version = "1.5.0"

var logw io.Writer = os.Stdout

func logf(format string, a ...any) {
	fmt.Fprintf(logw, format+"\n", a...)
}

func setupLog(cmd string) func() {
	exe, _ := os.Executable()
	dir := filepath.Join(filepath.Dir(exe), "logs")
	os.MkdirAll(dir, 0o755)
	name := filepath.Join(dir, cmd+".txt")
	if fi, err := os.Stat(name); err == nil && fi.Size() > 512*1024 {
		os.Rename(name, name+".old")
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if cmd == "run" {
		flags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}
	f, err := os.OpenFile(name, flags, 0o644)
	if err != nil {
		return func() {}
	}
	logw = &tsWriter{f}
	logf("Livery %s - %s - %s", version, cmd, time.Now().Format("2006-01-02 15:04:05"))
	return func() { f.Close() }
}

func main() {
	cmd := "ui"
	if len(os.Args) > 1 {
		cmd = strings.ToLower(os.Args[1])
	}
	done := setupLog(cmd)
	defer done()
	defer func() {
		if r := recover(); r != nil {
			logf("CRASH: %v", r)
		}
	}()

	switch cmd {
	case "scan":
		runScan()
	case "apply":
		runApply()
	case "power":
		forcePower = true
		runApply()
	case "run":
		runBackground()
	case "ui":
		openPanel()
	case "stop":
		stopBackground()
	case "import-settings":
		dest := ""
		if len(os.Args) > 2 {
			dest = os.Args[2]
		}
		importSettings(dest)
	case "install":
		installAutostart()
	case "uninstall":
		uninstallAutostart()
	default:
		logf("unknown command %q (use ui, scan, apply, run, stop, install or uninstall)", cmd)
	}
	logf("done.")
}

func isGlorious(h HidInfo) bool {
	p := strings.ToLower(h.Manufacturer + " " + h.Product)
	return h.VID == 0x3794 || strings.Contains(p, "glorious") || strings.Contains(p, "gmbk")
}

func runScan() {
	all, err := EnumerateHID()
	if err != nil {
		logf("enumerate failed: %v", err)
		return
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].VID != all[j].VID {
			return all[i].VID < all[j].VID
		}
		if all[i].PID != all[j].PID {
			return all[i].PID < all[j].PID
		}
		return all[i].UsagePage < all[j].UsagePage || (all[i].UsagePage == all[j].UsagePage && all[i].Usage < all[j].Usage)
	})

	logf("\n== HID collections (%d) ==", len(all))
	for _, h := range all {
		tag := ""
		switch {
		case detectAfxVersion(h) == 4:
			tag = "  <- Alienware AW-ELC (APIv4)"
		case detectAfxVersion(h) == 5:
			tag = "  <- Alienware per-key keyboard (APIv5)"
		case isLogiReceiver(h):
			tag = "  <- Logitech receiver HID++"
		case isGlorious(h):
			tag = "  <- Glorious"
		case isAK820Pro(h) && h.UsagePage == ajazzUsagePage:
			tag = "  <- Ajazz AK820 Pro lighting"
		case isAK820Pro(h):
			tag = "  <- Ajazz AK820 Pro"
		}
		rw := "rw"
		if !h.Writable {
			rw = "ro"
		}
		logf("%s %s%s", rw, h.Short(), tag)
		if tag != "" || h.UsagePage >= 0xFF00 {
			for _, r := range h.Reports {
				logf("      %-7s report id 0x%02X  %d bits", r.Type, r.ID, r.Bits)
			}
		}
	}

	logf("\n== Alienware ==")
	for _, h := range all {
		if detectAfxVersion(h) != 4 {
			continue
		}
		a, err := OpenAfx(h, logf)
		if err != nil {
			logf("  %v", err)
			continue
		}
		s, err := a.Status()
		logf("  AW-ELC status: %d err=%v", s, err)
		a.Close()
	}
	for _, h := range all {
		if detectAfxVersion(h) == 5 {
			logf("  per-key keyboard found: %s (PID %04X; map for m18 R2 is AAB1)", h.Short(), h.PID)
		}
	}

	logf("\n== Logitech ==")
	rx, err := OpenReceiver(all, logf)
	if err != nil {
		logf("  %v", err)
	} else {
		m, err := rx.FindMouse()
		if m != nil {
			logf("  slot %d: %q RGB feature 0x%04X at index %d", m.Dev, m.Name, m.RGBPage, m.RGBIdx)
			for _, z := range m.Zones {
				logf("    zone %d (%s) effects: %04X", z.Index, zoneName(z.Location), z.Effects)
			}
		}
		if err != nil {
			logf("  mouse: %v", err)
		}
		rx.Close()
	}

	logf("\n== Glorious (read-only feature report dump) ==")
	for _, h := range all {
		if !isGlorious(h) {
			continue
		}
		logf("  %s writable=%v", h.Short(), h.Writable)
		logf("    path %s", h.Path)
		if h.FeatLen == 0 || !h.Writable {
			continue
		}
		d, err := OpenHID(h)
		if err != nil {
			logf("    open: %v", err)
			continue
		}
		seen := map[byte]bool{}
		for _, r := range h.Reports {
			if r.Type != "feature" || seen[r.ID] {
				continue
			}
			seen[r.ID] = true
			b, err := d.GetFeature(r.ID)
			if err != nil {
				logf("    feature 0x%02X: %v", r.ID, err)
				continue
			}
			logf("    feature 0x%02X: %s", r.ID, hex.EncodeToString(b))
		}
		d.Close()
	}
}
