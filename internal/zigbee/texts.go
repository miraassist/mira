package zigbee

import "github.com/miraassist/mira/internal/i18n"

// texts are the tools' descriptions and results (facts for the model, which
// words them itself) and the few sentences Mira says herself.
type texts struct {
	startDesc, plan                                string
	noBridge, pairingFailed, pairingOpen, notVoice string
	// Said at the satellite by Mira herself.
	joining, nothingJoined, interviewFailed, known, found string
	// The facts of a device, for the thread and the question.
	facts string
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		startDesc:       "Startet das Einbinden eines neuen Zigbee-Geräts über Zigbee2MQTT: 3 Minuten lang kann sich ein Gerät anmelden. Nur, wenn der Nutzer es will. Sobald das Gerät da ist, meldet sich Mira am Satelliten von selbst.",
		plan:            "# Neues Zigbee-Gerät\n- [ ] Gerät anmelden (Knopf drücken)\n- [ ] Raum\n- [ ] Name",
		noBridge:        "Ergebnis: kein Einbinden. Grund: In Home Assistant gibt es kein Zigbee2MQTT. Sag das dem Nutzer.",
		pairingFailed:   "Ergebnis: kein Einbinden. Grund: Zigbee2MQTT hat abgelehnt: %s. Sag das dem Nutzer.",
		pairingOpen:     "Ergebnis: Das Einbinden läuft 3 Minuten. Sag dem Nutzer jetzt etwa so: „Das Einbinden läuft drei Minuten. Halt den Knopf am Gerät ein paar Sekunden gedrückt, bis es schnell blinkt, oder steck es neu ein. Ich melde mich, sobald ich es sehe.“ Frag nichts weiter.",
		notVoice:        "Ergebnis: kein Einbinden. Grund: Das geht nur an einem Satelliten, weil Mira sich dort meldet, sobald das Gerät da ist.",
		joining:         "Ich sehe ein Gerät, ich lerne es gerade kennen. Das kann bis zu zwei Minuten dauern. Drück ab und zu kurz den Knopf, dann geht's schneller.",
		nothingJoined:   "Ich habe kein neues Gerät gesehen. Soll ich es nochmal versuchen?",
		interviewFailed: "Da hat sich ein Gerät gemeldet, aber ich verstehe es nicht. Drück den Knopf am besten nochmal.",
		known:           "Das Gerät kenne ich schon: %s, im Raum %s.",
		found:           "Ich habe ein neues Gerät gefunden: %s %s. In welchen Raum kommt es?",
		facts:           "Neues Gerät: Hersteller %s, Modell %s, Beschreibung von Zigbee2MQTT (englisch): %s. Werte: %s.",
	},
	i18n.EN: {
		startDesc:       "Starts adding a new Zigbee device through Zigbee2MQTT: for 3 minutes a device can join. Only when the user wants it. As soon as the device is there, Mira speaks up at the satellite by herself.",
		plan:            "# New Zigbee device\n- [ ] Device joins (press the button)\n- [ ] Room\n- [ ] Name",
		noBridge:        "Result: nothing added. Reason: Home Assistant has no Zigbee2MQTT. Tell the user.",
		pairingFailed:   "Result: nothing added. Reason: Zigbee2MQTT refused: %s. Tell the user.",
		pairingOpen:     "Result: pairing is open for 3 minutes. Tell the user now roughly: \"Pairing is open for three minutes. Hold the button on the device for a few seconds until it blinks quickly, or plug it in again. I'll speak up as soon as I see it.\" Ask nothing else.",
		notVoice:        "Result: nothing added. Reason: this works only at a satellite, where Mira speaks up once the device is there.",
		joining:         "I see a device and I'm getting to know it. That can take up to two minutes. Press its button briefly now and then, that makes it quicker.",
		nothingJoined:   "I did not see a new device. Shall I try again?",
		interviewFailed: "A device came up, but I do not understand it. Best press its button again.",
		known:           "I know that device already: %s, in %s.",
		found:           "I found a new device: %s %s. Which room does it go in?",
		facts:           "New device: vendor %s, model %s, Zigbee2MQTT's description: %s. Values: %s.",
	},
}
