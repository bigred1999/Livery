package main

import (
	"regexp"
	"strconv"
	"strings"
)

// newerVersion reports whether a ("1.10.0") is newer than b ("1.9.2").
func newerVersion(a, b string) bool {
	pa, pb := strings.Split(strings.TrimPrefix(a, "v"), "."), strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < 3; i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x > y
		}
	}
	return false
}

var shaLine = regexp.MustCompile("(?i)\\b([0-9a-f]{64})\\b`?\\s+\\*?(Livery-Setup-[0-9.]+\\.exe)")
