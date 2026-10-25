//go:build windows

package main

// Panel routes for "Your ride": analyse a vehicle photo into a livery, and
// make / set the matching desktop wallpaper.

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	modUser32                = windows.NewLazySystemDLL("user32.dll")
	procEnumDisplaySettingsW = modUser32.NewProc("EnumDisplaySettingsW")
	procSystemParametersInfo = modUser32.NewProc("SystemParametersInfoW")
	procGetSystemMetrics     = modUser32.NewProc("GetSystemMetrics")
)

// The latest vehicle photo, kept next to Livery's settings so wallpapers
// can be re-rendered after a restart. "Remove photo" deletes it.
func vehiclePhotoPath() string { return filepath.Join(exeDir(), "vehicle-photo.dat") }

func loadVehiclePhoto() (image.Image, error) {
	b, err := os.ReadFile(vehiclePhotoPath())
	if err != nil {
		return nil, errors.New("add a photo of your vehicle first")
	}
	frames, _, err := loadImageFrames(b)
	if err != nil {
		return nil, err
	}
	return frames[0], nil
}

// devModeW is DEVMODEW (display variant), 220 bytes.
type devModeW struct {
	DeviceName         [32]uint16
	SpecVersion        uint16
	DriverVersion      uint16
	Size               uint16
	DriverExtra        uint16
	Fields             uint32
	PositionX          int32
	PositionY          int32
	DisplayOrientation uint32
	DisplayFixedOutput uint32
	Color              int16
	Duplex             int16
	YResolution        int16
	TTOption           int16
	Collate            int16
	FormName           [32]uint16
	LogPixels          uint16
	BitsPerPel         uint32
	PelsWidth          uint32
	PelsHeight         uint32
	DisplayFlags       uint32
	DisplayFrequency   uint32
	ICM                [8]uint32
}

// Compile-time check that the struct matches Windows' layout.
var _ [220 - unsafe.Sizeof(devModeW{})]byte
var _ [unsafe.Sizeof(devModeW{}) - 220]byte

// screenResolution returns the main display's real pixel size (not the
// DPI-scaled one), so the wallpaper is sharp.
func screenResolution() (int, int) {
	var dm devModeW
	dm.Size = uint16(unsafe.Sizeof(dm))
	const enumCurrentSettings = 0xFFFFFFFF
	if r, _, _ := procEnumDisplaySettingsW.Call(0, enumCurrentSettings, uintptr(unsafe.Pointer(&dm))); r != 0 && dm.PelsWidth >= 320 && dm.PelsHeight >= 240 {
		return int(dm.PelsWidth), int(dm.PelsHeight)
	}
	w, _, _ := procGetSystemMetrics.Call(0) // SM_CXSCREEN
	h, _, _ := procGetSystemMetrics.Call(1) // SM_CYSCREEN
	if w >= 320 && h >= 240 {
		return int(w), int(h)
	}
	return 2560, 1440
}

// wallpaperDir is Pictures\Livery (or next to Livery if Pictures is gone).
func wallpaperDir() string {
	if d, err := windows.KnownFolderPath(windows.FOLDERID_Pictures, 0); err == nil {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			return filepath.Join(d, "Livery")
		}
	}
	return filepath.Join(exeDir(), "wallpapers")
}

// setDesktopWallpaper shows the picture on the desktop, filling the screen.
func setDesktopWallpaper(path string) error {
	if k, err := registry.OpenKey(registry.CURRENT_USER, `Control Panel\Desktop`, registry.SET_VALUE); err == nil {
		k.SetStringValue("WallpaperStyle", "10") // Fill
		k.SetStringValue("TileWallpaper", "0")
		k.Close()
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	const spiSetDeskWallpaper = 0x0014
	const spifUpdateIniFile, spifSendChange = 0x01, 0x02
	r, _, e := procSystemParametersInfo.Call(spiSetDeskWallpaper, 0, uintptr(unsafe.Pointer(p)), spifUpdateIniFile|spifSendChange)
	if r == 0 {
		return fmt.Errorf("Windows didn't accept the wallpaper: %v", e)
	}
	return nil
}

func decodeDataURL(s string) ([]byte, error) {
	if i := strings.Index(s, ","); strings.HasPrefix(s, "data:") && i > 0 {
		s = s[i+1:]
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(raw) == 0 {
		return nil, errors.New("couldn't read that file")
	}
	if len(raw) > maxImageBytes {
		return nil, errors.New("the image is too large (up to 20 MB)")
	}
	return raw, nil
}

func registerPhotoRoutes(mux *http.ServeMux) {
	// Analyse a photo: store it and propose a livery.
	mux.HandleFunc("/api/photo/analyze", guard(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxImageBytes*4/3+1024)
		var body struct {
			Data string `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "the image is too large (up to 20 MB)", 400)
			return
		}
		raw, err := decodeDataURL(body.Data)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		res, err := AnalyzeVehiclePhoto(raw)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		tmp := vehiclePhotoPath() + ".tmp"
		if err := os.WriteFile(tmp, raw, 0o600); err == nil {
			os.Rename(tmp, vehiclePhotoPath())
		}
		sw, sh := screenResolution()
		logf("your ride: photo analysed -> %s / %s", res.Theme.Base, res.Theme.Accent)
		writeJSON(w, map[string]any{"livery": res, "screen": map[string]int{"w": sw, "h": sh}})
	}))
	// A small preview of the wallpaper, same shape as the screen.
	mux.HandleFunc("/api/photo/wallpaper-preview", guard(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		sw, sh := screenResolution()
		pw := 960
		ph := pw * sh / sw
		if n, _ := strconv.Atoi(q.Get("w")); n > 0 && n <= 1600 {
			pw, ph = n, n*sh/sw
		}
		o := WallpaperOpts{Style: q.Get("style"), Colors: strings.Split(q.Get("colors"), ","), W: pw, H: ph}
		var photo image.Image
		if o.Style == "photo" {
			var err error
			if photo, err = loadVehiclePhoto(); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
		}
		b, err := WallpaperPNG(o, photo)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(b)
	}))
	// Render at full screen resolution, save to Pictures\Livery and
	// optionally make it the desktop wallpaper.
	mux.HandleFunc("/api/photo/wallpaper", guard(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			WallpaperOpts
			Name string `json:"name"`
			Set  bool   `json:"set"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		o := body.WallpaperOpts
		if o.W == 0 || o.H == 0 {
			o.W, o.H = screenResolution()
		}
		var photo image.Image
		if o.Style == "photo" {
			var err error
			if photo, err = loadVehiclePhoto(); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
		}
		b, err := WallpaperPNG(o, photo)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		dir := wallpaperDir()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		name := strings.TrimPrefix(slug(body.Name), "custom-")
		if name == "" {
			name = "livery"
		}
		path := filepath.Join(dir, fmt.Sprintf("%s-%s-%dx%d.png", name, o.Style, o.W, o.H))
		if err := os.WriteFile(path, b, 0o644); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		set := false
		if body.Set {
			if err := setDesktopWallpaper(path); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			set = true
		} else {
			showReportInExplorer(path) // opens Pictures\Livery with it selected
		}
		logf("your ride: wallpaper %s (set=%v)", filepath.Base(path), set)
		writeJSON(w, map[string]any{"file": filepath.Base(path), "folder": dir, "set": set})
	}))
	// Forget the stored photo.
	mux.HandleFunc("/api/photo/forget", guard(func(w http.ResponseWriter, r *http.Request) {
		os.Remove(vehiclePhotoPath())
		writeJSON(w, map[string]bool{"ok": true})
	}))
}
