package tools

import "testing"

func TestAffirmative(t *testing.T) {
	yes := []string{"Ja", "ja bitte", "Ja, mach das.", "ok", "Okay!", "jo", "mach", "Bestätigt", "yes do it", "Klar, schließ ab"}
	no := []string{"", "Nein", "nein danke", "ja nicht", "Ist die Tür zu?", "ja?", "Mach lieber nicht",
		"Wie spät ist es", "Lass das", "jein", "ja aber erst wenn ich weg bin und die Katze drin ist ok",
		"stopp"}
	for _, s := range yes {
		if !Affirmative(s) {
			t.Errorf("Affirmative(%q) = false", s)
		}
	}
	for _, s := range no {
		if Affirmative(s) {
			t.Errorf("Affirmative(%q) = true", s)
		}
	}
}
