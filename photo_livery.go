package main

// "Your ride": turn a photo of the person's own vehicle into a livery.
//
// Everything runs on this PC; the photo never leaves it. The steps:
//  1. shrink the photo and learn what the background looks like from its
//     edges (sky, garage wall, pavement), so those colors count for little;
//  2. weight the rest toward the middle of the frame, where the vehicle is;
//  3. group the colors (weighted k-means in CIELAB, so "close" means close
//     to the eye) and drop the tiny groups;
//  4. pick the paint: the main body color, a contrasting accent and, when
//     there is one, a third livery color;
//  5. translate paint into LED light. A dark red paint is still a full
//     bright red on an LED; black paint becomes dim graphite (an LED can't
//     glow black) and silver/white become cool or warm white.

import (
	"errors"
	"fmt"
	"image"
	"math"
	"math/rand"
	"sort"
	"strings"
)

// Swatch is one color found in the photo.
type Swatch struct {
	Paint string  `json:"paint"` // as it looks in the photo
	LED   string  `json:"led"`   // what Livery shows on the lights
	Name  string  `json:"name"`  // plain color name ("Red", "Silver")
	Share float64 `json:"share"` // 0..1 of the vehicle's weighted pixels
	Role  string  `json:"role"`  // base | accent | third | ""
}

// PhotoLivery is what the panel gets back after analysing a photo.
type PhotoLivery struct {
	Theme   ThemeDef `json:"theme"`
	Palette []Swatch `json:"palette"` // biggest first
	Note    string   `json:"note,omitempty"`
}

type lab struct{ L, A, B float64 }

type px struct {
	c lab
	w float64
}

type cluster struct {
	c     lab
	w     float64
	share float64
}

// AnalyzeVehiclePhoto decodes an image and proposes a livery from it.
func AnalyzeVehiclePhoto(data []byte) (PhotoLivery, error) {
	frames, _, err := loadImageFrames(data)
	if err != nil {
		return PhotoLivery{}, err
	}
	return LiveryFromImage(frames[0])
}

// LiveryFromImage does the work on an already decoded image.
func LiveryFromImage(img image.Image) (PhotoLivery, error) {
	small := shrink(img, 200)
	b := small.Bounds()
	if b.Dx() < 8 || b.Dy() < 8 {
		return PhotoLivery{}, errors.New("that picture is too small to read colors from")
	}
	pixels, bg, bgShare := weightPixels(small)
	if len(pixels) == 0 {
		return PhotoLivery{}, errors.New("couldn't find any colors in that picture")
	}
	clusters := kmeansLab(pixels, 8, 14)
	clusters = mergeClose(clusters, 11)
	clusters = mergeShades(clusters)
	sort.Slice(clusters, func(i, j int) bool { return clusters[i].share > clusters[j].share })

	var sig []cluster
	for _, c := range clusters {
		if c.share >= 0.035 {
			sig = append(sig, c)
		}
	}
	if len(sig) == 0 {
		sig = clusters[:1]
	}

	base, accent, third := pickRoles(sig, bg)
	out := PhotoLivery{}
	seen := map[string]int{}
	for i, c := range sig {
		if j, ok := seen[colorName(c.c)]; ok && isNeutral(c.c) && i != base && i != accent {
			out.Palette[j].Share = math.Round((out.Palette[j].Share+c.share)*1000) / 1000
			continue
		}
		if len(out.Palette) == 6 {
			break
		}
		if i != base && i != accent && i != third && bgFactor(c.c, bg) < 0.5 {
			continue // most likely the background showing through
		}
		seen[colorName(c.c)] = len(out.Palette)
		role := ""
		switch i {
		case base:
			role = "base"
		case accent:
			role = "accent"
		case third:
			role = "third"
		}
		out.Palette = append(out.Palette, Swatch{
			Paint: labToRGB(c.c).String(), LED: ledFor(c.c).String(), Name: colorName(c.c),
			Share: math.Round(c.share*1000) / 1000, Role: role,
		})
	}

	b0 := sig[base].c
	a0 := b0
	if accent >= 0 {
		a0 = sig[accent].c
	}
	out.Theme = themeFromPaint(b0, a0, accent >= 0)
	if third >= 0 {
		out.Theme.Description += " Third color: " + strings.ToLower(colorName(sig[third].c)) + " (try it as a stripe)."
	}
	if bgShare > 0.82 {
		out.Note = "The vehicle looks small in this photo, so some background may have crept in. A closer shot works best."
	}
	return out, nil
}

// themeFromPaint fills every slot of a livery from the body and accent paint.
func themeFromPaint(base, accent lab, hasAccent bool) ThemeDef {
	bl, al := ledFor(base).String(), ledFor(accent).String()
	name := "My " + colorName(base)
	desc := fmt.Sprintf("From your photo: %s", strings.ToLower(colorName(base)))
	if hasAccent && colorName(accent) != colorName(base) {
		name += " & " + colorName(accent)
		desc += fmt.Sprintf(" with %s accents.", strings.ToLower(colorName(accent)))
	} else {
		desc += "."
	}
	t := ThemeDef{
		Name: name, Description: desc,
		Base: bl, Accent: al, AccentKeys: "wasd-arrows",
		RearLeft: bl, RearRight: bl, Lid: al, Power: bl,
		MouseLogo: bl, MouseSide: al, GMBKAccent: []int{1, 10},
	}
	// A dark base (black or graphite paint) reads better on the lights the
	// other way round: let the brighter color carry the keyboard.
	if isNeutral(base) && base.L < 45 && hasAccent && (!isNeutral(accent) || accent.L > 60) {
		t.Lid, t.Power, t.MouseLogo, t.RearLeft, t.RearRight = al, al, al, al, al
	}
	if !hasAccent {
		t.AccentKeys = "none"
		t.GMBKAccent = nil
	}
	return t
}

// pickRoles returns indexes into sig for base, accent and third (-1 if none).
// bgFactor is 1 for colors unlike the background and down to 0.3 for a
// color that matches it (sky behind the car, pavement under it).
func bgFactor(c lab, bg []cluster) float64 {
	f := 1.0
	for _, g := range bg {
		f = math.Min(f, 0.3+0.7*math.Min(1, math.Max(0, (deltaE(c, g.c)-5)/18)))
	}
	return f
}

func pickRoles(sig []cluster, bg []cluster) (base, accent, third int) {
	base, accent, third = 0, -1, -1
	bgf := func(c lab) float64 { return bgFactor(c, bg) }
	// Body color: the biggest *colorful* group wins over a slightly bigger
	// grey one (tyres, shadows and trim are usually neutral). A neutral body
	// (black, white, silver) only wins when no color has a real share.
	best := -1.0
	for i, c := range sig {
		score := c.share * bgf(c.c)
		if !isNeutral(c.c) && c.share >= 0.07 {
			score *= 1.6 + chroma(c.c)/60
		}
		if score > best {
			best, base = score, i
		}
	}
	b := sig[base].c
	best = 0
	for i, c := range sig {
		if i == base {
			continue
		}
		d := deltaE(b, c.c)
		if d < 22 {
			continue
		}
		score := d * math.Sqrt(c.share) * bgf(c.c)
		switch {
		case !isNeutral(c.c):
			score *= 1.35
		case c.c.L > 70:
			score *= 1.1 // white / silver trim makes a classic accent
		case c.c.L < 28:
			score *= 0.55 // black is dim on LEDs; only if nothing better
		}
		if score > best {
			best, accent = score, i
		}
	}
	best = 0
	for i, c := range sig {
		if i == base || i == accent || isNeutral(c.c) || c.share < 0.06 {
			continue
		}
		if deltaE(b, c.c) < 25 || (accent >= 0 && deltaE(sig[accent].c, c.c) < 25) {
			continue
		}
		if score := c.share * chroma(c.c) * bgf(c.c); score > best && bgf(c.c) > 0.6 {
			best, third = score, i
		}
	}
	return
}

// --- pixels -----------------------------------------------------------------

// shrink box-averages an image so the longest side is at most max pixels.
func shrink(img image.Image, max int) *image.RGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	s := 1.0
	if w > max || h > max {
		s = float64(max) / float64(maxInt(w, h))
	}
	nw, nh := maxInt(1, int(float64(w)*s+0.5)), maxInt(1, int(float64(h)*s+0.5))
	out := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		y0, y1 := b.Min.Y+y*h/nh, b.Min.Y+(y+1)*h/nh
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < nw; x++ {
			x0, x1 := b.Min.X+x*w/nw, b.Min.X+(x+1)*w/nw
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var r, g, bl, a, n uint64
			for yy := y0; yy < y1; yy++ {
				for xx := x0; xx < x1; xx++ {
					cr, cg, cb, ca := img.At(xx, yy).RGBA()
					r, g, bl, a, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), a+uint64(ca), n+1
				}
			}
			i := out.PixOffset(x, y)
			out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = byte(r/n>>8), byte(g/n>>8), byte(bl/n>>8), byte(a/n>>8)
		}
	}
	return out
}

// weightPixels turns the small image into weighted Lab samples. It also
// reports how much of the weight the background model ate (0..1).
func weightPixels(img *image.RGBA) ([]px, []cluster, float64) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	all := make([]lab, 0, w*h)
	alpha := make([]float64, 0, w*h)
	var border []px
	bw := maxInt(1, int(math.Round(float64(minInt(w, h))*0.08)))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := img.PixOffset(x, y)
			c := rgbToLab(RGB{img.Pix[i], img.Pix[i+1], img.Pix[i+2]})
			all = append(all, c)
			alpha = append(alpha, float64(img.Pix[i+3])/255)
			if img.Pix[i+3] > 128 && (x < bw || y < bw || x >= w-bw || y >= h-bw) {
				border = append(border, px{c, 1})
			}
		}
	}
	// Background: the main colors along the edges. With a transparent PNG
	// (a cut-out) the edges are empty and alpha does the job instead.
	var bg []cluster
	if len(border) > 20 {
		for _, c := range mergeClose(kmeansLab(border, 4, 10), 8) {
			if c.share >= 0.12 {
				bg = append(bg, c)
			}
		}
	}
	cx, cy := float64(w-1)/2, float64(h-1)/2
	sx, sy := float64(w)*0.42, float64(h)*0.45
	var out []px
	var total, kept float64
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			k := y*w + x
			if alpha[k] < 0.5 {
				continue
			}
			dx, dy := (float64(x)-cx)/sx, (float64(y)-cy)/sy
			r2 := dx*dx + dy*dy
			wt := math.Exp(-r2 / 2) // the middle matters most
			total += wt
			// Near the middle the background filter eases off: a blue car
			// against a blue sky is still a blue car.
			protect := 0.4 * math.Exp(-r2/0.35)
			for _, g := range bg {
				d := deltaE(all[k], g.c)
				// ~0 on a background color, back to full weight ~25 ΔE away.
				f := math.Min(1, math.Max(0.03, (d-6)/20))
				wt *= f + (1-f)*protect
			}
			// Specular highlights and deep shadow say little about paint.
			if all[k].L > 96 || all[k].L < 4 {
				wt *= 0.25
			}
			if wt > 1e-4 {
				out = append(out, px{all[k], wt})
				kept += wt
			}
		}
	}
	bgShare := 0.0
	if total > 0 {
		bgShare = 1 - kept/total
	}
	return out, bg, bgShare
}

// kmeansLab clusters weighted Lab samples. Seeded, so the same photo always
// gives the same livery.
func kmeansLab(p []px, k, iters int) []cluster {
	if len(p) == 0 {
		return nil
	}
	if k > len(p) {
		k = len(p)
	}
	rng := rand.New(rand.NewSource(7))
	// k-means++ start, weighted.
	cent := []lab{p[weightedPick(rng, p, nil)].c}
	dist := make([]float64, len(p))
	for len(cent) < k {
		for i := range p {
			dist[i] = math.Inf(1)
			for _, c := range cent {
				if d := sqDist(p[i].c, c); d < dist[i] {
					dist[i] = d
				}
			}
		}
		cent = append(cent, p[weightedPick(rng, p, dist)].c)
	}
	assign := make([]int, len(p))
	sums := make([]lab, k)
	ws := make([]float64, k)
	for it := 0; it < iters; it++ {
		for i := range sums {
			sums[i], ws[i] = lab{}, 0
		}
		for i, s := range p {
			bi, bd := 0, math.Inf(1)
			for j, c := range cent {
				if d := sqDist(s.c, c); d < bd {
					bi, bd = j, d
				}
			}
			assign[i] = bi
			sums[bi].L += s.c.L * s.w
			sums[bi].A += s.c.A * s.w
			sums[bi].B += s.c.B * s.w
			ws[bi] += s.w
		}
		for j := range cent {
			if ws[j] > 0 {
				cent[j] = lab{sums[j].L / ws[j], sums[j].A / ws[j], sums[j].B / ws[j]}
			}
		}
	}
	var total float64
	for _, w := range ws {
		total += w
	}
	var out []cluster
	for j := range cent {
		if ws[j] > 0 {
			out = append(out, cluster{c: cent[j], w: ws[j], share: ws[j] / total})
		}
	}
	return out
}

func weightedPick(rng *rand.Rand, p []px, dist []float64) int {
	var sum float64
	for i := range p {
		v := p[i].w
		if dist != nil {
			v *= dist[i]
		}
		sum += v
	}
	if sum <= 0 {
		return rng.Intn(len(p))
	}
	r := rng.Float64() * sum
	for i := range p {
		v := p[i].w
		if dist != nil {
			v *= dist[i]
		}
		if r -= v; r <= 0 {
			return i
		}
	}
	return len(p) - 1
}

// mergeClose folds together groups the eye would call the same color.
func mergeClose(cs []cluster, maxDE float64) []cluster {
	out := append([]cluster{}, cs...)
	for {
		bi, bj, bd := -1, -1, maxDE
		for i := range out {
			for j := i + 1; j < len(out); j++ {
				if d := deltaE(out[i].c, out[j].c); d < bd {
					bi, bj, bd = i, j, d
				}
			}
		}
		if bi < 0 {
			break
		}
		a, b := out[bi], out[bj]
		w := a.w + b.w
		out[bi] = cluster{
			c:     lab{(a.c.L*a.w + b.c.L*b.w) / w, (a.c.A*a.w + b.c.A*b.w) / w, (a.c.B*a.w + b.c.B*b.w) / w},
			w:     w,
			share: a.share + b.share,
		}
		out = append(out[:bj], out[bj+1:]...)
	}
	return out
}

// mergeShades joins groups that are the same paint in light and shadow:
// colorful groups with nearly the same hue and similar colorfulness. Black,
// grey, silver and white stay apart (they are different paints).
func mergeShades(cs []cluster) []cluster {
	out := append([]cluster{}, cs...)
	same := func(a, b lab) bool {
		if isNeutral(a) || isNeutral(b) {
			return false
		}
		dh := math.Abs(hueDeg(a) - hueDeg(b))
		if dh > 180 {
			dh = 360 - dh
		}
		ca, cb := chroma(a), chroma(b)
		return dh < 16 && math.Min(ca, cb)/math.Max(ca, cb) > 0.45 && math.Abs(a.L-b.L) < 40
	}
	for merged := true; merged; {
		merged = false
		for i := 0; i < len(out) && !merged; i++ {
			for j := i + 1; j < len(out); j++ {
				if !same(out[i].c, out[j].c) {
					continue
				}
				a, b := out[i], out[j]
				// Keep the better-lit shade's color (closest to the paint as
				// seen in daylight), weighted toward the bigger group.
				keep := a.c
				if b.w > a.w*1.4 || (b.w*1.4 >= a.w && b.c.L > a.c.L) {
					keep = b.c
				}
				out[i] = cluster{c: keep, w: a.w + b.w, share: a.share + b.share}
				out = append(out[:j], out[j+1:]...)
				merged = true
				break
			}
		}
	}
	return out
}

// --- color science ----------------------------------------------------------

func srgbToLinear(v byte) float64 {
	c := float64(v) / 255
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func linearToSrgb(c float64) byte {
	if c <= 0 {
		return 0
	}
	if c >= 1 {
		return 255
	}
	if c <= 0.0031308 {
		c *= 12.92
	} else {
		c = 1.055*math.Pow(c, 1/2.4) - 0.055
	}
	return byte(c*255 + 0.5)
}

const xn, yn, zn = 0.95047, 1.0, 1.08883

func labF(t float64) float64 {
	if t > 216.0/24389 {
		return math.Cbrt(t)
	}
	return (24389.0/27*t + 16) / 116
}

func labFInv(t float64) float64 {
	if t*t*t > 216.0/24389 {
		return t * t * t
	}
	return (116*t - 16) * 27 / 24389
}

func rgbToLab(c RGB) lab {
	r, g, b := srgbToLinear(c.R), srgbToLinear(c.G), srgbToLinear(c.B)
	x := 0.4124564*r + 0.3575761*g + 0.1804375*b
	y := 0.2126729*r + 0.7151522*g + 0.0721750*b
	z := 0.0193339*r + 0.1191920*g + 0.9503041*b
	fx, fy, fz := labF(x/xn), labF(y/yn), labF(z/zn)
	return lab{116*fy - 16, 500 * (fx - fy), 200 * (fy - fz)}
}

func labToRGB(c lab) RGB {
	fy := (c.L + 16) / 116
	fx, fz := fy+c.A/500, fy-c.B/200
	x, y, z := labFInv(fx)*xn, labFInv(fy)*yn, labFInv(fz)*zn
	r := 3.2404542*x - 1.5371385*y - 0.4985314*z
	g := -0.9692660*x + 1.8760108*y + 0.0415560*z
	b := 0.0556434*x - 0.2040259*y + 1.0572252*z
	return RGB{linearToSrgb(r), linearToSrgb(g), linearToSrgb(b)}
}

func sqDist(a, b lab) float64 {
	dl, da, db := a.L-b.L, a.A-b.A, a.B-b.B
	return dl*dl + da*da + db*db
}

// deltaE is CIE76: plain distance in Lab. Good enough for grouping paint.
func deltaE(a, b lab) float64 { return math.Sqrt(sqDist(a, b)) }

func chroma(c lab) float64 { return math.Hypot(c.A, c.B) }

func hueDeg(c lab) float64 {
	h := math.Atan2(c.B, c.A) * 180 / math.Pi
	if h < 0 {
		h += 360
	}
	return h
}

// isNeutral: black, greys, silver, white. Dark colors need less chroma to
// count as colorful (a deep blue has low chroma but is clearly blue).
func isNeutral(c lab) bool {
	limit := 14.0
	if c.L < 30 {
		limit = 13 // tinted glass and shadows; navy paint is well above this
	}
	return chroma(c) < limit
}

// ledFor turns a paint color into what an RGB LED should show.
func ledFor(c lab) RGB {
	if isNeutral(c) {
		switch {
		case c.L < 32:
			return mustHex(hexAlu) // black paint: dim graphite
		case c.L < 52:
			return mustHex(hexGraphite)
		case c.L < 78:
			return RGB{0xB4, 0xBE, 0xCD} // silver
		case c.B > 4:
			return mustHex(hexWarmWhite) // cream / pearl
		default:
			return RGB{0xE6, 0xEE, 0xFF} // bright white
		}
	}
	h, s, _ := rgbToHSV(labToRGB(c))
	// Paint photographs muted; LEDs look washed out unless saturated.
	s = math.Min(1, 0.25+s*1.05)
	if chroma(c) > 35 {
		s = math.Max(s, 0.9)
	}
	// Browns and tans: keep them warm but readable (dark orange on an LED).
	if h >= 15 && h < 50 && c.L < 45 {
		return hsvToRGB(h, math.Max(s, 0.85), 0.75)
	}
	return hsvToRGB(h, s, 1)
}

// colorName gives a plain, brand-free name for a paint color.
func colorName(c lab) string {
	if isNeutral(c) {
		switch {
		case c.L < 25:
			return "Black"
		case c.L < 50:
			return "Graphite"
		case c.L < 80:
			return "Silver"
		case c.B > 4:
			return "Cream"
		default:
			return "White"
		}
	}
	h, s, v := rgbToHSV(labToRGB(c))
	switch {
	case h < 12 || h >= 345:
		if v < 0.45 {
			return "Maroon"
		}
		return "Red"
	case h < 40:
		if c.L < 45 {
			return "Brown"
		}
		if s < 0.45 {
			return "Tan"
		}
		return "Orange"
	case h < 66:
		if c.L < 50 {
			return "Olive"
		}
		if s < 0.4 {
			return "Sand"
		}
		return "Yellow"
	case h < 95:
		if c.L < 50 {
			return "Olive"
		}
		return "Lime"
	case h < 165:
		return "Green"
	case h < 195:
		return "Teal"
	case h < 215:
		if c.L > 60 {
			return "Sky Blue"
		}
		return "Blue"
	case h < 250:
		if c.L < 30 {
			return "Navy"
		}
		return "Blue"
	case h < 290:
		return "Purple"
	default:
		return "Pink"
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
