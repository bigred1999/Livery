package main

import (
	"fmt"
	"strconv"
	"strings"
)

// gmbkZoneCount is the number of lighting columns on the Glorious GMBK 75%.
const gmbkZoneCount = 10

type RGB struct{ R, G, B byte }

func (c RGB) String() string { return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B) }

func (c RGB) Scale(f float64) RGB {
	if f < 0 {
		f = 0
	}
	if f > 1 {
		f = 1
	}
	return RGB{byte(float64(c.R)*f + 0.5), byte(float64(c.G)*f + 0.5), byte(float64(c.B)*f + 0.5)}
}

func ParseHex(s string) (RGB, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return RGB{}, fmt.Errorf("bad color %q", s)
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return RGB{}, fmt.Errorf("bad color %q", s)
	}
	return RGB{byte(v >> 16), byte(v >> 8), byte(v)}, nil
}

func mustHex(s string) RGB {
	c, err := ParseHex(s)
	if err != nil {
		return RGB{}
	}
	return c
}

// ThemeDef is what the control panel edits and what config.json stores.
// Colors are "#RRGGBB" strings so the file stays readable.
type ThemeDef struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Builtin     bool     `json:"builtin"`
	Base        string   `json:"base"`       // main color everywhere
	Accent      string   `json:"accent"`     // accent keys / accent zones
	AccentKeys  string   `json:"accentKeys"` // none | wasd-arrows | wasd | arrows | function-row
	Stripes     []string `json:"stripes"`    // optional: colors painted left-to-right instead of base
	RearLeft    string   `json:"rearLeft"`   // laptop rear light bar, left
	RearRight   string   `json:"rearRight"`  // laptop rear light bar, right
	Lid         string   `json:"lid"`        // Alien head on the lid
	Power       string   `json:"power"`      // power button
	MouseLogo   string   `json:"mouseLogo"`
	MouseSide   string   `json:"mouseSide"`
	GMBKAccent  []int    `json:"gmbkAccentZones"` // 1..10 columns that get the accent color
}

const (
	hexRed       = "#FF0000"
	hexWarmWhite = "#FFE1C8"
	hexGreen     = "#00C828"
	hexGraphite  = "#5A5F6E"
	hexAlu       = "#2A2C32"
	hexLime      = "#8CFF00"
	hexOrange    = "#FF4600"
	hexGiallo    = "#FFAA00"
	hexOff       = "#000000"
)

// Built-in liveries. Names describe colors, not brands.
var BuiltinThemes = []ThemeDef{
	{
		ID: "rosso", Name: "Rosso", Builtin: true,
		Description: "Racing red with white accents.",
		Base:        hexRed, Accent: hexWarmWhite, AccentKeys: "wasd-arrows",
		RearLeft: hexRed, RearRight: hexRed, Lid: hexWarmWhite, Power: hexRed,
		MouseLogo: hexRed, MouseSide: hexWarmWhite, GMBKAccent: []int{10},
	},
	{
		ID: "rosso-grafite", Name: "Rosso Grafite", Builtin: true,
		Description: "Racing red with graphite grey.",
		Base:        hexRed, Accent: hexGraphite, AccentKeys: "wasd-arrows",
		RearLeft: hexRed, RearRight: hexRed, Lid: hexGraphite, Power: hexRed,
		MouseLogo: hexRed, MouseSide: hexGraphite, GMBKAccent: []int{1, 10},
	},
	{
		ID: "tricolore", Name: "Tricolore", Builtin: true,
		Description: "Italian flag: green, white and red painted left to right.",
		Base:        hexWarmWhite, Accent: hexWarmWhite, AccentKeys: "none",
		Stripes:  []string{hexGreen, hexWarmWhite, hexRed},
		RearLeft: hexGreen, RearRight: hexRed, Lid: hexWarmWhite, Power: hexRed,
		MouseLogo: hexWarmWhite, MouseSide: hexGreen,
	},
	{
		ID: "winter-test", Name: "Winter Test", Builtin: true,
		Description: "Matte black and brushed aluminium with bright red details.",
		Base:        hexAlu, Accent: hexRed, AccentKeys: "wasd-arrows",
		RearLeft: hexRed, RearRight: hexRed, Lid: hexAlu, Power: hexRed,
		MouseLogo: hexRed, MouseSide: hexAlu, GMBKAccent: []int{1, 10},
	},
	{
		ID: "verde-arancio", Name: "Verde Arancio", Builtin: true,
		Description: "Lime green with orange accents.",
		Base:        hexLime, Accent: hexOrange, AccentKeys: "wasd-arrows",
		RearLeft: hexOrange, RearRight: hexOrange, Lid: hexLime, Power: hexOrange,
		MouseLogo: hexLime, MouseSide: hexOrange, GMBKAccent: []int{1, 10},
	},
	{
		ID: "giallo", Name: "Giallo", Builtin: true,
		Description: "Heritage racing yellow with white accents.",
		Base:        hexGiallo, Accent: hexWarmWhite, AccentKeys: "wasd-arrows",
		RearLeft: hexGiallo, RearRight: hexGiallo, Lid: hexWarmWhite, Power: hexGiallo,
		MouseLogo: hexGiallo, MouseSide: hexWarmWhite, GMBKAccent: []int{10},
	},
	{
		ID: "rally", Name: "Rally", Builtin: true,
		Description: "Desert-rally white with red accents.",
		Base:        hexWarmWhite, Accent: hexRed, AccentKeys: "wasd-arrows",
		RearLeft: hexRed, RearRight: hexRed, Lid: hexWarmWhite, Power: hexRed,
		MouseLogo: hexRed, MouseSide: hexWarmWhite, GMBKAccent: []int{1, 10},
	},
}

// Resolved is a theme turned into concrete per-device colors.
type Resolved struct {
	Keys      map[byte]RGB // laptop per-key colors
	RearLeft  RGB
	RearRight RGB
	Lid       RGB
	Power     RGB
	MouseLogo RGB
	MouseSide RGB
	GMBKBase  RGB
	GMBKZones [gmbkZoneCount]RGB
	GMBKUse   [gmbkZoneCount]bool // zone overrides the base
	Primary   RGB                 // color used for single-color hardware effects
}

func (t ThemeDef) stripeAt(x float64) (RGB, bool) {
	if len(t.Stripes) == 0 {
		return RGB{}, false
	}
	i := int(x * float64(len(t.Stripes)))
	if i >= len(t.Stripes) {
		i = len(t.Stripes) - 1
	}
	if i < 0 {
		i = 0
	}
	return mustHex(t.Stripes[i]), true
}

// Resolve applies brightness (0..100) and works out every light's color.
func (t ThemeDef) Resolve(brightness int) Resolved {
	f := float64(brightness) / 100
	base := mustHex(t.Base).Scale(f)
	accent := mustHex(t.Accent).Scale(f)
	r := Resolved{Keys: map[byte]RGB{}}
	accentSet := map[byte]bool{}
	for _, id := range accentKeySets[t.AccentKeys] {
		accentSet[id] = true
	}
	for id, x := range m18KeyX {
		c := base
		if s, ok := t.stripeAt(x); ok {
			c = s.Scale(f)
		}
		if accentSet[id] {
			c = accent
		}
		r.Keys[id] = c
	}
	r.RearLeft = mustHex(t.RearLeft).Scale(f)
	r.RearRight = mustHex(t.RearRight).Scale(f)
	r.Lid = mustHex(t.Lid).Scale(f)
	r.Power = mustHex(t.Power).Scale(f)
	r.MouseLogo = mustHex(t.MouseLogo).Scale(f)
	r.MouseSide = mustHex(t.MouseSide).Scale(f)
	r.GMBKBase = base
	if len(t.Stripes) > 0 {
		s, _ := t.stripeAt(0.5)
		r.GMBKBase = s.Scale(f)
		for z := 0; z < gmbkZoneCount; z++ {
			c, _ := t.stripeAt((float64(z) + 0.5) / gmbkZoneCount)
			r.GMBKZones[z] = c.Scale(f)
			r.GMBKUse[z] = true
		}
	}
	for _, z := range t.GMBKAccent {
		if z >= 1 && z <= gmbkZoneCount {
			r.GMBKZones[z-1] = accent
			r.GMBKUse[z-1] = true
		}
	}
	r.Primary = base
	if len(t.Stripes) > 0 {
		r.Primary = mustHex(t.Stripes[len(t.Stripes)-1]).Scale(f)
	}
	return r
}

func findTheme(id string, custom []ThemeDef) (ThemeDef, bool) {
	for _, t := range custom {
		if t.ID == id {
			return t, true
		}
	}
	for _, t := range BuiltinThemes {
		if t.ID == id {
			return t, true
		}
	}
	return ThemeDef{}, false
}
