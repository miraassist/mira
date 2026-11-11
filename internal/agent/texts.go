package agent

import "github.com/miraassist/mira/internal/i18n"

// texts are what the model reads when a tool call of a run does not run.
type texts struct {
	unknownTool, badArgs, errorPrefix string
	// interrupted is the result of a tool that changes something and was
	// running when the agent stopped.
	interrupted string
	// lastRound closes a run whose tool rounds are used up (Decided 42).
	lastRound string
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		unknownTool: "Unbekanntes Werkzeug %s.",
		badArgs:     "Fehler: Die Argumente sind kein gültiges JSON.",
		errorPrefix: "Fehler: ",
		interrupted: "Ergebnis unbekannt: Der Lauf wurde unterbrochen, während dieses Werkzeug lief. Prüf selbst, ob es gewirkt hat, bevor du es noch einmal aufrufst.",
		lastRound:   "Die Werkzeug-Runden sind aufgebraucht. Antworte jetzt ohne Werkzeuge mit dem, was du gefunden hast, und sag, was offen bleibt.",
	},
	i18n.EN: {
		unknownTool: "Unknown tool %s.",
		badArgs:     "Error: the arguments are not valid JSON.",
		errorPrefix: "Error: ",
		interrupted: "Result unknown: the run was interrupted while this tool was running. Check yourself whether it took effect before calling it again.",
		lastRound:   "The tool rounds are used up. Answer now, without tools, with what you have found, and say what is still open.",
	},
}
