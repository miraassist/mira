package threads

import (
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/store"
)

type texts struct {
	startDesc, requestDesc, fillDesc, fieldsDesc, confirmDesc, cancelDesc string
	noTarget, noThread, nothingToConfirm, unknownField, notInEnum         string
	checkFailed, started, working, workFailed, askUser, proposeUser       string
	cancelled, yesLogged, memorySaved                                     string
	headerCurrent, headerWaiting, lineTarget, lineRequest, lineFields     string
	lineLog, whoUser, whoMira, fieldMissing, stillMissing                 string
	stateHint                                                             map[string]string
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		startDesc:        "Eröffnet einen Vorgang für ein Anliegen, das mehr braucht als eine Antwort: Mira sammelt, was das Ziel braucht, fragt nach, was fehlt, und schließt ab. Erst öffnen, dann mit thread_fill füllen. Ziele:",
		requestDesc:      "Das Anliegen in den Worten des Nutzers.",
		fillDesc:         "Trägt Felder des laufenden oder hier wartenden Vorgangs ein, aus dem, was der Nutzer gesagt hat oder was du im Haus nachgesehen hast. Jedes Feld braucht eine Antwort; ist ein Feld bewusst nicht nötig, schreib \"- \" und den Grund (z. B. \"- der Wunsch enthält nichts Relatives\"). Du bekommst den Stand zurück: Fehlt etwas, frag den Nutzer kurz und gezielt danach. Auch eine Antwort oder ein Änderungswunsch zu einem Vorgang geht hierüber.",
		fieldsDesc:       "Feldname -> Wert, nur die neuen oder geänderten.",
		confirmDesc:      "Schließt den Vorgang ab, der auf ein Ja wartet, wenn der Nutzer zustimmt (auch wenn es im zusätzlichen Kontext steht). Danach nur kurz bestätigen.",
		cancelDesc:       "Bricht den laufenden oder hier wartenden Vorgang ab, wenn der Nutzer ihn nicht mehr will.",
		noTarget:         "kein Ziel %q",
		noThread:         "hier läuft kein Vorgang; eröffne einen mit thread_start",
		nothingToConfirm: "der Vorgang wartet auf kein Ja; fülle mit thread_fill, was fehlt",
		unknownField:     "unbekanntes Feld %s",
		notInEnum:        "%s muss eines davon sein: %s",
		checkFailed:      "%s: %v",
		started:          "Vorgang eröffnet. Anleitung des Ziels:",
		working:          "Alles beisammen; Mira arbeitet im Hintergrund daran und meldet sich von selbst. Sag dem Nutzer in einem kurzen Satz, dass du dich meldest, ohne Zeitangabe.",
		workFailed:       "Für „%s“ habe ich gerade keine Lösung gefunden.",
		askUser:          "Frag den Nutzer: %s",
		proposeUser:      "Sag dem Nutzer den Vorschlag und frag, ob es so passt: %s",
		cancelled:        "Vorgang abgebrochen.",
		yesLogged:        "Ja",
		memorySaved:      "Gemerkt: %s. Sag dem Nutzer kurz, dass es notiert ist.",
		headerCurrent:    "## Laufender Vorgang (diese Unterhaltung gehört dazu)",
		headerWaiting:    "## Hier wartet ein Vorgang auf den Nutzer\nGehört seine Nachricht dazu, führe ihn weiter (thread_fill, thread_confirm) oder brich ihn ab (thread_cancel); sonst lass ihn stehen.",
		lineTarget:       "Ziel: %s – %s\n",
		lineRequest:      "Anliegen: %s\n",
		lineFields:       "Felder:",
		lineLog:          "Bisher:",
		whoUser:          "Nutzer",
		whoMira:          "Mira",
		fieldMissing:     "(fehlt)",
		stillMissing:     "Fehlt noch: %s",
		stateHint: map[string]string{
			store.ThreadCollecting: "Stand: sammelt. Fehlt etwas, frag danach; trag Antworten mit thread_fill ein.",
			store.ThreadWaiting:    "Stand: wartet auf die Antwort des Nutzers auf Miras letzte Frage; trag sie mit thread_fill ein.",
			store.ThreadWorking:    "Stand: Mira arbeitet daran im Hintergrund.",
			store.ThreadProposed:   "Stand: Miras Vorschlag wartet auf ein Ja. Stimmt der Nutzer zu: thread_confirm. Will er etwas ändern: thread_fill. Will er nicht: thread_cancel.",
		},
	},
	i18n.EN: {
		startDesc:        "Opens a thread for a concern that needs more than one answer: Mira collects what the target needs, asks for what is missing and finishes. Open first, then fill with thread_fill. Targets:",
		requestDesc:      "The concern in the user's words.",
		fillDesc:         "Fills fields of the current thread or the one waiting here, from what the user said or what you looked up in the house. Every field needs an answer; if a field is deliberately not needed, write \"- \" and the reason (e.g. \"- nothing relative in the request\"). You get the state back: if something is missing, ask the user for it, briefly and specifically. An answer or a change to a thread also goes here.",
		fieldsDesc:       "field name -> value, only new or changed ones.",
		confirmDesc:      "Finishes the thread that waits for a yes, when the user agrees (also if it is in the additional context). Then confirm only briefly.",
		cancelDesc:       "Cancels the current thread or the one waiting here when the user no longer wants it.",
		noTarget:         "no target %q",
		noThread:         "no thread runs here; open one with thread_start",
		nothingToConfirm: "the thread waits for no yes; fill what is missing with thread_fill",
		unknownField:     "unknown field %s",
		notInEnum:        "%s must be one of: %s",
		checkFailed:      "%s: %v",
		started:          "Thread opened. The target's instructions:",
		working:          "Everything is there; Mira works on it in the background and comes back by herself. Tell the user in one short sentence that you will come back, without a time.",
		workFailed:       "I found no solution for \"%s\" just now.",
		askUser:          "Ask the user: %s",
		proposeUser:      "Tell the user the proposal and ask whether it suits: %s",
		cancelled:        "Thread cancelled.",
		yesLogged:        "yes",
		memorySaved:      "Remembered: %s. Tell the user briefly that it is noted.",
		headerCurrent:    "## Current thread (this conversation belongs to it)",
		headerWaiting:    "## A thread waits for the user here\nIf the message belongs to it, continue it (thread_fill, thread_confirm) or cancel it (thread_cancel); otherwise leave it.",
		lineTarget:       "Target: %s – %s\n",
		lineRequest:      "Concern: %s\n",
		lineFields:       "Fields:",
		lineLog:          "So far:",
		whoUser:          "user",
		whoMira:          "Mira",
		fieldMissing:     "(missing)",
		stillMissing:     "Still missing: %s",
		stateHint: map[string]string{
			store.ThreadCollecting: "State: collecting. If something is missing, ask for it; enter answers with thread_fill.",
			store.ThreadWaiting:    "State: waiting for the user's answer to Mira's last question; enter it with thread_fill.",
			store.ThreadWorking:    "State: Mira works on it in the background.",
			store.ThreadProposed:   "State: Mira's proposal waits for a yes. If the user agrees: thread_confirm. If they want a change: thread_fill. If not: thread_cancel.",
		},
	},
}
