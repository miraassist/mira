package i18n

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	for in, want := range map[string]Lang{"": DE, "de": DE, "DE-at": DE, "en": EN, "en_GB": EN} {
		if got, err := Parse(in); err != nil || got != want {
			t.Errorf("Parse(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := Parse("fr"); err == nil {
		t.Error("fr accepted")
	}
}

func TestFormatTime(t *testing.T) {
	ts := time.Date(2026, time.October, 14, 22, 5, 0, 0, time.UTC)
	if got := FormatTime(DE, ts); got != "Mittwoch, 14. Oktober 2026, 22:05 Uhr" {
		t.Error(got)
	}
	if got := FormatTime(EN, ts); got != "Wednesday, 14 October 2026, 22:05" {
		t.Error(got)
	}
}

func TestMissing(t *testing.T) {
	type texts struct{ a, b string }
	got := Missing(map[Lang]texts{DE: {a: "x", b: "y"}, EN: {a: "x"}})
	if len(got) != 1 || got[0] != "en.b" {
		t.Errorf("Missing = %v", got)
	}
}
