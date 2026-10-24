package main

import (
	"bytes"
	"image"
	"image/color"
	"image/color/palette"
	"image/gif"
	"image/png"
	"testing"
)

func TestImageGIF(t *testing.T) {
	g := &gif.GIF{}
	for i, c := range []color.RGBA{{255, 0, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 255}} {
		f := image.NewPaletted(image.Rect(0, 0, 40, 20), palette.WebSafe)
		for p := range f.Pix {
			f.Pix[p] = uint8(f.Palette.Index(c))
		}
		g.Image = append(g.Image, f)
		g.Delay = append(g.Delay, 5*(i+1)) // 50, 100, 150 ms
	}
	var b bytes.Buffer
	if err := gif.EncodeAll(&b, g); err != nil {
		t.Fatal(err)
	}
	screenImageLoader = func() ([]byte, error) { return b.Bytes(), nil }
	defer func() { screenImageLoader = nil }()

	frames, delays, err := imageScreenFrames("fill")
	if err != nil || len(frames) != 3 || delays[1] != 100 {
		t.Fatalf("frames %d delays %v err %v", len(frames), delays, err)
	}
	if c := frames[1].RGBAAt(64, 64); c.G < 250 || c.R > 5 {
		t.Errorf("frame 2 should be green, got %v", c)
	}
	// "fit": a 2:1 picture gets black bars top and bottom
	frames, _, _ = imageScreenFrames("fit")
	if c := frames[0].RGBAAt(64, 5); c.R != 0 {
		t.Errorf("fit should leave a black bar, got %v", c)
	}
	if c := frames[0].RGBAAt(64, 64); c.R < 250 {
		t.Errorf("fit middle should be red, got %v", c)
	}
	p, err := ScreenPayload(ScreenSettings{Model: "image", Body: "#000000", Accent: "#000000", Glow: "#000000"})
	if err != nil || p[0] != 3 || p[1] != 16 || p[2] != 20 || p[3] != 30 {
		t.Fatalf("header %v err %v", p[:4], err)
	}
}

func TestImagePNGAndLimits(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 300, 300))
	for i := 0; i < 300; i++ {
		for j := 0; j < 300; j++ {
			c := color.RGBA{0, 0, 255, 255}
			if i < 150 {
				c = color.RGBA{255, 255, 0, 255}
			}
			img.Set(i, j, c)
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	f, d, err := loadImageFrames(b.Bytes())
	if err != nil || len(f) != 1 || len(d) != 1 {
		t.Fatal(err)
	}
	out := fitSquare(f[0], "fill")
	if l, r := out.RGBAAt(10, 64), out.RGBAAt(120, 64); l.R < 250 || r.B < 250 {
		t.Errorf("scaled halves: %v %v", l, r)
	}
	if _, _, err := loadImageFrames([]byte("not an image")); err == nil {
		t.Error("garbage accepted")
	}
	// thinning keeps total time
	var fr []image.Image
	var dl []int
	for i := 0; i < 120; i++ {
		fr, dl = append(fr, img), append(dl, 50)
	}
	fr, dl = thinFrames(fr, dl, 60)
	sum := 0
	for _, v := range dl {
		sum += v
	}
	if len(fr) != 60 || sum != 6000 {
		t.Errorf("thin: %d frames, %d ms", len(fr), sum)
	}
}
