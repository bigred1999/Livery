package main

// Minimal client for the OpenRGB SDK server (default 127.0.0.1:6742), so Livery
// can paint any device OpenRGB supports. Implements protocol version 3 of the
// SDK wire format (documented by the OpenRGB project); no OpenRGB code is used.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"strings"
	"time"
)

const (
	orgbAddr     = "127.0.0.1:6742"
	orgbProtocol = 3

	orgbReqControllerCount = 0
	orgbReqControllerData  = 1
	orgbReqProtocolVersion = 40
	orgbSetClientName      = 50
	orgbUpdateLEDs         = 1050
	orgbSetCustomMode      = 1100
)

// OpenRGB device types (subset).
const (
	orgbTypeKeyboard = 5
	orgbTypeMouse    = 6
)

type orgbZone struct {
	Name      string
	Type      int32
	LEDCount  uint32
	Height    uint32
	Width     uint32
	MatrixMap []uint32
}

type orgbController struct {
	Index    uint32
	Type     int32
	Name     string
	Vendor   string
	Location string
	Zones    []orgbZone
	LEDNames []string
}

type OpenRGB struct {
	conn    net.Conn
	version uint32
}

func ConnectOpenRGB() (*OpenRGB, error) {
	c, err := net.DialTimeout("tcp", orgbAddr, 600*time.Millisecond)
	if err != nil {
		return nil, fmt.Errorf("OpenRGB server not running (%v)", err)
	}
	o := &OpenRGB{conn: c, version: orgbProtocol}
	if err := o.send(0, orgbReqProtocolVersion, u32(orgbProtocol)); err != nil {
		o.Close()
		return nil, err
	}
	_, data, err := o.recv(orgbReqProtocolVersion)
	if err != nil {
		o.Close()
		return nil, err
	}
	if len(data) >= 4 {
		if v := binary.LittleEndian.Uint32(data); v < o.version {
			o.version = v
		}
	}
	o.send(0, orgbSetClientName, append([]byte("Livery"), 0))
	return o, nil
}

func (o *OpenRGB) Close() {
	if o != nil && o.conn != nil {
		o.conn.Close()
	}
}

func u32(v uint32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	return b
}

func (o *OpenRGB) send(dev, id uint32, data []byte) error {
	hdr := make([]byte, 16)
	copy(hdr, "ORGB")
	binary.LittleEndian.PutUint32(hdr[4:], dev)
	binary.LittleEndian.PutUint32(hdr[8:], id)
	binary.LittleEndian.PutUint32(hdr[12:], uint32(len(data)))
	o.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if _, err := o.conn.Write(append(hdr, data...)); err != nil {
		return err
	}
	return nil
}

// recv reads packets until one with the wanted id arrives (others, like
// device-list-updated notifications, are skipped).
func (o *OpenRGB) recv(want uint32) (uint32, []byte, error) {
	for i := 0; i < 16; i++ {
		o.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		hdr := make([]byte, 16)
		if _, err := io.ReadFull(o.conn, hdr); err != nil {
			return 0, nil, err
		}
		if string(hdr[:4]) != "ORGB" {
			return 0, nil, errors.New("bad OpenRGB packet")
		}
		dev := binary.LittleEndian.Uint32(hdr[4:])
		id := binary.LittleEndian.Uint32(hdr[8:])
		size := binary.LittleEndian.Uint32(hdr[12:])
		if size > 8<<20 {
			return 0, nil, errors.New("OpenRGB packet too large")
		}
		data := make([]byte, size)
		if _, err := io.ReadFull(o.conn, data); err != nil {
			return 0, nil, err
		}
		if id == want {
			return dev, data, nil
		}
	}
	return 0, nil, errors.New("no reply from OpenRGB")
}

func (o *OpenRGB) Controllers() ([]orgbController, error) {
	if err := o.send(0, orgbReqControllerCount, nil); err != nil {
		return nil, err
	}
	_, data, err := o.recv(orgbReqControllerCount)
	if err != nil || len(data) < 4 {
		return nil, fmt.Errorf("controller count: %v", err)
	}
	n := binary.LittleEndian.Uint32(data)
	var out []orgbController
	for i := uint32(0); i < n && i < 256; i++ {
		if err := o.send(i, orgbReqControllerData, u32(o.version)); err != nil {
			return out, err
		}
		_, d, err := o.recv(orgbReqControllerData)
		if err != nil {
			return out, err
		}
		c, err := parseController(d, o.version)
		if err != nil {
			return out, fmt.Errorf("controller %d: %v", i, err)
		}
		c.Index = i
		out = append(out, c)
	}
	return out, nil
}

// SetLEDs switches the controller to its direct/custom mode and sets every LED.
func (o *OpenRGB) SetLEDs(dev uint32, colors []RGB) error {
	if err := o.send(dev, orgbSetCustomMode, nil); err != nil {
		return err
	}
	return o.UpdateLEDs(dev, colors)
}

// UpdateLEDs sets every LED of a controller already in direct mode.
func (o *OpenRGB) UpdateLEDs(dev uint32, colors []RGB) error {
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, uint32(4+2+4*len(colors)))
	binary.Write(&b, binary.LittleEndian, uint16(len(colors)))
	for _, c := range colors {
		b.Write([]byte{c.R, c.G, c.B, 0})
	}
	return o.send(dev, orgbUpdateLEDs, b.Bytes())
}

// ---- parsing ---------------------------------------------------------------

type rd struct {
	b   []byte
	off int
	err error
}

func (r *rd) need(n int) bool {
	if r.err != nil {
		return false
	}
	if r.off+n > len(r.b) {
		r.err = errors.New("truncated controller data")
		return false
	}
	return true
}
func (r *rd) u16() uint16 {
	if !r.need(2) {
		return 0
	}
	v := binary.LittleEndian.Uint16(r.b[r.off:])
	r.off += 2
	return v
}
func (r *rd) u32() uint32 {
	if !r.need(4) {
		return 0
	}
	v := binary.LittleEndian.Uint32(r.b[r.off:])
	r.off += 4
	return v
}
func (r *rd) str() string {
	n := int(r.u16())
	if !r.need(n) {
		return ""
	}
	s := strings.TrimRight(string(r.b[r.off:r.off+n]), "\x00")
	r.off += n
	return s
}
func (r *rd) skip(n int) {
	if r.need(n) {
		r.off += n
	}
}

func parseController(d []byte, version uint32) (orgbController, error) {
	r := &rd{b: d}
	var c orgbController
	r.u32() // total size
	c.Type = int32(r.u32())
	c.Name = r.str()
	if version >= 1 {
		c.Vendor = r.str()
	}
	r.str() // description
	r.str() // version
	r.str() // serial
	c.Location = r.str()
	modes := int(r.u16())
	r.u32() // active mode
	for i := 0; i < modes && r.err == nil; i++ {
		r.str()   // name
		r.skip(4) // value
		r.skip(4) // flags
		r.skip(8) // speed min/max
		if version >= 3 {
			r.skip(8) // brightness min/max
		}
		r.skip(8) // colors min/max
		r.skip(4) // speed
		if version >= 3 {
			r.skip(4) // brightness
		}
		r.skip(4) // direction
		r.skip(4) // color mode
		r.skip(4 * int(r.u16()))
	}
	zones := int(r.u16())
	for i := 0; i < zones && r.err == nil; i++ {
		var z orgbZone
		z.Name = r.str()
		z.Type = int32(r.u32())
		r.u32() // leds min
		r.u32() // leds max
		z.LEDCount = r.u32()
		size := int(r.u16())
		if size > 0 {
			z.Height = r.u32()
			z.Width = r.u32()
			cells := int(z.Height * z.Width)
			if cells > 0 && cells < 100000 {
				z.MatrixMap = make([]uint32, cells)
				for k := range z.MatrixMap {
					z.MatrixMap[k] = r.u32()
				}
			}
		}
		if version >= 4 {
			segs := int(r.u16())
			for s := 0; s < segs && r.err == nil; s++ {
				r.str()
				r.skip(12)
			}
		}
		c.Zones = append(c.Zones, z)
	}
	leds := int(r.u16())
	for i := 0; i < leds && r.err == nil; i++ {
		c.LEDNames = append(c.LEDNames, r.str())
		r.skip(4) // value
	}
	return c, r.err
}

// ---- theme mapping -----------------------------------------------------------

var accentKeyNames = map[string][]string{
	"wasd-arrows":  {"Key: W", "Key: A", "Key: S", "Key: D", "Key: Up Arrow", "Key: Down Arrow", "Key: Left Arrow", "Key: Right Arrow"},
	"wasd":         {"Key: W", "Key: A", "Key: S", "Key: D"},
	"arrows":       {"Key: Up Arrow", "Key: Down Arrow", "Key: Left Arrow", "Key: Right Arrow"},
	"function-row": {"Key: F1", "Key: F2", "Key: F3", "Key: F4", "Key: F5", "Key: F6", "Key: F7", "Key: F8", "Key: F9", "Key: F10", "Key: F11", "Key: F12"},
}

// ledX returns each LED's horizontal position 0..1 (matrix column when the
// device has a matrix layout, otherwise its order along the zone).
func (c orgbController) ledX() []float64 {
	x := make([]float64, len(c.LEDNames))
	base := 0
	for _, z := range c.Zones {
		n := int(z.LEDCount)
		for i := 0; i < n && base+i < len(x); i++ {
			if n > 1 {
				x[base+i] = float64(i) / float64(n-1)
			}
		}
		if z.Width > 1 && len(z.MatrixMap) == int(z.Width*z.Height) {
			for k, v := range z.MatrixMap {
				if v == 0xFFFFFFFF || int(v) >= n {
					continue
				}
				col := k % int(z.Width)
				if base+int(v) < len(x) {
					x[base+int(v)] = float64(col) / float64(z.Width-1)
				}
			}
		}
		base += n
	}
	return x
}

// ColorsFor works out every LED's color for a theme.
func (c orgbController) ColorsFor(t ThemeDef, brightness int) []RGB {
	f := float64(brightness) / 100
	base := mustHex(t.Base).Scale(f)
	accent := mustHex(t.Accent).Scale(f)
	xs := c.ledX()
	accents := map[string]bool{}
	for _, n := range accentKeyNames[t.AccentKeys] {
		accents[strings.ToLower(n)] = true
	}
	// Keyboards lit in zones rather than per key (e.g. SteelSeries Apex 3:
	// "LED 0".."LED 7") get the livery's accent columns instead of accent keys.
	zoneAccent := map[int]bool{}    // accent columns 1..10
	zoneAccentLED := map[int]bool{} // LEDs under accent key clusters
	if c.Type == orgbTypeKeyboard && !c.hasKeyNames() {
		if leds := c.zoneLEDsFor(t.AccentKeys); leds != nil {
			zoneAccentLED = leds
		} else if t.AccentKeys != "function-row" {
			for _, z := range t.GMBKAccent {
				zoneAccent[z] = true
			}
		}
	}
	out := make([]RGB, len(c.LEDNames))
	for i, name := range c.LEDNames {
		col := base
		if s, ok := t.stripeAt(xs[i]); ok {
			col = s.Scale(f)
		}
		lname := strings.ToLower(name)
		switch c.Type {
		case orgbTypeKeyboard:
			if accents[lname] || zoneAccent[accentColumn(xs[i])] || zoneAccentLED[i] {
				col = accent
			}
		case orgbTypeMouse:
			if strings.Contains(lname, "logo") {
				col = mustHex(t.MouseLogo).Scale(f)
			} else {
				col = mustHex(t.MouseSide).Scale(f)
			}
		}
		out[i] = col
	}
	return out
}

func (c orgbController) hasKeyNames() bool {
	for _, n := range c.LEDNames {
		if strings.HasPrefix(n, "Key: ") {
			return true
		}
	}
	return false
}

// Where WASD and the arrow keys sit, as the middle of each cluster across the
// keyboard's width (0..1). Tenkeyless boards are 18.25 key-widths wide, full
// size ones 22.5; zone-lit boards split that width into equal zones.
var clusterX = map[string]map[bool]float64{
	"wasd":   {true: 3.06 / 18.25, false: 3.06 / 22.5},
	"arrows": {true: 16.75 / 18.25, false: 16.75 / 22.5},
}

// zoneLEDsFor returns the zone LEDs that sit under the theme's accent keys
// (nil when the accent set isn't WASD/arrows).
func (c orgbController) zoneLEDsFor(accentKeys string) map[int]bool {
	var clusters []string
	switch accentKeys {
	case "wasd-arrows":
		clusters = []string{"wasd", "arrows"}
	case "wasd", "arrows":
		clusters = []string{accentKeys}
	default:
		return nil
	}
	n := len(c.LEDNames)
	if n < 3 {
		return nil
	}
	tkl := strings.Contains(strings.ToUpper(c.Name), "TKL") || n <= 8
	out := map[int]bool{}
	for _, cl := range clusters {
		z := int(clusterX[cl][tkl] * float64(n))
		if z >= n {
			z = n - 1
		}
		out[z] = true
	}
	return out
}

// accentColumn maps a horizontal position 0..1 onto the 1..10 accent columns
// liveries define (the same columns the Glorious keyboard uses).
func accentColumn(x float64) int {
	return int(math.Round(x*9)) + 1
}

// ---- software breathing ------------------------------------------------------
// OpenRGB devices have no shared breathing mode, so Livery animates them
// itself while the Breathe effect is on.

// breathPeriod matches Livery's speed setting (1 slow .. 10 fast).
func breathPeriod(speed int) time.Duration {
	return time.Duration(7000-550*float64(speed)) * time.Millisecond
}

// breathLevel is the brightness factor at time t: a smooth rise and fall
// between 12% and 100%.
func breathLevel(t time.Duration, period time.Duration) float64 {
	ph := float64(t%period) / float64(period)
	return 0.12 + 0.88*(0.5-0.5*math.Cos(2*math.Pi*ph))
}

// handledNatively reports devices Livery drives itself, so OpenRGB doesn't
// fight over them.
func handledNatively(location string) bool {
	l := strings.ToUpper(location)
	for _, id := range []string{"VID_187C", "VID_0D62&PID_AAB1", "VID_3794", "VID_0C45&PID_8009", "VID_0C45&PID_800A", "0C45:8009", "0C45:800A", "187C:", "0D62:AAB1", "3794:",
		// Logitech Lightspeed receivers (Livery's own HID++ driver handles the mouse)
		"VID_046D&PID_C539", "VID_046D&PID_C53A", "VID_046D&PID_C541", "VID_046D&PID_C545", "VID_046D&PID_C547", "VID_046D&PID_C54D"} {
		if strings.Contains(l, id) {
			return true
		}
	}
	return false
}
