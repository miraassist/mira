package telegram

import (
	"strings"
	"testing"
)

func TestToHTML(t *testing.T) {
	cases := map[string]string{
		"# Titel":                       "<b>Titel</b>",
		"- eins\n- zwei":                "• eins\n• zwei",
		"a < b & c":                     "a &lt; b &amp; c",
		"*kursiv* und **fett**":         "<i>kursiv</i> und <b>fett</b>",
		"sensor.bad_temp_1 ist 2*3":     "sensor.bad_temp_1 ist 2*3",
		"[HA](https://example.org/a_b)": `<a href="https://example.org/a_b">HA</a>`,
		"```\nx <y>\n```":               "<pre>x &lt;y&gt;\n</pre>",
		"`a*b*c`":                       "<code>a*b*c</code>",
	}
	for in, want := range cases {
		if got := ToHTML(in); got != want {
			t.Errorf("ToHTML(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplit(t *testing.T) {
	para := strings.Repeat("Wort ", 300) // ~1500 chars
	text := para + "\n\n" + para + "\n\n" + para
	chunks := Split(text, 2000)
	if len(chunks) != 3 {
		t.Fatalf("chunks = %d", len(chunks))
	}
	for _, c := range chunks {
		if len(c) > 2000 {
			t.Errorf("chunk of %d chars", len(c))
		}
	}
	if got := Split(strings.Repeat("ä", 3000), 1001); strings.Join(got, "") != strings.Repeat("ä", 3000) {
		t.Error("splitting without spaces lost or broke runes")
	}
}
