package main

import (
	"os"
	"regexp"
	"testing"
)

// Every element the panel's script looks up by id must exist in the page;
// a missing one throws and stops the whole panel from working.
func TestUIElementsExist(t *testing.T) {
	b, err := os.ReadFile("ui.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(b)
	ids := map[string]bool{}
	for _, m := range regexp.MustCompile(`id="([A-Za-z0-9_-]+)"`).FindAllStringSubmatch(page, -1) {
		ids[m[1]] = true
	}
	for _, m := range regexp.MustCompile(`\$\("#([A-Za-z0-9_-]+)"\)`).FindAllStringSubmatch(page, -1) {
		if !ids[m[1]] {
			t.Errorf("script uses #%s but the page has no element with that id", m[1])
		}
	}
}
