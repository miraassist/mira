package conversation

import "testing"

func TestExpectsReply(t *testing.T) {
	yes := []string{
		"Welche Lampe meinst du?",
		"Bitte gib an, was du einstellen möchtest und in welchem Raum.",
		"Das Licht ist an. Soll ich auch die Heizung einschalten.",
		"Sag mir bitte, in welchem Raum.",
		"In welchem Raum soll es sein?",
		"Okay. Welche Temperatur hättest du gern.",
		"Bitte bestätige, dass ich die Haustür abschließen soll.",
	}
	no := []string{
		"Das Küchenlicht ist an.",
		"Es sind einundzwanzig Grad.",
		"Ich weiß nicht, wo die Waschmaschine steht.",
		"Erledigt!",
		"",
		"Was für ein schöner Tag. Das Licht ist an.",
		"Wie gewünscht ist das Licht an.",
		"Erledigt. Wie besprochen läuft die Heizung jetzt.",
	}
	for _, s := range yes {
		if !expectsReply(s) {
			t.Errorf("expectsReply(%q) = false", s)
		}
	}
	for _, s := range no {
		if expectsReply(s) {
			t.Errorf("expectsReply(%q) = true", s)
		}
	}
}
