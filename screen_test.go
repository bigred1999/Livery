package main

import (
	"image"
	"os"
	"testing"
)

func TestScreenPayload(t *testing.T) {
	sc := ScreenSettings{Model: "coupe", Body: "#FF6A00", Accent: "#00AAFF", Glow: "#00AAFF"}
	p, err := ScreenPayload(sc)
	if err != nil {
		t.Fatal(err)
	}
	if len(p)%4096 != 0 || p[0] != screenFrames || p[1] != 16 {
		t.Fatalf("header %v len %d", p[:3], len(p))
	}
	if want := 256 + screenFrames*128*128*2; len(p) < want || len(p)-want >= 4096 {
		t.Fatalf("payload size %d", len(p))
	}
	// RGB565 little-endian: pure red is F800 -> 00 F8
	if r := rgb565(solid565(255, 0, 0)); r[0] != 0x00 || r[1] != 0xF8 {
		t.Fatalf("red = % x", r[:2])
	}
	if dir := os.Getenv("LIVERY_SCREEN_PREVIEW"); dir != "" {
		for _, m := range []string{"coupe", "sedan", "bike", "flag"} {
			sc.Model = m
			if m == "sedan" {
				sc.Body, sc.Accent, sc.Glow = "#F2F2F0", "#1E64FF", "#00C8FF"
			}
			if m == "bike" {
				sc.Body, sc.Accent, sc.Glow = "#E00000", "#F2F2F0", "#FF2020"
			}
			b, err := ScreenPreviewPNG(sc)
			if err != nil {
				t.Fatal(err)
			}
			os.WriteFile(dir+"/"+m+".png", b, 0o644)
		}
	}
}

func solid565(r, g, b uint8) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, screenSize, screenSize))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = r, g, b, 255
	}
	return img
}
