package main

import (
	"testing"
	"time"
)

func TestEffects(t *testing.T) {
	red, white := RGB{255, 0, 0}, RGB{255, 255, 255}
	p := lightPoint{X: 0.3, Color: red, Seed: 7}
	base := effectParams{Speed: 0.5, Intensity: 0.6, Palette: []RGB{red, white}, AccentCol: white}
	for _, name := range effectNames {
		e := base
		e.Name = name
		lit, dark := 0, 0
		for ms := 0; ms < 20000; ms += 10 {
			c := EffectColor(p, time.Duration(ms)*time.Millisecond, e)
			if c == (RGB{}) {
				dark++
			} else {
				lit++
			}
		}
		if lit == 0 {
			t.Errorf("%s never lights", name)
		}
		if name == "strobe" && dark == 0 {
			t.Errorf("strobe never goes dark")
		}
	}
	// static is untouched
	if c := EffectColor(p, time.Second, effectParams{Name: "static"}); c != red {
		t.Error("static changed the color")
	}
	// flicker is mostly lit, with some dips
	dips := 0
	for ms := 0; ms < 60000; ms += 35 {
		if flickerLevel(time.Duration(ms)*time.Millisecond, 0.5, 0.6) < 0.8 {
			dips++
		}
	}
	if total := 60000 / 35; dips == 0 || dips > total/3 {
		t.Errorf("flicker dips %d of %d", dips, total)
	}
	// wave moves: the same light changes color over time
	e := base
	e.Name = "wave"
	if EffectColor(p, 0, e) == EffectColor(p, 700*time.Millisecond, e) {
		t.Error("wave doesn't move")
	}
	if !validEffect("flicker") || validEffect("disco") || softwareEffect("breathe") {
		t.Error("effect lists")
	}
}
