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
		// Seen live while a notice waited.
		"Nein, ich habe keine Benachrichtigungen.": true,
		"You have no new notifications.":           true,
		// Seen live: "Ja" to a proposal, answered without task_confirm.
		"Auftrag bestätigt. Ich überwache den Wasserverbrauch.": true,
		// Seen live without any tool call; the sensors said otherwise.
		"Im Wohnzimmer ist die Temperatur 21,3 Grad. Die Luftfeuchtigkeit beträgt 48 Prozent.": true,
		"Alle Lichter sind aus, und die Fenster sind geschlossen.":                             true,
		"Es sind 23 °C draußen.":                               true,
		"The living room is at 21.3 degrees.":                  true,
		"Der Restmüll kommt am 29. Oktober.":                   false,
		"Timer für 10 Minuten gestellt? Sag einfach Bescheid.": false,
		"Um 19 Uhr erinnere ich dich gern, wenn du möchtest.":  false,
	} {
		if got := unbacked(text); got != want {
			t.Errorf("unbacked(%q) = %v", text, got)
		}
	}
}
