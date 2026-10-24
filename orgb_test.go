package main

import (
	"bytes"
	"encoding/binary"
	"testing"
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
