//go:build windows

package main

import (
	"encoding/json"
	"net/http"
	"time"
)

// Light finder: paint arbitrary light ids on the laptop controllers so we can
// see which physical LED each id drives.
type probeRequest struct {
	Target string `json:"target"` // keyboard | elc
	IDs    []int  `json:"ids"`
	Color  string `json:"color"`
	done   chan error
}

var probeCh = make(chan probeRequest)

func runProbe(all []HidInfo, p probeRequest) error {
	c, err := ParseHex(p.Color)
	if err != nil {
		return err
	}
	var ids []byte
	for _, id := range p.IDs {
		if id >= 0 && id < 256 {
			ids = append(ids, byte(id))
		}
	}
	want := 5
	if p.Target == "elc" {
		want = 4
	}
	for _, h := range all {
		if detectAfxVersion(h) != want {
			continue
		}
		a, err := OpenAfx(h, logf)
		if err != nil {
			return err
		}
		defer a.Close()
		if err := a.SetColor(ids, c); err != nil {
			return err
		}
		err = a.Update()
		logf("probe %s ids %v -> %s (%v)", p.Target, ids, c, err)
		return err
	}
	return nil
}

func probeHandler(w http.ResponseWriter, r *http.Request) {
	var p probeRequest
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	p.done = make(chan error, 1)
	select {
	case probeCh <- p:
	case <-time.After(5 * time.Second):
		http.Error(w, "busy", 503)
		return
	}
	select {
	case err := <-p.done:
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	case <-time.After(10 * time.Second):
		http.Error(w, "timeout", 504)
		return
	}
	writeJSON(w, map[string]string{"ok": "1"})
}
