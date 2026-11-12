package devices

import "github.com/miraassist/mira/internal/i18n"

// coreTexts are the descriptions and results of device_assign and
// device_finish: facts for the model, which words them itself.
type coreTexts struct {
	assignDesc, finishDesc, argRoom, argName string
	noDevice, unknownRoom, assignFailed      string
	assigned, finishFailed, renameFailed     string
	finished                                 string
}

var coreCatalog = map[i18n.Lang]coreTexts{
	i18n.DE: {
		assignDesc:   "Setzt das neu eingebundene Gerät in einen Raum.",
		finishDesc:   "Schließt das Einbinden ab: gibt die Werte des Geräts für Sprache frei und benennt es um. Erst, wenn der Name feststeht.",
		argRoom:      "Der Raum, wie der Nutzer ihn nennt, z. B. Wohnzimmer.",
		argName:      "Der Name des Geräts, z. B. Temperatursensor Wohnzimmer.",
		noDevice:     "Ergebnis: nichts getan. Grund: Es ist noch kein neues Gerät da.",
		unknownRoom:  "Ergebnis: nichts getan. Grund: Einen Raum „%s“ gibt es nicht. Räume: %s. Frag nach.",
		assignFailed: "Ergebnis: nichts getan. Grund: Home Assistant hat abgelehnt: %s. Sag das dem Nutzer.",
		assigned:     "Ergebnis: Das Gerät ist jetzt im Raum %s. Schlag jetzt einen Namen vor (was es ist und der Raum) und frag, ob er passt.",
		finishFailed: "Ergebnis: nicht freigegeben, nicht umbenannt. Grund: Home Assistant hat abgelehnt: %s. Sag das dem Nutzer.",
		renameFailed: "Ergebnis: %d Werte für Sprache freigegeben, aber nicht umbenannt. Grund: %s. Der Raum bleibt. Sag das dem Nutzer.",
		finished:     "Ergebnis: fertig. Das Gerät heißt jetzt %s, ist im Raum %s, und %d Werte sind für Sprache freigegeben. Sag das kurz.",
	},
	i18n.EN: {
		assignDesc:   "Puts the newly added device into a room.",
		finishDesc:   "Finishes adding the device: exposes its values to voice and renames it. Only once the name is settled.",
		argRoom:      "The room as the user names it, e.g. Living Room.",
		argName:      "The device's name, e.g. Temperature sensor living room.",
		noDevice:     "Result: nothing done. Reason: no new device is there yet.",
		unknownRoom:  "Result: nothing done. Reason: there is no room \"%s\". Rooms: %s. Ask.",
		assignFailed: "Result: nothing done. Reason: Home Assistant refused: %s. Tell the user.",
		assigned:     "Result: the device is now in %s. Now propose a name (what it is and the room) and ask whether it fits.",
		finishFailed: "Result: not exposed, not renamed. Reason: Home Assistant refused: %s. Tell the user.",
		renameFailed: "Result: %d values exposed to voice, but not renamed. Reason: %s. The room stays. Tell the user.",
		finished:     "Result: done. The device is now called %s, is in %s, and %d values are exposed to voice. Say so briefly.",
	},
}

// foundTexts are what the found devices say and their tools return.
type foundTexts struct {
	// An entry of the house's device message, one per device.
	entryLine, details string
	// The tools.
	argItem, foundDesc, adoptDesc, confirmDesc, ignoreDesc     string
	waitsForYes, noRoom, listFailed, nothingOpen, openList     string
	notOpen, plan, facts, askRoom, flowFacts, confirmFailed    string
	onlyInHA, stepNoFields, wifiMissing, stepWifi, stepError   string
	noFlow, setUpNoDevice, setUpSeveral, ignored, ignoreFailed string
}

var foundCatalog = map[i18n.Lang]foundTexts{
	i18n.DE: {
		entryLine: "Neues Gerät gefunden: %s",
		details:   "Skill geraet-einbinden. Gefunden (für device_adopt oder device_ignore):",

		argItem:     "Was gefunden wurde, wie es in der Nachricht oder in devices_found steht: flow:<id> oder device:<id>.",
		foundDesc:   "Listet die Geräte, die Home Assistant gefunden hat und die noch nicht eingebunden sind: entdeckte, die auf ein Ja warten, und neue ohne Raum. Für „Gibt's neue Geräte?“.",
		adoptDesc:   "Beginnt, ein gefundenes Gerät einzubinden, auf das Ja des Nutzers hin. Danach geht es mit dem Raum weiter, oder bei einem entdeckten Gerät erst mit dessen Schritt in Home Assistant.",
		confirmDesc: "Bestätigt den Schritt eines entdeckten Geräts in Home Assistant, nachdem der Nutzer Ja gesagt oder getan hat, was der Schritt verlangt.",
		ignoreDesc:  "Ignoriert ein gefundenes Gerät auf Wunsch des Nutzers („nein“, „ignorieren“, „das ist vom Nachbarn“); Mira nennt es dann nie wieder.",
		waitsForYes: "entdeckt, wartet auf ein Ja",
		noRoom:      "ohne Raum",
		listFailed:  "Ergebnis: nichts gelesen. Grund: Home Assistant hat abgelehnt: %s. Sag das dem Nutzer.",
		nothingOpen: "Ergebnis: Es gibt keine neuen Geräte, die auf das Einbinden warten.",
		openList:    "Ergebnis: Diese Geräte warten aufs Einbinden. Nenn sie mit Namen und frag, welches; die Kennung nicht vorlesen.",
		notOpen:     "Ergebnis: nichts getan. Grund: Das Gerät wartet nicht mehr, es wurde inzwischen in Home Assistant eingerichtet oder ignoriert. Sag das kurz.",
		plan:        "# Neues Gerät\n- [ ] Einbinden\n- [ ] Raum\n- [ ] Name",
		facts:       "Neues Gerät: %s, Hersteller %s, Modell %s.",
		askRoom:     "Ergebnis: %s Frag jetzt, in welchen Raum es kommt.",
		flowFacts:   "Gefunden: %s (Home Assistant, Integration %s).",

		confirmFailed: "Ergebnis: nicht eingerichtet. Grund: Home Assistant hat abgelehnt: %s. Das Gerät wartet weiter in Home Assistant unter Einstellungen → Geräte & Dienste → Entdeckt. Sag das dem Nutzer.",
		onlyInHA:      "Ergebnis: Dieses Gerät braucht Angaben, die nur in Home Assistant eingetragen werden können: Einstellungen → Geräte & Dienste → Entdeckt. Sag das dem Nutzer.",
		stepNoFields:  "Ergebnis: Home Assistant fragt: „%s“. Ist es nur eine Bestätigung, ruf device_confirm gleich auf. Verlangt es, dass der Nutzer etwas tut (einen Knopf drücken), sag ihm das kurz und ruf device_confirm nach seinem „erledigt“ auf.",
		wifiMissing:   "Ergebnis: nicht eingerichtet. Grund: Das Gerät braucht das WLAN, und Mira kennt es noch nicht. Es wird auf Miras Seite beim Skill geraet-einbinden eingetragen. Sag das dem Nutzer.",
		stepWifi:      "Ergebnis: Das Gerät braucht das WLAN; Mira trägt „%s“ ein. Ruf device_confirm auf.",
		stepError:     "Fehler: ",
		noFlow:        "Ergebnis: nichts getan. Grund: In diesem Vorgang wartet kein entdecktes Gerät auf eine Bestätigung.",
		setUpNoDevice: "Ergebnis: in Home Assistant eingerichtet; ein Gerät dazu ist noch nicht da. Kommt es später, meldet sich Mira. Sag das kurz.",
		setUpSeveral:  "Ergebnis: in Home Assistant eingerichtet, mit %d Geräten. Sie stehen in der Liste der neuen Geräte und werden einzeln eingebunden; frag, ob der Nutzer sie jetzt durchgehen will (devices_found).",
		ignored:       "Ergebnis: %s wird ignoriert; Mira nennt es nicht wieder. In Home Assistant lässt sich das rückgängig machen. Sag das kurz.",
		ignoreFailed:  "Ergebnis: nicht ignoriert. Grund: Home Assistant hat abgelehnt: %s. Sag das dem Nutzer.",
	},
	i18n.EN: {
		entryLine: "New device found: %s",
		details:   "Skill geraet-einbinden. Found (for device_adopt or device_ignore):",

		argItem:     "What was found, as the message or devices_found names it: flow:<id> or device:<id>.",
		foundDesc:   "Lists the devices Home Assistant found that are not added yet: discovered ones waiting for a yes, and new ones without a room. For \"Any new devices?\".",
		adoptDesc:   "Starts adding a found device, on the user's yes. Then the room is next, or for a discovered device first its step in Home Assistant.",
		confirmDesc: "Confirms a discovered device's step in Home Assistant, after the user said yes or did what the step asks.",
		ignoreDesc:  "Ignores a found device at the user's wish (\"no\", \"ignore it\", \"that's the neighbour's\"); Mira never names it again.",
		waitsForYes: "discovered, waits for a yes",
		noRoom:      "without a room",
		listFailed:  "Result: nothing read. Reason: Home Assistant refused: %s. Tell the user.",
		nothingOpen: "Result: no new devices are waiting to be added.",
		openList:    "Result: these devices wait to be added. Name them and ask which; do not read out the ids.",
		notOpen:     "Result: nothing done. Reason: the device is no longer waiting; it was set up or ignored in Home Assistant meanwhile. Say so briefly.",
		plan:        "# New device\n- [ ] Add\n- [ ] Room\n- [ ] Name",
		facts:       "New device: %s, maker %s, model %s.",
		askRoom:     "Result: %s Now ask which room it goes in.",
		flowFacts:   "Found: %s (Home Assistant, integration %s).",

		confirmFailed: "Result: not set up. Reason: Home Assistant refused: %s. The device still waits in Home Assistant under Settings → Devices & services → Discovered. Tell the user.",
		onlyInHA:      "Result: this device needs details that can only be entered in Home Assistant: Settings → Devices & services → Discovered. Tell the user.",
		stepNoFields:  "Result: Home Assistant asks: \"%s\". If it is only a confirmation, call device_confirm right away. If it asks the user to do something (press a button), tell them briefly and call device_confirm after their \"done\".",
		wifiMissing:   "Result: not set up. Reason: the device needs the Wi-Fi, and Mira does not know it yet. It is set on Mira's page with the skill geraet-einbinden. Tell the user.",
		stepWifi:      "Result: the device needs the Wi-Fi; Mira fills in \"%s\". Call device_confirm.",
		stepError:     "Error: ",
		noFlow:        "Result: nothing done. Reason: no discovered device waits for a confirmation in this thread.",
		setUpNoDevice: "Result: set up in Home Assistant; no device of it is there yet. When one comes, Mira speaks up. Say so briefly.",
		setUpSeveral:  "Result: set up in Home Assistant, with %d devices. They are in the list of new devices and are added one by one; ask whether the user wants to go through them now (devices_found).",
		ignored:       "Result: %s is ignored; Mira does not name it again. It can be undone in Home Assistant. Say so briefly.",
		ignoreFailed:  "Result: not ignored. Reason: Home Assistant refused: %s. Tell the user.",
	},
}
