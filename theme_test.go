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

// Row stripes (custom liveries can use them): one stripe per key row,
// doubling the middle stripe when there are fewer stripes than rows.
func TestStripeRows(t *testing.T) {
	th := ThemeDef{Base: "#000000", Accent: "#000000", AccentKeys: "none", StripeRows: true,
		Stripes:  []string{"#0000FF", "#FF00FF", "#FFFFFF", "#FF00FF", "#0000FF"},
		RearLeft: "#000000", RearRight: "#000000", Lid: "#000000", Power: "#000000", MouseLogo: "#000000", MouseSide: "#000000"}
	r := th.Resolve(100)
	want := []string{"#0000FF", "#FF00FF", "#FFFFFF", "#FFFFFF", "#FF00FF", "#0000FF"}
	for i, row := range m18Rows {
		if r.Keys[row[3]] != mustHex(want[i]) {
			t.Errorf("row %d = %v want %s", i, r.Keys[row[3]], want[i])
		}
	}
}
