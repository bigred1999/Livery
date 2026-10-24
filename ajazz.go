//go:build windows

package main

// Ajazz AK820 Pro (Sonix SN32F290, USB 0C45:8009 wireless / 0C45:800A wired).
// Whole-keyboard lighting modes over 64-byte feature reports on the vendor
// control interface (usage page 0xFF13). Command layout from the protocol
// notes in ak820pro-linux (https://github.com/ErvinUrban-Bratislava/ak820pro-linux,
// MIT, Copyright (c) 2024-2026 Ervin Urban). The keyboard exposes no per-key
// lighting command, so it shows the livery's main color.

import (
	"errors"
	"fmt"
	"math"
	"time"
)

const (
	ajazzVID       = 0x0C45
	ajazzUsagePage = 0xFF13

	ajazzModeOff    = 0
	ajazzModeStatic = 1
	ajazzModeBreath = 7
)

func isAK820Pro(h HidInfo) bool {
	return h.VID == ajazzVID && (h.PID == 0x8009 || h.PID == 0x800A)
}

type AK820Pro struct{ dev *HidDev }

func OpenAK820Pro(all []HidInfo) (*AK820Pro, error) {
	var lastErr error = errors.New("not connected")
	for _, h := range all {
		if !isAK820Pro(h) || h.UsagePage != ajazzUsagePage {
			continue
		}
		if h.FeatLen < 65 {
			lastErr = fmt.Errorf("unexpected feature report size %d", h.FeatLen)
			continue
		}
		d, err := OpenHID(h)
		if err != nil {
			lastErr = err
			continue
		}
		return &AK820Pro{dev: d}, nil
	}
	return nil, lastErr
}

func (a *AK820Pro) Close() { a.dev.Close() }

// cmd sends a 64-byte command (report ID 0 in front) and reads the
// acknowledgement the firmware expects before the next command.
func (a *AK820Pro) cmd(pkt []byte) error {
	if err := a.dev.SetFeature(append([]byte{0}, pkt...)); err != nil {
		return err
	}
	time.Sleep(50 * time.Millisecond)
	_, err := a.dev.GetFeature(0)
	return err
}

func ak820Cmd(op byte, enable bool) []byte {
	p := make([]byte, 64)
	p[0], p[1] = 0x04, op
	if enable {
		p[8] = 1
	}
	return p
}

// ak820ModePacket builds the lighting-mode payload. brightness and speed are 0..5.
func ak820ModePacket(mode byte, c RGB, brightness, speed byte) []byte {
	p := make([]byte, 64)
	p[0] = mode
	p[1], p[2], p[3] = c.R, c.G, c.B
	p[8] = 0 // not rainbow
	p[9] = min(brightness, 5)
	p[10] = min(speed, 5)
	p[11] = 0 // direction
	p[14], p[15] = 0x55, 0xAA
	return p
}

// SetMode applies a lighting mode without writing it to the keyboard's flash
// (Livery re-applies it when the keyboard reconnects).
func (a *AK820Pro) SetMode(mode byte, c RGB, brightness, speed byte) error {
	for _, step := range [][]byte{
		ak820Cmd(0x18, true), // start
		ak820Cmd(0x13, true), // lighting mode follows
		ak820ModePacket(mode, c, brightness, speed),
		ak820Cmd(0xF0, true), // finish (no flash save)
	} {
		if err := a.cmd(step); err != nil {
			return err
		}
	}
	return nil
}

func applyAjazz(all []HidInfo, t ThemeDef, s Settings) bool {
	k, err := OpenAK820Pro(all)
	if err != nil {
		setStatus(func(d *DeviceStatus) { d.Ajazz = "not connected" })
		return false
	}
	defer k.Close()
	// Full-strength color; brightness goes to the keyboard's own 0-5 scale.
	c := mustHex(t.Base)
	if len(t.Stripes) > 0 {
		c, _ = t.stripeAt(0.5)
	}
	level := byte(math.Round(float64(s.Brightness) / 20))
	mode, speed := byte(ajazzModeStatic), byte(3)
	if level == 0 || c == (RGB{}) {
		mode, level = ajazzModeOff, 0
	} else if s.Effect == "breathe" {
		mode = ajazzModeBreath
		speed = byte(min(5, max(1, s.Speed/2)))
	}
	err = k.SetMode(mode, c, level, speed)
	if err != nil {
		logf("Ajazz AK820 Pro: %v", err)
	} else {
		logf("Ajazz AK820 Pro: mode %d color %s brightness %d", mode, c, level)
	}
	setStatus(func(d *DeviceStatus) { d.Ajazz = errText(err) })
	return err == nil
}
