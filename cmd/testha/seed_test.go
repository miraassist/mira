package main

import "testing"

// The .env file is read by the shell too, so values with spaces or
// characters like | must come back as they went in.
func TestEnvQuoting(t *testing.T) {
	for _, v := range []string{"plain-token_1.2:3,4", "light.x|Wohnzimmer|Farb lampe", "it's \"odd\" $HOME `x`", "", "a;b"} {
		if got := unquoteEnv(quoteEnv(v)); got != v {
			t.Errorf("%q -> %q -> %q", v, quoteEnv(v), got)
		}
	}
	if quoteEnv("abc") != "abc" {
		t.Error("a plain value must stay plain")
	}
}

func TestParseExtras(t *testing.T) {
	got := parseExtras(" light.lampe | Wohnzimmer | Farblampe ;switch.x|Büro; ;cover.y")
	want := []extra{{"light.lampe", "Wohnzimmer", "Farblampe"}, {"switch.x", "Büro", ""}, {"cover.y", "", ""}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: %+v, want %+v", i, got[i], want[i])
		}
	}
	if len(parseExtras("")) != 0 {
		t.Error("empty list must stay empty")
	}
}
