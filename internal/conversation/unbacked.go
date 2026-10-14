package conversation

import (
	"regexp"
	"strings"
)

// A local model sometimes answers "Ich prüfe die Temperatur." or "Das Licht
// ist eingeschaltet." without calling any tool. The first leaves the user
// with nothing, the second is a lie. The prompt asks the model not to do
// this; this file is the check in code that the plan requires, because a
// claim of success must never rest on the prompt alone.

// announces matches an answer that only says what the model is about to do.
var announces = regexp.MustCompile(`^(ich )?(prüfe|überprüfe|schaue|sehe|frage|erfrage|ermittle|ermittele|hole|rufe|schalte|mache|starte|stelle|speichere|merke|suche)\b`)

// claims matches an answer that reports an action as done.
var claims = regexp.MustCompile(`\b(eingeschaltet|ausgeschaltet|angeschaltet|abgeschaltet|angemacht|ausgemacht|gespeichert|gemerkt|notiert|gelöscht|vergessen|gesendet|geschickt|gestartet|gestoppt|eingestellt|gedimmt|geöffnet|aufgeschlossen|abgeschlossen|erledigt)\b`)

// unbacked reports whether a final answer, given in a turn that called no
// tool, announces an action or claims one as done.
func unbacked(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	return announces.MatchString(t) || claims.MatchString(t)
}

// claimsAction reports whether the answer says an action happened.
func claimsAction(text string) bool {
	return claims.MatchString(strings.ToLower(text))
}

// unbackedNudge goes back to the model once per turn. It is a user-role
// message because chat templates of local models accept a system message
// only at the start; it is never stored in the transcript.
const unbackedNudge = "[Hinweis des Systems, nicht des Nutzers] In dieser Anfrage wurde noch kein Werkzeug aufgerufen. " +
	"Wenn die Anfrage einen Zustand im Haus, eine Aktion oder das Gedächtnis betrifft, rufe jetzt das passende Werkzeug auf. " +
	"Sonst antworte erneut, ohne eine Aktion anzukündigen oder als erledigt zu melden."

// notDoneText replaces an answer that still claims success after the nudge.
const notDoneText = "Das habe ich nicht ausgeführt. Bitte sag es mir noch einmal."
