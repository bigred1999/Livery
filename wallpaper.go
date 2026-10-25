package main

// Desktop wallpapers that match a livery. Two looks:
//   - "stripes": a deep background tinted from the body paint with a band
//     of racing stripes crossing the screen, softly glowing like the lights;
//   - "photo": the person's own vehicle photo, with a dark livery panel on
//     the left and the stripes running down its edge.
// Pure Go, no network; rendered at the screen's native resolution.

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"math"
)

type WallpaperOpts struct {
	Style  string   `json:"style"`  // stripes | photo
	Colors []string `json:"colors"` // 1-4 "#RRGGBB": body, accent, third...
	W      int      `json:"w"`
	H      int      `json:"h"`
}

func (o *WallpaperOpts) normalize() ([]RGB, error) {
	if o.Style != "photo" {
		o.Style = "stripes"
	}
	if o.W == 0 || o.H == 0 {
		o.W, o.H = 2560, 1440
	}
	if o.W < 320 || o.H < 240 || o.W > 7680 || o.H > 4320 {
		return nil, errors.New("wallpaper size must be between 320x240 and 7680x4320")
	}
	if len(o.Colors) == 0 || len(o.Colors) > 4 {
		return nil, errors.New("pick 1 to 4 colors")
	}
	var cs []RGB
	for _, h := range o.Colors {
		c, err := ParseHex(h)
		if err != nil {
			return nil, err
		}
		cs = append(cs, c)
	}
	return cs, nil
}

// WallpaperPNG renders and encodes. photo may be nil for "stripes".
func WallpaperPNG(o WallpaperOpts, photo image.Image) ([]byte, error) {
	img, err := RenderWallpaper(o, photo)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type fcol struct{ r, g, b float64 }

func toF(c RGB) fcol { return fcol{float64(c.R), float64(c.G), float64(c.B)} }

func mix(a, b fcol, t float64) fcol {
	return fcol{a.r + (b.r-a.r)*t, a.g + (b.g-a.g)*t, a.b + (b.b-a.b)*t}
}

func clamp255(v float64) byte {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return byte(v + 0.5)
}

// wallColors works out the background and the stripe colors (most
// prominent first) from the livery colors.
func wallColors(cs []RGB) (bgTop, bgBot fcol, stripes []fcol) {
	body := rgbToLab(cs[0])
	tint := func(L, k float64) fcol {
		c := lab{L, body.A * k, body.B * k}
		return toF(labToRGB(c))
	}
	if isNeutral(body) {
		bgTop, bgBot = toF(RGB{0x16, 0x17, 0x1B}), toF(RGB{0x07, 0x07, 0x09})
	} else {
		// Same hue as the paint, very dark: reads as "red" or "blue"
		// without fighting the icons on top.
		k := math.Min(0.45, 18/math.Max(1, chroma(body)))
		bgTop, bgBot = tint(11, k), tint(3.5, k*0.7)
	}
	for _, c := range cs {
		l := rgbToLab(c)
		// A stripe as dark as the background would vanish: lift it.
		if l.L < 22 {
			l.L = 30
		}
		stripes = append(stripes, toF(labToRGB(l)))
	}
	// A black body: lead with the brightest color instead.
	if isNeutral(body) && body.L < 30 && len(stripes) > 1 {
		stripes = append(stripes[1:], stripes[0])
	}
	return
}

type stripe struct {
	off, w float64 // offset of the stripe's near edge from the band line, width (pixels)
	c      fcol
}

// bandStripes lays out up to three stripes plus a pinstripe, sized from u
// (a unit: the screen height).
func bandStripes(cols []fcol, u float64) []stripe {
	widths := []float64{0.15, 0.065, 0.028}
	gap := 0.016
	var out []stripe
	pos := 0.0
	for i, c := range cols {
		if i == 3 {
			break
		}
		w := widths[i] * u
		out = append(out, stripe{pos, w, c})
		pos += w + gap*u
	}
	// Finish with a pinstripe: the accent (or the body color on a
	// one-color livery), the way stripe kits are usually edged.
	pin := cols[0]
	if len(cols) > 1 {
		pin = cols[1]
	}
	if len(cols) < 3 {
		out = append(out, stripe{pos, 0.011 * u, pin})
	}
	return out
}

// RenderWallpaper draws the wallpaper.
func RenderWallpaper(o WallpaperOpts, photo image.Image) (*image.RGBA, error) {
	cs, err := o.normalize()
	if err != nil {
		return nil, err
	}
	if o.Style == "photo" && photo == nil {
		return nil, errors.New("add a photo of your vehicle first")
	}
	W, H := o.W, o.H
	fw, fh := float64(W), float64(H)
	u := math.Min(fw, fh*1.6)
	bgTop, bgBot, cols := wallColors(cs)
	img := image.NewRGBA(image.Rect(0, 0, W, H))

	// The photo fills the area right of the panel, so the vehicle sits in
	// the open part of the screen rather than under the panel.
	var src *image.RGBA
	photoX := int(fw * 0.3)
	if o.Style == "photo" {
		src = coverFit(photo, W-photoX, H)
	}

	// Stripe band geometry. "stripes": a line rising left to right across
	// the lower middle. "photo": a near-vertical line at the panel's edge.
	var ang, ox, oy float64
	if o.Style == "photo" {
		ang = (90 - 14) * math.Pi / 180
		ox, oy = fw*0.37, fh/2
	} else {
		ang = -17 * math.Pi / 180
		ox, oy = fw*0.5, fh*0.6
	}
	dirX, dirY := math.Cos(ang), math.Sin(ang)
	nX, nY := -dirY, dirX // normal: stripes stack along it
	if o.Style == "photo" && nX < 0 {
		nX, nY = -nX, -nY // stack toward the photo (to the right)
	}
	sts := bandStripes(cols, u*0.62)
	bandW := sts[len(sts)-1].off + sts[len(sts)-1].w
	if o.Style == "stripes" {
		// Center the band on the line.
		for i := range sts {
			sts[i].off -= bandW / 2
		}
	}
	glowC := cols[0]
	glowR := u * 0.22

	for y := 0; y < H; y++ {
		fy := float64(y) + 0.5
		t := fy / fh
		row := mix(bgTop, bgBot, t*t*0.9+t*0.1)
		for x := 0; x < W; x++ {
			fx := float64(x) + 0.5
			c := row
			d := (fx-ox)*nX + (fy-oy)*nY // signed distance from the band line

			if o.Style == "photo" {
				i := src.PixOffset(maxInt(0, x-photoX), y)
				p := fcol{float64(src.Pix[i]), float64(src.Pix[i+1]), float64(src.Pix[i+2])}
				// Panel: solid to the left of the band, feathered into the
				// photo over a short run to the right of the stripes.
				edge := bandW + u*0.012
				a := 1 - smooth((d-edge)/(u*0.07))
				if d < 0 {
					a = 1
				}
				panel := mix(row, p, 0.04) // a hint of the photo's light
				c = mix(p, panel, a*0.94)
			} else {
				// Soft glow around the band, as if the stripes were lit.
				dd := math.Abs(d) - bandW/2
				if dd > 0 {
					g := math.Exp(-dd/glowR) * 0.16
					c = mix(c, glowC, g)
				} else {
					c = mix(c, glowC, 0.16)
				}
				// Fine diagonal grain so large flat areas don't band.
				gr := math.Sin((fx*dirX+fy*dirY)*0.9) * 1.4
				c = fcol{c.r + gr, c.g + gr, c.b + gr}
			}

			for _, s := range sts {
				cov := clamp01(d-s.off+0.5) * clamp01(s.off+s.w-d+0.5)
				if cov > 0 {
					// Slight sheen along each stripe.
					along := (fx-ox)*dirX + (fy-oy)*dirY
					sheen := 1 + 0.07*math.Sin(along/u*3.2)
					sc := fcol{s.c.r * sheen, s.c.g * sheen, s.c.b * sheen}
					c = mix(c, sc, cov)
				}
			}

			// Vignette.
			vx, vy := (fx/fw-0.5)*1.25, (fy/fh-0.5)*1.25
			v := 1 - 0.38*math.Pow(math.Min(1, vx*vx+vy*vy), 1.4)
			i := img.PixOffset(x, y)
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = clamp255(c.r*v), clamp255(c.g*v), clamp255(c.b*v), 255
		}
	}
	return img, nil
}

func smooth(t float64) float64 {
	t = clamp01(t)
	return t * t * (3 - 2*t)
}

// coverFit scales a photo to fill W x H, cropping the overflow evenly.
func coverFit(photo image.Image, W, H int) *image.RGBA {
	b := photo.Bounds()
	sw, sh := float64(b.Dx()), float64(b.Dy())
	// Shrink big photos with box filtering first (sharp, no aliasing),
	// then sample bilinearly.
	scale := math.Max(float64(W)/sw, float64(H)/sh)
	var src *image.RGBA
	if scale < 0.75 {
		target := int(math.Ceil(math.Max(sw, sh) * scale * 1.25))
		src = shrink(photo, target)
	} else {
		src = shrink(photo, int(math.Max(sw, sh)))
	}
	sb := src.Bounds()
	sw, sh = float64(sb.Dx()), float64(sb.Dy())
	scale = math.Max(float64(W)/sw, float64(H)/sh)
	cw, ch := float64(W)/scale, float64(H)/scale
	x0, y0 := (sw-cw)/2, (sh-ch)/2
	out := image.NewRGBA(image.Rect(0, 0, W, H))
	for y := 0; y < H; y++ {
		sy := y0 + (float64(y)+0.5)/scale - 0.5
		for x := 0; x < W; x++ {
			sx := x0 + (float64(x)+0.5)/scale - 0.5
			r, g, bl := bilinear(src, sx, sy)
			i := out.PixOffset(x, y)
			out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = r, g, bl, 255
		}
	}
	return out
}

func bilinear(img *image.RGBA, x, y float64) (byte, byte, byte) {
	b := img.Bounds()
	maxX, maxY := b.Dx()-1, b.Dy()-1
	x = math.Max(0, math.Min(float64(maxX), x))
	y = math.Max(0, math.Min(float64(maxY), y))
	x0, y0 := int(x), int(y)
	x1, y1 := minInt(x0+1, maxX), minInt(y0+1, maxY)
	fx, fy := x-float64(x0), y-float64(y0)
	var out [3]byte
	for k := 0; k < 3; k++ {
		a := float64(img.Pix[img.PixOffset(x0, y0)+k])
		bb := float64(img.Pix[img.PixOffset(x1, y0)+k])
		c := float64(img.Pix[img.PixOffset(x0, y1)+k])
		d := float64(img.Pix[img.PixOffset(x1, y1)+k])
		out[k] = clamp255((a*(1-fx)+bb*fx)*(1-fy) + (c*(1-fx)+d*fx)*fy)
	}
	return out[0], out[1], out[2]
}
