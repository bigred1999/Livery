package main

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
)

// blob builds a v3 controller description exactly as OpenRGB's
// RGBController::GetDeviceDescriptionData writes it.
func blob() []byte {
	var b bytes.Buffer
	w := func(v any) { binary.Write(&b, binary.LittleEndian, v) }
	str := func(s string) { w(uint16(len(s) + 1)); b.WriteString(s); b.WriteByte(0) }
	w(int32(5)) // keyboard
	str("Test KB")
	str("TestVendor")
	str("desc")
	str("1.0")
	str("SN")
	str(`HID: \\?\HID#VID_1234&PID_5678`)
	w(uint16(2)) // modes
	w(int32(0))  // active
	for i, name := range []string{"Direct", "Static"} {
		str(name)
		w(int32(i))    // value
		w(uint32(0))   // flags
		w(uint32(0))   // speed min
		w(uint32(0))   // speed max
		w(uint32(0))   // brightness min
		w(uint32(100)) // brightness max
		w(uint32(1))   // colors min
		w(uint32(1))   // colors max
		w(uint32(0))   // speed
		w(uint32(100)) // brightness
		w(uint32(0))   // direction
		w(uint32(1))   // color mode
		w(uint16(i))   // num colors
		for c := 0; c < i; c++ {
			w(uint32(0x00112233))
		}
	}
	w(uint16(2)) // zones
	str("Keyboard")
	w(int32(2)) // matrix
	w(uint32(4))
	w(uint32(4))
	w(uint32(4))
	mm := []uint32{0, 1, 0xFFFFFFFF, 2, 0xFFFFFFFF, 3}
	w(uint16(8 + 4*len(mm)))
	w(uint32(2)) // height
	w(uint32(3)) // width
	for _, v := range mm {
		w(v)
	}
	str("Logo")
	w(int32(0))
	w(uint32(1))
	w(uint32(1))
	w(uint32(1))
	w(uint16(0))
	names := []string{"Key: Escape", "Key: W", "Key: A", "Key: Right Arrow", "Logo"}
	w(uint16(len(names)))
	for i, n := range names {
		str(n)
		w(uint32(i))
	}
	w(uint16(len(names)))
	for range names {
		w(uint32(0))
	}
	out := b.Bytes()
	full := make([]byte, 4+len(out))
	binary.LittleEndian.PutUint32(full, uint32(len(full)))
	copy(full[4:], out)
	return full
}

func TestParseControllerV3(t *testing.T) {
	c, err := parseController(blob(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "Test KB" || c.Vendor != "TestVendor" || c.Type != orgbTypeKeyboard || len(c.LEDNames) != 5 || len(c.Zones) != 2 {
		t.Fatalf("bad parse %+v", c)
	}
	if c.Zones[0].Width != 3 || c.Zones[0].Height != 2 || c.Location == "" {
		t.Fatalf("matrix %+v", c.Zones[0])
	}
	th := BuiltinThemes[0]
	cols := c.ColorsFor(th, 100)
	t.Logf("x=%v colors=%v", c.ledX(), cols)
	if cols[1] != mustHex(th.Accent) || cols[0] != mustHex(th.Base) || cols[3] != mustHex(th.Accent) {
		t.Fatalf("accent mapping wrong: %v", cols)
	}
	tri, _ := findTheme("tricolore", nil)
	tc := c.ColorsFor(tri, 100)
	if tc[0] != mustHex(tri.Stripes[0]) || tc[3] != mustHex(tri.Stripes[2]) {
		t.Fatalf("stripes wrong: %v", tc)
	}
}

// A zone-lit keyboard like the SteelSeries Apex 3 TKL: one linear zone of
// 8 LEDs named "LED 0".."LED 7".
func TestZoneKeyboardAccents(t *testing.T) {
	c := orgbController{Name: "SteelSeries Apex 3 TKL", Type: orgbTypeKeyboard, Zones: []orgbZone{{Name: "Keyboard", LEDCount: 8}}}
	for i := 0; i < 8; i++ {
		c.LEDNames = append(c.LEDNames, "LED "+string(rune('0'+i)))
	}
	// WASD + arrows: the zone under WASD (2nd of 8) and under the arrows (8th).
	th, _ := findTheme("rosso", nil)
	got := c.ColorsFor(th, 100)
	base, acc := mustHex(th.Base), mustHex(th.Accent)
	for i, col := range got {
		want := base
		if i == 1 || i == 7 {
			want = acc
		}
		if col != want {
			t.Errorf("TKL LED %d = %v want %v", i, col, want)
		}
	}
	// Full-size Apex 3: 10 zones, WASD in the 2nd, arrows in the 8th.
	full := orgbController{Name: "SteelSeries Apex 3", Type: orgbTypeKeyboard, Zones: []orgbZone{{LEDCount: 10}}}
	for i := 0; i < 10; i++ {
		full.LEDNames = append(full.LEDNames, "LED")
	}
	got = full.ColorsFor(th, 100)
	for i, col := range got {
		if (col == acc) != (i == 1 || i == 7) {
			t.Errorf("full LED %d accent=%v", i, col == acc)
		}
	}
	// No accent keys: fall back to the livery's accent columns.
	th2 := th
	th2.AccentKeys, th2.GMBKAccent = "none", []int{1, 10}
	got = c.ColorsFor(th2, 100)
	if got[0] != acc || got[7] != acc || got[3] != base {
		t.Errorf("columns: %v", got)
	}
	// Stripes still spread across the zones.
	th, _ = findTheme("flame-job", nil)
	got = c.ColorsFor(th, 100)
	if got[0] != mustHex(hexFlameYellow) || got[7] != mustHex(hexCandyRed) {
		t.Errorf("stripes: %v", got)
	}
}

func TestBreathLevel(t *testing.T) {
	p := breathPeriod(5)
	if lo, hi := breathLevel(0, p), breathLevel(p/2, p); lo < 0.11 || lo > 0.13 || hi < 0.99 {
		t.Errorf("breath range %v..%v", lo, hi)
	}
	if breathPeriod(1) <= breathPeriod(10) || breathPeriod(10) < time.Second {
		t.Error("speed mapping")
	}
}
