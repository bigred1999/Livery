//go:build windows

package main

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modHid = windows.NewLazySystemDLL("hid.dll")

	procHidD_GetHidGuid            = modHid.NewProc("HidD_GetHidGuid")
	procHidD_GetAttributes         = modHid.NewProc("HidD_GetAttributes")
	procHidD_GetPreparsedData      = modHid.NewProc("HidD_GetPreparsedData")
	procHidD_FreePreparsedData     = modHid.NewProc("HidD_FreePreparsedData")
	procHidP_GetCaps               = modHid.NewProc("HidP_GetCaps")
	procHidP_GetValueCaps          = modHid.NewProc("HidP_GetValueCaps")
	procHidP_GetButtonCaps         = modHid.NewProc("HidP_GetButtonCaps")
	procHidD_SetFeature            = modHid.NewProc("HidD_SetFeature")
	procHidD_GetFeature            = modHid.NewProc("HidD_GetFeature")
	procHidD_SetOutputReport       = modHid.NewProc("HidD_SetOutputReport")
	procHidD_GetInputReport        = modHid.NewProc("HidD_GetInputReport")
	procHidD_GetProductString      = modHid.NewProc("HidD_GetProductString")
	procHidD_GetManufacturerString = modHid.NewProc("HidD_GetManufacturerString")
	procHidD_GetSerialNumberString = modHid.NewProc("HidD_GetSerialNumberString")
)

const hidpStatusSuccess = 0x00110000

const (
	hidpInput   = 0
	hidpOutput  = 1
	hidpFeature = 2
)

type hiddAttributes struct {
	Size          uint32
	VendorID      uint16
	ProductID     uint16
	VersionNumber uint16
	_             uint16
}

type hidpCaps struct {
	Usage                     uint16
	UsagePage                 uint16
	InputReportByteLength     uint16
	OutputReportByteLength    uint16
	FeatureReportByteLength   uint16
	Reserved                  [17]uint16
	NumberLinkCollectionNodes uint16
	NumberInputButtonCaps     uint16
	NumberInputValueCaps      uint16
	NumberInputDataIndices    uint16
	NumberOutputButtonCaps    uint16
	NumberOutputValueCaps     uint16
	NumberOutputDataIndices   uint16
	NumberFeatureButtonCaps   uint16
	NumberFeatureValueCaps    uint16
	NumberFeatureDataIndices  uint16
}

// ReportInfo describes one report ID found in a collection's descriptor.
type ReportInfo struct {
	Type string // input / output / feature
	ID   byte
	Bits int // payload bits declared for this ID (values only, approximate)
}

// HidInfo is everything we learn about one HID collection without writing to it.
type HidInfo struct {
	Path         string
	VID, PID     uint16
	Version      uint16
	UsagePage    uint16
	Usage        uint16
	InLen        int
	OutLen       int
	FeatLen      int
	Product      string
	Manufacturer string
	Serial       string
	Writable     bool
	Reports      []ReportInfo
	OpenErr      string
}

func (h HidInfo) Short() string {
	return fmt.Sprintf("%04X:%04X up=%04X u=%04X in=%d out=%d feat=%d %q", h.VID, h.PID, h.UsagePage, h.Usage, h.InLen, h.OutLen, h.FeatLen, strings.TrimSpace(h.Manufacturer+" "+h.Product))
}

func hidGUID() windows.GUID {
	var g windows.GUID
	procHidD_GetHidGuid.Call(uintptr(unsafe.Pointer(&g)))
	return g
}

func hidString(proc *windows.LazyProc, h windows.Handle) string {
	buf := make([]uint16, 256)
	r, _, _ := proc.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)*2))
	if r == 0 {
		return ""
	}
	return windows.UTF16ToString(buf)
}

func openPath(path string, access uint32, overlapped bool) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, err
	}
	flags := uint32(0)
	if overlapped {
		flags = windows.FILE_FLAG_OVERLAPPED
	}
	return windows.CreateFile(p, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, flags, 0)
}

func collectReports(prep uintptr, caps hidpCaps) []ReportInfo {
	type kind struct {
		name string
		rt   uintptr
		nVal uint16
		nBtn uint16
	}
	kinds := []kind{
		{"input", hidpInput, caps.NumberInputValueCaps, caps.NumberInputButtonCaps},
		{"output", hidpOutput, caps.NumberOutputValueCaps, caps.NumberOutputButtonCaps},
		{"feature", hidpFeature, caps.NumberFeatureValueCaps, caps.NumberFeatureButtonCaps},
	}
	var out []ReportInfo
	for _, k := range kinds {
		bits := map[byte]int{}
		order := []byte{}
		add := func(id byte, b int) {
			if _, ok := bits[id]; !ok {
				order = append(order, id)
			}
			bits[id] += b
		}
		if k.nVal > 0 {
			n := k.nVal
			buf := make([]byte, 72*int(n))
			r, _, _ := procHidP_GetValueCaps.Call(k.rt, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)), prep)
			if r == hidpStatusSuccess {
				for i := 0; i < int(n); i++ {
					c := buf[i*72:]
					id := c[2]
					bitSize := int(c[18]) | int(c[19])<<8
					count := int(c[20]) | int(c[21])<<8
					add(id, bitSize*count)
				}
			}
		}
		if k.nBtn > 0 {
			n := k.nBtn
			buf := make([]byte, 72*int(n))
			r, _, _ := procHidP_GetButtonCaps.Call(k.rt, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)), prep)
			if r == hidpStatusSuccess {
				for i := 0; i < int(n); i++ {
					c := buf[i*72:]
					add(c[2], 0)
				}
			}
		}
		for _, id := range order {
			out = append(out, ReportInfo{Type: k.name, ID: id, Bits: bits[id]})
		}
	}
	return out
}

// EnumerateHID lists every present HID collection. It only opens devices to
// read their descriptors; nothing is written.
func EnumerateHID() ([]HidInfo, error) {
	g := hidGUID()
	paths, err := windows.CM_Get_Device_Interface_List("", &g, windows.CM_GET_DEVICE_INTERFACE_LIST_PRESENT)
	if err != nil {
		return nil, err
	}
	var list []HidInfo
	for _, path := range paths {
		if path == "" {
			continue
		}
		info := HidInfo{Path: path}
		h, err := openPath(path, windows.GENERIC_READ|windows.GENERIC_WRITE, false)
		if err == nil {
			info.Writable = true
		} else {
			info.OpenErr = err.Error()
			h, err = openPath(path, 0, false)
			if err != nil {
				info.OpenErr += "; info-open: " + err.Error()
				list = append(list, info)
				continue
			}
		}
		var attr hiddAttributes
		attr.Size = uint32(unsafe.Sizeof(attr))
		if r, _, _ := procHidD_GetAttributes.Call(uintptr(h), uintptr(unsafe.Pointer(&attr))); r != 0 {
			info.VID, info.PID, info.Version = attr.VendorID, attr.ProductID, attr.VersionNumber
		}
		var prep uintptr
		if r, _, _ := procHidD_GetPreparsedData.Call(uintptr(h), uintptr(unsafe.Pointer(&prep))); r != 0 {
			var caps hidpCaps
			if r, _, _ := procHidP_GetCaps.Call(prep, uintptr(unsafe.Pointer(&caps))); r == hidpStatusSuccess {
				info.UsagePage, info.Usage = caps.UsagePage, caps.Usage
				info.InLen, info.OutLen, info.FeatLen = int(caps.InputReportByteLength), int(caps.OutputReportByteLength), int(caps.FeatureReportByteLength)
				info.Reports = collectReports(prep, caps)
			}
			procHidD_FreePreparsedData.Call(prep)
		}
		info.Product = hidString(procHidD_GetProductString, h)
		info.Manufacturer = hidString(procHidD_GetManufacturerString, h)
		info.Serial = hidString(procHidD_GetSerialNumberString, h)
		windows.CloseHandle(h)
		list = append(list, info)
	}
	return list, nil
}

// HidDev is an open HID collection.
type HidDev struct {
	Info HidInfo
	h    windows.Handle
	ev   windows.Handle
}

func OpenHID(info HidInfo) (*HidDev, error) {
	h, err := openPath(info.Path, windows.GENERIC_READ|windows.GENERIC_WRITE, true)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", info.Short(), err)
	}
	ev, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	return &HidDev{Info: info, h: h, ev: ev}, nil
}

func (d *HidDev) Close() {
	if d == nil {
		return
	}
	windows.CloseHandle(d.h)
	windows.CloseHandle(d.ev)
}

func pad(buf []byte, n int) []byte {
	if len(buf) >= n {
		return buf[:n]
	}
	out := make([]byte, n)
	copy(out, buf)
	return out
}

func (d *HidDev) SetFeature(buf []byte) error {
	b := pad(buf, d.Info.FeatLen)
	r, _, e := procHidD_SetFeature.Call(uintptr(d.h), uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	if r == 0 {
		return fmt.Errorf("SetFeature: %v", e)
	}
	return nil
}

func (d *HidDev) GetFeature(id byte) ([]byte, error) {
	b := make([]byte, d.Info.FeatLen)
	if len(b) == 0 {
		return nil, errors.New("no feature reports")
	}
	b[0] = id
	r, _, e := procHidD_GetFeature.Call(uintptr(d.h), uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	if r == 0 {
		return nil, fmt.Errorf("GetFeature: %v", e)
	}
	return b, nil
}

func (d *HidDev) SetOutputReport(buf []byte) error {
	b := pad(buf, d.Info.OutLen)
	r, _, e := procHidD_SetOutputReport.Call(uintptr(d.h), uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	if r == 0 {
		return fmt.Errorf("SetOutputReport: %v", e)
	}
	return nil
}

func (d *HidDev) GetInputReport(id byte) ([]byte, error) {
	b := make([]byte, d.Info.InLen)
	if len(b) == 0 {
		return nil, errors.New("no input reports")
	}
	b[0] = id
	r, _, e := procHidD_GetInputReport.Call(uintptr(d.h), uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	if r == 0 {
		return nil, fmt.Errorf("GetInputReport: %v", e)
	}
	return b, nil
}

// Write sends an output report through the interrupt pipe (WriteFile).
func (d *HidDev) Write(buf []byte) error {
	b := pad(buf, d.Info.OutLen)
	var ov windows.Overlapped
	ov.HEvent = d.ev
	windows.ResetEvent(d.ev)
	var n uint32
	err := windows.WriteFile(d.h, b, &n, &ov)
	if err != nil && err != windows.ERROR_IO_PENDING {
		return fmt.Errorf("write: %w", err)
	}
	if ev, _ := windows.WaitForSingleObject(d.ev, 1000); ev != windows.WAIT_OBJECT_0 {
		windows.CancelIoEx(d.h, &ov)
		windows.GetOverlappedResult(d.h, &ov, &n, true)
		return errors.New("write timeout")
	}
	if err := windows.GetOverlappedResult(d.h, &ov, &n, false); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}

// Read waits up to timeout for one input report. Returns nil, nil on timeout.
func (d *HidDev) Read(timeout time.Duration) ([]byte, error) {
	b := make([]byte, d.Info.InLen)
	if len(b) == 0 {
		return nil, errors.New("no input reports")
	}
	var ov windows.Overlapped
	ov.HEvent = d.ev
	windows.ResetEvent(d.ev)
	var n uint32
	err := windows.ReadFile(d.h, b, &n, &ov)
	if err != nil && err != windows.ERROR_IO_PENDING {
		return nil, fmt.Errorf("read: %w", err)
	}
	ev, _ := windows.WaitForSingleObject(d.ev, uint32(timeout/time.Millisecond))
	if ev != windows.WAIT_OBJECT_0 {
		windows.CancelIoEx(d.h, &ov)
		windows.GetOverlappedResult(d.h, &ov, &n, true)
		return nil, nil
	}
	if err := windows.GetOverlappedResult(d.h, &ov, &n, false); err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	return b[:n], nil
}

func isAccessDenied(err error) bool {
	var e syscall.Errno
	return errors.As(err, &e) && e == windows.ERROR_ACCESS_DENIED
}
