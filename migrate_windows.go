//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// importSettings copies config.json from an existing Livery copy (a running
// livery.exe, or the one registered to start at sign-in) into dest, so moving
// from a portable copy to the installed one keeps your liveries. It never
// overwrites a config that's already in dest. Used by the installer.
func importSettings(dest string) {
	if dest == "" {
		logf("import: no destination")
		return
	}
	target := filepath.Join(dest, "config.json")
	if _, err := os.Stat(target); err == nil {
		logf("import: %s already has settings", dest)
		return
	}
	for _, dir := range liveryDirs() {
		if strings.EqualFold(filepath.Clean(dir), filepath.Clean(dest)) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, "config.json"))
		if err != nil {
			continue
		}
		os.MkdirAll(dest, 0o755)
		if err := os.WriteFile(target, b, 0o644); err != nil {
			logf("import: %v", err)
			return
		}
		logf("import: copied settings from %s", dir)
		return
	}
	logf("import: no earlier settings found")
}

// liveryDirs lists folders of other Livery copies: running ones first, then
// the start-at-sign-in entry.
func liveryDirs() []string {
	var dirs []string
	self := windows.GetCurrentProcessId()
	if snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0); err == nil {
		var pe windows.ProcessEntry32
		pe.Size = uint32(unsafe.Sizeof(pe))
		for err = windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
			if pe.ProcessID == self || !strings.EqualFold(windows.UTF16ToString(pe.ExeFile[:]), "livery.exe") {
				continue
			}
			h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pe.ProcessID)
			if err != nil {
				continue
			}
			buf := make([]uint16, windows.MAX_LONG_PATH)
			n := uint32(len(buf))
			if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) == nil {
				dirs = append(dirs, filepath.Dir(windows.UTF16ToString(buf[:n])))
			}
			windows.CloseHandle(h)
		}
		windows.CloseHandle(snap)
	}
	if k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE); err == nil {
		if v, _, err := k.GetStringValue(runValueName); err == nil {
			exe := strings.Trim(strings.Fields(strings.TrimSpace(v) + " ")[0], `"`)
			if strings.HasPrefix(strings.TrimSpace(v), `"`) {
				if i := strings.Index(v[1:], `"`); i >= 0 {
					exe = v[1 : i+1]
				}
			}
			dirs = append(dirs, filepath.Dir(exe))
		}
		k.Close()
	}
	return dirs
}
