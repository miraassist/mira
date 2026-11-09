package zigbee

import "github.com/miraassist/mira/internal/i18n"

// texts are the tools' descriptions and results (facts for the model, which
// words them itself) and the few sentences Mira says herself.
type texts struct {
	startDesc, assignDesc, finishDesc, argRoom, argName string
	plan                                                string
	noBridge, pairingFailed, pairingOpen, notVoice      string
	noDevice, unknownRoom, assignFailed, assigned       string
	finishFailed, renameFailed, finished                string
	// Said at the satellite by Mira herself.
	nothingJoined, interviewFailed, known, found string
	// The facts of a device, for the thread and the question.
	facts string
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		startDesc:       "Startet das Einbinden eines neuen Zigbee-Geräts über Zigbee2MQTT: 3 Minuten lang kann sich ein Gerät anmelden. Nur, wenn der Nutzer es will. Sobald das Gerät da ist, meldet sich Mira am Satelliten von selbst.",
		assignDesc:      "Setzt das neu eingebundene Gerät in einen Raum.",
		finishDesc:      "Schließt das Einbinden ab: gibt die Werte des Geräts für Sprache frei und benennt es um. Erst, wenn der Name feststeht.",
		argRoom:         "Der Raum, wie der Nutzer ihn nennt, z. B. Wohnzimmer.",
		argName:         "Der Name des Geräts, z. B. Temperatursensor Wohnzimmer.",
		plan:            "# Neues Zigbee-Gerät\n- [ ] Gerät anmelden (Knopf drücken)\n- [ ] Raum\n- [ ] Name",
		noBridge:        "Ergebnis: kein Einbinden. Grund: In Home Assistant gibt es kein Zigbee2MQTT. Sag das dem Nutzer.",
		pairingFailed:   "Ergebnis: kein Einbinden. Grund: Zigbee2MQTT hat abgelehnt: %s. Sag das dem Nutzer.",
		pairingOpen:     "Ergebnis: Das Einbinden läuft 3 Minuten. Sag dem Nutzer jetzt etwa so: „Das Einbinden läuft drei Minuten. Halt den Knopf am Gerät ein paar Sekunden gedrückt, bis es schnell blinkt, oder steck es neu ein. Ich melde mich, sobald ich es sehe.“ Frag nichts weiter.",
		notVoice:        "Ergebnis: kein Einbinden. Grund: Das geht nur an einem Satelliten, weil Mira sich dort meldet, sobald das Gerät da ist.",
		noDevice:        "Ergebnis: nichts getan. Grund: Es ist noch kein neues Gerät da.",
		unknownRoom:     "Ergebnis: nichts getan. Grund: Einen Raum „%s“ gibt es nicht. Räume: %s. Frag nach.",
		assignFailed:    "Ergebnis: nichts getan. Grund: Home Assistant hat abgelehnt: %s. Sag das dem Nutzer.",
		assigned:        "Ergebnis: Das Gerät ist jetzt im Raum %s. Schlag jetzt einen Namen vor (was es ist und der Raum) und frag, ob er passt.",
		finishFailed:    "Ergebnis: nicht freigegeben, nicht umbenannt. Grund: Home Assistant hat abgelehnt: %s. Sag das dem Nutzer.",
		renameFailed:    "Ergebnis: %d Werte für Sprache freigegeben, aber nicht umbenannt. Grund: Zigbee2MQTT hat abgelehnt: %s. Der Raum bleibt. Sag das dem Nutzer.",
		finished:        "Ergebnis: fertig. Das Gerät heißt jetzt %s, ist im Raum %s, und %d Werte sind für Sprache freigegeben. Sag das kurz.",
		nothingJoined:   "Ich habe kein neues Gerät gesehen. Soll ich es nochmal versuchen?",
		interviewFailed: "Da hat sich ein Gerät gemeldet, aber ich verstehe es nicht. Drück den Knopf am besten nochmal.",
		known:           "Das Gerät kenne ich schon: %s, im Raum %s.",
		found:           "Ich habe ein neues Gerät gefunden: %s %s. In welchen Raum kommt es?",
		facts:           "Neues Gerät: Hersteller %s, Modell %s, Beschreibung von Zigbee2MQTT (englisch): %s. Werte: %s.",
	},
	i18n.EN: {
		startDesc:       "Starts adding a new Zigbee device through Zigbee2MQTT: for 3 minutes a device can join. Only when the user wants it. As soon as the device is there, Mira speaks up at the satellite by herself.",
		assignDesc:      "Puts the newly added device into a room.",
		finishDesc:      "Finishes adding the device: exposes its values to voice and renames it. Only once the name is settled.",
		argRoom:         "The room as the user names it, e.g. Living Room.",
		argName:         "The device's name, e.g. Temperature sensor living room.",
		plan:            "# New Zigbee device\n- [ ] Device joins (press the button)\n- [ ] Room\n- [ ] Name",
		noBridge:        "Result: nothing added. Reason: Home Assistant has no Zigbee2MQTT. Tell the user.",
		pairingFailed:   "Result: nothing added. Reason: Zigbee2MQTT refused: %s. Tell the user.",
		pairingOpen:     "Result: pairing is open for 3 minutes. Tell the user now roughly: \"Pairing is open for three minutes. Hold the button on the device for a few seconds until it blinks quickly, or plug it in again. I'll speak up as soon as I see it.\" Ask nothing else.",
		notVoice:        "Result: nothing added. Reason: this works only at a satellite, where Mira speaks up once the device is there.",
		noDevice:        "Result: nothing done. Reason: no new device is there yet.",
		unknownRoom:     "Result: nothing done. Reason: there is no room \"%s\". Rooms: %s. Ask.",
		assignFailed:    "Result: nothing done. Reason: Home Assistant refused: %s. Tell the user.",
		assigned:        "Result: the device is now in %s. Now propose a name (what it is and the room) and ask whether it fits.",
		finishFailed:    "Result: not exposed, not renamed. Reason: Home Assistant refused: %s. Tell the user.",
		renameFailed:    "Result: %d values exposed to voice, but not renamed. Reason: Zigbee2MQTT refused: %s. The room stays. Tell the user.",
		finished:        "Result: done. The device is now called %s, is in %s, and %d values are exposed to voice. Say so briefly.",
		nothingJoined:   "I did not see a new device. Shall I try again?",
		interviewFailed: "A device came up, but I do not understand it. Best press its button again.",
		known:           "I know that device already: %s, in %s.",
		found:           "I found a new device: %s %s. Which room does it go in?",
		facts:           "New device: vendor %s, model %s, Zigbee2MQTT's description: %s. Values: %s.",
	},
}
