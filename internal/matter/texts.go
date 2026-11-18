package matter

import "github.com/miraassist/mira/internal/i18n"

// texts are the tools' descriptions and results (facts for the model, which
// words them itself) and the few sentences Mira says herself at the
// satellite.
type texts struct {
	startDesc, codeDesc, argCode, plan                  string
	onlyAtSatellite, noMatter, noMatterEntry, noNetwork string
	readCode, scanAgain, codeRefused, noThread          string
	wifiFailed, commissionFailed, plugItIn              string
	nothingFound, foundFixed, note                      string
	// Matter over Thread: the networks Mira gives, the errors of Home
	// Assistant's Thread networks, and what was missing (decision 27).
	threadListFailed, threadFailed                  string
	netsBoth, netsThread, netsWifi, netsNoPreferred string
	missingWifi, missingThread, missingPreferred    string
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		startDesc: "Startet das Einbinden eines neuen Matter-Geräts (WLAN oder Thread; der Nutzer wird nie gefragt, welches). Nur, wenn der Nutzer es will. Mira liest den QR-Code mit der Kamera des Satelliten oder bittet darum, die elf Ziffern daneben vorzulesen. Sobald das Gerät da ist, meldet sie sich am Satelliten von selbst.",
		codeDesc:  "Gibt den Setup-Code eines Matter-Geräts weiter, den der Nutzer vorgelesen hat: die elf Ziffern neben dem QR-Code, als Ziffern, oder den QR-Inhalt (beginnt mit MT:).",
		argCode:   "Der Setup-Code: elf Ziffern (Zahlwörter vorher in Ziffern umschreiben) oder der QR-Inhalt, der mit MT: beginnt.",
		plan:      "# Neues Matter-Gerät\n- [ ] Gerät anmelden (Code, dann Pairing-Modus)\n- [ ] Raum\n- [ ] Name",

		onlyAtSatellite:  "Ergebnis: kein Einbinden. Grund: Das geht nur an einem Satelliten, weil Mira sich dort meldet, sobald das Gerät da ist.",
		noMatter:         "Ergebnis: kein Einbinden. Grund: Home Assistant konnte nicht nach Matter gefragt werden: %s. Sag das dem Nutzer.",
		noMatterEntry:    "Ergebnis: kein Einbinden. Grund: In Home Assistant ist Matter nicht eingerichtet. Sag das dem Nutzer.",
		noNetwork:        "Ergebnis: kein Einbinden. Grund: Mira hat kein Netz für das Gerät: weder Name und Passwort des WLANs (Einstellungen wlan_ssid und wlan_password beim Skill) noch ein Thread-Netz in Home Assistant. Sag dem Nutzer, dass eins davon fehlt.",
		readCode:         "Ergebnis: Dieser Satellit hat keine Kamera. Bitte den Nutzer, die elf Ziffern neben dem QR-Code vorzulesen, und ruf danach matter_code damit auf.",
		scanAgain:        "Ergebnis: Ich habe keinen Code gelesen. Bitte den Nutzer, den Code ruhiger und näher vor die Kamera zu halten, und ruf matter_pairing_start noch einmal auf. Klappt es wieder nicht, bitte ihn, die elf Ziffern vorzulesen.",
		codeRefused:      "Ergebnis: Der Code passt nicht (es sind nicht elf Ziffern, oder die Prüfziffer stimmt nicht). Bitte den Nutzer, ihn noch einmal vorzulesen.",
		noThread:         "Ergebnis: kein Code übernommen. Grund: Es läuft gerade kein Einbinden. Ruf zuerst matter_pairing_start auf.",
		wifiFailed:       "Ergebnis: kein Einbinden. Grund: Das WLAN konnte nicht an Matter übergeben werden: %s. Sag das dem Nutzer.",
		commissionFailed: "Ergebnis: Das Einbinden hat nicht geklappt: %s. Sag das dem Nutzer und frag, ob er es noch einmal versuchen will.",
		plugItIn:         "Ergebnis: Der Code passt, ich suche das Gerät jetzt drei Minuten lang. Sag dem Nutzer jetzt etwa so: „Bring das Gerät jetzt in den Pairing-Modus, wie seine Anleitung sagt. Ich melde mich, sobald ich es habe.“ Frag nichts weiter.",
		threadListFailed: "Ergebnis: kein Einbinden. Grund: Home Assistant konnte nicht nach seinen Thread-Netzen gefragt werden: %s. Sag das dem Nutzer.",
		threadFailed:     "Ergebnis: kein Einbinden. Grund: Das Thread-Netz konnte nicht an Matter übergeben werden: %s. Sag das dem Nutzer.",
		netsBoth:         "Netze für das Gerät: das WLAN und das Thread-Netz %s; der Matter-Server nimmt, was das Gerät kann.",
		netsThread:       "Netz für das Gerät: nur das Thread-Netz %s (Mira hat keine WLAN-Einstellungen); WLAN-Geräte gehen so nicht.",
		netsWifi:         "Netz für das Gerät: nur das WLAN (Home Assistant hat kein Thread-Netz, das Mira nehmen kann); Thread-Geräte gehen so nicht.",
		netsNoPreferred:  "Home Assistant hat mehrere Thread-Netze und keins ist bevorzugt: Sag dem Nutzer, dass Thread-Geräte ein bevorzugtes Thread-Netz in den Thread-Einstellungen von Home Assistant brauchen.",

		nothingFound: "Ich habe das Gerät nicht gefunden. Setz es noch einmal zurück: Halt den Knopf gedrückt, bis es blinkt. Sag mir Bescheid, dann versuche ich es wieder.",
		foundFixed:   "Ich habe ein neues Gerät eingebunden: %s. In welchen Raum kommt es?",
		note:         "Neues Matter-Gerät: %s.",

		missingWifi:      "Ich hatte kein WLAN dafür: Ein WLAN-Gerät braucht Name und Passwort des WLANs in den Einstellungen des Skills.",
		missingThread:    "Ich hatte kein Thread-Netz dafür: Ein Thread-Gerät braucht ein Thread-Netz in Home Assistant.",
		missingPreferred: "Ich hatte kein Thread-Netz dafür: Home Assistant hat mehrere und keins ist bevorzugt.",
	},
	i18n.EN: {
		startDesc: "Starts adding a new Matter device (Wi-Fi or Thread; the user is never asked which). Only when the user wants it. Mira reads the QR code with the satellite's camera, or asks for the eleven digits next to it to be read out. As soon as the device is there, she speaks up at the satellite by herself.",
		codeDesc:  "Passes on the setup code of a Matter device the user read out: the eleven digits next to the QR code, as digits, or the QR code's content (it begins with MT:).",
		argCode:   "The setup code: eleven digits (write number words as digits first) or the QR content beginning with MT:.",
		plan:      "# New Matter device\n- [ ] Device joins (the code, then pairing mode)\n- [ ] Room\n- [ ] Name",

		onlyAtSatellite:  "Result: nothing added. Reason: this works only at a satellite, where Mira speaks up once the device is there.",
		noMatter:         "Result: nothing added. Reason: Home Assistant could not be asked about Matter: %s. Tell the user.",
		noMatterEntry:    "Result: nothing added. Reason: Home Assistant has no Matter set up. Tell the user.",
		noNetwork:        "Result: nothing added. Reason: Mira has no network for the device: neither the Wi-Fi's name and password (the skill's settings wlan_ssid and wlan_password) nor a Thread network in Home Assistant. Tell the user that one of them is missing.",
		readCode:         "Result: this satellite has no camera. Ask the user to read out the eleven digits next to the QR code, then call matter_code with them.",
		scanAgain:        "Result: I read no code. Ask the user to hold it steadier and closer to the camera, then call matter_pairing_start again. If that fails too, ask for the eleven digits.",
		codeRefused:      "Result: the code does not fit (not eleven digits, or the check digit is wrong). Ask the user to read it out again.",
		noThread:         "Result: no code taken. Reason: no adding is running. Call matter_pairing_start first.",
		wifiFailed:       "Result: nothing added. Reason: the Wi-Fi could not be given to Matter: %s. Tell the user.",
		commissionFailed: "Result: adding the device failed: %s. Tell the user and ask whether to try again.",
		plugItIn:         "Result: the code fits, I am looking for the device for three minutes. Tell the user now roughly: \"Put the device into pairing mode now, as its instructions say. I'll speak up as soon as I have it.\" Ask nothing else.",
		threadListFailed: "Result: nothing added. Reason: Home Assistant could not be asked for its Thread networks: %s. Tell the user.",
		threadFailed:     "Result: nothing added. Reason: the Thread network could not be given to Matter: %s. Tell the user.",
		netsBoth:         "Networks for the device: the Wi-Fi and the Thread network %s; the Matter server takes what the device speaks.",
		netsThread:       "Network for the device: only the Thread network %s (Mira has no Wi-Fi settings); Wi-Fi devices do not work this way.",
		netsWifi:         "Network for the device: only the Wi-Fi (Home Assistant has no Thread network Mira can take); Thread devices do not work this way.",
		netsNoPreferred:  "Home Assistant has several Thread networks and none is preferred: tell the user that Thread devices need a preferred Thread network in Home Assistant's Thread settings.",

		nothingFound: "I did not find the device. Reset it once more: hold its button until it blinks. Tell me when it does, then I'll try again.",
		foundFixed:   "I added a new device: %s. Which room does it go in?",
		note:         "New Matter device: %s.",

		missingWifi:      "I had no Wi-Fi for it: a Wi-Fi device needs the Wi-Fi's name and password in the skill's settings.",
		missingThread:    "I had no Thread network for it: a Thread device needs a Thread network in Home Assistant.",
		missingPreferred: "I had no Thread network for it: Home Assistant has several and none is preferred.",
	},
}
