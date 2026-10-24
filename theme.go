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
	Category    string   `json:"category,omitempty"` // built-ins only; custom themes are grouped as "Your liveries"
	Builtin     bool     `json:"builtin"`
	Base        string   `json:"base"`                 // main color everywhere
	Accent      string   `json:"accent"`               // accent keys / accent zones
	AccentKeys  string   `json:"accentKeys"`           // none | wasd-arrows | wasd | arrows | function-row
	Stripes     []string `json:"stripes"`              // optional: colors painted left-to-right instead of base
	StripeRows  bool     `json:"stripeRows,omitempty"` // paint the stripes top-to-bottom (like a flag) where the device has rows
	RearLeft    string   `json:"rearLeft"`             // laptop rear light bar, left
	RearRight   string   `json:"rearRight"`            // laptop rear light bar, right
	Lid         string   `json:"lid"`                  // Alien head on the lid
	Power       string   `json:"power"`                // power button
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
		ID: "rosso", Name: "Rosso", Builtin: true, Category: catItalian,
		Description: "Racing red with white accents.",
		Base:        hexRed, Accent: hexWarmWhite, AccentKeys: "wasd-arrows",
		RearLeft: hexRed, RearRight: hexRed, Lid: hexWarmWhite, Power: hexRed,
		MouseLogo: hexRed, MouseSide: hexWarmWhite, GMBKAccent: []int{10},
	},
	{
		ID: "rosso-grafite", Name: "Rosso Grafite", Builtin: true, Category: catItalian,
		Description: "Racing red with graphite grey.",
		Base:        hexRed, Accent: hexGraphite, AccentKeys: "wasd-arrows",
		RearLeft: hexRed, RearRight: hexRed, Lid: hexGraphite, Power: hexRed,
		MouseLogo: hexRed, MouseSide: hexGraphite, GMBKAccent: []int{1, 10},
	},
	{
		ID: "tricolore", Name: "Tricolore", Builtin: true, Category: catItalian,
		Description: "Italian flag: green, white and red painted left to right.",
		Base:        hexWarmWhite, Accent: hexWarmWhite, AccentKeys: "none",
		Stripes:  []string{hexGreen, hexWarmWhite, hexRed},
		RearLeft: hexGreen, RearRight: hexRed, Lid: hexWarmWhite, Power: hexRed,
		MouseLogo: hexWarmWhite, MouseSide: hexGreen,
	},
	{
		ID: "winter-test", Name: "Winter Test", Builtin: true, Category: catItalian,
		Description: "Matte black and brushed aluminium with bright red details.",
		Base:        hexAlu, Accent: hexRed, AccentKeys: "wasd-arrows",
		RearLeft: hexRed, RearRight: hexRed, Lid: hexAlu, Power: hexRed,
		MouseLogo: hexRed, MouseSide: hexAlu, GMBKAccent: []int{1, 10},
	},
	{
		ID: "verde-arancio", Name: "Verde Arancio", Builtin: true, Category: catItalian,
		Description: "Lime green with orange accents.",
		Base:        hexLime, Accent: hexOrange, AccentKeys: "wasd-arrows",
		RearLeft: hexOrange, RearRight: hexOrange, Lid: hexLime, Power: hexOrange,
		MouseLogo: hexLime, MouseSide: hexOrange, GMBKAccent: []int{1, 10},
	},
	{
		ID: "giallo", Name: "Giallo", Builtin: true, Category: catItalian,
		Description: "Heritage racing yellow with white accents.",
		Base:        hexGiallo, Accent: hexWarmWhite, AccentKeys: "wasd-arrows",
		RearLeft: hexGiallo, RearRight: hexGiallo, Lid: hexWarmWhite, Power: hexGiallo,
		MouseLogo: hexGiallo, MouseSide: hexWarmWhite, GMBKAccent: []int{10},
	},
	{
		ID: "rally", Name: "Rally", Builtin: true, Category: catItalian,
		Description: "Desert-rally white with red accents.",
		Base:        hexWarmWhite, Accent: hexRed, AccentKeys: "wasd-arrows",
		RearLeft: hexRed, RearRight: hexRed, Lid: hexWarmWhite, Power: hexRed,
		MouseLogo: hexRed, MouseSide: hexWarmWhite, GMBKAccent: []int{1, 10},
	},

	// ---- Group B rally ----
	{
		ID: "turin-stripes", Name: "Turin Stripes", Builtin: true, Category: catGroupB,
		Description: "White with light-blue, dark-blue and red stripes, the classic 80s rally look.",
		Base:        hexWarmWhite, Accent: hexWarmWhite, AccentKeys: "none",
		Stripes:  []string{hexWarmWhite, hexSkyBlue, hexNavy, hexRed, hexWarmWhite},
		RearLeft: hexSkyBlue, RearRight: hexRed, Lid: hexWarmWhite, Power: hexRed,
		MouseLogo: hexRed, MouseSide: hexSkyBlue,
	},
	solid(catGroupB, "blue-gold", "Blue & Gold", "Deep rally blue with gold details.",
		hexRallyBlue, hexGold, hexGold, hexRallyBlue, 1, 10),
	solid(catGroupB, "turbo-white", "Turbo White", "White with red keys and yellow-orange chassis lights.",
		hexWarmWhite, hexRed, hexGiallo, hexRed, 10),
	solid(catGroupB, "night-stage", "Night Stage", "Dark car, amber light pods: graphite keys with amber details.",
		hexAlu, hexAmber, hexAmber, hexAmber, 1, 10),
	solid(catGroupB, "tarmac-red", "Tarmac Red", "Red and white with blue details.",
		hexRed, hexRallyBlue, hexRallyBlue, hexWarmWhite, 10),

	// ---- Formula 1 / Grand Prix ----
	solid(catF1, "british-racing-green", "British Racing Green", "Deep green with a yellow nose band.",
		hexBRG, hexYellow, hexBRG, hexYellow, 10),
	solid(catF1, "papaya", "Papaya", "Papaya orange with electric-blue details.",
		hexPapaya, hexElecBlue, hexPapaya, hexElecBlue, 1, 10),
	solid(catF1, "silver-teal", "Silver & Teal", "Silver bodywork with teal details.",
		hexSilver, hexTeal, hexTeal, hexSilver, 1, 10),
	solid(catF1, "black-gold", "Black & Gold", "Near-black with gold pinstripes.",
		hexNearBlack, hexGold, hexGold, hexGold, 1, 10),
	solid(catF1, "scarlet-yellow", "Scarlet & Yellow", "Grand Prix scarlet with a yellow badge.",
		hexScarlet, hexYellow, hexScarlet, hexYellow, 10),
	solid(catF1, "french-blue", "French Racing Blue", "National racing blue with white details.",
		hexFrenchBlue, hexWarmWhite, hexFrenchBlue, hexWarmWhite, 10),
	{
		ID: "midnight-blue", Name: "Midnight Blue", Builtin: true, Category: catF1,
		Description: "Dark blue with red and yellow details.",
		Base:        hexMidnight, Accent: hexRed, AccentKeys: "wasd-arrows",
		RearLeft: hexRed, RearRight: hexRed, Lid: hexYellow, Power: hexYellow,
		MouseLogo: hexRed, MouseSide: hexMidnight, GMBKAccent: []int{1, 10},
	},

	// ---- Endurance / 24-hour ----
	solid(catEndurance, "powder-blue-orange", "Powder Blue & Orange", "Powder blue with an orange stripe, the classic 24-hour look.",
		hexPowderBlue, hexOrange, hexOrange, hexPowderBlue, 5, 6),
	solid(catEndurance, "purple-white", "Purple & White", "Deep purple with white details.",
		hexPurple, hexWarmWhite, hexPurple, hexWarmWhite, 10),
	solid(catEndurance, "navy-gold-stripe", "White, Navy & Gold", "White with a navy stripe and gold details.",
		hexWarmWhite, hexNavy, hexGold, hexNavy, 5, 6),
	{
		ID: "endurance-night", Name: "Endurance Night", Builtin: true, Category: catEndurance,
		Description: "Racing through the night: dark keys, yellow headlights up front, red tail lights out back.",
		Base:        hexAlu, Accent: hexHeadlight, AccentKeys: "function-row",
		RearLeft: hexRed, RearRight: hexRed, Lid: hexHeadlight, Power: hexHeadlight,
		MouseLogo: hexHeadlight, MouseSide: hexRed, GMBKAccent: []int{1, 10},
	},

	// ---- Expedition & off-road ----
	solid(catExpedition, "expedition-yellow", "Expedition Yellow", "Sand-yellow jungle-expedition trucks with dark green details.",
		hexSandYellow, hexJungle, hexSandYellow, hexJungle, 1, 10),
	solid(catExpedition, "desert-raid", "Desert Raid", "Dune sand with orange and white, for long desert stages.",
		hexSand, hexOrange, hexOrange, hexWarmWhite, 10),
	solid(catExpedition, "safari-green", "Safari Green", "Olive green with canvas tan.",
		hexOlive, hexTan, hexOlive, hexTan, 1, 10),
	solid(catExpedition, "ice-raid", "Ice Raid", "Ice blue with high-visibility orange.",
		hexIce, hexOrange, hexOrange, hexIce, 1, 10),

	// ---- Pride ----
	pride(catPride, "rainbow-pride", "Rainbow Pride", "The six-stripe rainbow flag, one color per key row. Try it with Wave.",
		hexPrideRed, hexPrideOrange, hexPrideYellow, hexPrideGreen, hexPrideBlue, hexPrideViolet),
	pride(catPride, "trans-pride", "Trans Pride", "Light blue, pink and white.",
		hexTransBlue, hexTransPink, hexWarmWhite, hexTransPink, hexTransBlue),
	pride(catPride, "bi-pride", "Bi Pride", "Magenta, purple and blue.",
		hexBiPink, hexBiPink, hexBiPurple, hexBiBlue, hexBiBlue),
	pride(catPride, "pan-pride", "Pan Pride", "Pink, yellow and cyan.",
		hexPanPink, hexPanYellow, hexPanCyan),
	pride(catPride, "lesbian-pride", "Lesbian Pride", "Sunset oranges through white to pinks.",
		hexLesOrange, hexLesLight, hexWarmWhite, hexLesPink, hexLesRose),
	pride(catPride, "nonbinary-pride", "Nonbinary Pride", "Yellow, white, purple and black.",
		hexNbYellow, hexWarmWhite, hexNbPurple, hexAlu),

	// ---- Choppers & customs ----
	{
		ID: "flame-job", Name: "Flame Job", Builtin: true, Category: catChoppers,
		Description: "Hot-rod flames licking across the keys: yellow, orange and red.",
		Base:        hexFlameOrange, Accent: hexFlameYellow, AccentKeys: "none",
		Stripes:  []string{hexFlameYellow, hexFlameOrange, hexFlameRed, hexCandyRed},
		RearLeft: hexFlameRed, RearRight: hexFlameRed, Lid: hexFlameYellow, Power: hexFlameOrange,
		MouseLogo: hexFlameYellow, MouseSide: hexFlameRed,
	},
	solid(catChoppers, "chrome-black", "Chrome & Black", "Gloss black tins with polished chrome details.",
		hexAlu, hexChrome, hexChrome, hexChrome, 1, 10),
	solid(catChoppers, "candy-apple", "Candy Apple", "Deep candy red with gold pinstriping.",
		hexCandyRed, hexGold, hexCandyRed, hexGold, 10),
	solid(catChoppers, "black-orange", "Black & Orange", "Classic V-twin black with bright orange.",
		hexAlu, hexOrange, hexOrange, hexOrange, 1, 10),
	solid(catChoppers, "candy-purple", "Candy Purple", "Metal-flake purple with chrome highlights.",
		hexPurple, hexChrome, hexPurple, hexChrome, 10),
	{
		ID: "stars-stripes", Name: "Stars & Stripes", Builtin: true, Category: catChoppers,
		Description: "Open-road red, white and blue painted across the keys.",
		Base:        hexWarmWhite, Accent: hexWarmWhite, AccentKeys: "none",
		Stripes:  []string{hexNavy, hexWarmWhite, hexScarlet},
		RearLeft: hexNavy, RearRight: hexScarlet, Lid: hexWarmWhite, Power: hexScarlet,
		MouseLogo: hexScarlet, MouseSide: hexNavy,
	},
}

// Theme categories, in display order.
const (
	catItalian    = "Italian Superbikes"
	catGroupB     = "Group B Rally"
	catF1         = "Formula 1"
	catEndurance  = "Endurance"
	catExpedition = "Expedition & Off-road"
	catChoppers   = "Choppers & Customs"
	catPride      = "Pride"
)

// More paint colors (chosen to look right on RGB LEDs, so some are brighter
// than the paint they're named after).
const (
	hexSkyBlue     = "#00A0FF"
	hexNavy        = "#0A1E9C"
	hexRallyBlue   = "#0030C8"
	hexGold        = "#FFB000"
	hexAmber       = "#FF9A00"
	hexYellow      = "#FFD200"
	hexBRG         = "#00662A"
	hexPapaya      = "#FF6A00"
	hexElecBlue    = "#00AAFF"
	hexSilver      = "#B4BECD"
	hexTeal        = "#00D2B4"
	hexNearBlack   = "#141418"
	hexScarlet     = "#E60000"
	hexFrenchBlue  = "#0050FF"
	hexMidnight    = "#1428A0"
	hexPowderBlue  = "#50B4FF"
	hexPurple      = "#7800DC"
	hexHeadlight   = "#FFE650"
	hexSandYellow  = "#FFB400"
	hexJungle      = "#1E8C28"
	hexSand        = "#FFB464"
	hexOlive       = "#507814"
	hexTan         = "#FFC88C"
	hexIce         = "#B4E6FF"
	hexFlameYellow = "#FFD200"
	hexFlameOrange = "#FF7800"
	hexFlameRed    = "#FF2000"
	hexCandyRed    = "#C80018"
	hexChrome      = "#C8D2DC"

	// Pride flag colors, tuned for LEDs
	hexPrideRed    = "#FF0018"
	hexPrideOrange = "#FF7A00"
	hexPrideYellow = "#FFE600"
	hexPrideGreen  = "#00C832"
	hexPrideBlue   = "#0050FF"
	hexPrideViolet = "#8C00FF"
	hexTransBlue   = "#3CC8FF"
	hexTransPink   = "#FF6EA0"
	hexBiPink      = "#E6006E"
	hexBiPurple    = "#9B3CB4"
	hexBiBlue      = "#0038C8"
	hexPanPink     = "#FF1E8C"
	hexPanYellow   = "#FFD800"
	hexPanCyan     = "#1EB4FF"
	hexLesOrange   = "#E63C00"
	hexLesLight    = "#FF8C50"
	hexLesPink     = "#E650A0"
	hexLesRose     = "#B4005A"
	hexNbYellow    = "#FFE600"
	hexNbPurple    = "#9B4BE6"
)

// pride builds a flag livery: stripes in rows (top to bottom) where a device
// has rows, left to right elsewhere; chassis lights take the outer stripes.
func pride(cat, id, name, desc string, stripes ...string) ThemeDef {
	mid := stripes[len(stripes)/2]
	return ThemeDef{
		ID: id, Name: name, Description: desc, Builtin: true, Category: cat,
		Base: mid, Accent: stripes[0], AccentKeys: "none",
		Stripes: stripes, StripeRows: true,
		RearLeft: stripes[0], RearRight: stripes[len(stripes)-1], Lid: mid, Power: stripes[0],
		MouseLogo: stripes[0], MouseSide: stripes[len(stripes)-1],
	}
}

// solid builds the common two-color livery: base keys, accent WASD + arrows,
// chassis lights in one color, the lid logo in another, and accent GMBK columns.
func solid(cat, id, name, desc, base, accent, chassis, lid string, gmbkZones ...int) ThemeDef {
	return ThemeDef{
		ID: id, Name: name, Description: desc, Builtin: true, Category: cat,
		Base: base, Accent: accent, AccentKeys: "wasd-arrows",
		RearLeft: chassis, RearRight: chassis, Lid: lid, Power: chassis,
		MouseLogo: chassis, MouseSide: accent, GMBKAccent: gmbkZones,
	}
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
		pos := x
		if t.StripeRows {
			pos = m18KeyY[id]
		}
		if s, ok := t.stripeAt(pos); ok {
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
