package agent

import "github.com/miraassist/mira/internal/i18n"

// texts are what the model reads when a tool call of a run does not run.
type texts struct {
	unknownTool, noRight, badArgs, errorPrefix string
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		unknownTool: "Unbekanntes Werkzeug %s.",
		noRight:     "Abgelehnt: Dieser Lauf darf das nicht (es fehlt das Recht %s). Sag das, statt es anders zu versuchen.",
		badArgs:     "Fehler: Die Argumente sind kein gültiges JSON.",
		errorPrefix: "Fehler: ",
	},
	i18n.EN: {
		unknownTool: "Unknown tool %s.",
		noRight:     "Refused: this run may not do that (it lacks the right %s). Say so instead of trying another way.",
		badArgs:     "Error: the arguments are not valid JSON.",
		errorPrefix: "Error: ",
	},
}
