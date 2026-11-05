package conversation

import (
	"context"

	"github.com/miraassist/mira/internal/i18n"
)

// texts are what the engine itself writes: into layer 7, back to the model,
// or to the user when the model fails. What the engine only reads
// (follow-up questions, stopwords) knows both languages at once, since the
// user may speak either.
type texts struct {
	voiceInArea string // %[1]s = area name
	voiceNoArea string
	telegram    string
	background  string
	// notFinished answers a turn the model did not finish: its tool rounds
	// were used up, or it answered nothing.
	notFinished string
	// alreadySaid follows the result of a tool whose round's text was
	// spoken already, so the model does not say it again.
	alreadySaid string
	// streamBroken ends a voice answer whose model failed after part of
	// it was said.
	streamBroken    string
	errorPrefix     string
	unknownTool     string // %s = tool name
	invalidJSONArgs string
	delegated       string
	// delegateLater is the delegate tool's result when the task takes
	// longer than the conversation waits.
	delegateLater string
	waitingForHA  string
	// noHousePrompt answers a turn without Home Assistant's prompt.
	noHousePrompt string
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		voiceInArea:     "Kanal: Sprache, Satellit im Bereich %[1]s. Ohne Raumangabe ist der Bereich %[1]s gemeint.",
		voiceNoArea:     "Kanal: Sprache. Der Raum des Satelliten ist unbekannt; bei Raumbezug nachfragen.",
		telegram:        "Kanal: Telegram, Nutzer ist evtl. nicht zu Hause. Bei Raumbezug nachfragen.",
		background:      "Kanal: Hintergrundjob, kein Mensch im Gespräch.",
		notFinished:     "Ich bin damit nicht ganz fertig geworden.",
		alreadySaid:     "\n\n(Schon gesprochen: „%s“. Wiederhole das nicht; sag nur, was danach noch fehlt, oder nichts.)",
		streamBroken:    "Da ist etwas schiefgegangen.",
		errorPrefix:     "Fehler: ",
		unknownTool:     "Das Werkzeug %s gibt es nicht.",
		invalidJSONArgs: "Die Argumente sind kein gültiges JSON.",
		delegated:       "Abgegebene Aufgabe: Mira hat dir diese Aufgabe gegeben und gibt deine Antwort an den Nutzer weiter. Antworte vollständig und sachlich; du darfst nachschlagen, aber nichts verändern.",
		delegateLater:   "Ergebnis: braucht länger, läuft im Hintergrund weiter. Das Ergebnis kommt als Nachricht, sobald es da ist.",
		waitingForHA:    "Ich warte noch auf die Verbindung mit Home Assistant und weiß deshalb noch nicht, was ich sehen und schalten darf. Bitte versuch es gleich noch einmal.",
		noHousePrompt:   "Mir fehlt gerade die Beschreibung des Hauses von Home Assistant, deshalb kann ich gerade nichts tun. Unter Reparaturen in Home Assistant steht, woran es liegt.",
	},
	i18n.EN: {
		voiceInArea:     "Channel: voice, satellite in area %[1]s. Without a room given, area %[1]s is meant.",
		voiceNoArea:     "Channel: voice. The satellite's room is unknown; ask when a room matters.",
		telegram:        "Channel: Telegram, the user may not be at home. Ask when a room matters.",
		background:      "Channel: background job, no person in the conversation.",
		notFinished:     "I did not quite finish that.",
		alreadySaid:     "\n\n(Already said: \"%s\". Do not repeat it; say only what is still missing, or nothing.)",
		streamBroken:    "Something went wrong.",
		errorPrefix:     "Error: ",
		unknownTool:     "There is no tool %s.",
		invalidJSONArgs: "The arguments are not valid JSON.",
		delegated:       "Delegated task: Mira gave you this task and passes your answer on to the user. Answer completely and factually; you may look things up but must not change anything.",
		delegateLater:   "Result: takes longer, goes on in the background. The result comes as a message as soon as it is there.",
		waitingForHA:    "I am still waiting for the connection to Home Assistant, so I do not know yet what I may see and switch. Please try again in a moment.",
		noHousePrompt:   "I don't have Home Assistant's description of the house right now, so I can't do anything. Repairs in Home Assistant say why.",
	},
}

func (e *Engine) texts(ctx context.Context) texts { return i18n.Pick(catalog, e.lang(ctx)) }

// lang is the language of what is being said now: the turn's, carried in
// ctx, otherwise the one last spoken (docs/spec/language.md).
func (e *Engine) lang(ctx context.Context) i18n.Lang {
	if l, ok := i18n.FromContext(ctx); ok {
		return l
	}
	if e.languages != nil {
		return e.languages.Last(ctx)
	}
	return i18n.Default
}
