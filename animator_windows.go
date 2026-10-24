//go:build windows

package main

// Runs the software effects (and Breathe on OpenRGB devices) frame by frame.
// Two loops: OpenRGB devices over the SDK connection, and the Alienware
// keyboard plus rear/lid lights over HID. The power button and devices that
// store lighting in flash (Glorious, the wireless mouse, bulbs) are never
// animated.

import (
	"math"
	"sync"
	"time"
)

var anim struct {
	sync.Mutex
	stop chan struct{}
}

func stopAnimator() {
	anim.Lock()
	if anim.stop != nil {
		close(anim.stop)
		anim.stop = nil
	}
	anim.Unlock()
}

func makeEffectParams(t ThemeDef, s Settings) effectParams {
	f := float64(s.Brightness) / 100
	e := effectParams{
		Name:      s.Effect,
		Speed:     float64(s.Speed-1) / 9,
		Intensity: float64(s.Intensity-1) / 9,
		Reverse:   s.Reverse,
		AccentCol: mustHex(t.Accent).Scale(f),
	}
	pal := t.Stripes
	if len(pal) == 0 {
		pal = []string{t.Base, t.Accent}
	}
	for _, h := range pal {
		e.Palette = append(e.Palette, mustHex(h).Scale(f))
	}
	return e
}

// startAnimator (re)starts the animation for the current settings. Static
// stops it; Breathe only animates OpenRGB devices (the others breathe in
// hardware); the software effects animate everything that can take it.
func startAnimator(t ThemeDef, s Settings) {
	stopAnimator()
	if s.Effect == "static" {
		return
	}
	stop := make(chan struct{})
	anim.Lock()
	anim.stop = stop
	anim.Unlock()
	e := makeEffectParams(t, s)
	start := time.Now()
	go animateOpenRGB(t, s, e, start, stop)
	if softwareEffect(s.Effect) {
		go animateAlienware(t, s, e, start, stop)
	}
}

func animateOpenRGB(t ThemeDef, s Settings, e effectParams, start time.Time, stop chan struct{}) {
	for {
		o, err := ConnectOpenRGB()
		if err == nil {
			ctrls, err := o.Controllers()
			type target struct {
				c   orgbController
				pts []lightPoint
			}
			var targets []target
			if err == nil {
				for _, c := range ctrls {
					if len(c.LEDNames) == 0 || handledNatively(c.Location) {
						continue
					}
					o.send(c.Index, orgbSetCustomMode, nil)
					cols := c.ColorsFor(t, s.Brightness)
					xs := c.ledX()
					var pts []lightPoint
					for i, col := range cols {
						pts = append(pts, lightPoint{X: xs[i], Color: col, Accent: col == e.AccentCol, Seed: c.Index*1009 + uint32(i)})
					}
					targets = append(targets, target{c, pts})
				}
			}
			tick := time.NewTicker(33 * time.Millisecond)
		frames:
			for len(targets) > 0 {
				select {
				case <-stop:
					tick.Stop()
					o.Close()
					return
				case <-tick.C:
					now := time.Since(start)
					for _, tg := range targets {
						out := make([]RGB, len(tg.pts))
						for i, p := range tg.pts {
							out[i] = EffectColor(p, now, e)
						}
						if err := o.UpdateLEDs(tg.c.Index, out); err != nil {
							break frames // connection lost: reconnect
						}
					}
				}
			}
			tick.Stop()
			o.Close()
		}
		select {
		case <-stop:
			return
		case <-time.After(10 * time.Second):
		}
	}
}

// quantize rounds colors so lights that differ only slightly share one
// command (the keyboard groups keys by identical color).
func quantize(c RGB) RGB {
	q := func(v uint8) uint8 { return uint8(math.Min(255, math.Round(float64(v)/6)*6)) }
	return RGB{q(c.R), q(c.G), q(c.B)}
}

func animateAlienware(t ThemeDef, s Settings, e effectParams, start time.Time, stop chan struct{}) {
	r := t.Resolve(s.Brightness)
	accent := map[byte]bool{}
	for _, id := range accentKeySets[t.AccentKeys] {
		accent[id] = true
	}
	var keyPts []lightPoint
	var keyIDs []byte
	for id, x := range m18KeyX {
		keyIDs = append(keyIDs, id)
		keyPts = append(keyPts, lightPoint{X: x, Color: r.Keys[id], Accent: accent[id], Seed: uint32(id)})
	}
	elcIDs := []byte{elcRearLeft, elcRearRight, elcLid}
	elcPts := []lightPoint{{X: 0, Color: r.RearLeft, Seed: 1001}, {X: 1, Color: r.RearRight, Seed: 1002}, {X: 0.5, Color: r.Lid, Seed: 1003}}

	devMu.Lock()
	all, _ := EnumerateHID()
	var kb, elc *AfxDevice
	for _, h := range all {
		switch v := detectAfxVersion(h); {
		case v == 5 && h.PID == 0xAAB1 && kb == nil:
			if a, err := OpenAfx(h, logf); err == nil {
				kb = a
				a.ClearGlobalEffect()
				a.SetBrightness(100, nil)
			}
		case v == 4 && elc == nil:
			if a, err := OpenAfx(h, logf); err == nil {
				elc = a
				a.SetBrightness(100, elcIDs)
			}
		}
	}
	devMu.Unlock()
	if kb == nil && elc == nil {
		return
	}
	defer func() {
		devMu.Lock()
		if kb != nil {
			kb.Close()
		}
		if elc != nil {
			elc.Close()
		}
		devMu.Unlock()
	}()
	logf("animating %s on the Alienware lights", e.Name)
	fails := 0
	for {
		select {
		case <-stop:
			return
		default:
		}
		begin := time.Now()
		now := begin.Sub(start)
		devMu.Lock()
		select {
		case <-stop: // settings changed while waiting: don't paint a stale frame
			devMu.Unlock()
			return
		default:
		}
		var err error
		if kb != nil {
			m := make(map[byte]RGB, len(keyIDs))
			for i, p := range keyPts {
				m[keyIDs[i]] = quantize(EffectColor(p, now, e))
			}
			if err = kb.SetPerKey(m); err == nil {
				err = kb.Update()
			}
		}
		if elc != nil && err == nil {
			m := map[byte]RGB{}
			for i, p := range elcPts {
				m[elcIDs[i]] = quantize(EffectColor(p, now, e))
			}
			if err = elc.SetPerKey(m); err == nil {
				err = elc.Update()
			}
		}
		devMu.Unlock()
		if err != nil {
			fails++
			if fails == 1 || fails%100 == 0 {
				logf("Alienware animation: %v", err)
			}
			if fails > 20 {
				logf("Alienware animation stopped after repeated errors")
				return
			}
			time.Sleep(200 * time.Millisecond)
			continue
		}
		fails = 0
		// ~25 frames a second, never faster than the hardware takes them
		if d := 40*time.Millisecond - time.Since(begin); d > 0 {
			select {
			case <-stop:
				return
			case <-time.After(d):
			}
		}
	}
}
