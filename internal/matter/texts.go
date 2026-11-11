package matter

import "github.com/miraassist/mira/internal/i18n"

// texts are the tools' descriptions and results (facts for the model, which
// words them itself) and the few sentences Mira says herself at the
// satellite.
type texts struct {
	startDesc, codeDesc, argCode, plan               string
	onlyAtSatellite, noMatter, noMatterEntry, noWifi string
	readCode, scanAgain, codeRefused, noThread       string
	wifiFailed, commissionFailed, plugItIn           string
	nothingFound, foundFixed, note                   string
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		startDesc: "Startet das Einbinden eines neuen Matter-Geräts (WLAN). Nur, wenn der Nutzer es will. Mira liest den QR-Code mit der Kamera des Satelliten oder bittet darum, die elf Ziffern daneben vorzulesen. Sobald das Gerät da ist, meldet sie sich am Satelliten von selbst.",
		codeDesc:  "Gibt den Setup-Code eines Matter-Geräts weiter, den der Nutzer vorgelesen hat: die elf Ziffern neben dem QR-Code, als Ziffern, oder den QR-Inhalt (beginnt mit MT:).",
		argCode:   "Der Setup-Code: elf Ziffern (Zahlwörter vorher in Ziffern umschreiben) oder der QR-Inhalt, der mit MT: beginnt.",
		plan:      "# Neues Matter-Gerät\n- [ ] Gerät anmelden (Code, dann einstecken)\n- [ ] Raum\n- [ ] Name",

		onlyAtSatellite:  "Ergebnis: kein Einbinden. Grund: Das geht nur an einem Satelliten, weil Mira sich dort meldet, sobald das Gerät da ist.",
		noMatter:         "Ergebnis: kein Einbinden. Grund: Home Assistant konnte nicht nach Matter gefragt werden: %s. Sag das dem Nutzer.",
		noMatterEntry:    "Ergebnis: kein Einbinden. Grund: In Home Assistant ist Matter nicht eingerichtet. Sag das dem Nutzer.",
		noWifi:           "Ergebnis: kein Einbinden. Grund: Mira fehlen Name und Passwort des WLANs (Einstellungen wlan_ssid und wlan_password beim Skill). Sag das dem Nutzer.",
		readCode:         "Ergebnis: Dieser Satellit hat keine Kamera. Bitte den Nutzer, die elf Ziffern neben dem QR-Code vorzulesen, und ruf danach matter_code damit auf.",
		scanAgain:        "Ergebnis: Ich habe keinen Code gelesen. Bitte den Nutzer, den Code ruhiger und näher vor die Kamera zu halten, und ruf matter_pairing_start noch einmal auf. Klappt es wieder nicht, bitte ihn, die elf Ziffern vorzulesen.",
		codeRefused:      "Ergebnis: Der Code passt nicht (es sind nicht elf Ziffern, oder die Prüfziffer stimmt nicht). Bitte den Nutzer, ihn noch einmal vorzulesen.",
		noThread:         "Ergebnis: kein Code übernommen. Grund: Es läuft gerade kein Einbinden. Ruf zuerst matter_pairing_start auf.",
		wifiFailed:       "Ergebnis: kein Einbinden. Grund: Das WLAN konnte nicht an Matter übergeben werden: %s. Sag das dem Nutzer.",
		commissionFailed: "Ergebnis: Das Einbinden hat nicht geklappt: %s. Sag das dem Nutzer und frag, ob er es noch einmal versuchen will.",
		plugItIn:         "Ergebnis: Der Code passt, ich suche das Gerät jetzt drei Minuten lang. Sag dem Nutzer jetzt etwa so: „Steck das Gerät jetzt ein. Ich melde mich, sobald ich es habe.“ Frag nichts weiter.",

		nothingFound: "Ich habe das Gerät nicht gefunden. Setz es noch einmal zurück: Halt den Knopf gedrückt, bis es blinkt. Sag mir Bescheid, dann versuche ich es wieder.",
		foundFixed:   "Ich habe ein neues Gerät eingebunden: %s. In welchen Raum kommt es?",
		note:         "Neues Matter-Gerät: %s.",
	},
	i18n.EN: {
		startDesc: "Starts adding a new Matter device (Wi-Fi). Only when the user wants it. Mira reads the QR code with the satellite's camera, or asks for the eleven digits next to it to be read out. As soon as the device is there, she speaks up at the satellite by herself.",
		codeDesc:  "Passes on the setup code of a Matter device the user read out: the eleven digits next to the QR code, as digits, or the QR code's content (it begins with MT:).",
		argCode:   "The setup code: eleven digits (write number words as digits first) or the QR content beginning with MT:.",
		plan:      "# New Matter device\n- [ ] Device joins (the code, then plug it in)\n- [ ] Room\n- [ ] Name",

		onlyAtSatellite:  "Result: nothing added. Reason: this works only at a satellite, where Mira speaks up once the device is there.",
		noMatter:         "Result: nothing added. Reason: Home Assistant could not be asked about Matter: %s. Tell the user.",
		noMatterEntry:    "Result: nothing added. Reason: Home Assistant has no Matter set up. Tell the user.",
		noWifi:           "Result: nothing added. Reason: Mira has no name and password of the Wi-Fi (the skill's settings wlan_ssid and wlan_password). Tell the user.",
		readCode:         "Result: this satellite has no camera. Ask the user to read out the eleven digits next to the QR code, then call matter_code with them.",
		scanAgain:        "Result: I read no code. Ask the user to hold it steadier and closer to the camera, then call matter_pairing_start again. If that fails too, ask for the eleven digits.",
		codeRefused:      "Result: the code does not fit (not eleven digits, or the check digit is wrong). Ask the user to read it out again.",
		noThread:         "Result: no code taken. Reason: no adding is running. Call matter_pairing_start first.",
		wifiFailed:       "Result: nothing added. Reason: the Wi-Fi could not be given to Matter: %s. Tell the user.",
		commissionFailed: "Result: adding the device failed: %s. Tell the user and ask whether to try again.",
		plugItIn:         "Result: the code fits, I am looking for the device for three minutes. Tell the user now roughly: \"Plug the device in now. I'll speak up as soon as I have it.\" Ask nothing else.",

		nothingFound: "I did not find the device. Reset it once more: hold its button until it blinks. Tell me when it does, then I'll try again.",
		foundFixed:   "I added a new device: %s. Which room does it go in?",
		note:         "New Matter device: %s.",
	},
}
