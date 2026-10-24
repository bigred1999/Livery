package main

import "testing"

func TestBuiltinThemes(t *testing.T) {
	seen := map[string]bool{}
	for _, th := range BuiltinThemes {
		if seen[th.ID] {
			t.Errorf("duplicate id %s", th.ID)
		}
		seen[th.ID] = true
		if th.Category == "" || th.Name == "" || th.Description == "" {
			t.Errorf("%s: missing name/category/description", th.ID)
		}
		for _, c := range append([]string{th.Base, th.Accent, th.RearLeft, th.RearRight, th.Lid, th.Power, th.MouseLogo, th.MouseSide}, th.Stripes...) {
			if _, err := ParseHex(c); err != nil {
				t.Errorf("%s: bad color %q", th.ID, c)
			}
		}
		if _, ok := accentKeySets[th.AccentKeys]; !ok {
			t.Errorf("%s: bad accent set %q", th.ID, th.AccentKeys)
		}
		th.Resolve(100)
	}
}
