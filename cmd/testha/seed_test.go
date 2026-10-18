package main

import "testing"

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
