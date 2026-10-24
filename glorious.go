//go:build windows

package main

// Glorious GMBK 75% (3794:B000 ANSI, 3794:B002 ISO). Protocol taken from the
// device behaviour of the vendor's configuration app (CORE 2.1.23), studied for
// interoperability; no vendor code is included:
//   feature report 7, 264 bytes, on usage page 0xFF01 / usage 0x01
//   [1] request: 2 = SetLighting, 129 = GetProfile
//   SetLighting: [2] profile (1..3), [3] layer (0 = global layer, 1..3)
//   then 11 blocks of 8 bytes from offset 4: global zone, zones 1..10 (left to right columns)
//   block: effect, brightness 0-100, speed 0-100, direction, colorMode (1 = single color), R, G, B
//   effects: 0 off/inactive, 1 static, 2 breathing, 3 acid, 4 wave

import (
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

const (
	vidGlorious  = 0x3794
	gmbkReportID = 7
	gmbkSetLight = 2
	gmbkGetProf  = 129

	GMBKOff       = 0
	GMBKStatic    = 1
	GMBKBreathing = 2
	GMBKWave      = 4
)

type GMBKZone struct {
	Effect     byte
	Brightness byte
	Speed      byte
	Color      RGB
}

type GMBK struct {
	ctl    *HidDev // FF01/0001 feature reports
	events *HidDev // FF00/FF00 status input reports (optional)
	log    func(string, ...any)
}

func isGMBK(h HidInfo) bool {
	return h.VID == vidGlorious && (h.PID == 0xB000 || h.PID == 0xB002)
}

func OpenGMBK(all []HidInfo, logf func(string, ...any)) (*GMBK, error) {
	g := &GMBK{log: logf}
	for _, h := range all {
		if !isGMBK(h) {
			continue
		}
		switch {
		case h.UsagePage == 0xFF01 && h.Usage == 0x01 && h.FeatLen >= 264 && g.ctl == nil:
			d, err := OpenHID(h)
			if err != nil {
				return nil, err
			}
			g.ctl = d
		case h.UsagePage == 0xFF00 && h.Usage == 0xFF00 && h.InLen > 0 && g.events == nil:
			if d, err := OpenHID(h); err == nil {
				g.events = d
			}
		}
	}
	if g.ctl == nil {
		g.Close()
		return nil, errors.New("GMBK 75% control interface not found")
	}
	g.drain()
	return g, nil
}

func (g *GMBK) Close() {
	if g == nil {
		return
	}
	g.ctl.Close()
	g.events.Close()
}

func (g *GMBK) drain() {
	if g.events == nil {
		return
	}
	for i := 0; i < 20; i++ {
		if b, _ := g.events.Read(15 * time.Millisecond); b == nil {
			return
		}
	}
}

// send writes one command and reports the keyboard's status reply if any.
func (g *GMBK) send(buf []byte) (string, error) {
	g.drain()
	if err := g.ctl.SetFeature(buf); err != nil {
		return "", err
	}
	status := "no status"
	if g.events != nil {
		if b, _ := g.events.Read(250 * time.Millisecond); b != nil {
			status = hex.EncodeToString(b)
			if len(b) > 3 {
				switch b[3] {
				case 170:
					status += " (OK)"
				case 238:
					status += " (ERROR)"
				}
			}
		}
	}
	time.Sleep(60 * time.Millisecond)
	return status, nil
}

func newGMBKBuf(request byte) []byte {
	b := make([]byte, 264)
	b[0], b[1] = gmbkReportID, request
	return b
}

// ProfileLayer returns the keyboard's active profile and layer (both 1..3).
func (g *GMBK) ProfileLayer() (byte, byte, error) {
	if _, err := g.send(newGMBKBuf(gmbkGetProf)); err != nil {
		return 0, 0, err
	}
	r, err := g.ctl.GetFeature(gmbkReportID)
	if err != nil {
		return 0, 0, err
	}
	// Windows returns [report id][echoed request][profile][layer]...
	p, l := r[1], r[2]
	if r[1] == gmbkGetProf {
		p, l = r[2], r[3]
	}
	if p < 1 || p > 3 || l < 1 || l > 3 {
		return 0, 0, fmt.Errorf("unexpected profile/layer reply %s", hex.EncodeToString(r[:8]))
	}
	return p, l, nil
}

func putZone(b []byte, off int, z GMBKZone, global bool) {
	if !global && z.Effect == GMBKOff {
		return // inactive zone: follows the global block
	}
	b[off] = z.Effect
	b[off+1] = z.Brightness
	b[off+2] = z.Speed
	b[off+3] = 0 // direction
	b[off+4] = 1 // single color
	b[off+5], b[off+6], b[off+7] = z.Color.R, z.Color.G, z.Color.B
}

// SetLighting writes the global block plus optional per-zone overrides (index 0..9).
func (g *GMBK) SetLighting(profile, layer byte, global GMBKZone, zones [gmbkZoneCount]GMBKZone) (string, error) {
	b := newGMBKBuf(gmbkSetLight)
	b[2], b[3] = profile, layer
	putZone(b, 4, global, true)
	for i, z := range zones {
		putZone(b, 4+(i+1)*8, z, false)
	}
	return g.send(b)
}

// defaultGlobalLayer mirrors what CORE sends before a lighting change:
// the profile's layer 0 set to its default wave with every zone reset.
func (g *GMBK) defaultGlobalLayer(profile byte) (string, error) {
	b := newGMBKBuf(gmbkSetLight)
	b[2], b[3] = profile, 0
	global := []byte{GMBKWave, 100, 50, 0, 0, 8, 0, 0}
	zone := []byte{0, 100, 50, 0, 0, 8, 0, 0}
	copy(b[4:], global)
	for i := 0; i < gmbkZoneCount; i++ {
		copy(b[4+(i+1)*8:], zone)
	}
	return g.send(b)
}

// Apply writes a full lighting setup: global block plus up to 10 zone overrides.
func (g *GMBK) Apply(global GMBKZone, zones [gmbkZoneCount]GMBKZone) error {
	profile, layer, err := g.ProfileLayer()
	if err != nil {
		g.log("GMBK: could not read active profile (%v); using profile 1 layer 1", err)
		profile, layer = 1, 1
	}
	if _, err := g.defaultGlobalLayer(profile); err != nil {
		return err
	}
	st, err := g.SetLighting(profile, layer, global, zones)
	g.log("GMBK: profile %d layer %d, effect %d base %s -> %s (%v)", profile, layer, global.Effect, global.Color, st, err)
	return err
}
