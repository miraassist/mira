package wifi

import "github.com/miraassist/mira/internal/i18n"

// texts are the tools' descriptions and results (facts for the model) and
// what Mira says herself at the thread's satellite. The home Wi-Fi's
// password is in none of them.
type texts struct {
	devicesDesc, handoverDesc, argDevice                   string
	noHome, noMQTT, scanFailed, noneFound, found           string
	homeNotSeen, home5GHz, unknownDevice, sameSubnet, busy string
	plan, facts, handingOver                               string
	joinFailed, handoverFailed, notDiscovered              string
	inWifiConfirm, inWifiRoom                              string
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		devicesDesc:  "Sucht mit dem WLAN von Miras Rechner nach neuen WLAN-Geräten im Einrichtungsmodus (Shelly, Tasmota, ESPHome, WLED). Für „Ich hab einen neuen Shelly“ und ähnliche.",
		handoverDesc: "Holt ein WLAN-Gerät aus wifi_devices ins Heim-WLAN, auf das Ja des Nutzers. Mira meldet sich danach am Satelliten von selbst.",
		argDevice:    "Der WLAN-Name des Geräts, wie wifi_devices ihn nennt.",

		noHome:        "Ergebnis: nichts getan. Grund: Mira kennt das Heim-WLAN noch nicht. Es wird auf Miras Seite beim Skill geraet-einbinden eingetragen (WLAN-Name und Passwort). Sag das dem Nutzer.",
		noMQTT:        "Ergebnis: nichts getan. Grund: Ein Tasmota-Gerät braucht auch den MQTT-Broker, und den kennt Mira noch nicht. Er wird auf Miras Seite beim Skill geraet-einbinden eingetragen. Sag das dem Nutzer.",
		scanFailed:    "Ergebnis: nicht gesucht. Grund: %s. Sag das dem Nutzer.",
		noneFound:     "Ergebnis: Kein Gerät im Einrichtungsmodus zu sehen. Sag, wie man die üblichen hineinbringt: Ein neuer Shelly ist es schon, sonst den Knopf 10 Sekunden halten. Ein frisches Tasmota ist es 3 Minuten nach dem Einstecken, sonst den Knopf 6-mal drücken. ESPHome nach einer Minute ohne sein WLAN. WLED, wenn es neu ist oder sein WLAN nicht findet. Frag, ob du nochmal suchen sollst.",
		found:         "Ergebnis: Diese Geräte sind im Einrichtungsmodus zu sehen. Nenn sie kurz und frag, ob du das gewünschte ins WLAN holen sollst (wifi_handover).",
		homeNotSeen:   "Das Heim-WLAN „%s“ ist hier nicht zu sehen; ohne das geht die Übergabe nicht. Sag das dem Nutzer.",
		home5GHz:      "Das Heim-WLAN „%s“ ist hier nur mit 5 GHz zu sehen; solche Geräte können nur 2,4 GHz. Sag das dem Nutzer.",
		unknownDevice: "Ergebnis: nichts getan. Grund: „%s“ ist kein Gerät, das Mira ins WLAN holen kann. Ruf erst wifi_devices auf.",
		sameSubnet:    "Ergebnis: nichts getan. Grund: Das Gerät nutzt im Einrichtungsmodus die Adressen %s, und das Heimnetz nutzt dieselben. Es muss mit seiner App oder einem Handy eingerichtet werden. Sag das dem Nutzer.",
		busy:          "Ergebnis: nichts getan. Grund: Mira holt gerade schon ein Gerät ins WLAN. Sag, dass es gleich weitergeht.",

		plan:        "# Neues WLAN-Gerät\n- [ ] Ins WLAN\n- [ ] Einbinden\n- [ ] Raum\n- [ ] Name",
		facts:       "Neues WLAN-Gerät: %s, im Einrichtungsmodus mit dem WLAN „%s“.",
		handingOver: "Ergebnis: Mira holt das Gerät jetzt ins WLAN; das dauert ein bis zwei Minuten, sie meldet sich danach am Satelliten von selbst. Sag das kurz und frag nichts weiter.",

		joinFailed:     "Ich komme nicht an das Gerät heran. Leg es näher an meinen Rechner oder versuch es nochmal.",
		handoverFailed: "Das Gerät hat das WLAN nicht angenommen. Soll ich es nochmal versuchen?",
		notDiscovered:  "Das Gerät ist nicht in deinem WLAN aufgetaucht, meistens stimmt dann das Passwort nicht. Soll ich es nochmal versuchen?",
		inWifiConfirm:  "%s ist jetzt im WLAN. Soll ich es einbinden?",
		inWifiRoom:     "%s ist jetzt im WLAN. In welchen Raum kommt es?",
	},
	i18n.EN: {
		devicesDesc:  "Searches with the Wi-Fi of Mira's machine for new Wi-Fi devices in their setup mode (Shelly, Tasmota, ESPHome, WLED). For \"I have a new Shelly\" and the like.",
		handoverDesc: "Brings a Wi-Fi device from wifi_devices into the home Wi-Fi, on the user's yes. Mira speaks up at the satellite by herself afterwards.",
		argDevice:    "The device's Wi-Fi name, as wifi_devices names it.",

		noHome:        "Result: nothing done. Reason: Mira does not know the home Wi-Fi yet. It is set on Mira's page with the skill geraet-einbinden (Wi-Fi name and password). Tell the user.",
		noMQTT:        "Result: nothing done. Reason: a Tasmota device needs the MQTT broker too, and Mira does not know it yet. It is set on Mira's page with the skill geraet-einbinden. Tell the user.",
		scanFailed:    "Result: not searched. Reason: %s. Tell the user.",
		noneFound:     "Result: no device in setup mode is seen. Say how the common ones get there: a new Shelly is in it, else hold its button 10 seconds. A fresh Tasmota is for 3 minutes after plugging it in, else press its button 6 times. ESPHome after a minute without its Wi-Fi. WLED when it is new or does not find its Wi-Fi. Ask whether to search again.",
		found:         "Result: these devices are seen in setup mode. Name them briefly and ask whether to bring the wanted one into the Wi-Fi (wifi_handover).",
		homeNotSeen:   "The home Wi-Fi \"%s\" is not seen here; without it the hand-over does not work. Tell the user.",
		home5GHz:      "The home Wi-Fi \"%s\" is seen here only on 5 GHz; such devices only do 2.4 GHz. Tell the user.",
		unknownDevice: "Result: nothing done. Reason: \"%s\" is no device Mira can bring into the Wi-Fi. Call wifi_devices first.",
		sameSubnet:    "Result: nothing done. Reason: in setup mode the device uses the addresses %s, and the home network uses the same. It has to be set up with its app or a phone. Tell the user.",
		busy:          "Result: nothing done. Reason: Mira is bringing a device into the Wi-Fi already. Say it goes on in a moment.",

		plan:        "# New Wi-Fi device\n- [ ] Into the Wi-Fi\n- [ ] Add\n- [ ] Room\n- [ ] Name",
		facts:       "New Wi-Fi device: %s, in setup mode with the Wi-Fi \"%s\".",
		handingOver: "Result: Mira brings the device into the Wi-Fi now; it takes a minute or two, and she speaks up at the satellite by herself afterwards. Say so briefly and ask nothing else.",

		joinFailed:     "I can't reach the device. Put it closer to my machine or try again.",
		handoverFailed: "The device did not take the Wi-Fi. Shall I try again?",
		notDiscovered:  "The device did not turn up in your Wi-Fi; most often the password is wrong. Shall I try again?",
		inWifiConfirm:  "%s is in the Wi-Fi now. Shall I add it?",
		inWifiRoom:     "%s is in the Wi-Fi now. Which room does it go in?",
	},
}
