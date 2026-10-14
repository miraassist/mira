package conversation

import "testing"

func TestUnbacked(t *testing.T) {
	for text, want := range map[string]bool{
		"Ich prüfe die Temperatur.":                         true,
		"Ich ermittele die Leistung der Solaranlage.":       true,
		"Licht in der Küche ist eingeschaltet.":             true,
		"Ich habe gespeichert, dass du Milch kaufen musst.": true,
		"Es ist einundzwanzig Uhr einundzwanzig.":           false,
		"Mir geht es gut. Wie kann ich dir helfen?":         false,
		"Welches Licht meinst du?":                          false,
	} {
		if got := unbacked(text); got != want {
			t.Errorf("unbacked(%q) = %v", text, got)
		}
	}
}
