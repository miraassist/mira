package tools

import "github.com/sker65/mira/internal/i18n"

// texts are the descriptions of the internal tools (part of prompt layer 3),
// their results, and the guard's messages to the model.
type texts struct {
	memorySaveDesc, memorySaveText, memorySaveTags, memorySaved string // memorySaved: %s = id
	memorySearchDesc, memorySearchQuery, memoryNothing          string
	memoryForgetDesc, memoryForgotten, noSuchFact               string
	noticesListDesc, noNotices, noticeLine, noticeDetails       string // noticeLine: id, importance, state, text
	noticeAckDesc, noticeAcked, noSuchNotice                    string
	telegramDesc, telegramText, telegramSent, noTelegram        string
	textMissing, invalidArgs                                    string

	denyBackground, denyAfterWeb, denyStoreFailed string
	needsConfirmation                             string // %s = summary of the action
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		memorySaveDesc:    "Merkt sich dauerhaft eine Tatsache über den Haushalt oder den Nutzer, z.B. Vorlieben, Gewohnheiten, wo etwas steht. Nur speichern, wenn der Nutzer es möchte oder es eindeutig dauerhaft nützlich ist.",
		memorySaveText:    "Die Tatsache als vollständiger Satz.",
		memorySaveTags:    "Optionale Stichwörter.",
		memorySaved:       "Gespeichert (id %s).",
		memorySearchDesc:  "Sucht in den gespeicherten Tatsachen über den Haushalt und den Nutzer.",
		memorySearchQuery: "Suchbegriffe.",
		// Said this bluntly on purpose: with a plain "nothing found", local
		// models fill the gap with a plausible guess.
		memoryNothing:    "Nichts gefunden. Dazu ist nichts gespeichert, du weißt es also nicht. Sag dem Nutzer genau das und rate nicht.",
		memoryForgetDesc: "Löscht eine gespeicherte Tatsache anhand ihrer id (aus memory_search).",
		memoryForgotten:  "Gelöscht.",
		noSuchFact:       "keine Tatsache mit dieser id",
		noticesListDesc:  "Listet offene Hinweise und Meldungen (z.B. Gerätestörungen, Nachrichten), die noch nicht erledigt sind.",
		noNotices:        "Keine offenen Hinweise.",
		noticeLine:       "- [%s] Wichtigkeit %d, %s: %s",
		noticeDetails:    " Details: %s",
		noticeAckDesc:    "Markiert einen Hinweis als erledigt, wenn der Nutzer ihn zur Kenntnis genommen hat.",
		noticeAcked:      "Erledigt.",
		noSuchNotice:     "kein Hinweis mit dieser id",
		telegramDesc:     "Schickt dem Nutzer eine Nachricht aufs Handy (Telegram), z.B. eine Liste, einen Link oder eine längere Antwort, die sich nicht gut vorlesen lässt.",
		telegramText:     "Die Nachricht, Markdown erlaubt.",
		telegramSent:     "Gesendet.",
		noTelegram:       "Telegram ist nicht eingerichtet",
		textMissing:      "text fehlt",
		invalidArgs:      "ungültige Argumente: %w",

		denyBackground:    "Abgelehnt: Hintergrundjobs dürfen nur lesende Werkzeuge verwenden.",
		denyAfterWeb:      "Abgelehnt: In dieser Anfrage wurden bereits Inhalte aus dem Web gelesen. Kritische Aktionen sind deshalb gesperrt. Sag dem Nutzer, dass er den Befehl bitte noch einmal direkt geben soll.",
		denyStoreFailed:   "Abgelehnt: Die Bestätigung konnte nicht vorgemerkt werden. Die Aktion wurde nicht ausgeführt.",
		needsConfirmation: "NICHT AUSGEFÜHRT – Bestätigung nötig. Die Aktion (%s) ist kritisch. Frage den Nutzer jetzt kurz, ob du sie ausführen sollst, und beende deine Antwort mit dieser Frage. Erst wenn er im nächsten Schritt zustimmt, rufe das Werkzeug mit denselben Argumenten erneut auf.",
	},
	i18n.EN: {
		memorySaveDesc:    "Permanently remembers a fact about the household or the user, e.g. preferences, habits, where something is. Only save when the user wants it or it is clearly useful for good.",
		memorySaveText:    "The fact as a complete sentence.",
		memorySaveTags:    "Optional keywords.",
		memorySaved:       "Saved (id %s).",
		memorySearchDesc:  "Searches the saved facts about the household and the user.",
		memorySearchQuery: "Search terms.",
		memoryNothing:     "Nothing found. Nothing about this is saved, so you do not know it. Tell the user exactly that and do not guess.",
		memoryForgetDesc:  "Deletes a saved fact by its id (from memory_search).",
		memoryForgotten:   "Deleted.",
		noSuchFact:        "no fact with this id",
		noticesListDesc:   "Lists open notices and messages (e.g. device faults, news) that are not done yet.",
		noNotices:         "No open notices.",
		noticeLine:        "- [%s] importance %d, %s: %s",
		noticeDetails:     " Details: %s",
		noticeAckDesc:     "Marks a notice as done once the user has taken note of it.",
		noticeAcked:       "Done.",
		noSuchNotice:      "no notice with this id",
		telegramDesc:      "Sends the user a message to their phone (Telegram), e.g. a list, a link or a longer answer that does not read aloud well.",
		telegramText:      "The message, Markdown allowed.",
		telegramSent:      "Sent.",
		noTelegram:        "Telegram is not set up",
		textMissing:       "text is missing",
		invalidArgs:       "invalid arguments: %w",

		denyBackground:    "Denied: background jobs may only use read-only tools.",
		denyAfterWeb:      "Denied: this request has already read content from the web. Critical actions are therefore locked. Tell the user to give the command again directly.",
		denyStoreFailed:   "Denied: the confirmation could not be recorded. The action was not performed.",
		needsConfirmation: "NOT PERFORMED – confirmation needed. The action (%s) is critical. Ask the user briefly now whether you should perform it, and end your answer with that question. Only when they agree in the next step, call the tool again with the same arguments.",
	},
}

func textsFor(l i18n.Lang) texts { return i18n.Pick(catalog, l) }
