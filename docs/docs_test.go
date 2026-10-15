package docs

import (
	"regexp"
	"testing"
)

// The guides are one text in two languages: same sections, same tables.
func TestGuidesMatch(t *testing.T) {
	shape := regexp.MustCompile(`(?m)^(#{1,3} |\|---)`)
	de, err := Guide("de")
	if err != nil {
		t.Fatal(err)
	}
	en, err := Guide("en")
	if err != nil {
		t.Fatal(err)
	}
	if a, b := len(shape.FindAll(de, -1)), len(shape.FindAll(en, -1)); a != b || a < 10 {
		t.Errorf("de has %d headings/tables, en %d", a, b)
	}
	if fr, _ := Guide("fr"); string(fr) != string(de) {
		t.Error("unknown language does not fall back to German")
	}
}
