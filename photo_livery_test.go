package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"math/rand"
	"regexp"
	"testing"
)

// testScene paints a stand-in "vehicle photo": a background (sky over road,
// grass, studio wall, or transparent), a body shape in the paint color with
// shading, a band of accent paint, black tyres, a windscreen, some
// reflections and sensor noise. Real photos are messier, but this keeps the
// things the engine must cope with: big background areas, shading on the
// paint, black rubber and bright highlights.
func testScene(bg string, body, accent, third color.RGBA, w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	rng := rand.New(rand.NewSource(3))
	put := func(x, y int, c color.RGBA, shade float64) {
		n := (rng.Float64() - 0.5) * 10
		f := func(v uint8) uint8 { return uint8(math.Max(0, math.Min(255, float64(v)*shade+n))) }
		img.SetRGBA(x, y, color.RGBA{f(c.R), f(c.G), f(c.B), c.A})
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			fy := float64(y) / float64(h)
			switch bg {
			case "road":
				if fy < 0.62 {
					put(x, y, color.RGBA{120, 170, 225, 255}, 0.9+0.2*fy)
				} else {
					put(x, y, color.RGBA{88, 88, 92, 255}, 1)
				}
			case "grass":
				put(x, y, color.RGBA{70, 120, 50, 255}, 0.85+0.3*fy)
			case "studio":
				put(x, y, color.RGBA{235, 235, 238, 255}, 1-0.1*fy)
			case "clear":
				img.SetRGBA(x, y, color.RGBA{})
			}
		}
	}
	cx, cy := float64(w)/2, float64(h)*0.55
	rx, ry := float64(w)*0.32, float64(h)*0.2
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := (float64(x)-cx)/rx, (float64(y)-cy)/ry
			if dx*dx+dy*dy > 1 {
				continue
			}
			shade := 0.75 + 0.35*(1-(dy+1)/2) // lit from above
			c := body
			switch {
			case math.Abs(dy+0.05) < 0.16:
				c = accent // a band of accent along the side
			case third.A > 0 && math.Abs(dy-0.4) < 0.08:
				c = third
			case dy < -0.55 && math.Abs(dx) < 0.35:
				c = color.RGBA{30, 40, 55, 255} // windscreen
			}
			if dx > 0.1 && dx < 0.18 && dy < -0.2 {
				c, shade = color.RGBA{255, 255, 255, 255}, 1 // reflection
			}
			put(x, y, c, shade)
		}
	}
	for _, wx := range []float64{cx - rx*0.6, cx + rx*0.6} {
		wy, r := cy+ry*0.9, float64(h)*0.11
		for y := int(wy - r); y <= int(wy+r); y++ {
			for x := int(wx - r); x <= int(wx+r); x++ {
				if x < 0 || y < 0 || x >= w || y >= h {
					continue
				}
				d := math.Hypot(float64(x)-wx, float64(y)-wy)
				if d <= r {
					c := color.RGBA{22, 22, 24, 255}
					if d < r*0.45 {
						c = color.RGBA{150, 150, 155, 255} // rim
					}
					put(x, y, c, 1)
				}
			}
		}
	}
	return img
}

var (
	cRed    = color.RGBA{190, 18, 28, 255}
	cWhite  = color.RGBA{238, 238, 236, 255}
	cYellow = color.RGBA{245, 200, 20, 255}
	cBlack  = color.RGBA{20, 20, 22, 255}
	cOrange = color.RGBA{240, 110, 20, 255}
	cBlue   = color.RGBA{95, 160, 210, 255}
	cGreen  = color.RGBA{0, 90, 50, 255}
	cNone   = color.RGBA{}
)

func TestPhotoLiveryScenes(t *testing.T) {
	cases := []struct {
		name         string
		img          *image.RGBA
		base, accent string
		swapToAccent bool
		eitherOrder  bool // body matches the sky: both colors must be found, in either role
	}{
		{"red bike, white band, sky and road", testScene("road", cRed, cWhite, cNone, 640, 420), "Red", "White", false, false},
		{"yellow car, black band, on grass", testScene("grass", cYellow, cBlack, cNone, 600, 400), "Yellow", "Black", false, false},
		{"black car, orange band, white studio", testScene("studio", cBlack, cOrange, cNone, 600, 400), "Black", "Orange", true, false},
		{"light blue with orange, on road", testScene("road", cBlue, cOrange, cNone, 600, 400), "Sky Blue", "Orange", false, true},
		{"green cut-out PNG with white", testScene("clear", cGreen, cWhite, cNone, 500, 340), "Green", "White", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := LiveryFromImage(tc.img)
			if err != nil {
				t.Fatal(err)
			}
			var base, accent string
			for _, s := range r.Palette {
				switch s.Role {
				case "base":
					base = s.Name
				case "accent":
					accent = s.Name
				}
			}
			if tc.eitherOrder && base == tc.accent && accent == tc.base {
				base, accent = accent, base
			}
			if base != tc.base || accent != tc.accent {
				t.Errorf("got base %q accent %q, want %q %q; palette %+v", base, accent, tc.base, tc.accent, r.Palette)
			}
			th := r.Theme
			if th.Name == "" || len(th.Name) > 40 {
				t.Errorf("bad name %q", th.Name)
			}
			hex := regexp.MustCompile(`^#[0-9A-F]{6}$`)
			for _, c := range []string{th.Base, th.Accent, th.RearLeft, th.RearRight, th.Lid, th.Power, th.MouseLogo, th.MouseSide} {
				if !hex.MatchString(c) {
					t.Errorf("bad color %q in %+v", c, th)
				}
			}
			if tc.swapToAccent && th.Lid != th.Accent {
				t.Errorf("dark body: lid should carry the brighter accent, got %s", th.Lid)
			}
			if _, ok := accentKeySets[th.AccentKeys]; !ok {
				t.Errorf("bad accent keys %q", th.AccentKeys)
			}
		})
	}
}

func TestPhotoLiveryDeterministicAndEncoded(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, testScene("road", cRed, cWhite, cNone, 400, 260)); err != nil {
		t.Fatal(err)
	}
	a, err := AnalyzeVehiclePhoto(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := AnalyzeVehiclePhoto(buf.Bytes())
	if a.Theme.Base != b.Theme.Base || a.Theme.Accent != b.Theme.Accent || len(a.Palette) != len(b.Palette) {
		t.Errorf("same photo gave different liveries: %+v vs %+v", a.Theme, b.Theme)
	}
	if _, err := AnalyzeVehiclePhoto([]byte("not an image")); err == nil {
		t.Error("garbage should be refused")
	}
}

func TestLEDTranslation(t *testing.T) {
	// Dark red paint must become a bright red LED, not a dim one.
	led := ledFor(rgbToLab(RGB{110, 12, 18}))
	if led.R < 240 || led.G > 60 || led.B > 70 {
		t.Errorf("dark red paint -> %s", led)
	}
	if got := ledFor(rgbToLab(RGB{15, 15, 16})).String(); got != hexAlu {
		t.Errorf("black paint -> %s, want dim graphite", got)
	}
	// Lab round trip stays within a step.
	for _, c := range []RGB{{0, 0, 0}, {255, 255, 255}, {190, 18, 28}, {12, 200, 90}, {40, 60, 200}} {
		back := labToRGB(rgbToLab(c))
		if absInt(int(back.R)-int(c.R)) > 1 || absInt(int(back.G)-int(c.G)) > 1 || absInt(int(back.B)-int(c.B)) > 1 {
			t.Errorf("lab round trip %v -> %v", c, back)
		}
	}
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
