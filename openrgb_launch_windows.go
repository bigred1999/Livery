//go:build windows

package main

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// processRunning reports whether a process with this image name (e.g.
// "openrgb.exe") is running, other than this one.
func processRunning(image string) bool {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snap)
	self := windows.GetCurrentProcessId()
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	for err = windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		if pe.ProcessID != self && strings.EqualFold(windows.UTF16ToString(pe.ExeFile[:]), image) {
			return true
		}
	}
	return false
}

// findOpenRGB looks for OpenRGB.exe in the places its installer and the
// portable zip usually end up.
func findOpenRGB() string {
	var dirs []string
	for _, env := range []string{"ProgramFiles", "ProgramW6432", "ProgramFiles(x86)"} {
		if p := os.Getenv(env); p != "" {
			dirs = append(dirs, filepath.Join(p, "OpenRGB"))
		}
	}
	if p := os.Getenv("LOCALAPPDATA"); p != "" {
		dirs = append(dirs, filepath.Join(p, "Programs", "OpenRGB"))
	}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Join(filepath.Dir(exe), "OpenRGB"))
	}
	for _, d := range dirs {
		p := filepath.Join(d, "OpenRGB.exe")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func openRGBServerUp() bool {
	c, err := net.DialTimeout("tcp", orgbAddr, 400*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// openRGBMissingStatus explains why OpenRGB couldn't be reached.
func openRGBMissingStatus() string {
	if findOpenRGB() == "" {
		return "not installed"
	}
	if processRunning("OpenRGB.exe") {
		return "open, but SDK server is off"
	}
	return "not running"
}

// ensureOpenRGB starts OpenRGB minimized to the tray with its SDK server on,
// when it's installed but nothing is listening (for example the OpenRGB
// service wasn't installed). It never starts a second copy. Returns true if
// the server came up because of this call.
func ensureOpenRGB() bool {
	if openRGBServerUp() {
		return false
	}
	exe := findOpenRGB()
	if exe == "" || processRunning("OpenRGB.exe") {
		return false
	}
	cmd := exec.Command(exe, "--server", "--startminimized")
	cmd.Dir = filepath.Dir(exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP}
	if err := cmd.Start(); err != nil {
		logf("OpenRGB: could not start %s: %v", exe, err)
		return false
	}
	cmd.Process.Release()
	logf("OpenRGB: started %s --server --startminimized", exe)
	// Device detection can take a while on the first run.
	for i := 0; i < 60; i++ {
		time.Sleep(time.Second)
		if openRGBServerUp() {
			time.Sleep(3 * time.Second) // let it finish listing devices
			return true
		}
	}
	logf("OpenRGB: server did not come up")
	return false
}
