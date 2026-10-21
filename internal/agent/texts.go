package agent

import "github.com/miraassist/mira/internal/i18n"

// texts are what the model reads when a tool call of a run does not run.
type texts struct {
	unknownTool, noRight, badArgs, errorPrefix string
	// interrupted is the result of a tool that changes something and was
	// running when the agent stopped.
	interrupted string
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		unknownTool: "Unbekanntes Werkzeug %s.",
		noRight:     "Abgelehnt: Dieser Lauf darf das nicht (es fehlt das Recht %s). Sag das, statt es anders zu versuchen.",
		badArgs:     "Fehler: Die Argumente sind kein gültiges JSON.",
		errorPrefix: "Fehler: ",
		interrupted: "Ergebnis unbekannt: Der Lauf wurde unterbrochen, während dieses Werkzeug lief. Prüf selbst, ob es gewirkt hat, bevor du es noch einmal aufrufst.",
	},
	i18n.EN: {
		unknownTool: "Unknown tool %s.",
		noRight:     "Refused: this run may not do that (it lacks the right %s). Say so instead of trying another way.",
		badArgs:     "Error: the arguments are not valid JSON.",
		errorPrefix: "Error: ",
		interrupted: "Result unknown: the run was interrupted while this tool was running. Check yourself whether it took effect before calling it again.",
	},
}
