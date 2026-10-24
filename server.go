//go:build windows

package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
)

//go:embed ui.html
var uiHTML []byte

type stateResponse struct {
	Version   string       `json:"version"`
	Themes    []ThemeDef   `json:"themes"`
	Settings  Settings     `json:"settings"`
	Status    DeviceStatus `json:"status"`
	Autostart bool         `json:"autostart"`
}

func buildState() stateResponse {
	cfg := loadConfig()
	themes := append([]ThemeDef{}, BuiltinThemes...)
	themes = append(themes, cfg.Custom...)
	return stateResponse{
		Version:   version,
		Themes:    themes,
		Settings:  cfg.Settings,
		Status:    getStatus(),
		Autostart: autostartEnabled(),
	}
}

// guard rejects anything that isn't the local control panel talking to us:
// wrong Host (DNS rebinding), or a state-changing request without our header
// (cross-site form posts can't add custom headers without a CORS preflight,
// which we never approve).
func guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if host != panelAddr && host != "localhost:47820" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet {
			if r.Header.Get("X-Livery") != "1" || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func requestApply() DeviceStatus {
	req := applyRequest{done: make(chan DeviceStatus, 1)}
	select {
	case applyCh <- req:
	case <-time.After(5 * time.Second):
		return getStatus()
	}
	select {
	case st := <-req.done:
		return st
	case <-time.After(20 * time.Second):
		return getStatus()
	}
}

var hexRe = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

func validTheme(t *ThemeDef) error {
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" || len(t.Name) > 40 {
		return fmt.Errorf("name must be 1-40 characters")
	}
	colors := []*string{&t.Base, &t.Accent, &t.RearLeft, &t.RearRight, &t.Lid, &t.Power, &t.MouseLogo, &t.MouseSide}
	for _, c := range colors {
		if !hexRe.MatchString(*c) {
			return fmt.Errorf("bad color %q", *c)
		}
		*c = strings.ToUpper(*c)
	}
	if len(t.Stripes) > 5 {
		return fmt.Errorf("at most 5 stripes")
	}
	for i := range t.Stripes {
		if !hexRe.MatchString(t.Stripes[i]) {
			return fmt.Errorf("bad stripe color %q", t.Stripes[i])
		}
	}
	if _, ok := accentKeySets[t.AccentKeys]; !ok {
		t.AccentKeys = "none"
	}
	var zones []int
	for _, z := range t.GMBKAccent {
		if z >= 1 && z <= gmbkZoneCount {
			zones = append(zones, z)
		}
	}
	t.GMBKAccent = zones
	t.Builtin = false
	return nil
}

func slug(name string) string {
	s := strings.ToLower(name)
	s = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, "-")
	return "custom-" + strings.Trim(s, "-")
}

func startPanel() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", guard(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(uiHTML)
	}))
	mux.HandleFunc("/api/ping", guard(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"app": "livery", "ok": version})
	}))
	mux.HandleFunc("/api/state", guard(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, buildState())
	}))
	// Apply settings (theme id, brightness, effect, speed) and save them.
	mux.HandleFunc("/api/apply", guard(func(w http.ResponseWriter, r *http.Request) {
		var s Settings
		if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		cfg := loadConfig()
		if _, ok := findTheme(s.ThemeID, cfg.Custom); !ok {
			http.Error(w, "unknown theme", 400)
			return
		}
		cfg.Settings = s
		if err := saveConfig(cfg); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		logf("panel: theme %s, %s, brightness %d, speed %d", s.ThemeID, s.Effect, s.Brightness, s.Speed)
		requestApply()
		writeJSON(w, buildState())
	}))
	// Save (create or update) a custom theme, optionally applying it.
	mux.HandleFunc("/api/themes/save", guard(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Theme ThemeDef `json:"theme"`
			Apply bool     `json:"apply"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		t := body.Theme
		if err := validTheme(&t); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if t.ID == "" || !strings.HasPrefix(t.ID, "custom-") {
			t.ID = slug(t.Name)
		}
		cfg := loadConfig()
		replaced := false
		for i := range cfg.Custom {
			if cfg.Custom[i].ID == t.ID {
				cfg.Custom[i], replaced = t, true
			}
		}
		if !replaced {
			if len(cfg.Custom) >= 30 {
				http.Error(w, "too many custom themes", 400)
				return
			}
			cfg.Custom = append(cfg.Custom, t)
		}
		if body.Apply {
			cfg.Settings.ThemeID = t.ID
		}
		if err := saveConfig(cfg); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		logf("panel: saved theme %q", t.Name)
		if body.Apply {
			requestApply()
		}
		writeJSON(w, buildState())
	}))
	mux.HandleFunc("/api/themes/delete", guard(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		cfg := loadConfig()
		out := cfg.Custom[:0]
		for _, t := range cfg.Custom {
			if t.ID != body.ID {
				out = append(out, t)
			}
		}
		cfg.Custom = out
		reapply := false
		if cfg.Settings.ThemeID == body.ID {
			cfg.Settings.ThemeID = BuiltinThemes[0].ID
			reapply = true
		}
		saveConfig(cfg)
		if reapply {
			requestApply()
		}
		writeJSON(w, buildState())
	}))
	// Preview a theme without saving it.
	mux.HandleFunc("/api/preview", guard(func(w http.ResponseWriter, r *http.Request) {
		var t ThemeDef
		if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if t.Name == "" {
			t.Name = "Preview"
		}
		if err := validTheme(&t); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		t.ID = "custom-preview"
		cfg := loadConfig()
		out := cfg.Custom[:0]
		for _, c := range cfg.Custom {
			if c.ID != t.ID {
				out = append(out, c)
			}
		}
		cfg.Custom = append(out, t)
		cfg.Settings.ThemeID = t.ID
		saveConfig(cfg)
		requestApply()
		writeJSON(w, buildState())
	}))
	mux.HandleFunc("/api/probe", guard(probeHandler))
	mux.HandleFunc("/api/reapply", guard(func(w http.ResponseWriter, r *http.Request) {
		requestApply()
		writeJSON(w, buildState())
	}))
	mux.HandleFunc("/api/autostart", guard(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if err := setAutostart(body.Enabled); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, buildState())
	}))

	ln, err := net.Listen("tcp", panelAddr)
	if err != nil {
		logf("control panel: %v", err)
		return
	}
	logf("control panel at %s", panelURL)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	if err := srv.Serve(ln); err != nil {
		logf("control panel stopped: %v", err)
	}
}
