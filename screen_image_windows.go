//go:build windows

package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// The chosen picture is kept next to Livery's settings so uploads and the
// preview can re-read it (and it survives restarts).
func screenImagePath() string { return filepath.Join(exeDir(), "screen-image.dat") }

func init() {
	screenImageLoader = func() ([]byte, error) { return os.ReadFile(screenImagePath()) }
}

// screenImageHandler receives a picture from the panel as a data URL or
// plain base64, checks that it decodes, and stores it.
func screenImageHandler(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImageBytes*4/3+1024)
	var body struct {
		Data string `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "the image is too large (up to 20 MB)", 400)
		return
	}
	data := body.Data
	if i := strings.Index(data, ","); strings.HasPrefix(data, "data:") && i > 0 {
		data = data[i+1:]
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil || len(raw) == 0 {
		http.Error(w, "couldn't read that file", 400)
		return
	}
	if len(raw) > maxImageBytes {
		http.Error(w, "the image is too large (up to 20 MB)", 400)
		return
	}
	frames, _, err := loadImageFrames(raw)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	tmp := screenImagePath() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if err := os.Rename(tmp, screenImagePath()); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	n := len(frames)
	note := ""
	if n > maxImageFrames {
		note = "long animation: thinned to 60 frames"
		n = maxImageFrames
	}
	logf("keyboard screen: new picture, %d frame(s)", len(frames))
	writeJSON(w, map[string]any{"frames": n, "note": note})
}
