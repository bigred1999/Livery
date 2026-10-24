//go:build windows

package main

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// tsWriter prefixes each write with a timestamp (used for log files).
type tsWriter struct{ w io.Writer }

func (t *tsWriter) Write(p []byte) (int, error) {
	if _, err := t.w.Write([]byte(time.Now().Format("15:04:05 "))); err != nil {
		return 0, err
	}
	return t.w.Write(p)
}

// Settings is what the control panel changes besides the theme itself.
type Settings struct {
	ThemeID    string `json:"themeId"`
	Brightness int    `json:"brightness"` // 5..100
	Effect     string `json:"effect"`     // static | breathe
	Speed      int    `json:"speed"`      // 1 (slow) .. 10 (fast)
}

func (s *Settings) normalize() {
	if s.Brightness < 5 || s.Brightness > 100 {
		s.Brightness = 100
	}
	if s.Effect != "breathe" {
		s.Effect = "static"
	}
	if s.Speed < 1 || s.Speed > 10 {
		s.Speed = 5
	}
	if s.ThemeID == "" {
		s.ThemeID = "rosso"
	}
}

// breathe period from speed: 1 -> 8 s, 10 -> 2 s
func (s Settings) period() time.Duration {
	return time.Duration(8000-(s.Speed-1)*667) * time.Millisecond
}

// DeviceStatus is reported to the control panel.
type DeviceStatus struct {
	Laptop  string `json:"laptop"`
	Lights  string `json:"lights"`
	GMBK    string `json:"gmbk"`
	Mouse   string `json:"mouse"`
	OpenRGB string `json:"openrgb"`
	At      string `json:"at"`
}

var forcePower bool // set by the "power" command to rewrite even if unchanged

var (
	devMu      sync.Mutex // one writer at a time to the hardware
	statusMu   sync.Mutex
	lastStatus DeviceStatus
)

func setStatus(f func(*DeviceStatus)) {
	statusMu.Lock()
	f(&lastStatus)
	lastStatus.At = time.Now().Format("15:04:05")
	statusMu.Unlock()
}

func getStatus() DeviceStatus {
	statusMu.Lock()
	defer statusMu.Unlock()
	return lastStatus
}

func errText(err error) string {
	if err == nil {
		return "ok"
	}
	return err.Error()
}

// applyLaptop sets the Alienware keyboard and AW-ELC lights.
func applyLaptop(all []HidInfo, r Resolved, s Settings) bool {
	ok := false
	for _, h := range all {
		switch detectAfxVersion(h) {
		case 5:
			if h.PID != 0xAAB1 {
				logf("keyboard %04X:%04X is not the mapped m18 R2 keyboard, skipping", h.VID, h.PID)
				continue
			}
			a, err := OpenAfx(h, logf)
			if err != nil {
				logf("laptop keyboard: %v", err)
				setStatus(func(d *DeviceStatus) { d.Laptop = err.Error() })
				continue
			}
			var firstErr error
			step := func(err error) {
				if err != nil && firstErr == nil {
					firstErr = err
				}
			}
			if s.Effect == "breathe" {
				tempo := byte(220 - (s.Speed-1)*22)
				step(a.SetBrightness(100, nil))
				step(a.V5GlobalEffect(2, tempo, r.Primary, RGB{}, 1))
			} else {
				step(a.ClearGlobalEffect())
				step(a.SetBrightness(100, nil))
				step(a.SetPerKey(r.Keys))
				step(a.Update())
			}
			a.Close()
			logf("laptop keyboard: %s (%v)", s.Effect, firstErr)
			setStatus(func(d *DeviceStatus) { d.Laptop = errText(firstErr) })
			ok = ok || firstErr == nil
		case 4:
			a, err := OpenAfx(h, logf)
			if err != nil {
				logf("AW-ELC: %v", err)
				setStatus(func(d *DeviceStatus) { d.Lights = err.Error() })
				continue
			}
			ids := []byte{elcRearLeft, elcRearRight, elcLid, elcPower[0]}
			var firstErr error
			step := func(err error) {
				if err != nil && firstErr == nil {
					firstErr = err
				}
			}
			step(a.SetBrightness(100, ids))
			if s.Effect == "breathe" {
				tempo := byte(200 - (s.Speed-1)*20)
				step(a.V4Breathe(elcRearLeft, r.RearLeft, 3, tempo))
				step(a.V4Breathe(elcRearRight, r.RearRight, 3, tempo))
				step(a.V4Breathe(elcLid, r.Lid, 3, tempo))
			} else {
				step(a.SetPerKey(map[byte]RGB{
					elcRearLeft: r.RearLeft, elcRearRight: r.RearRight, elcLid: r.Lid,
				}))
			}
			step(a.Update())
			// The power button follows power-state colors stored in the controller.
			// Writing them is persistent, so only do it when the color changes.
			if want := r.RearLeft.String() + r.RearRight.String() + r.Lid.String() + r.Power.String(); want != appliedPower() || forcePower {
				err := a.SetPowerSlots(map[byte]RGB{
					elcRearLeft: r.RearLeft, elcRearRight: r.RearRight, elcLid: r.Lid, elcPower[0]: r.Power,
				})
				logf("power button -> %s (%v)", want, err)
				step(err)
				if err == nil {
					setAppliedPower(want)
				}
			}
			a.Close()
			logf("AW-ELC: rear %s/%s lid %s power %s %s (%v)", r.RearLeft, r.RearRight, r.Lid, r.Power, s.Effect, firstErr)
			setStatus(func(d *DeviceStatus) { d.Lights = errText(firstErr) })
			ok = ok || firstErr == nil
		}
	}
	return ok
}

func applyGMBK(all []HidInfo, r Resolved, s Settings) bool {
	g, err := OpenGMBK(all, logf)
	if err != nil {
		logf("GMBK: %v", err)
		setStatus(func(d *DeviceStatus) { d.GMBK = "not connected" })
		return false
	}
	defer g.Close()
	effect := byte(GMBKStatic)
	speed := byte(50)
	if s.Effect == "breathe" {
		effect = GMBKBreathing
		speed = byte(10 * s.Speed)
	}
	global := GMBKZone{Effect: effect, Brightness: 100, Speed: speed, Color: r.GMBKBase}
	var zones [gmbkZoneCount]GMBKZone
	for i := range zones {
		if r.GMBKUse[i] {
			zones[i] = GMBKZone{Effect: effect, Brightness: 100, Speed: speed, Color: r.GMBKZones[i]}
		}
	}
	err = g.Apply(global, zones)
	setStatus(func(d *DeviceStatus) { d.GMBK = errText(err) })
	return err == nil
}

func applyMouseWith(rx *Receiver, r Resolved, s Settings) bool {
	m, err := rx.FindMouse()
	if err != nil {
		logf("mouse: %v", err)
		msg := err.Error()
		if err == errOffline {
			msg = "asleep (move it to wake)"
		}
		setStatus(func(d *DeviceStatus) { d.Mouse = msg })
		return false
	}
	if err := m.SetSoftwareControl(false); err != nil {
		logf("mouse sw-control off: %v", err)
	}
	var firstErr error
	for _, z := range m.Zones {
		c := r.MouseSide
		if z.Location == 2 {
			c = r.MouseLogo
		}
		var err error
		if s.Effect == "breathe" {
			err = m.SetBreathing(z, c, s.period())
		} else {
			err = m.SetStatic(z, c)
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	logf("mouse %q: logo %s side %s %s (%v)", m.Name, r.MouseLogo, r.MouseSide, s.Effect, firstErr)
	setStatus(func(d *DeviceStatus) { d.Mouse = errText(firstErr) })
	return firstErr == nil
}

func applyMouse(all []HidInfo, r Resolved, s Settings) bool {
	rx, err := OpenReceiver(all, logf)
	if err != nil {
		logf("mouse: %v", err)
		setStatus(func(d *DeviceStatus) { d.Mouse = "receiver not found" })
		return false
	}
	defer rx.Close()
	return applyMouseWith(rx, r, s)
}

// applyEverything is used by the one-shot "apply" command.
func applyEverything(t ThemeDef, s Settings) {
	devMu.Lock()
	defer devMu.Unlock()
	all, err := EnumerateHID()
	if err != nil {
		logf("enumerate failed: %v", err)
		return
	}
	r := t.Resolve(s.Brightness)
	logf("theme %q, brightness %d%%, %s", t.Name, s.Brightness, s.Effect)
	applyLaptop(all, r, s)
	applyGMBK(all, r, s)
	st := applyOpenRGB(t, s)
	setStatus(func(d *DeviceStatus) { d.OpenRGB = st })
	if !applyMouse(all, r, s) {
		logf("(if the mouse was asleep, move it and run again)")
	}
}

func runApply() {
	cfg := loadConfig()
	t, ok := findTheme(cfg.Settings.ThemeID, cfg.Custom)
	if !ok {
		logf("theme %q not found, using %s", cfg.Settings.ThemeID, BuiltinThemes[0].Name)
		t = BuiltinThemes[0]
	}
	applyEverything(t, cfg.Settings)
}

var _ = fmt.Sprintf

// applyOpenRGB paints every OpenRGB device Livery doesn't drive itself.
// Returns a short status for the control panel.
func applyOpenRGB(t ThemeDef, s Settings) string {
	o, err := ConnectOpenRGB()
	if err != nil {
		return "not running"
	}
	defer o.Close()
	ctrls, err := o.Controllers()
	if err != nil {
		logf("OpenRGB: %v", err)
		return "error"
	}
	painted := 0
	for _, c := range ctrls {
		if len(c.LEDNames) == 0 || handledNatively(c.Location) {
			continue
		}
		if err := o.SetLEDs(c.Index, c.ColorsFor(t, s.Brightness)); err != nil {
			logf("OpenRGB %q: %v", c.Name, err)
			continue
		}
		painted++
		logf("OpenRGB %q (%d LEDs) painted", c.Name, len(c.LEDNames))
	}
	if painted == 0 {
		return fmt.Sprintf("ok, %d device(s), none extra", len(ctrls))
	}
	return fmt.Sprintf("ok, %d painted", painted)
}
