package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWallpaperRenders(t *testing.T) {
	photo := testScene("road", cRed, cWhite, cNone, 900, 600)
	r, err := LiveryFromImage(photo)
	if err != nil {
		t.Fatal(err)
	}
	var cols []string
	for _, role := range []string{"base", "accent", "third"} {
		for _, s := range r.Palette {
			if s.Role == role {
				cols = append(cols, s.Paint)
			}
		}
	}
	for _, style := range []string{"stripes", "photo"} {
		start := time.Now()
		b, err := WallpaperPNG(WallpaperOpts{Style: style, Colors: cols, W: 2560, H: 1440}, photo)
		if err != nil {
			t.Fatal(style, err)
		}
		img, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		if img.Bounds().Dx() != 2560 || img.Bounds().Dy() != 1440 {
			t.Errorf("%s: size %v", style, img.Bounds())
		}
		if d := time.Since(start); d > 8*time.Second {
			t.Errorf("%s: took %v at 1440p", style, d)
		}
		if dir := os.Getenv("LIVERY_DUMP"); dir != "" {
			os.WriteFile(filepath.Join(dir, "wall-"+style+".png"), b, 0o644)
		}
	}
	if _, err := RenderWallpaper(WallpaperOpts{Style: "photo", Colors: cols}, nil); err == nil {
		t.Error("photo style without a photo should be refused")
	}
	if _, err := RenderWallpaper(WallpaperOpts{Colors: []string{"red"}}, nil); err == nil {
		t.Error("bad color should be refused")
	}
	if _, err := RenderWallpaper(WallpaperOpts{Colors: cols, W: 99999, H: 10}, nil); err == nil {
		t.Error("absurd size should be refused")
	}
}
