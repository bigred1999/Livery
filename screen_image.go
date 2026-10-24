package main

// "Your image" for the keyboard screen: a PNG, JPEG or (animated) GIF the
// person picks, scaled to 128x128.

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
)

const (
	maxImageBytes  = 20 << 20 // what the panel accepts
	maxImageSide   = 4096     // larger images are refused before decoding
	maxImageFrames = 60       // ~2 MB on the keyboard; longer GIFs are thinned out
)

// screenImageLoader returns the stored picture (set by the Windows side).
var screenImageLoader func() ([]byte, error)

// loadImageFrames decodes a picture into full frames plus per-frame delays
// in milliseconds. GIF frames are composited the way browsers show them.
func loadImageFrames(data []byte) ([]image.Image, []int, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, nil, errors.New("that file isn't a PNG, JPEG or GIF image Livery can read")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxImageSide || cfg.Height > maxImageSide {
		return nil, nil, fmt.Errorf("the image is %dx%d; please use one up to %d pixels on each side", cfg.Width, cfg.Height, maxImageSide)
	}
	if format != "gif" {
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, nil, err
		}
		return []image.Image{img}, []int{1000}, nil
	}
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return nil, nil, err
	}
	if len(g.Image) == 0 {
		return nil, nil, errors.New("the GIF has no frames")
	}
	w, h := g.Config.Width, g.Config.Height
	if w <= 0 || h <= 0 {
		b := g.Image[0].Bounds()
		w, h = b.Dx(), b.Dy()
	}
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	var frames []image.Image
	var delays []int
	for i, fr := range g.Image {
		var saved *image.RGBA
		disposal := byte(0)
		if i < len(g.Disposal) {
			disposal = g.Disposal[i]
		}
		if disposal == gif.DisposalPrevious {
			saved = image.NewRGBA(canvas.Bounds())
			copy(saved.Pix, canvas.Pix)
		}
		draw.Draw(canvas, fr.Bounds(), fr, fr.Bounds().Min, draw.Over)
		snap := image.NewRGBA(canvas.Bounds())
		copy(snap.Pix, canvas.Pix)
		frames = append(frames, snap)
		d := 100
		if i < len(g.Delay) && g.Delay[i] > 0 {
			d = g.Delay[i] * 10
		}
		delays = append(delays, d)
		switch disposal {
		case gif.DisposalBackground:
			draw.Draw(canvas, fr.Bounds(), image.Transparent, image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			copy(canvas.Pix, saved.Pix)
		}
	}
	return frames, delays, nil
}

// thinFrames keeps at most max frames, spreading the dropped time over the
// frames that remain so the animation runs at the same overall speed.
func thinFrames(frames []image.Image, delays []int, max int) ([]image.Image, []int) {
	if len(frames) <= max {
		return frames, delays
	}
	var outF []image.Image
	var outD []int
	step := float64(len(frames)) / float64(max)
	for k := 0; k < max; k++ {
		a, b := int(float64(k)*step), int(float64(k+1)*step)
		sum := 0
		for i := a; i < b && i < len(delays); i++ {
			sum += delays[i]
		}
		outF = append(outF, frames[a])
		outD = append(outD, sum)
	}
	return outF, outD
}

// fitSquare scales src to the screen. "fill" crops the middle square,
// "fit" shows the whole picture with black bars. Downscaling averages every
// source pixel under each screen pixel; upscaling interpolates.
func fitSquare(src image.Image, mode string) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, screenSize, screenSize))
	draw.Draw(out, out.Bounds(), image.Black, image.Point{}, draw.Src)
	b := src.Bounds()
	sw, sh := float64(b.Dx()), float64(b.Dy())
	// source rectangle shown and destination rectangle used
	sx0, sy0, sx1, sy1 := 0.0, 0.0, sw, sh
	dx0, dy0, dx1, dy1 := 0.0, 0.0, float64(screenSize), float64(screenSize)
	if mode == "fit" {
		if sw > sh {
			hgt := float64(screenSize) * sh / sw
			dy0, dy1 = (float64(screenSize)-hgt)/2, (float64(screenSize)+hgt)/2
		} else {
			wid := float64(screenSize) * sw / sh
			dx0, dx1 = (float64(screenSize)-wid)/2, (float64(screenSize)+wid)/2
		}
	} else {
		if sw > sh {
			sx0, sx1 = (sw-sh)/2, (sw+sh)/2
		} else {
			sy0, sy1 = (sh-sw)/2, (sh+sw)/2
		}
	}
	scaleX, scaleY := (sx1-sx0)/(dx1-dx0), (sy1-sy0)/(dy1-dy0)
	at := func(x, y int) (float64, float64, float64, float64) {
		r, g, bl, a := src.At(b.Min.X+x, b.Min.Y+y).RGBA()
		return float64(r >> 8), float64(g >> 8), float64(bl >> 8), float64(a >> 8)
	}
	for y := int(math.Floor(dy0)); y < int(math.Ceil(dy1)) && y < screenSize; y++ {
		for x := int(math.Floor(dx0)); x < int(math.Ceil(dx1)) && x < screenSize; x++ {
			fx0 := sx0 + (float64(x)-dx0)*scaleX
			fy0 := sy0 + (float64(y)-dy0)*scaleY
			var r, g, bl, a, n float64
			if scaleX >= 1 && scaleY >= 1 {
				for yy := int(fy0); yy < int(math.Ceil(fy0+scaleY)) && yy < b.Dy(); yy++ {
					for xx := int(fx0); xx < int(math.Ceil(fx0+scaleX)) && xx < b.Dx(); xx++ {
						pr, pg, pb, pa := at(xx, yy)
						r, g, bl, a, n = r+pr*pa/255, g+pg*pa/255, bl+pb*pa/255, a+pa, n+1
					}
				}
			} else { // bilinear
				cx, cy := fx0+scaleX/2-0.5, fy0+scaleY/2-0.5
				x0, y0 := int(math.Floor(cx)), int(math.Floor(cy))
				tx, ty := cx-float64(x0), cy-float64(y0)
				for _, s := range [4][3]float64{{0, 0, (1 - tx) * (1 - ty)}, {1, 0, tx * (1 - ty)}, {0, 1, (1 - tx) * ty}, {1, 1, tx * ty}} {
					xx := min(max(x0+int(s[0]), 0), b.Dx()-1)
					yy := min(max(y0+int(s[1]), 0), b.Dy()-1)
					pr, pg, pb, pa := at(xx, yy)
					w := s[2]
					r, g, bl, a, n = r+pr*pa/255*w, g+pg*pa/255*w, bl+pb*pa/255*w, a+pa*w, n+w
				}
			}
			if n == 0 {
				continue
			}
			// transparent areas show black, like the rest of the screen
			c := func(v float64) uint8 { return uint8(math.Max(0, math.Min(255, math.Round(v/n)))) }
			out.SetRGBA(x, y, color.RGBA{c(r), c(g), c(bl), 255})
		}
	}
	return out
}

// imageScreenFrames turns the stored picture into screen frames and delays.
func imageScreenFrames(fit string) ([]*image.RGBA, []int, error) {
	if screenImageLoader == nil {
		return nil, nil, errors.New("no image chosen yet")
	}
	data, err := screenImageLoader()
	if err != nil {
		return nil, nil, errors.New("no image chosen yet")
	}
	src, delays, err := loadImageFrames(data)
	if err != nil {
		return nil, nil, err
	}
	src, delays = thinFrames(src, delays, maxImageFrames)
	out := make([]*image.RGBA, len(src))
	for i, f := range src {
		out[i] = fitSquare(f, fit)
	}
	return out, delays, nil
}
