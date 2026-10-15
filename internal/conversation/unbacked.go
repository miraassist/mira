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

// Both languages are checked whatever the configured one is: the model
// answers in the language it is addressed in.

// announces matches an answer that only says what the model is about to do.
var announces = regexp.MustCompile(`^((ich )?(prüfe|überprüfe|schaue|sehe|frage|erfrage|ermittle|ermittele|hole|rufe|schalte|mache|starte|stelle|speichere|merke|suche)\b|(i'll|i will|i'm going to|let me|i am going to) (check|look|turn|switch|set|save|note|search|find|get|start|open|close)\b)`)

// claims matches an answer that reports an action as done.
var claims = regexp.MustCompile(`\b(eingeschaltet|ausgeschaltet|angeschaltet|abgeschaltet|angemacht|ausgemacht|gespeichert|gemerkt|notiert|gelöscht|vergessen|gesendet|geschickt|gestartet|gestoppt|eingestellt|gedimmt|geöffnet|aufgeschlossen|abgeschlossen|erledigt|turned (on|off)|switched (on|off)|saved|noted|remembered|deleted|forgotten|sent|started|stopped|dimmed|opened|unlocked|locked|done)\b`)

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
