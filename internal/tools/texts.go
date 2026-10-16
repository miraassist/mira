package tools

import "github.com/sker65/mira/internal/i18n"

// texts are the descriptions of the internal tools (part of prompt layer 3),
// their results, and the guard's messages to the model.
type texts struct {
	memorySaveDesc, memorySaveText, memorySaveTags, memorySaved                          string // memorySaved: %s = id
	memorySaveHouse, memorySavedPerson, personalMark                                     string // memorySavedPerson: %s = person, %s = id
	memorySearchDesc, memorySearchQuery, memoryNothing                                   string
	memoryForgetDesc, memoryForgotten, noSuchFact                                        string
	messagesListDesc, noMessages, messageLine, messageDetails                            string // messageLine: id, importance, state, text
	messageAckDesc, messageAcked, noSuchMessage                                          string
	telegramDesc, telegramText, telegramSent, noTelegram                                 string
	textMissing, invalidArgs                                                             string
	delegateDesc, delegateTask                                                           string
	reminderSetDesc, reminderText, reminderAt, reminderInMinutes, reminderFor            string
	reminderBadTime, reminderPast, reminderTooFar, reminderSaved                         string // reminderSaved: time, id
	reminderListDesc, noReminders, reminderCancelDesc, noSuchReminder, reminderCancelled string
	messagesReadDesc, messagesNothing, messagesReadOut                                   string

	denyBackground, denyAfterWeb, denyStoreFailed string
	needsConfirmation                             string // %s = summary of the action
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		memorySaveDesc:    "Merkt sich dauerhaft eine Tatsache über den Haushalt oder den Nutzer, z.B. Vorlieben, Gewohnheiten, wo etwas steht. Nur speichern, wenn der Nutzer es möchte oder es eindeutig dauerhaft nützlich ist. Ist bekannt, wer spricht, kommt es in dessen persönliches Gedächtnis, sonst ins Gedächtnis des Hauses.",
		memorySaveHouse:   "true nur, wenn der Nutzer ausdrücklich sagt, dass es für alle im Haus gilt (\"fürs Haus\", \"für alle\"). Sonst weglassen.",
		memorySavedPerson: "Gespeichert im persönlichen Gedächtnis von %s (id %s).",
		personalMark:      " (persönlich)",
		memorySaveText:    "Die Tatsache als vollständiger Satz.",
		memorySaveTags:    "Optionale Stichwörter.",
		memorySaved:       "Gespeichert im Gedächtnis des Hauses (id %s).",
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
		telegramDesc:       "Schickt dem Nutzer eine Nachricht aufs Handy (Telegram), z.B. eine Liste, einen Link oder eine längere Antwort, die sich nicht gut vorlesen lässt.",
		telegramText:       "Die Nachricht, Markdown erlaubt.",
		telegramSent:       "Gesendet.",
		noTelegram:         "Telegram ist nicht eingerichtet",
		textMissing:        "text fehlt",
		invalidArgs:        "ungültige Argumente: %w",
		delegateDesc:       "Gibt eine Frage oder Aufgabe an ein stärkeres Modell ab, wenn sie viel Wissen, gründliches Nachdenken oder mehrere Rechercheschritte braucht, z.B. Erklärungen, Planungen, Vergleiche, Rezepte. Nicht für Schaltbefehle, Zustandsabfragen oder Smalltalk. Das stärkere Modell kann nachschlagen, aber nichts verändern. Fasse sein Ergebnis für den Nutzer im Stil des Kanals zusammen.",
		delegateTask:       "Die vollständige Aufgabe mit allem nötigen Kontext aus dem Gespräch; das andere Modell kennt das Gespräch nicht.",
		reminderSetDesc:    "Stellt eine Erinnerung oder einen Timer: Zur Zeit sagt Mira den Text im Raum an, in dem gefragt wurde, oder schickt ihn aufs Handy. Gib entweder at oder in_minutes an.",
		reminderText:       "Woran erinnert werden soll, kurz, z.B. \"Müll rausbringen\".",
		reminderAt:         "Datum und Uhrzeit in Ortszeit als YYYY-MM-DD HH:MM, z.B. 2026-10-16 19:00. Rechne es aus der aktuellen Zeit aus.",
		reminderInMinutes:  "Für Timer und \"in 20 Minuten\": Minuten ab jetzt, statt at.",
		reminderFor:        "Optional: Kürzel der Person, für die die Erinnerung ist.",
		reminderBadTime:    "Zeit nicht verstanden: %w",
		reminderPast:       "Diese Zeit liegt in der Vergangenheit.",
		reminderTooFar:     "Erinnerungen gehen höchstens ein Jahr voraus.",
		reminderSaved:      "Erinnerung gestellt für %s (id %s).",
		reminderListDesc:   "Listet die offenen Erinnerungen und Timer.",
		noReminders:        "Keine offenen Erinnerungen.",
		reminderCancelDesc: "Löscht eine offene Erinnerung anhand ihrer id (aus reminder_list).",
		noSuchReminder:     "keine offene Erinnerung mit dieser id",
		reminderCancelled:  "Erinnerung gelöscht.",
		messagesReadDesc:   "Liest die wartenden Benachrichtigungen für diesen Raum oder diese Person vor und markiert sie als gehört. Rufe es auf, wenn der Nutzer fragt: \"Hast du eine Benachrichtigung?\", \"Gibt es was Neues?\", \"Was gibt's?\".",
		messagesNothing:    "Keine neuen Benachrichtigungen.",
		messagesReadOut:    "Sag dem Nutzer diese Benachrichtigungen, sinngemäß und vollständig: ",

		denyBackground:    "Abgelehnt: Hintergrundjobs dürfen nur lesende Werkzeuge verwenden.",
		denyAfterWeb:      "Abgelehnt: In dieser Anfrage wurden bereits Inhalte aus dem Web gelesen. Kritische Aktionen sind deshalb gesperrt. Sag dem Nutzer, dass er den Befehl bitte noch einmal direkt geben soll.",
		denyStoreFailed:   "Abgelehnt: Die Bestätigung konnte nicht vorgemerkt werden. Die Aktion wurde nicht ausgeführt.",
		needsConfirmation: "NICHT AUSGEFÜHRT – Bestätigung nötig. Die Aktion (%s) ist kritisch. Frage den Nutzer jetzt kurz, ob du sie ausführen sollst, und beende deine Antwort mit dieser Frage. Erst wenn er im nächsten Schritt zustimmt, rufe das Werkzeug mit denselben Argumenten erneut auf.",
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
		telegramDesc:       "Sends the user a message to their phone (Telegram), e.g. a list, a link or a longer answer that does not read aloud well.",
		telegramText:       "The message, Markdown allowed.",
		telegramSent:       "Sent.",
		noTelegram:         "Telegram is not set up",
		textMissing:        "text is missing",
		invalidArgs:        "invalid arguments: %w",
		delegateDesc:       "Hands a question or task to a stronger model when it needs a lot of knowledge, careful thought or several research steps, e.g. explanations, planning, comparisons, recipes. Not for switching, state lookups or small talk. The stronger model can look things up but change nothing. Summarize its result for the user in the channel's style.",
		delegateTask:       "The complete task with all needed context from the conversation; the other model does not know the conversation.",
		reminderSetDesc:    "Sets a reminder or timer: at the time Mira speaks the text in the room it was asked in, or sends it to the phone. Give either at or in_minutes.",
		reminderText:       "What to remind of, short, e.g. \"take out the trash\".",
		reminderAt:         "Local date and time as YYYY-MM-DD HH:MM, e.g. 2026-10-16 19:00. Work it out from the current time.",
		reminderInMinutes:  "For timers and \"in 20 minutes\": minutes from now, instead of at.",
		reminderFor:        "Optional: key of the person the reminder is for.",
		reminderBadTime:    "time not understood: %w",
		reminderPast:       "That time is in the past.",
		reminderTooFar:     "Reminders go at most a year ahead.",
		reminderSaved:      "Reminder set for %s (id %s).",
		reminderListDesc:   "Lists the open reminders and timers.",
		noReminders:        "No open reminders.",
		reminderCancelDesc: "Deletes an open reminder by its id (from reminder_list).",
		noSuchReminder:     "no open reminder with this id",
		reminderCancelled:  "Reminder deleted.",
		messagesReadDesc:   "Reads out the waiting notifications for this room or person and marks them as heard. Call it when the user asks: \"Do you have a notification?\", \"Anything new?\", \"What's up?\".",
		messagesNothing:    "No new notifications.",
		messagesReadOut:    "Tell the user these notifications, faithfully and completely: ",

		denyBackground:    "Denied: background jobs may only use read-only tools.",
		denyAfterWeb:      "Denied: this request has already read content from the web. Critical actions are therefore locked. Tell the user to give the command again directly.",
		denyStoreFailed:   "Denied: the confirmation could not be recorded. The action was not performed.",
		needsConfirmation: "NOT PERFORMED – confirmation needed. The action (%s) is critical. Ask the user briefly now whether you should perform it, and end your answer with that question. Only when they agree in the next step, call the tool again with the same arguments.",
	},
}

func textsFor(l i18n.Lang) texts { return i18n.Pick(catalog, l) }
