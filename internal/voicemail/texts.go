package voicemail

import "github.com/miraassist/mira/internal/i18n"

// texts are the tools' descriptions and results: facts for the model, which
// words them itself.
type texts struct {
	listDesc, playDesc, deleteDesc, argID, argIDs, argNew  string
	noMessages, messagesHead, isNew, heard, minutes        string
	noMissed, missedHead, missedLine                       string
	notVoice, noSpeaker, unknown, playing, screen, deleted string
	playingMany, next, planHead                            string
	today, yesterday                                       string
	missing, boxFailed, newsLine, onlyNewest               string
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		listDesc:     "Die Nachrichten auf dem Anrufbeantworter (von wem, wann, wie lang, neu oder gehört, die neuesten zuerst) und die verpassten Anrufe, die noch nicht genannt wurden. Danach gelten die verpassten Anrufe als gesehen.",
		playDesc:     "Spielt Nachrichten des Anrufbeantworters am Satelliten ab, an dem gerade gesprochen wird, direkt nach deiner Antwort, mehrere nacheinander; jede gilt danach als gehört. Will der Nutzer mehrere oder alle hören, gib alle ids in einem Aufruf, in der gewünschten Reihenfolge.",
		deleteDesc:   "Löscht eine Nachricht auf dem Anrufbeantworter. Nur, wenn der Nutzer es ausdrücklich will.",
		argID:        "Die id der Nachricht aus voicemail_list, z. B. 0:13.",
		argIDs:       "Die ids der Nachrichten aus voicemail_list, in der Reihenfolge zum Abspielen, z. B. [\"0:13\"].",
		argNew:       "true: nur neue Nachrichten.",
		noMessages:   "Nachrichten: keine.",
		messagesHead: "Nachrichten (%d):",
		isNew:        "neu",
		heard:        "gehört",
		minutes:      "bis %d Min.",
		noMissed:     "Verpasste Anrufe: keine neuen.",
		missedHead:   "Verpasste Anrufe, ohne Nachricht (jetzt als gesehen markiert):",
		missedLine:   "%s, %d-mal, zuletzt %s",
		notVoice:     "Ergebnis: nichts abgespielt. Grund: Nachrichten spielen nur an einem Satelliten; hier geht nur die Liste.",
		noSpeaker:    "Ergebnis: nichts abgespielt. Grund: Dieser Satellit hat keinen Lautsprecher, auf dem Mira abspielen kann.",
		unknown:      "Ergebnis: nichts abgespielt. Grund: Eine Nachricht %s gibt es nicht; hol die ids mit voicemail_list.",
		playing:      "Ergebnis: Die Nachricht von %s (%s) spielt am Satelliten direkt nach deiner Antwort. Sag jetzt nur kurz, von wem und wann sie ist, und sonst nichts, keine Frage.",
		playingMany:  "Ergebnis: %d Nachrichten spielen am Satelliten nacheinander, direkt nach deiner Antwort; vor jeder weiteren sagt der Satellit selbst, von wem sie ist. Die erste ist von %s (%s). Sag jetzt nur kurz, wie viele es sind und von wem und wann die erste ist, und sonst nichts, keine Frage.",
		next:         "Nächste: von %s, %s.",
		planHead:     "# Anrufbeantworter",
		screen:       "Nachricht von %s · %s",
		deleted:      "Ergebnis: Die Nachricht ist gelöscht.",
		today:        "heute %s",
		yesterday:    "gestern %s",
		missing:      "Die Integration Mira FRITZ!Box fehlt. Installiere sie über HACS und richte sie ein; dann liest Mira dort den Anrufbeantworter.",
		boxFailed:    "Ergebnis: Der Anrufbeantworter ließ sich nicht lesen (%s).",
		onlyNewest:   " Nenn dem Nutzer nur die neuesten %d (die ersten unten) mit Absender und Zeit und sag, dass es %d weitere gibt; abspielen geht jede.",
		newsLine:     "Auf dem Anrufbeantworter: %d neue Nachrichten, %d neue verpasste Anrufe. Einzelheiten und Abspielen mit dem Skill anrufbeantworter.",
	},
	i18n.EN: {
		listDesc:     "The messages on the answering machine (from whom, when, how long, new or heard, newest first) and the missed calls not told yet. Afterwards the missed calls count as seen.",
		playDesc:     "Plays messages of the answering machine at the satellite being spoken at, right after your answer, several one after the other; each counts as heard afterwards. When the user wants to hear several or all, give all ids in one call, in the order wanted.",
		deleteDesc:   "Deletes a message on the answering machine. Only when the user asks for it.",
		argID:        "The message's id from voicemail_list, e.g. 0:13.",
		argIDs:       "The messages' ids from voicemail_list, in the order to play, e.g. [\"0:13\"].",
		argNew:       "true: only new messages.",
		noMessages:   "Messages: none.",
		messagesHead: "Messages (%d):",
		isNew:        "new",
		heard:        "heard",
		minutes:      "up to %d min",
		noMissed:     "Missed calls: no new ones.",
		missedHead:   "Missed calls, without a message (now marked as seen):",
		missedLine:   "%s, %d times, last %s",
		notVoice:     "Result: nothing played. Reason: messages play only at a satellite; here only the list works.",
		noSpeaker:    "Result: nothing played. Reason: this satellite has no speaker Mira can play on.",
		unknown:      "Result: nothing played. Reason: there is no message %s; get the ids with voicemail_list.",
		playing:      "Result: the message from %s (%s) plays at the satellite right after your answer. Now say only briefly from whom and when it is, nothing else, no question.",
		playingMany:  "Result: %d messages play at the satellite one after the other, right after your answer; before each further one the satellite says itself whom it is from. The first is from %s (%s). Now say only briefly how many there are and from whom and when the first is, nothing else, no question.",
		next:         "Next: from %s, %s.",
		planHead:     "# Answering machine",
		screen:       "Message from %s · %s",
		deleted:      "Result: the message is deleted.",
		today:        "today %s",
		yesterday:    "yesterday %s",
		missing:      "The integration Mira FRITZ!Box is missing. Install it through HACS and set it up; Mira then reads the answering machine there.",
		boxFailed:    "Result: the answering machine could not be read (%s).",
		onlyNewest:   " Tell the user only the newest %d (the first below) with caller and time, and that there are %d more; any of them can be played.",
		newsLine:     "On the answering machine: %d new messages, %d new missed calls. Details and playing with the skill anrufbeantworter.",
	},
}
