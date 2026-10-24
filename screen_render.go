package main

// Animations for small keyboard screens: original low-poly vehicles on a
// turntable over a neon grid, drawn with a tiny software renderer
// (flat-shaded polygons, painter's algorithm, 3x supersampling).

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"sort"
)

const (
	screenSize   = 128
	screenSS     = 3 // supersampling factor
	screenFrames = 24
	screenTickMS = 80 // per frame; the firmware's minimum is 16 ticks of 5 ms
)

// ScreenSettings is what the panel's "Keyboard screen" box sends.
type ScreenSettings struct {
	Model  string `json:"model"`          // coupe | sedan | bike
	Body   string `json:"body"`           // main paint
	Accent string `json:"accent"`         // stripes, wing, wheels trim
	Glow   string `json:"glow"`           // grid and underglow
	Flag   string `json:"flag,omitempty"` // which flag, for the "flag" model
}

// Flags for the waving-flag animation, stripes top to bottom (screen colors).
var screenFlags = map[string][]string{
	"rainbow":   {"#E40303", "#FF8C00", "#FFED00", "#008026", "#004CFF", "#732982"},
	"trans":     {"#5BCEFA", "#F5A9B8", "#FFFFFF", "#F5A9B8", "#5BCEFA"},
	"bi":        {"#D60270", "#D60270", "#9B4F96", "#0038A8", "#0038A8"},
	"pan":       {"#FF218C", "#FFD800", "#21B1FF"},
	"lesbian":   {"#D52D00", "#FF9A56", "#FFFFFF", "#D362A4", "#A30262"},
	"nonbinary": {"#FCF434", "#FFFFFF", "#9C59D1", "#2C2C2C"},
}

var screenModels = map[string]string{
	"coupe": "Street coupe",
	"sedan": "Tuner sedan",
	"bike":  "Superbike",
	"flag":  "Pride flag",
}

func (sc *ScreenSettings) normalize() error {
	if _, ok := screenModels[sc.Model]; !ok {
		return errors.New("unknown model")
	}
	if sc.Model == "flag" {
		if _, ok := screenFlags[sc.Flag]; !ok {
			sc.Flag = "rainbow"
		}
	}
	for _, c := range []*string{&sc.Body, &sc.Accent, &sc.Glow} {
		if _, err := ParseHex(*c); err != nil {
			return err
		}
	}
	return nil
}

type vec3 [3]float64

func (a vec3) sub(b vec3) vec3    { return vec3{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }
func (a vec3) add(b vec3) vec3    { return vec3{a[0] + b[0], a[1] + b[1], a[2] + b[2]} }
func (a vec3) mul(f float64) vec3 { return vec3{a[0] * f, a[1] * f, a[2] * f} }
func (a vec3) dot(b vec3) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }
func (a vec3) cross(b vec3) vec3 {
	return vec3{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
}
func (a vec3) norm() float64 { return math.Sqrt(a.dot(a)) }
func rgbVec(c RGB) vec3      { return vec3{float64(c.R), float64(c.G), float64(c.B)} }
func hexVec(h string) vec3   { return rgbVec(mustHex(h)) }
func centroid(p []vec3) (c vec3) {
	for _, v := range p {
		c = c.add(v)
	}
	return c.mul(1 / float64(len(p)))
}

type face struct {
	pts    []vec3
	col    vec3
	emit   bool
	center vec3 // center of the part this face belongs to (orients the normal)
}

type mesh struct{ faces []face }

func (m *mesh) add(pts []vec3, col vec3, emit bool, center vec3) {
	m.faces = append(m.faces, face{pts, col, emit, center})
}

func (m *mesh) box(x0, x1, y0, y1, z0, z1 float64, col vec3) {
	var p [8]vec3
	i := 0
	for _, x := range []float64{x0, x1} {
		for _, y := range []float64{y0, y1} {
			for _, z := range []float64{z0, z1} {
				p[i] = vec3{x, y, z}
				i++
			}
		}
	}
	c := centroid(p[:])
	for _, q := range [][4]int{{0, 1, 3, 2}, {4, 6, 7, 5}, {0, 4, 5, 1}, {2, 3, 7, 6}, {0, 2, 6, 4}, {1, 5, 7, 3}} {
		m.add([]vec3{p[q[0]], p[q[1]], p[q[2]], p[q[3]]}, col, false, c)
	}
}

func (m *mesh) scale(f float64) {
	for i := range m.faces {
		for j := range m.faces[i].pts {
			m.faces[i].pts[j] = m.faces[i].pts[j].mul(f)
		}
		m.faces[i].center = m.faces[i].center.mul(f)
	}
}

// extrude makes a solid from a side profile (x,y) between z0 and z1. glass
// lists edge indexes drawn in the glass color.
func (m *mesh) extrude(prof [][2]float64, z0, z1 float64, col vec3, edgeCol map[int]vec3) {
	n := len(prof)
	L := make([]vec3, n)
	R := make([]vec3, n)
	for i, p := range prof {
		L[i] = vec3{p[0], p[1], z0}
		R[i] = vec3{p[0], p[1], z1}
	}
	c := centroid(append(append([]vec3{}, L...), R...))
	m.add(L, col, false, c)
	m.add(R, col, false, c)
	for i := 0; i < n; i++ {
		j := (i + 1) % n
		ec := col
		if v, ok := edgeCol[i]; ok {
			ec = v
		}
		m.add([]vec3{L[i], L[j], R[j], R[i]}, ec, false, c)
	}
}

// wheel adds a tire (cylinder) with a rim disc on the outside face.
func (m *mesh) wheel(x, y, z, r, width, side float64, tire, rim vec3) {
	const k = 16
	c := vec3{x, y, z - side*width/2}
	var outer, inner, rimPts []vec3
	for i := 0; i < k; i++ {
		a := 2 * math.Pi * float64(i) / k
		outer = append(outer, vec3{x + r*math.Cos(a), y + r*math.Sin(a), z})
		inner = append(inner, vec3{x + r*math.Cos(a), y + r*math.Sin(a), z - side*width})
		rimPts = append(rimPts, vec3{x + 0.62*r*math.Cos(a), y + 0.62*r*math.Sin(a), z + side*0.01})
	}
	for i := 0; i < k; i++ {
		j := (i + 1) % k
		m.add([]vec3{outer[i], outer[j], inner[j], inner[i]}, tire, false, c)
	}
	m.add(outer, tire, false, c)
	m.add(inner, tire, false, c)
	m.add(rimPts, rim, false, c.sub(vec3{0, 0, side * width}))
}

var (
	colGlass = hexVec("#0E1218")
	colTire  = hexVec("#141418")
	colDark  = hexVec("#1E1E24")
	colHead  = hexVec("#FFF4D0")
	colTail  = hexVec("#FF1A1A")
)

// Street coupe: long hood, low roof, short deck, small wing.
func coupeMesh(body, accent vec3) mesh {
	var m mesh
	prof := [][2]float64{{-2.3, 0.28}, {-2.35, 0.52}, {-2.05, 0.68}, {-0.5, 0.82}, {0.15, 1.2}, {0.85, 1.2}, {1.75, 0.9}, {2.2, 0.88}, {2.28, 0.58}, {2.22, 0.28}}
	m.extrude(prof, -0.95, 0.95, body, map[int]vec3{3: colGlass, 5: colGlass})
	bodyC := vec3{0, 0.7, 0}
	for _, z := range []float64{-0.96, 0.96} {
		m.add([]vec3{{-0.4, 0.84, z}, {0.2, 1.15, z}, {0.85, 1.15, z}, {1.6, 0.9, z}}, colGlass, false, bodyC)
	}
	// twin accent stripes over hood, roof and deck
	for _, zc := range []float64{-0.24, 0.24} {
		for a := 1; a < 7; a++ {
			p1, p2 := prof[a], prof[a+1]
			m.add([]vec3{{p1[0], p1[1] + 0.012, zc - 0.1}, {p2[0], p2[1] + 0.012, zc - 0.1}, {p2[0], p2[1] + 0.012, zc + 0.1}, {p1[0], p1[1] + 0.012, zc + 0.1}}, accent, false, vec3{(p1[0] + p2[0]) / 2, 0, zc})
		}
	}
	m.box(1.9, 2.32, 1.12, 1.18, -0.9, 0.9, accent)
	m.box(1.98, 2.06, 0.88, 1.12, -0.62, -0.52, colDark)
	m.box(1.98, 2.06, 0.88, 1.12, 0.52, 0.62, colDark)
	for _, z := range []float64{-0.62, 0.62} {
		m.add([]vec3{{-2.34, 0.5, z - 0.22}, {-2.34, 0.5, z + 0.22}, {-2.22, 0.6, z + 0.22}, {-2.22, 0.6, z - 0.22}}, colHead, true, bodyC)
		m.add([]vec3{{2.27, 0.62, z - 0.26}, {2.27, 0.62, z + 0.26}, {2.27, 0.76, z + 0.26}, {2.27, 0.76, z - 0.26}}, colTail, true, bodyC)
	}
	for _, x := range []float64{-1.45, 1.45} {
		m.wheel(x, 0.42, -0.97, 0.42, 0.28, -1, colTire, accent)
		m.wheel(x, 0.42, 0.97, 0.42, 0.28, 1, colTire, accent)
	}
	return m
}

// Tuner sedan: compact four-door with a taller cabin, side graphic and a lip spoiler.
func sedanMesh(body, accent vec3) mesh {
	var m mesh
	prof := [][2]float64{{-2.05, 0.3}, {-2.1, 0.6}, {-1.85, 0.78}, {-0.75, 0.86}, {-0.15, 1.38}, {1.05, 1.38}, {1.6, 0.92}, {2.02, 0.9}, {2.08, 0.62}, {2.02, 0.3}}
	m.extrude(prof, -0.88, 0.88, body, map[int]vec3{3: colGlass, 5: colGlass})
	bodyC := vec3{0, 0.75, 0}
	for _, z := range []float64{-0.89, 0.89} {
		// two side windows with a pillar between them
		m.add([]vec3{{-0.6, 0.9, z}, {-0.12, 1.32, z}, {0.42, 1.32, z}, {0.42, 0.9, z}}, colGlass, false, bodyC)
		m.add([]vec3{{0.52, 0.9, z}, {0.52, 1.32, z}, {1.0, 1.32, z}, {1.45, 0.92, z}}, colGlass, false, bodyC)
		// side graphic: a swoosh along the doors
		m.add([]vec3{{-1.9, 0.5, z}, {1.9, 0.62, z}, {1.9, 0.7, z}, {-1.9, 0.6, z}}, accent, false, bodyC)
	}
	m.box(1.88, 2.1, 0.92, 0.98, -0.8, 0.8, accent)
	for _, z := range []float64{-0.58, 0.58} {
		m.add([]vec3{{-2.09, 0.55, z - 0.22}, {-2.09, 0.55, z + 0.22}, {-1.98, 0.68, z + 0.22}, {-1.98, 0.68, z - 0.22}}, colHead, true, bodyC)
		m.add([]vec3{{2.07, 0.66, z - 0.25}, {2.07, 0.66, z + 0.25}, {2.07, 0.8, z + 0.25}, {2.07, 0.8, z - 0.25}}, colTail, true, bodyC)
	}
	for _, x := range []float64{-1.3, 1.3} {
		m.wheel(x, 0.4, -0.9, 0.4, 0.26, -1, colTire, hexVec("#C8CCD4"))
		m.wheel(x, 0.4, 0.9, 0.4, 0.26, 1, colTire, hexVec("#C8CCD4"))
	}
	return m
}

// Superbike: full fairing, tank, tail, twin-spar frame hints.
func bikeMesh(body, accent vec3) mesh {
	var m mesh
	// fairing + tank + tail as one slim extruded side profile
	prof := [][2]float64{{-1.45, 0.55}, {-1.55, 0.85}, {-1.2, 1.25}, {-0.75, 1.42}, {-0.35, 1.35}, {0.25, 1.38}, {0.75, 1.2}, {1.45, 1.42}, {1.55, 1.35}, {0.9, 0.95}, {0.2, 0.62}, {-0.6, 0.5}}
	m.extrude(prof, -0.24, 0.24, body, map[int]vec3{2: colGlass, 6: colDark})
	c := vec3{0, 1.0, 0}
	// accent racing number panel and stripe
	for _, z := range []float64{-0.25, 0.25} {
		m.add([]vec3{{-1.3, 0.8, z}, {-0.7, 0.75, z}, {-0.75, 1.1, z}, {-1.25, 1.15, z}}, accent, false, c)
		m.add([]vec3{{0.95, 1.18, z}, {1.45, 1.36, z}, {1.42, 1.28, z}, {0.92, 1.12, z}}, accent, false, c)
	}
	// seat
	m.box(0.25, 0.85, 1.36, 1.44, -0.16, 0.16, colDark)
	// headlight
	m.add([]vec3{{-1.56, 0.92, -0.12}, {-1.56, 0.92, 0.12}, {-1.5, 1.08, 0.12}, {-1.5, 1.08, -0.12}}, colHead, true, c)
	m.add([]vec3{{1.56, 1.32, -0.1}, {1.56, 1.32, 0.1}, {1.56, 1.38, 0.1}, {1.56, 1.38, -0.1}}, colTail, true, c)
	// forks, swingarm, exhaust
	m.box(-1.32, -1.22, 0.38, 1.25, -0.2, -0.14, hexVec("#C8B060"))
	m.box(-1.32, -1.22, 0.38, 1.25, 0.14, 0.2, hexVec("#C8B060"))
	m.box(0.2, 1.3, 0.4, 0.5, -0.18, -0.1, colDark)
	m.box(0.6, 1.25, 0.62, 0.78, 0.2, 0.32, hexVec("#9AA0A8"))
	// wheels (thin)
	m.wheel(-1.3, 0.42, 0.07, 0.42, 0.14, 1, colTire, accent)
	m.wheel(1.25, 0.42, 0.08, 0.44, 0.16, 1, colTire, accent)
	return m
}

// ---- rasterizing ----------------------------------------------------------------

type canvas struct {
	w   int
	pix []vec3
}

func newCanvas(w int) *canvas { return &canvas{w: w, pix: make([]vec3, w*w)} }

// fillPoly fills a polygon (even-odd) with scanlines.
func (c *canvas) fillPoly(pts [][2]float64, col vec3, alpha float64) {
	if len(pts) < 3 {
		return
	}
	minY, maxY := pts[0][1], pts[0][1]
	for _, p := range pts {
		minY = math.Min(minY, p[1])
		maxY = math.Max(maxY, p[1])
	}
	y0 := max(0, int(math.Ceil(minY-0.5)))
	y1 := min(c.w-1, int(math.Floor(maxY-0.5)))
	xs := make([]float64, 0, 8)
	for y := y0; y <= y1; y++ {
		sy := float64(y) + 0.5
		xs = xs[:0]
		for i := range pts {
			a, b := pts[i], pts[(i+1)%len(pts)]
			if (a[1] <= sy) != (b[1] <= sy) {
				xs = append(xs, a[0]+(sy-a[1])*(b[0]-a[0])/(b[1]-a[1]))
			}
		}
		sort.Float64s(xs)
		for i := 0; i+1 < len(xs); i += 2 {
			xa := max(0, int(math.Ceil(xs[i]-0.5)))
			xb := min(c.w-1, int(math.Floor(xs[i+1]-0.5)))
			row := y * c.w
			for x := xa; x <= xb; x++ {
				p := &c.pix[row+x]
				*p = p.mul(1 - alpha).add(col.mul(alpha))
			}
		}
	}
}

func (c *canvas) line(x0, y0, x1, y1, width float64, col vec3, alpha float64) {
	dx, dy := x1-x0, y1-y0
	l := math.Hypot(dx, dy)
	if l == 0 {
		return
	}
	nx, ny := -dy/l*width/2, dx/l*width/2
	c.fillPoly([][2]float64{{x0 + nx, y0 + ny}, {x1 + nx, y1 + ny}, {x1 - nx, y1 - ny}, {x0 - nx, y0 - ny}}, col, alpha)
}

// background: dark gradient sky, glowing floor, scrolling neon grid.
func (c *canvas) backdrop(glow vec3, phase float64) {
	w := float64(c.w)
	horizon := 0.56 * w
	for y := 0; y < c.w; y++ {
		t := float64(y) / w
		base := vec3{8 + 10*t, 8 + 8*t, 16 + 18*t}
		for x := 0; x < c.w; x++ {
			p := base
			if float64(y) > horizon {
				// soft glow pool under the vehicle
				dx := (float64(x) - w/2) / (0.34 * w)
				dy := (float64(y) - 0.73*w) / (0.11 * w)
				g := math.Exp(-(dx*dx + dy*dy))
				p = p.add(glow.mul(0.42 * g))
			}
			c.pix[y*c.w+x] = p
		}
	}
	gc := glow.mul(0.6)
	for i := -12; i <= 12; i++ {
		c.line(w/2+float64(i)*w*0.06, horizon, w/2+float64(i)*w*0.45, w, screenSS, gc, 0.9)
	}
	for k := 0; k < 10; k++ {
		t := (float64(k) + phase) / 10
		y := horizon + t*t*(w-horizon)
		c.line(0, y, w, y, screenSS, gc, 0.9)
	}
}

func (c *canvas) drawMesh(m mesh, ang float64) {
	w := float64(c.w)
	ca, sa := math.Cos(ang), math.Sin(ang)
	tilt := 18 * math.Pi / 180
	ct, st := math.Cos(tilt), math.Sin(tilt)
	pivot := vec3{0, 0.6, 0}
	xf := func(p vec3) vec3 {
		p = p.sub(pivot)
		x, y, z := ca*p[0]+sa*p[2], p[1], -sa*p[0]+ca*p[2]
		return vec3{x, ct*y - st*z, st*y + ct*z}
	}
	cam := vec3{0, 0.45, 11}
	light := vec3{-0.4, 0.8, 0.6}
	light = light.mul(1 / light.norm())
	f := 1.9 * w
	type poly struct {
		depth float64
		pts   [][2]float64
		col   vec3
	}
	var polys []poly
	for _, fc := range m.faces {
		q := make([]vec3, len(fc.pts))
		for i, p := range fc.pts {
			q[i] = xf(p)
		}
		n := q[1].sub(q[0]).cross(q[2].sub(q[0]))
		nn := n.norm()
		if nn == 0 {
			continue
		}
		n = n.mul(1 / nn)
		mid := centroid(q)
		if n.dot(mid.sub(xf(fc.center))) < 0 {
			n = n.mul(-1)
		}
		if n.dot(cam.sub(mid)) <= 0 {
			continue // facing away
		}
		shade := 1.0
		if !fc.emit {
			shade = 0.35 + 0.65*math.Max(0, n.dot(light))
		}
		pts := make([][2]float64, len(q))
		for i, v := range q {
			d := cam[2] - v[2]
			pts[i] = [2]float64{w/2 + f*v[0]/d, 0.47*w - f*(v[1]-cam[1])/d}
		}
		polys = append(polys, poly{mid[2], pts, fc.col.mul(shade)})
	}
	sort.Slice(polys, func(i, j int) bool { return polys[i].depth < polys[j].depth })
	for _, p := range polys {
		c.fillPoly(p.pts, p.col, 1)
	}
}

// downsample averages screenSS x screenSS blocks into the final image.
func (c *canvas) downsample() *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, screenSize, screenSize))
	n := float64(screenSS * screenSS)
	for y := 0; y < screenSize; y++ {
		for x := 0; x < screenSize; x++ {
			var s vec3
			for dy := 0; dy < screenSS; dy++ {
				for dx := 0; dx < screenSS; dx++ {
					s = s.add(c.pix[(y*screenSS+dy)*c.w+x*screenSS+dx])
				}
			}
			s = s.mul(1 / n)
			cl := func(v float64) uint8 { return uint8(math.Max(0, math.Min(255, math.Round(v)))) }
			out.SetRGBA(x, y, color.RGBA{cl(s[0]), cl(s[1]), cl(s[2]), 255})
		}
	}
	return out
}

// RenderScreenFrames draws one full turn of the chosen vehicle.
func RenderScreenFrames(sc ScreenSettings) ([]*image.RGBA, error) {
	if err := sc.normalize(); err != nil {
		return nil, err
	}
	body, accent, glow := hexVec(sc.Body), hexVec(sc.Accent), hexVec(sc.Glow)
	var m mesh
	switch sc.Model {
	case "sedan":
		m = sedanMesh(body, accent)
	case "bike":
		m = bikeMesh(body, accent)
		m.scale(1.5) // a bike is much smaller than a car: fill the screen
	default:
		m = coupeMesh(body, accent)
	}
	frames := make([]*image.RGBA, screenFrames)
	if sc.Model == "flag" {
		var stripes []vec3
		for _, h := range screenFlags[sc.Flag] {
			stripes = append(stripes, hexVec(h))
		}
		for i := range frames {
			c := newCanvas(screenSize * screenSS)
			c.flagScene(stripes, 2*math.Pi*float64(i)/screenFrames, i)
			frames[i] = c.downsample()
		}
		return frames, nil
	}
	for i := range frames {
		c := newCanvas(screenSize * screenSS)
		phase := math.Mod(float64(i)*3/float64(screenFrames), 1)
		c.backdrop(glow, phase)
		if sc.Model == "sedan" {
			// neon underglow
			w := float64(c.w)
			for k := 0; k < 3; k++ {
				r := 0.3 + 0.06*float64(k)
				c.fillPoly(ellipse(w/2, 0.7*w, r*w, 0.06*w), glow, 0.22)
			}
		}
		c.drawMesh(m, 2*math.Pi*float64(i)/screenFrames+0.6)
		frames[i] = c.downsample()
	}
	return frames, nil
}

// flagScene draws a flag waving from a pole against a night sky with a
// few twinkling stars. phase loops over 2*pi so the animation repeats cleanly.
func (c *canvas) flagScene(stripes []vec3, phase float64, frame int) {
	w := float64(c.w)
	for y := 0; y < c.w; y++ {
		t := float64(y) / w
		col := vec3{10 + 14*t, 8 + 10*t, 22 + 26*t}
		for x := 0; x < c.w; x++ {
			c.pix[y*c.w+x] = col
		}
	}
	// stars: fixed positions, each twinkling on its own beat
	for i := uint32(0); i < 22; i++ {
		sx, sy := rand01(i, 11)*w, rand01(i, 12)*w
		tw := 0.35 + 0.65*(0.5+0.5*math.Sin(phase*float64(1+i%3)+6.28*rand01(i, 13)))
		r := float64(screenSS) * (0.5 + rand01(i, 14))
		c.fillPoly(ellipse(sx, sy, r, r), vec3{255, 255, 255}, 0.55*tw)
	}
	// pole
	poleX := 0.13 * w
	c.fillPoly([][2]float64{{poleX - 1.2*screenSS, 0.12 * w}, {poleX + 1.2*screenSS, 0.12 * w}, {poleX + 1.2*screenSS, 0.95 * w}, {poleX - 1.2*screenSS, 0.95 * w}}, vec3{190, 196, 206}, 1)
	c.fillPoly(ellipse(poleX, 0.12*w, 2.6*screenSS, 2.6*screenSS), vec3{230, 200, 90}, 1)
	// flag: one vertical slice per pixel column, displaced by a travelling wave
	// that grows away from the pole; slope gives the light and shadow.
	left, width := poleX+1.2*screenSS, 0.78*w
	top, height := 0.17*w, 0.5*w
	n := len(stripes)
	for px := 0; px < int(width); px++ {
		u := float64(px) / width
		amp := 0.055 * w * (0.15 + 0.85*u)
		k := 2 * math.Pi * 1.35
		dy := amp * math.Sin(k*u-phase)
		slope := math.Cos(k*u - phase)
		shade := 0.86 + 0.2*slope
		x := left + float64(px)
		y0 := top + dy
		for i, sc := range stripes {
			a := y0 + height*float64(i)/float64(n)
			b := y0 + height*float64(i+1)/float64(n)
			col := sc.mul(shade)
			c.fillPoly([][2]float64{{x, a}, {x + 1, a}, {x + 1, b + 0.5}, {x, b + 0.5}}, col, 1)
		}
	}
	_ = frame
}

func ellipse(cx, cy, rx, ry float64) [][2]float64 {
	pts := make([][2]float64, 32)
	for i := range pts {
		a := 2 * math.Pi * float64(i) / 32
		pts[i] = [2]float64{cx + rx*math.Cos(a), cy + ry*math.Sin(a)}
	}
	return pts
}

// rgb565 converts a frame to the screen's little-endian RGB565 layout.
func rgb565(img *image.RGBA) []byte {
	out := make([]byte, 0, screenSize*screenSize*2)
	for y := 0; y < screenSize; y++ {
		for x := 0; x < screenSize; x++ {
			c := img.RGBAAt(x, y)
			v := uint16(c.R&0xF8)<<8 | uint16(c.G&0xFC)<<3 | uint16(c.B>>3)
			out = append(out, byte(v), byte(v>>8))
		}
	}
	return out
}

// ScreenPayload is the full upload: a 256-byte header (frame count, then one
// duration byte per frame in 5 ms ticks) followed by the frames, padded to
// whole 4096-byte chunks.
func ScreenPayload(sc ScreenSettings) ([]byte, error) {
	frames, err := RenderScreenFrames(sc)
	if err != nil {
		return nil, err
	}
	return buildScreenPayload(frames, screenTickMS), nil
}

func buildScreenPayload(frames []*image.RGBA, frameMS int) []byte {
	hdr := make([]byte, 256)
	hdr[0] = byte(len(frames))
	for i := range frames {
		if 1+i < 256 {
			hdr[1+i] = byte(min(255, max(16, frameMS/5)))
		}
	}
	p := append([]byte{}, hdr...)
	for _, f := range frames {
		p = append(p, rgb565(f)...)
	}
	if r := len(p) % 4096; r != 0 {
		p = append(p, make([]byte, 4096-r)...)
	}
	return p
}

// ScreenPreviewPNG returns all frames side by side, for the panel's preview.
func ScreenPreviewPNG(sc ScreenSettings) ([]byte, error) {
	frames, err := RenderScreenFrames(sc)
	if err != nil {
		return nil, err
	}
	strip := image.NewRGBA(image.Rect(0, 0, screenSize*len(frames), screenSize))
	for i, f := range frames {
		for y := 0; y < screenSize; y++ {
			copy(strip.Pix[y*strip.Stride+i*screenSize*4:], f.Pix[y*f.Stride:y*f.Stride+screenSize*4])
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, strip); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
