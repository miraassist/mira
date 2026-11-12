package inbox

import "github.com/miraassist/mira/internal/i18n"

// texts are the inbox's messages to the people and its tool's texts.
type texts struct {
	// Messages; %s is the document's title.
	ask, unclear, more, cut, noText, unreachable, unreadable, notMarked string
	weekdays                                                            [7]string
	allDay, past, one, many, forWhom                                    string
	// The tool.
	toolDesc, argSteps, argCalendar, argChanges                       string
	noThread, noCalendar, foreignCalendar, unknownCalendar, noStep    string
	entered, failed, badChange, note, calendarsFailed, enteredSummary string
	from                                                              string // %s ref, %s title
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		ask:         "In deiner Post: %s. Ich habe %s gefunden: %s. Soll ich sie eintragen?",
		unclear:     "In der Post: %s. Darin steht: %s. Was soll ich damit machen?",
		more:        " Dazu kommen %d weitere, die ich nicht aufgezählt habe.",
		cut:         " Ich habe nur den Anfang gelesen.",
		noText:      "In „%s“ konnte ich keinen Text lesen.",
		unreachable: "Ich komme an „%s“ in Paperless nicht heran.",
		unreadable:  "Ich konnte „%s“ gerade nicht lesen, ich versuche es beim nächsten Start noch einmal.",
		notMarked:   "Ich konnte „%s“ in Paperless nicht als gelesen markieren; bitte setz das Tag Mira gelesen selbst.",
		weekdays:    [7]string{"So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"},
		allDay:      "ganztägig",
		past:        "schon vorbei",
		one:         "einen Termin",
		many:        "%d Termine",
		forWhom:     "für",

		toolDesc: "Trägt Termine aus Miras Postfach (dem Plan des Vorgangs postfach) in einen Kalender ein, " +
			"genau so, wie sie im Dokument geprüft wurden. Nimm die Nummern der Schritte, die der Nutzer will " +
			"(alle, einige). Ändert der Nutzer etwas („der Elternabend ist um 20 Uhr“), gib es in changes mit; " +
			"schreib den Plan nie selbst um. Ohne calendar nimmt Mira den Kalender der Person; nennt der Nutzer " +
			"einen oder steht ein Standardkalender in seinem Gedächtnis, gib ihn an.",
		argSteps:    "Die Nummern der Schritte im Plan, die eingetragen werden sollen, z. B. [1, 2].",
		argCalendar: "Optional: der Name des Kalenders; nur einer der Person oder einer des Hauses.",
		argChanges:  "Optional: Änderungen des Nutzers je Schritt; nur die Felder, die er ändert. day YYYY-MM-DD, start und end HH:MM.",

		noThread:        "Ergebnis: nichts eingetragen. Grund: Kein offener Vorgang postfach in diesem Gespräch.",
		noCalendar:      "Ergebnis: nichts eingetragen. Grund: Die Person hat keinen eigenen Kalender. Frag, in welchen Kalender die Termine sollen, und ruf inbox_enter mit calendar auf.",
		foreignCalendar: "Ergebnis: nichts eingetragen. Grund: „%s“ ist der Kalender einer anderen Person; dort trägt Mira nichts ein.",
		unknownCalendar: "Ergebnis: nichts eingetragen. Grund: Einen beschreibbaren Kalender „%s“ gibt es nicht. Vorhanden: %s.",
		noStep:          "Schritt %d gibt es nicht im Plan.",
		entered:         "Schritt %d eingetragen: %s.",
		failed:          "Schritt %d nicht eingetragen: %s.",
		badChange:       "Schritt %d nicht eingetragen: Die Änderung passt nicht (%s).",
		note:            "eingetragen",
		calendarsFailed: "Ergebnis: nichts eingetragen. Grund: Die Kalender sind gerade nicht lesbar (%s).",
		enteredSummary:  "Kalender: %s.",
		from:            "aus %s: %s",
	},
	i18n.EN: {
		ask:         "In your post: %s. I found %s: %s. Shall I add them?",
		unclear:     "In the post: %s. It says: %s. What shall I do with it?",
		more:        " There are %d more I did not list.",
		cut:         " I only read the beginning.",
		noText:      "I could not read any text in “%s”.",
		unreachable: "I cannot get at “%s” in Paperless.",
		unreadable:  "I could not read “%s” just now; I will try again at the next start.",
		notMarked:   "I could not mark “%s” as read in Paperless; please set the tag Mira gelesen yourself.",
		weekdays:    [7]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"},
		allDay:      "all day",
		past:        "already past",
		one:         "one appointment",
		many:        "%d appointments",
		forWhom:     "for",

		toolDesc: "Adds appointments from Mira's inbox (the plan of the thread postfach) to a calendar, exactly as " +
			"they were checked in the document. Give the numbers of the steps the user wants (all, some). If the " +
			"user changes something (“the parents' evening is at 8 pm”), pass it in changes; never rewrite the plan " +
			"yourself. Without calendar Mira takes the person's calendar; if the user names one or their memory " +
			"holds a default calendar, give it.",
		argSteps:    "The numbers of the plan's steps to add, e.g. [1, 2].",
		argCalendar: "Optional: the calendar's name; only one of the person's or one of the house.",
		argChanges:  "Optional: the user's changes per step; only the fields they change. day YYYY-MM-DD, start and end HH:MM.",

		noThread:        "Result: nothing added. Reason: no open thread postfach in this conversation.",
		noCalendar:      "Result: nothing added. Reason: the person has no calendar of their own. Ask which calendar the appointments go to and call inbox_enter with calendar.",
		foreignCalendar: "Result: nothing added. Reason: “%s” is another person's calendar; Mira adds nothing there.",
		unknownCalendar: "Result: nothing added. Reason: there is no writable calendar “%s”. There are: %s.",
		noStep:          "There is no step %d in the plan.",
		entered:         "Step %d added: %s.",
		failed:          "Step %d not added: %s.",
		badChange:       "Step %d not added: the change does not fit (%s).",
		note:            "added",
		calendarsFailed: "Result: nothing added. Reason: the calendars cannot be read just now (%s).",
		enteredSummary:  "Calendar: %s.",
		from:            "from %s: %s",
	},
}
