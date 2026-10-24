//go:build windows

package main

// Logitech HID++ 2.0 over a Lightspeed receiver. Message layout and the
// 0x8070 / 0x8071 RGB feature commands follow OpenRGB's Logitech controller.

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	vidLogitech = 0x046D
	hidppShort  = 0x10
	hidppLong   = 0x11
	swID        = 0x0A

	featRoot    = 0x0000
	featName    = 0x0005
	featRGB8070 = 0x8070
	featRGB8071 = 0x8071
)

var errOffline = errors.New("device offline or asleep")

type Receiver struct {
	short *HidDev // usage 1 (7-byte reports)
	long  *HidDev // usage 2 (20-byte reports)
	log   func(string, ...any)
}

func isLogiReceiver(h HidInfo) bool {
	return h.VID == vidLogitech && h.UsagePage == 0xFF00 && (h.Usage == 1 || h.Usage == 2) &&
		(h.PID == 0xC539 || h.PID == 0xC53A || h.PID == 0xC541 || h.PID == 0xC545 || h.PID == 0xC547 || h.PID == 0xC54D)
}

func OpenReceiver(all []HidInfo, logf func(string, ...any)) (*Receiver, error) {
	r := &Receiver{log: logf}
	for _, h := range all {
		if !isLogiReceiver(h) {
			continue
		}
		d, err := OpenHID(h)
		if err != nil {
			logf("  could not open %s: %v", h.Short(), err)
			continue
		}
		if h.Usage == 1 && r.short == nil {
			r.short = d
		} else if h.Usage == 2 && r.long == nil {
			r.long = d
		} else {
			d.Close()
		}
	}
	if r.long == nil {
		r.Close()
		return nil, errors.New("no Logitech Lightspeed receiver (long-report interface) found")
	}
	r.flush()
	return r, nil
}

func (r *Receiver) Close() {
	r.short.Close()
	r.long.Close()
}

func (r *Receiver) flush() {
	for _, d := range []*HidDev{r.short, r.long} {
		if d == nil {
			continue
		}
		for i := 0; i < 50; i++ {
			b, _ := d.Read(20 * time.Millisecond)
			if b == nil {
				break
			}
		}
	}
}

// request sends a long HID++ 2.0 request and waits for the matching reply.
func (r *Receiver) request(dev, featIdx, fn byte, params ...byte) ([]byte, error) {
	msg := make([]byte, 20)
	msg[0], msg[1], msg[2], msg[3] = hidppLong, dev, featIdx, fn<<4|swID
	copy(msg[4:], params)
	if err := r.long.Write(msg); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(600 * time.Millisecond)
	for time.Now().Before(deadline) {
		// replies arrive on the long interface; HID++ 1.0 errors on the short one
		if b, _ := r.long.Read(60 * time.Millisecond); b != nil && len(b) >= 4 && b[1] == dev {
			if b[2] == featIdx && b[3] == msg[3] {
				return b[4:], nil
			}
			if b[2] == 0xFF && b[3] == featIdx && len(b) > 5 && b[4] == msg[3] {
				return nil, fmt.Errorf("HID++ error 0x%02X", b[5])
			}
		}
		if r.short != nil {
			if b, _ := r.short.Read(20 * time.Millisecond); b != nil && len(b) >= 6 && b[1] == dev && b[2] == 0x8F {
				if b[5] == 0x09 || b[5] == 0x08 || b[5] == 0x04 {
					return nil, errOffline
				}
				return nil, fmt.Errorf("HID++ 1.0 error 0x%02X", b[5])
			}
		}
	}
	return nil, errOffline
}

func (r *Receiver) featureIndex(dev byte, feature uint16) (byte, error) {
	b, err := r.request(dev, 0, 0, byte(feature>>8), byte(feature))
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

func (r *Receiver) deviceName(dev byte) string {
	idx, err := r.featureIndex(dev, featName)
	if err != nil || idx == 0 {
		return ""
	}
	b, err := r.request(dev, idx, 0)
	if err != nil {
		return ""
	}
	n := int(b[0])
	var sb strings.Builder
	for sb.Len() < n {
		c, err := r.request(dev, idx, 1, byte(sb.Len()))
		if err != nil {
			break
		}
		for _, ch := range c {
			if ch == 0 || sb.Len() >= n {
				break
			}
			sb.WriteByte(ch)
		}
	}
	return sb.String()
}

type LogiZone struct {
	Index    byte
	Location uint16 // 1 primary, 2 logo
	Effects  []uint16
}

type LogiMouse struct {
	rx      *Receiver
	Dev     byte
	Name    string
	RGBPage uint16
	RGBIdx  byte
	Zones   []LogiZone
}

func zoneName(loc uint16) string {
	switch loc {
	case 1:
		return "primary"
	case 2:
		return "logo"
	case 3:
		return "left"
	case 4:
		return "right"
	}
	return fmt.Sprintf("loc%d", loc)
}

// FindMouse probes device slots 1..6 for an online device with RGB effects.
func (r *Receiver) FindMouse() (*LogiMouse, error) {
	var lastErr error = errOffline
	for dev := byte(1); dev <= 6; dev++ {
		m := &LogiMouse{rx: r, Dev: dev}
		for _, page := range []uint16{featRGB8070, featRGB8071} {
			idx, err := r.featureIndex(dev, page)
			if err != nil {
				lastErr = err
				break
			}
			if idx != 0 {
				m.RGBPage, m.RGBIdx = page, idx
				break
			}
		}
		if m.RGBIdx == 0 {
			if lastErr == nil || !errors.Is(lastErr, errOffline) {
				r.log("  slot %d: online, no RGB feature", dev)
			}
			continue
		}
		m.Name = r.deviceName(dev)
		if err := m.readZones(); err != nil {
			return m, err
		}
		return m, nil
	}
	return nil, lastErr
}

func (m *LogiMouse) readZones() error {
	if m.RGBPage != featRGB8070 {
		return fmt.Errorf("feature 0x%04X found; only 0x8070 is implemented so far", m.RGBPage)
	}
	b, err := m.rx.request(m.Dev, m.RGBIdx, 0)
	if err != nil {
		return err
	}
	count := int(b[0])
	for z := 0; z < count; z++ {
		zi, err := m.rx.request(m.Dev, m.RGBIdx, 1, byte(z))
		if err != nil {
			return err
		}
		zone := LogiZone{Index: zi[0], Location: uint16(zi[1])<<8 | uint16(zi[2])}
		for e := 0; e < int(zi[3]); e++ {
			ei, err := m.rx.request(m.Dev, m.RGBIdx, 2, byte(z), byte(e))
			if err != nil {
				return err
			}
			zone.Effects = append(zone.Effects, uint16(ei[2])<<8|uint16(ei[3]))
		}
		m.Zones = append(m.Zones, zone)
	}
	return nil
}

// SetStatic sets one zone to a fixed color.
func (m *LogiMouse) SetStatic(zone LogiZone, c RGB) error {
	eff := -1
	for i, id := range zone.Effects {
		if id == 0x0001 {
			eff = i
			break
		}
	}
	if eff < 0 {
		return fmt.Errorf("zone %d has no static effect", zone.Index)
	}
	params := make([]byte, 16)
	params[0], params[1] = zone.Index, byte(eff)
	params[2], params[3], params[4] = c.R, c.G, c.B
	_, err := m.rx.request(m.Dev, m.RGBIdx, 3, params...)
	return err
}

// SetSoftwareControl toggles host (software) control of the lighting.
func (m *LogiMouse) SetSoftwareControl(on bool) error {
	v := byte(0)
	if on {
		v = 1
	}
	_, err := m.rx.request(m.Dev, m.RGBIdx, 8, v, v)
	return err
}

// SetBreathing makes one zone breathe in color c with the given period.
func (m *LogiMouse) SetBreathing(zone LogiZone, c RGB, period time.Duration) error {
	eff := -1
	for i, id := range zone.Effects {
		if id == 0x000A {
			eff = i
			break
		}
	}
	if eff < 0 {
		return m.SetStatic(zone, c)
	}
	ms := uint16(period / time.Millisecond)
	params := make([]byte, 16)
	params[0], params[1] = zone.Index, byte(eff)
	params[2], params[3], params[4] = c.R, c.G, c.B
	params[5], params[6] = byte(ms>>8), byte(ms)
	params[8] = 100 // brightness
	_, err := m.rx.request(m.Dev, m.RGBIdx, 3, params...)
	return err
}
