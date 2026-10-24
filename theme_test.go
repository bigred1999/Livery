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

// Flag liveries put one stripe per key row, doubling the middle stripe when
// the flag has fewer stripes than the keyboard has rows.
func TestFlagRows(t *testing.T) {
	th, _ := findTheme("trans-pride", nil)
	r := th.Resolve(100)
	var got []RGB
	for _, row := range m18Rows {
		got = append(got, r.Keys[row[3]])
	}
	want := []string{hexTransBlue, hexTransPink, hexWarmWhite, hexWarmWhite, hexTransPink, hexTransBlue}
	for i := range want {
		if got[i] != mustHex(want[i]) {
			t.Errorf("row %d = %v want %s", i, got[i], want[i])
		}
	}
	th, _ = findTheme("rainbow-pride", nil)
	r = th.Resolve(100)
	for i, row := range m18Rows {
		if r.Keys[row[0]] != mustHex(th.Stripes[i]) {
			t.Errorf("rainbow row %d", i)
		}
	}
}
