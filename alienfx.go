//go:build windows

package main

// Alienware lighting, ported from T-Troll's AlienFX-SDK (alienfx-tools, MIT).
//   APIv4: Alienware AW-ELC (187C:0551) - output reports, 34 bytes, report id 0
//   APIv5: Darfon per-key keyboard (0D62:xxxx, usage 0xCC) - feature reports, id 0xCC

import (
	"fmt"
	"time"
)

const (
	vidAlienware = 0x187C
	vidDarfon    = 0x0D62

	v4Ready   = 33
	v4Busy    = 34
	v5WaitUpd = 0x80
)

type AfxDevice struct {
	dev     *HidDev
	version int
	inSet   bool
	log     func(string, ...any)
}

func detectAfxVersion(h HidInfo) int {
	switch h.VID {
	case vidDarfon:
		if h.Usage == 0xCC && h.OutLen == 0 && h.FeatLen > 0 {
			return 5
		}
	case vidAlienware:
		switch h.OutLen {
		case 34:
			return 4
		}
	}
	return 0
}

func OpenAfx(h HidInfo, logf func(string, ...any)) (*AfxDevice, error) {
	v := detectAfxVersion(h)
	if v == 0 {
		return nil, fmt.Errorf("not a supported AlienFX device: %s", h.Short())
	}
	d, err := OpenHID(h)
	if err != nil {
		return nil, err
	}
	return &AfxDevice{dev: d, version: v, log: logf}, nil
}

func (a *AfxDevice) Close() { a.dev.Close() }

func (a *AfxDevice) send(cmd []byte, mods map[int][]byte) error {
	var length int
	var reportID byte
	if a.version == 5 {
		length, reportID = a.dev.Info.FeatLen, 0xCC
	} else {
		length, reportID = a.dev.Info.OutLen, 0
	}
	buf := make([]byte, length)
	copy(buf, cmd)
	buf[0] = reportID
	for pos, v := range mods {
		copy(buf[pos:], v)
	}
	if a.version == 5 {
		return a.dev.SetFeature(buf)
	}
	return a.dev.SetOutputReport(buf)
}

// commands: first byte is a placeholder for the report id.
var (
	v4Control     = []byte{0, 0x03, 0x21, 0x00, 0x03, 0xff, 0xff}
	v4SetOneColor = []byte{0, 0x03, 0x27}
	v4TurnOn      = []byte{0, 0x03, 0x26}

	v5Reset     = []byte{0, 0x94}
	v5Status    = []byte{0, 0x93}
	v5ColorSet  = []byte{0, 0x8c, 0x02}
	v5Loop      = []byte{0, 0x8c, 0x13}
	v5Update    = []byte{0, 0x8b, 0x01, 0xff}
	v5TurnOnSet = []byte{0, 0x83, 0x38, 0x9c}
	v5SetEffect = []byte{0, 0x80, 0x02, 0x07, 0x00, 0x00, 0x01, 0x01, 0x01}
)

func (a *AfxDevice) Status() (byte, error) {
	switch a.version {
	case 5:
		if err := a.send(v5Status, nil); err != nil {
			return 0, err
		}
		b, err := a.dev.GetFeature(0xCC)
		if err != nil {
			return 0, err
		}
		return b[2], nil
	case 4:
		b, err := a.dev.GetInputReport(0)
		if err != nil {
			return 0, err
		}
		return b[2], nil
	}
	return 0, nil
}

func (a *AfxDevice) waitReady() {
	if a.version != 4 {
		return
	}
	for i := 0; i < 50; i++ {
		s, err := a.Status()
		if err != nil || s != v4Busy {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (a *AfxDevice) Reset() error {
	var err error
	switch a.version {
	case 5:
		err = a.send(v5Reset, nil)
		a.Status()
	case 4:
		a.waitReady()
		a.send(v4Control, map[int][]byte{4: {4}})
		err = a.send(v4Control, map[int][]byte{4: {1}})
	}
	a.inSet = err == nil
	return err
}

func (a *AfxDevice) Update() error {
	if !a.inSet {
		return nil
	}
	var err error
	switch a.version {
	case 5:
		err = a.send(v5Update, nil)
	case 4:
		err = a.send(v4Control, nil)
	}
	a.inSet = false
	return err
}

// SetColor sets the same color on a list of light ids (SDK light indexes).
func (a *AfxDevice) SetColor(ids []byte, c RGB) error {
	if !a.inSet {
		if err := a.Reset(); err != nil {
			return err
		}
	}
	switch a.version {
	case 5:
		length := a.dev.Info.FeatLen
		for i := 0; i < len(ids); {
			mods := map[int][]byte{}
			for pos := 4; pos+4 <= length && i < len(ids); pos += 4 {
				mods[pos] = []byte{ids[i] + 1, c.R, c.G, c.B}
				i++
			}
			if err := a.send(v5ColorSet, mods); err != nil {
				return err
			}
		}
		return a.send(v5Loop, nil)
	case 4:
		for start := 0; start < len(ids); start += 26 {
			end := start + 26
			if end > len(ids) {
				end = len(ids)
			}
			chunk := ids[start:end]
			mods := map[int][]byte{3: {c.R, c.G, c.B, 0, byte(len(chunk))}, 8: chunk}
			if err := a.send(v4SetOneColor, mods); err != nil {
				return err
			}
			if end < len(ids) {
				a.Update()
				a.Reset()
			}
		}
	}
	return nil
}

// ClearGlobalEffect switches a v5 keyboard from a hardware effect back to per-key colors.
func (a *AfxDevice) ClearGlobalEffect() error {
	if a.version != 5 {
		return nil
	}
	if a.inSet {
		a.Update()
	}
	a.Reset()
	if err := a.send(v5SetEffect, map[int][]byte{2: {1, 0xfe}}); err != nil {
		return err
	}
	return a.Update()
}

// SetBrightness 0..100 (percent).
func (a *AfxDevice) SetBrightness(pct int, ids []byte) error {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	switch a.version {
	case 5:
		if a.inSet {
			a.Update()
		}
		a.Reset()
		return a.send(v5TurnOnSet, map[int][]byte{4: {byte(pct * 0xff / 100)}})
	case 4:
		if a.inSet {
			a.Update()
		}
		mods := map[int][]byte{3: {byte(0x64 - pct), 0, byte(len(ids))}, 6: ids}
		return a.send(v4TurnOn, mods)
	}
	return nil
}

// SetPerKey sets many lights to (possibly) different colors in one session.
// Lights sharing a color are sent together.
func (a *AfxDevice) SetPerKey(colors map[byte]RGB) error {
	groups := map[RGB][]byte{}
	var order []RGB
	for id, c := range colors {
		if _, ok := groups[c]; !ok {
			order = append(order, c)
		}
		groups[c] = append(groups[c], id)
	}
	for _, c := range order {
		ids := groups[c]
		sortBytes(ids)
		if err := a.SetColor(ids, c); err != nil {
			return err
		}
	}
	return nil
}

func sortBytes(b []byte) {
	for i := 1; i < len(b); i++ {
		for j := i; j > 0 && b[j] < b[j-1]; j-- {
			b[j], b[j-1] = b[j-1], b[j]
		}
	}
}

var (
	v4ColorSel = []byte{0, 0x03, 0x23, 0x01, 0x00, 0x01}
	v4ColorSet = []byte{0, 0x03, 0x24, 0x00, 0x07, 0xd0, 0x00, 0xfa}
)

// V4Breathe makes one AW-ELC light morph between c and off, looping.
// tempo/phase are 0..255 (bigger = slower).
func (a *AfxDevice) V4Breathe(id byte, c RGB, phase, tempo byte) error {
	if a.version != 4 {
		return nil
	}
	if !a.inSet {
		if err := a.Reset(); err != nil {
			return err
		}
	}
	if err := a.send(v4ColorSel, map[int][]byte{6: {id}}); err != nil {
		return err
	}
	mods := map[int][]byte{
		3:  {2, phase, 0xcf, 0, tempo, c.R, c.G, c.B},
		11: {2, phase, 0xcf, 0, tempo, 0, 0, 0},
	}
	return a.send(v4ColorSet, mods)
}

// V5GlobalEffect starts a hardware effect on the whole v5 keyboard.
// effect: 2 breathing, 3 single-color wave, 8 pulse. tempo 0..255.
func (a *AfxDevice) V5GlobalEffect(effect, tempo byte, c1, c2 RGB, colors byte) error {
	if a.version != 5 {
		return nil
	}
	if a.inSet {
		a.Update()
	}
	a.Reset()
	if err := a.send(v5SetEffect, map[int][]byte{2: {effect, tempo}, 9: {colors - 1, c1.R, c1.G, c1.B, c2.R, c2.G, c2.B}}); err != nil {
		return err
	}
	return a.Update()
}

// ---- power button (APIv4), ported from AlienFX-SDK Functions::SetPowerAction ----

const (
	actColor  = 0
	actPulse  = 1
	actMorph  = 2
	actBreath = 3
	actPower  = 6
)

type afxAction struct {
	Type, Time, Tempo byte
	C                 RGB
}

var (
	v4OpCodes  = []byte{0xd0, 0xdc, 0xcf, 0xdc, 0x82, 0xac, 0xe8}
	v4SetPower = []byte{0, 0x03, 0x22}
)

func (a *AfxDevice) setV4Action(index byte, acts []afxAction) error {
	if err := a.send(v4ColorSel, map[int][]byte{6: {index}}); err != nil {
		return err
	}
	length := a.dev.Info.OutLen
	for i := 0; i < len(acts); {
		mods := map[int][]byte{}
		for bPos := 3; bPos < length && i < len(acts); bPos += 8 {
			ca := acts[i]
			t := ca.Type
			if t >= actBreath {
				t = actMorph
			}
			tempo := ca.Tempo
			if ca.Type == actColor {
				tempo = 0xfa
			}
			mods[bPos] = []byte{t, ca.Time, v4OpCodes[ca.Type], 0, tempo, ca.C.R, ca.C.G, ca.C.B}
			i++
		}
		if err := a.send(v4ColorSet, mods); err != nil {
			return err
		}
	}
	return nil
}

// SetPowerButton stores the power-button colors for every power state
// (AC = plugged in, Batt = on battery) in the AW-ELC controller.
func (a *AfxDevice) SetPowerButton(ids []byte, ac, batt RGB) error {
	if a.version != 4 {
		return nil
	}
	if a.inSet {
		a.Update()
	}
	base := []afxAction{{actPower, 3, 0x64, ac}, {actPower, 3, 0x64, batt}}
	off := afxAction{actPower, 3, 0x64, RGB{}}
	for cid := byte(0x5b); cid < 0x61; cid++ {
		if err := a.send(v4SetPower, map[int][]byte{4: {4, 0, cid}}); err != nil {
			return err
		}
		if err := a.send(v4SetPower, map[int][]byte{4: {1, 0, cid}}); err != nil {
			return err
		}
		t := append([]afxAction{}, base...)
		switch cid {
		case 0x5b: // AC sleep
			t = append(t, base[0], off)
		case 0x5c: // AC power
			t = append(t, base[0])
			t[0].Type = actColor
		case 0x5d: // charging
			t = append(t, base[0], base[1])
		case 0x5e: // battery sleep
			t = append(t, base[1], off)
		case 0x5f: // battery power
			t = append(t, base[1])
			t[0].Type = actColor
		case 0x60: // battery critical
			t = append(t, base[1])
			t[0].Type = actPulse
		}
		for _, id := range ids {
			if err := a.setV4Action(id, t); err != nil {
				return err
			}
		}
		if err := a.send(v4SetPower, map[int][]byte{4: {2, 0, cid}}); err != nil {
			return err
		}
	}
	if err := a.send(v4Control, map[int][]byte{4: {5}}); err != nil {
		return err
	}
	a.waitReady()
	return nil
}

// ---- m18 AW-ELC power-state slots, sequence from digin1/alienware-m18-rgb ----
// On the m18 R2 the power button (light 4) only shows colors stored in the
// power-state animation slots; each slot must be saved and set as default.

const (
	elcPowerAnim   = 0x22
	elcStartSeries = 0x23
	elcAddAction   = 0x24
	elcDimming     = 0x26

	elcStartNew   = 0x01
	elcFinishSave = 0x02
	elcRemove     = 0x04
	elcSetDefault = 0x06

	slotACSleep    = 0x5b
	slotACCharged  = 0x5c
	slotACCharging = 0x5d
	slotDCSleep    = 0x5e
	slotDCOn       = 0x5f
)

// elcCmd sends [0x03, payload...] as output report 0 and reads the reply.
func (a *AfxDevice) elcCmd(payload ...byte) error {
	buf := make([]byte, a.dev.Info.OutLen)
	buf[1] = 0x03
	copy(buf[2:], payload)
	if err := a.dev.SetOutputReport(buf); err != nil {
		return err
	}
	a.dev.GetInputReport(0)
	time.Sleep(4 * time.Millisecond)
	return nil
}

func (a *AfxDevice) elcAnim(op byte, slot byte) error {
	return a.elcCmd(elcPowerAnim, 0x00, op, 0x00, slot)
}

// elcSlot writes one power-state slot: one series per color group.
func (a *AfxDevice) elcSlot(slot byte, colors map[byte]RGB) error {
	if err := a.elcAnim(elcRemove, slot); err != nil {
		return err
	}
	if err := a.elcAnim(elcStartNew, slot); err != nil {
		return err
	}
	ids := make([]byte, 0, len(colors))
	for id := range colors {
		ids = append(ids, id)
	}
	sortBytes(ids)
	for _, id := range ids {
		c := colors[id]
		if err := a.elcCmd(elcStartSeries, 1, 0, 1, id); err != nil {
			return err
		}
		// effect 0 (color), duration 0xffff, tempo 0x0001, r, g, b
		if err := a.elcCmd(elcAddAction, 0x00, 0xff, 0xff, 0x00, 0x01, c.R, c.G, c.B); err != nil {
			return err
		}
	}
	if err := a.elcAnim(elcFinishSave, slot); err != nil {
		return err
	}
	return a.elcAnim(elcSetDefault, slot)
}

// SetPowerSlots stores colors for every power state (lights on while awake,
// off while asleep) and undims all accent lights including the power button.
func (a *AfxDevice) SetPowerSlots(colors map[byte]RGB) error {
	if a.version != 4 {
		return nil
	}
	if a.inSet {
		a.Update()
	}
	ids := make([]byte, 0, len(colors))
	off := map[byte]RGB{}
	for id := range colors {
		ids = append(ids, id)
		off[id] = RGB{}
	}
	sortBytes(ids)
	if err := a.elcCmd(append([]byte{elcDimming, 0, 0, byte(len(ids))}, ids...)...); err != nil {
		return err
	}
	for _, s := range []struct {
		slot byte
		c    map[byte]RGB
	}{
		{slotACSleep, off}, {slotACCharged, colors}, {slotACCharging, colors},
		{slotDCSleep, off}, {slotDCOn, colors},
	} {
		if err := a.elcSlot(s.slot, s.c); err != nil {
			return err
		}
	}
	return nil
}
