//go:build windows

package main

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// killLegacy ends DucatiSync, the private prototype Livery grew out of
// (ducatisync.exe / ducatisync-v4.exe), so the two never fight over the
// lights. Only those exact image names are touched, and never this process.
func killLegacy() {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return
	}
	defer windows.CloseHandle(snap)
	self := windows.GetCurrentProcessId()
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	for err = windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		name := strings.ToLower(windows.UTF16ToString(pe.ExeFile[:]))
		if (name != "ducatisync.exe" && name != "ducatisync-v4.exe") || pe.ProcessID == self {
			continue
		}
		h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, pe.ProcessID)
		if err != nil {
			logf("could not stop DucatiSync (pid %d): %v", pe.ProcessID, err)
			continue
		}
		if err := windows.TerminateProcess(h, 0); err == nil {
			logf("stopped DucatiSync (pid %d)", pe.ProcessID)
			windows.WaitForSingleObject(h, 3000)
		}
		windows.CloseHandle(h)
	}
}
