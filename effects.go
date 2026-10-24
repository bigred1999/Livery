package main

// Software lighting effects. Every light is a point with a horizontal
// position (0 = left edge, 1 = right edge), its livery color and whether it's
// an accent light; an effect turns that plus the time into a color. Devices
// that can take a fast stream of updates (Alienware keyboard and chassis
// lights, OpenRGB devices) are animated with this; the rest use their own
// built-in effect or stay on the livery color.

import (
	"math"
	"time"
)

// Effect ids, in panel order.
var effectNames = []string{"static", "breathe", "flicker", "strobe", "wave", "redline", "sparkle"}

func validEffect(e string) bool {
	for _, n := range effectNames {
		if n == e {
			return true
		}
	}
	return false
}

// softwareEffect reports effects that need Livery to animate the lights.
func softwareEffect(e string) bool {
	switch e {
	case "flicker", "strobe", "wave", "redline", "sparkle":
		return true
	}
	return false
}

type lightPoint struct {
	X      float64 // 0..1 left to right
	Color  RGB     // livery color at this light (brightness already applied)
	Accent bool    // an accent light (WASD, accent column, ...)
	Seed   uint32  // per-light randomness
}

// effectParams are the panel's controls, normalised.
type effectParams struct {
	Name      string
	Speed     float64 // 0 (slowest) .. 1 (fastest)
	Intensity float64 // 0 (subtle) .. 1 (strong)
	Reverse   bool    // wave/redline run right to left
	Palette   []RGB   // colors the wave travels through (brightness applied)
	AccentCol RGB     // accent color (brightness applied)
}

// hash32 is a small integer hash (lowbias32) for repeatable randomness.
func hash32(x uint32) uint32 {
	x ^= x >> 16
	x *= 0x7feb352d
	x ^= x >> 15
	x *= 0x846ca68b
	x ^= x >> 16
	return x
}

func rand01(a, b uint32) float64 { return float64(hash32(a*0x9E3779B1^hash32(b))) / float64(1<<32) }

func lerpRGB(a, b RGB, f float64) RGB {
	f = math.Max(0, math.Min(1, f))
	l := func(x, y uint8) uint8 { return uint8(math.Round(float64(x) + (float64(y)-float64(x))*f)) }
	return RGB{l(a.R, b.R), l(a.G, b.G), l(a.B, b.B)}
}

// paletteAt samples the palette as a smooth loop (u in any range).
func paletteAt(p []RGB, u float64) RGB {
	if len(p) == 0 {
		return RGB{}
	}
	if len(p) == 1 {
		return p[0]
	}
	u = u - math.Floor(u)
	pos := u * float64(len(p))
	i := int(pos) % len(p)
	f := pos - math.Floor(pos)
	f = f * f * (3 - 2*f) // ease between colors
	return lerpRGB(p[i], p[(i+1)%len(p)], f)
}

// flickerLevel is the shared brightness of a failing neon tube at time t:
// mostly lit, with sudden dips and stutters. All devices use the same value,
// so the whole setup flickers together.
func flickerLevel(t time.Duration, speed, intensity float64) float64 {
	const slot = 35 * time.Millisecond
	n := uint32(t / slot)
	// Chance a slot starts a stutter grows with speed; depth with intensity.
	chance := 0.015 + 0.09*speed
	depth := 0.35 + 0.6*intensity
	for back := uint32(0); back < 4; back++ { // a stutter lasts up to 4 slots
		if n < back {
			break
		}
		s := n - back
		if rand01(s, 1) < chance {
			length := 1 + uint32(rand01(s, 2)*4)
			if back < length {
				// alternate dark/half-lit inside a stutter
				d := depth * (0.55 + 0.45*rand01(s+back, 3))
				if back%2 == 1 {
					d *= 0.4
				}
				return 1 - d
			}
		}
	}
	// a slight constant hum
	return 1 - 0.04*intensity*rand01(n, 4)
}

// EffectColor is the color of one light at time t.
func EffectColor(p lightPoint, t time.Duration, e effectParams) RGB {
	sec := t.Seconds()
	switch e.Name {
	case "breathe":
		period := 7.0 - 5.5*e.Speed
		low := 1 - 0.88*(0.35+0.65*e.Intensity)
		lvl := low + (1-low)*(0.5-0.5*math.Cos(2*math.Pi*sec/period))
		return p.Color.Scale(lvl)

	case "flicker":
		return p.Color.Scale(flickerLevel(t, e.Speed, e.Intensity))

	case "strobe":
		period := 1.2 - 1.05*e.Speed // 1.2 s .. 0.15 s
		on := 0.5 - 0.4*e.Intensity  // stronger = shorter, sharper flashes
		if math.Mod(sec, period)/period < on {
			return p.Color
		}
		return RGB{}

	case "wave":
		period := 6.0 - 5.0*e.Speed // seconds for the pattern to cross once
		x := p.X
		if e.Reverse {
			x = 1 - x
		}
		width := 1.6 - 1.2*e.Intensity // how much of the palette is on screen
		return paletteAt(e.Palette, x*width-sec/period)

	case "redline":
		// A rev counter: a bar of accent color sweeps across as revs rise,
		// the limiter flashes, then the revs drop and it starts again.
		period := 4.0 - 3.0*e.Speed
		ph := math.Mod(sec, period) / period
		x := p.X
		if e.Reverse {
			x = 1 - x
		}
		switch {
		case ph < 0.78: // revving up
			rpm := ph / 0.78
			rpm = rpm * rpm
			if x <= rpm {
				return lerpRGB(p.Color, e.AccentCol, 0.35+0.65*e.Intensity)
			}
			return p.Color.Scale(0.25 + 0.5*rpm)
		case ph < 0.92: // limiter: fast flashes
			if int(math.Mod(sec, period)*14)%2 == 0 {
				return e.AccentCol
			}
			return p.Color.Scale(0.15)
		default: // shift: revs drop
			return p.Color.Scale(0.25)
		}

	case "sparkle":
		const slot = 120 * time.Millisecond
		step := slot - time.Duration(float64(80*time.Millisecond)*e.Speed)
		n := uint32(t / step)
		f := float64(t%step) / float64(step)
		density := 0.01 + 0.07*e.Intensity
		base := p.Color.Scale(0.55)
		for back := uint32(0); back < 3; back++ { // a sparkle fades over 3 slots
			if n < back {
				break
			}
			if rand01(n-back, p.Seed) < density {
				fade := 1 - (float64(back)+f)/3
				return lerpRGB(base, e.AccentCol, fade*fade)
			}
		}
		return base
	}
	return p.Color
}
