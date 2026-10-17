package timeline

import (
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/i18n"
)

func TestTextsComplete(t *testing.T) {
	if m := i18n.Missing(catalog); len(m) > 0 {
		t.Errorf("missing texts: %v", m)
	}
}

// Grouped by day with today and tomorrow named, times short, the place cut,
// details only on request and without links, labels instead of ids, and a
// limit that counts the rest.
func TestRender(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	at := func(d, h, m int) time.Time { return time.Date(2026, 10, d, h, m, 0, 0, berlin) }
	entries := []Entry{
		{Start: at(20, 10, 0), End: at(20, 10, 30), Title: "Arzt Dr. Berger", Source: "Familie", Ref: "uid-1",
			Place:   "Praxis Dr. Berger, Hauptstraße 12, 76275 Ettlingen, 2. OG, Eingang über den Hof",
			Details: "Bitte Versichertenkarte mitbringen. Online: https://example.org/termin?id=1&token=abc"},
		{Start: at(17, 16, 0), End: at(17, 17, 30), Title: "Fußballtraining", Source: "Familie", Ref: "uid-2"},
		{Start: at(24, 0, 0), End: at(25, 0, 0), AllDay: true, Title: "Geburtstag Oma", Source: "Familie", Ref: "uid-3"},
	}
	n := 0
	o := Options{Lang: i18n.DE, Location: berlin, Now: at(17, 9, 0), Label: func(Entry) string { n++; return "t" + string(rune('0'+n)) }}
	out := Render(entries, o)
	for _, want := range []string{
		"heute, Samstag, 17. Oktober 2026\n- [t1] 16:00–17:30 Fußballtraining (Familie)",
		"Dienstag, 20. Oktober 2026", // not today or tomorrow: plain
		"- [t2] 10:00–10:30 Arzt Dr. Berger · Praxis Dr. Berger, Hauptstraße 12 … (Familie)",
		"- [t3] ganztägig Geburtstag Oma",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "Versichertenkarte") || strings.Contains(out, "uid-") {
		t.Errorf("details or ids leaked:\n%s", out)
	}
	n = 0
	o.Details, o.Limit = true, 2
	out = Render(entries, o)
	if !strings.Contains(out, "Versichertenkarte mitbringen. Online: (Link)") || strings.Contains(out, "example.org") {
		t.Errorf("details:\n%s", out)
	}
	if !strings.Contains(out, "… und 1 weitere") || strings.Contains(out, "Oma") {
		t.Errorf("limit:\n%s", out)
	}
	if Render(nil, o) != "Keine Einträge." {
		t.Error("empty")
	}
}
