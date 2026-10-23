package tools

import "github.com/miraassist/mira/internal/i18n"

// texts are the descriptions of the internal tools (part of prompt layer 3),
// their results, and the guard's messages to the model.
type texts struct {
	memorySaveDesc, memorySaveText, memorySaveTags, memorySaved                                   string // memorySaved: %s = id
	memorySaveHouse, memorySavedPerson, personalMark                                              string // memorySavedPerson: %s = person, %s = id
	memorySearchDesc, memorySearchQuery, memoryNothing                                            string
	memoryForgetDesc, memoryForgotten, noSuchFact                                                 string
	messagesListDesc, noMessages, messageLine, messageDetails                                     string // messageLine: id, importance, state, text
	messageAckDesc, messageAcked, noSuchMessage                                                   string
	messageSendDesc, messageTo, messageSendText, messageSent, unknownReceiver                     string
	refusedNone, refusedPersons, fromPerson, fromPersonHouse, fromArea, fromAreaHouse, remindFrom string
	textMissing, invalidArgs                                                                      string
	delegateDesc, delegateTask                                                                    string
	calculateDesc, calculateExpr, calculateBad                                                    string
	reminderSetDesc, reminderText, reminderAt, reminderInMinutes, reminderFor                     string
	reminderBadTime, reminderPast, reminderTooFar, reminderSaved                                  string // reminderSaved: time, id
	reminderListDesc, noReminders, reminderCancelDesc, noSuchReminder, reminderCancelled          string
	messagesReadDesc, messagesNothing, messagesReadOut                                            string

	denyBackground, denyNoConfirm, denyAfterWeb, denyStoreFailed string
	needsConfirmation                                            string // %s = summary of the action
	asksAfterWeb                                                 string // %s = summary of the call
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		memorySaveDesc:    "Merkt sich dauerhaft eine Tatsache über den Haushalt oder den Nutzer, z.B. Vorlieben, Gewohnheiten, wo etwas steht. Nur speichern, wenn der Nutzer es möchte oder es eindeutig dauerhaft nützlich ist. Ist bekannt, wer spricht, kommt es in dessen persönliches Gedächtnis, sonst ins Gedächtnis des Hauses.",
		memorySaveHouse:   "true nur, wenn der Nutzer ausdrücklich sagt, dass es für alle im Haus gilt (\"fürs Haus\", \"für alle\"). Sonst weglassen.",
		memorySavedPerson: "Gemerkt für %s, persönlich (id %s).",
		personalMark:      " (persönlich)",
		memorySaveText:    "Die Tatsache als vollständiger Satz.",
		memorySaveTags:    "Optionale Stichwörter.",
		memorySaved:       "Gemerkt fürs Haus (id %s).",
		memorySearchDesc:  "Sucht in den gespeicherten Tatsachen über den Haushalt und den Nutzer.",
		memorySearchQuery: "Suchbegriffe.",
		// Said this bluntly on purpose: with a plain "nothing found", local
		// models fill the gap with a plausible guess.
		memoryNothing:      "Nichts gefunden. Dazu ist nichts gespeichert, du weißt es also nicht. Sag dem Nutzer genau das und rate nicht.",
		memoryForgetDesc:   "Löscht eine gespeicherte Tatsache anhand ihrer id (aus memory_search).",
		memoryForgotten:    "Gelöscht.",
		noSuchFact:         "keine Tatsache mit dieser id",
		messagesListDesc:   "Listet offene Nachrichten (z.B. Gerätestörungen, Abfuhrtermine, Nachrichten von anderen), die noch nicht erledigt sind.",
		noMessages:         "Keine offenen Nachrichten.",
		messageLine:        "- [%s] Wichtigkeit %d, %s: %s",
		messageDetails:     " Details: %s",
		messageAckDesc:     "Markiert eine Nachricht als erledigt, wenn der Nutzer sie zur Kenntnis genommen hat.",
		messageAcked:       "Erledigt.",
		noSuchMessage:      "keine Nachricht mit dieser id",
		messageSendDesc:    "Schickt jemandem eine Nachricht oder hinterlässt sie fürs Haus: „Sag Anna, dass es später wird“, „Hinterlass fürs Haus: …“, „Schick mir das aufs Handy“. Mira stellt sie zu, sobald der Empfänger erreichbar ist (bei einer Person über ihre Kanäle, im Haus per Ansage). Mit at oder in_minutes erst zu dieser Zeit, z. B. „Erinnere Ben morgen um 7 an den Müll“. Nicht für eine Durchsage an alle, die gerade da sind.",
		messageTo:          "Empfänger: house (das Haus, alle im Haushalt) oder eine Person (Kürzel oder Name).",
		messageSendText:    "Was ausgerichtet werden soll, in eigenen Worten, ohne den Absender.",
		messageSent:        "Nachricht für %s gespeichert, sie wird zugestellt.",
		unknownReceiver:    "Ich kenne niemanden namens %s.",
		refusedNone:        "%s nimmt gerade keine Nachrichten an.",
		refusedPersons:     "%s nimmt nur Nachrichten von Personen an, die ich erkenne.",
		fromPerson:         "%s lässt dir ausrichten: %s",
		fromPersonHouse:    "%s lässt ausrichten: %s",
		fromArea:           "Jemand im Bereich %s lässt dir ausrichten: %s",
		fromAreaHouse:      "Jemand im Bereich %s lässt ausrichten: %s",
		remindFrom:         "%s erinnert dich: %s",
		textMissing:        "text fehlt",
		invalidArgs:        "ungültige Argumente: %w",
		delegateDesc:       "Gibt eine Frage oder Aufgabe an ein stärkeres Modell ab, wenn sie viel Wissen, gründliches Nachdenken oder mehrere Rechercheschritte braucht, z.B. Erklärungen, Planungen, Vergleiche, Rezepte. Nicht für Schaltbefehle, Zustandsabfragen oder Smalltalk. Das stärkere Modell kann nachschlagen, aber nichts verändern. Fasse sein Ergebnis für den Nutzer im Stil des Kanals zusammen.",
		delegateTask:       "Die vollständige Aufgabe mit allem nötigen Kontext aus dem Gespräch; das andere Modell kennt das Gespräch nicht.",
		calculateDesc:      "Rechnet einen Ausdruck exakt aus. Nimm das immer, wenn du mehr als zwei Zahlen zusammenrechnest, Mengen umrechnest oder Prozente ausrechnest, statt im Kopf zu rechnen.",
		calculateExpr:      "Der Ausdruck mit Zahlen (Dezimalpunkt, kein Tausenderpunkt), + - * / und Klammern, z. B. 307.93 + 276.77 + 54.86 oder 400 * 2 / 4.",
		calculateBad:       "Das lässt sich nicht ausrechnen: %v",
		reminderSetDesc:    "Stellt eine Erinnerung mit Text: Zur Zeit sagt Mira den Text im Raum an, in dem gefragt wurde, oder schickt ihn aufs Handy. Gib entweder at oder in_minutes an. Ein reiner Timer zum Herunterzählen („Timer auf 10 Minuten“) ist HassStartTimer von Home Assistant, nicht dies.",
		reminderText:       "Woran erinnert werden soll, kurz, z.B. \"Müll rausbringen\".",
		reminderAt:         "Datum und Uhrzeit in Ortszeit als YYYY-MM-DD HH:MM, z.B. 2026-10-16 19:00. Rechne es aus der aktuellen Zeit aus.",
		reminderInMinutes:  "Für \"in 20 Minuten\": Minuten ab jetzt, statt at.",
		reminderFor:        "Optional: Kürzel der Person, für die die Erinnerung ist.",
		reminderBadTime:    "Zeit nicht verstanden: %w",
		reminderPast:       "Diese Zeit liegt in der Vergangenheit.",
		reminderTooFar:     "Erinnerungen gehen höchstens ein Jahr voraus.",
		reminderSaved:      "Erinnerung gestellt für %s (id %s).",
		reminderListDesc:   "Listet die offenen Erinnerungen.",
		noReminders:        "Keine offenen Erinnerungen.",
		reminderCancelDesc: "Löscht eine offene Erinnerung anhand ihrer id (aus reminder_list).",
		noSuchReminder:     "keine offene Erinnerung mit dieser id",
		reminderCancelled:  "Erinnerung gelöscht.",
		messagesReadDesc:   "Liest die wartenden Benachrichtigungen für diesen Raum oder diese Person vor und markiert sie als gehört. Rufe es auf, wenn der Nutzer fragt: \"Hast du eine Benachrichtigung?\", \"Gibt es was Neues?\", \"Was gibt's?\".",
		messagesNothing:    "Keine neuen Benachrichtigungen.",
		messagesReadOut:    "Sag dem Nutzer diese Benachrichtigungen, sinngemäß und vollständig: ",

		denyBackground:    "Abgelehnt: Hintergrundjobs dürfen nur lesende Werkzeuge verwenden.",
		denyNoConfirm:     "Abgelehnt: Das braucht eine Bestätigung, und im Hintergrund kann niemand bestätigen.",
		denyAfterWeb:      "Abgelehnt: In dieser Anfrage wurden bereits Inhalte aus dem Web gelesen. Kritische Aktionen sind deshalb gesperrt. Sag dem Nutzer, dass er den Befehl bitte noch einmal direkt geben soll.",
		denyStoreFailed:   "Abgelehnt: Die Bestätigung konnte nicht vorgemerkt werden. Die Aktion wurde nicht ausgeführt.",
		needsConfirmation: "NICHT AUSGEFÜHRT – Bestätigung nötig. Die Aktion (%s) ist kritisch. Frage den Nutzer jetzt kurz, ob du sie ausführen sollst, und beende deine Antwort mit dieser Frage. Erst wenn er im nächsten Schritt zustimmt, rufe das Werkzeug mit denselben Argumenten erneut auf.",
		asksAfterWeb:      "NICHT AUSGEFÜHRT – Nachfrage nötig. In dieser Anfrage wurden Inhalte aus dem Web gelesen; deshalb merke ich mir oder gebe ich nichts ohne das Ja des Nutzers weiter (%s). Sag dem Nutzer kurz und genau, was du dir merken oder weitergeben würdest, frage ihn zum Beispiel „Soll ich mir das für dich merken?“, und beende deine Antwort mit dieser Frage. Erst wenn er im nächsten Schritt zustimmt, rufe das Werkzeug mit denselben Argumenten erneut auf.",
	},
	i18n.EN: {
		memorySaveDesc:     "Permanently remembers a fact about the household or the user, e.g. preferences, habits, where something is. Only save when the user wants it or it is clearly useful for good. When it is known who is speaking, it goes into their personal memory, otherwise into the house's.",
		memorySaveHouse:    "true only when the user says explicitly that it is for everyone in the house (\"for the house\", \"for everyone\"). Otherwise leave it out.",
		memorySavedPerson:  "Saved in the personal memory of %s (id %s).",
		personalMark:       " (personal)",
		memorySaveText:     "The fact as a complete sentence.",
		memorySaveTags:     "Optional keywords.",
		memorySaved:        "Saved in the house's memory (id %s).",
		memorySearchDesc:   "Searches the saved facts about the household and the user.",
		memorySearchQuery:  "Search terms.",
		memoryNothing:      "Nothing found. Nothing about this is saved, so you do not know it. Tell the user exactly that and do not guess.",
		memoryForgetDesc:   "Deletes a saved fact by its id (from memory_search).",
		memoryForgotten:    "Deleted.",
		noSuchFact:         "no fact with this id",
		messagesListDesc:   "Lists open messages (e.g. device faults, collection days, messages from others) that are not done yet.",
		noMessages:         "No open messages.",
		messageLine:        "- [%s] importance %d, %s: %s",
		messageDetails:     " Details: %s",
		messageAckDesc:     "Marks a message as done once the user has taken note of it.",
		messageAcked:       "Done.",
		noSuchMessage:      "no message with this id",
		messageSendDesc:    "Sends someone a message or leaves it for the house: \"Tell Anna I'll be late\", \"Leave a note for the house: …\", \"Send that to my phone\". Mira delivers it as soon as the receiver can be reached (a person over their channels, the house by announcement). With at or in_minutes only at that time, e.g. \"Remind Ben tomorrow at 7 to take out the trash\". Not for a broadcast to everyone present.",
		messageTo:          "Receiver: house (everyone in the household) or a person (key or name).",
		messageSendText:    "What to pass on, in your own words, without the sender.",
		messageSent:        "Message for %s saved; it will be delivered.",
		unknownReceiver:    "I don't know anyone called %s.",
		refusedNone:        "%s doesn't take messages right now.",
		refusedPersons:     "%s only takes messages from persons I recognise.",
		fromPerson:         "%s asks me to tell you: %s",
		fromPersonHouse:    "%s asks me to pass on: %s",
		fromArea:           "Someone in %s asks me to tell you: %s",
		fromAreaHouse:      "Someone in %s asks me to pass on: %s",
		remindFrom:         "%s reminds you: %s",
		textMissing:        "text is missing",
		invalidArgs:        "invalid arguments: %w",
		delegateDesc:       "Hands a question or task to a stronger model when it needs a lot of knowledge, careful thought or several research steps, e.g. explanations, planning, comparisons, recipes. Not for switching, state lookups or small talk. The stronger model can look things up but change nothing. Summarize its result for the user in the channel's style.",
		delegateTask:       "The complete task with all needed context from the conversation; the other model does not know the conversation.",
		calculateDesc:      "Computes an expression exactly. Use it whenever you add up more than two numbers, scale quantities or work out percentages, instead of calculating in your head.",
		calculateExpr:      "The expression with numbers (decimal point, no thousands separator), + - * / and brackets, e.g. 307.93 + 276.77 + 54.86 or 400 * 2 / 4.",
		calculateBad:       "That cannot be computed: %v",
		reminderSetDesc:    "Sets a reminder with a text: at the time Mira speaks the text in the room it was asked in, or sends it to the phone. Give either at or in_minutes. A plain countdown timer (\"timer for 10 minutes\") is HassStartTimer of Home Assistant, not this.",
		reminderText:       "What to remind of, short, e.g. \"take out the trash\".",
		reminderAt:         "Local date and time as YYYY-MM-DD HH:MM, e.g. 2026-10-16 19:00. Work it out from the current time.",
		reminderInMinutes:  "For \"in 20 minutes\": minutes from now, instead of at.",
		reminderFor:        "Optional: key of the person the reminder is for.",
		reminderBadTime:    "time not understood: %w",
		reminderPast:       "That time is in the past.",
		reminderTooFar:     "Reminders go at most a year ahead.",
		reminderSaved:      "Reminder set for %s (id %s).",
		reminderListDesc:   "Lists the open reminders.",
		noReminders:        "No open reminders.",
		reminderCancelDesc: "Deletes an open reminder by its id (from reminder_list).",
		noSuchReminder:     "no open reminder with this id",
		reminderCancelled:  "Reminder deleted.",
		messagesReadDesc:   "Reads out the waiting notifications for this room or person and marks them as heard. Call it when the user asks: \"Do you have a notification?\", \"Anything new?\", \"What's up?\".",
		messagesNothing:    "No new notifications.",
		messagesReadOut:    "Tell the user these notifications, faithfully and completely: ",

		denyBackground:    "Denied: background jobs may only use read-only tools.",
		denyNoConfirm:     "Denied: this needs a confirmation, and nobody can confirm in the background.",
		denyAfterWeb:      "Denied: this request has already read content from the web. Critical actions are therefore locked. Tell the user to give the command again directly.",
		denyStoreFailed:   "Denied: the confirmation could not be recorded. The action was not performed.",
		needsConfirmation: "NOT PERFORMED – confirmation needed. The action (%s) is critical. Ask the user briefly now whether you should perform it, and end your answer with that question. Only when they agree in the next step, call the tool again with the same arguments.",
		asksAfterWeb:      "NOT PERFORMED – question needed. Content from the web was read in this request, so I do not keep or pass on anything without the user's yes (%s). Tell the user briefly and exactly what you would keep or pass on, ask whether you should, and end your answer with that question. Only when they agree in the next step, call the tool again with the same arguments.",
	},
}

func textsFor(l i18n.Lang) texts { return i18n.Pick(catalog, l) }
