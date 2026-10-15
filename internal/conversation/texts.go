package conversation

import "github.com/sker65/mira/internal/i18n"

// texts are what the engine itself writes: into layer 7, back to the model,
// or to the user when the model fails. What the engine only reads (the
// checks for unbacked claims, follow-up questions, stopwords) knows both
// languages at once, since the user may speak either.
type texts struct {
	voiceInArea     string // %[1]s = area name
	voiceNoArea     string
	telegram        string
	background      string
	fallback        string
	notDone         string
	unbackedNudge   string
	errorPrefix     string
	unknownTool     string // %s = tool name
	invalidJSONArgs string
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		voiceInArea:     "Kanal: Sprache, Satellit im Bereich %[1]s. Ohne Raumangabe ist der Bereich %[1]s gemeint.",
		voiceNoArea:     "Kanal: Sprache. Der Raum des Satelliten ist unbekannt; bei Raumbezug nachfragen.",
		telegram:        "Kanal: Telegram, Nutzer ist evtl. nicht zu Hause. Bei Raumbezug nachfragen.",
		background:      "Kanal: Hintergrundjob, kein Mensch im Gespräch.",
		fallback:        "Entschuldigung, darauf habe ich gerade keine Antwort.",
		notDone:         "Das habe ich nicht ausgeführt. Bitte sag es mir noch einmal.",
		unbackedNudge:   "[Hinweis des Systems, nicht des Nutzers] In dieser Anfrage wurde noch kein Werkzeug aufgerufen. Wenn die Anfrage einen Zustand im Haus, eine Aktion oder das Gedächtnis betrifft, rufe jetzt das passende Werkzeug auf. Sonst antworte erneut, ohne eine Aktion anzukündigen oder als erledigt zu melden.",
		errorPrefix:     "Fehler: ",
		unknownTool:     "Das Werkzeug %s gibt es nicht.",
		invalidJSONArgs: "Die Argumente sind kein gültiges JSON.",
	},
	i18n.EN: {
		voiceInArea:     "Channel: voice, satellite in area %[1]s. Without a room given, area %[1]s is meant.",
		voiceNoArea:     "Channel: voice. The satellite's room is unknown; ask when a room matters.",
		telegram:        "Channel: Telegram, the user may not be at home. Ask when a room matters.",
		background:      "Channel: background job, no person in the conversation.",
		fallback:        "Sorry, I have no answer to that right now.",
		notDone:         "I did not do that. Please tell me again.",
		unbackedNudge:   "[Note from the system, not the user] No tool has been called in this request yet. If the request concerns a state in the house, an action or the memory, call the matching tool now. Otherwise answer again without announcing an action or reporting one as done.",
		errorPrefix:     "Error: ",
		unknownTool:     "There is no tool %s.",
		invalidJSONArgs: "The arguments are not valid JSON.",
	},
}

func (e *Engine) texts() texts { return i18n.Pick(catalog, i18n.Of(e.cfg.Prompt.Language)) }

func (e *Engine) lang() i18n.Lang { return i18n.Of(e.cfg.Prompt.Language) }
