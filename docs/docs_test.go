package docs

import (
	"regexp"
	"strconv"
	"strings"
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

// A recording in the guide is a line of its own ("▶ [caption](url)": a link on
// GitHub) that the web UI turns into a video player; the time marks must be a start before the end.
func TestGuideVideosAreWellFormed(t *testing.T) {
	line := regexp.MustCompile(`^▶ \[[^\]]*\]\(https://[^\s)]+\.(?:mp4|webm)(?:#t=([\d:.,]+))?\)$`)
	any := regexp.MustCompile(`\.(?:mp4|webm)`)
	for _, lang := range []string{"de", "en"} {
		raw, err := Guide(lang)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range strings.Split(string(raw), "\n") {
			if !any.MatchString(l) || !(strings.HasPrefix(l, "![") || strings.HasPrefix(l, "▶")) {
				continue
			}
			m := line.FindStringSubmatch(l)
			if m == nil {
				t.Errorf("%s: video line not in the form the UI reads: %s", lang, l)
				continue
			}
			if parts := strings.Split(m[1], ","); len(parts) == 2 {
				a, _ := strconv.Atoi(parts[0])
				b, _ := strconv.Atoi(parts[1])
				if strings.Contains(m[1], ":") || b <= a {
					t.Errorf("%s: time marks %q are not seconds with start < end: %s", lang, m[1], l)
				}
			}
		}
	}
}
