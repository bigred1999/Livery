package main

// Alienware m18 R2 per-key keyboard (Darfon 0D62:AAB1), light ids from the
// alienfx-tools device map, arranged in physical left-to-right order per row.
var m18Rows = [][]byte{
	{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19},                     // Esc F1..F12 Home End Del Mute Vol- Vol+ Mic
	{20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 34, 35, 36, 37, 38, 39},               // ` 1..0 - = Backspace NumLk / * -
	{40, 41, 42, 43, 44, 45, 46, 47, 48, 49, 50, 51, 52, 53, 54, 55, 56, 57, 58, 59},           // Tab Q..P [ ] \ Num7 8 9
	{60, 61, 62, 63, 64, 65, 66, 67, 68, 69, 70, 71, 72, 74, 76, 77, 78, 79},                   // Caps A..L ; ' Enter Num4 5 6 +
	{80, 81, 83, 84, 85, 86, 87, 88, 89, 90, 91, 92, 93, 94, 96, 97, 98, 99},                   // Shift Z..M , . / RShift Num1 2 3 Enter
	{100, 101, 102, 103, 104, 106, 107, 109, 111, 112, 133, 134, 114, 135, 116, 117, 118, 119}, // Ctrl Fn Win Alt Space RAlt RCtrl Left Down Up Right Num0 . Enter
}

// m18KeyX maps each key id to its horizontal position, 0 (left) .. 1 (right).
var m18KeyX = func() map[byte]float64 {
	m := map[byte]float64{}
	for _, row := range m18Rows {
		for i, id := range row {
			m[id] = float64(i) / float64(len(row)-1)
		}
	}
	return m
}()

// m18KeyY maps each key id to the middle of its row as a fraction of the
// keyboard's height (top row ~0.08 .. bottom row ~0.92), so flag stripes
// split evenly across the rows.
var m18KeyY = func() map[byte]float64 {
	m := map[byte]float64{}
	for r, row := range m18Rows {
		for _, id := range row {
			m[id] = (float64(r) + 0.5) / float64(len(m18Rows))
		}
	}
	return m
}()

var accentKeySets = map[string][]byte{
	"none":         nil,
	"wasd-arrows":  {43, 62, 63, 64, 114, 133, 134, 135},
	"wasd":         {43, 62, 63, 64},
	"arrows":       {114, 133, 134, 135},
	"function-row": {1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12},
}

// Alienware m18 R2 AW-ELC (187C:0551) lights.
var (
	elcRearLeft  byte = 0 // "Exhaust Lights 1"
	elcRearRight byte = 1 // "Exhaust Lights 2"
	elcLid       byte = 2
	elcPower          = []byte{4} // m18: power button is light 4 (there is no light 3)
)
