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
var claims = regexp.MustCompile(`\b(eingeschaltet|ausgeschaltet|angeschaltet|abgeschaltet|angemacht|ausgemacht|gespeichert|gemerkt|notiert|gelöscht|vergessen|gesendet|geschickt|gestartet|gestoppt|eingestellt|gedimmt|geöffnet|aufgeschlossen|abgeschlossen|erledigt|bestätigt|aktiviert|deaktiviert|angelegt|eingerichtet|läuft jetzt|turned (on|off)|switched (on|off)|saved|noted|remembered|deleted|forgotten|sent|started|stopped|dimmed|opened|unlocked|locked|done|confirmed|activated|deactivated|created|set up|is running now)\b`)

// readings matches a measured value ("21,3 Grad", "48 %", "3 kW") and
// states matches a reported state ("Alle Lichter sind aus"). Without a tool
// call in the turn, both are made up: the model knows no states by heart.
// Seen live: asked "Wie war es jetzt im Wohnzimmer?" (the speech recognition
// for "warm"), it answered 21,3 Grad and 48 Prozent without looking; the
// sensors said 23,4 and 40,3.
var (
	readings = regexp.MustCompile(`\d+(?:[,.]\d+)?\s*(?:°|%|(?:grad|prozent|watt|kilowatt(?:stunden)?|kwh|kw|lux|hpa|liter|degrees?|percent)\b)`)
	states   = regexp.MustCompile(`\b(?:ist|sind|is|are)\s+(?:gerade\s+|noch\s+|alle\s+|all\s+)?(?:an|aus|offen|geschlossen|zu|eingeschaltet|ausgeschaltet|on|off|open|closed)\b`)
)

// noNotices matches "keine Benachrichtigungen": whether notices wait is a
// state like any other; seen live without notices_read while one waited.
var noNotices = regexp.MustCompile(`\b(?:keine|no)\s+(?:neuen\s+|new\s+)?(?:benachrichtigung|meldung|nachricht|neuigkeit|notification|notice|message|news)`)

// unbacked reports whether a final answer, given in a turn that called no
// tool, announces an action, claims one as done, or reports a state.
func unbacked(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	return announces.MatchString(t) || claims.MatchString(t) || reportsState(t)
}

// reportsState reports whether the answer gives readings or states.
func reportsState(text string) bool {
	t := strings.ToLower(text)
	return readings.MatchString(t) || states.MatchString(t) || noNotices.MatchString(t)
}

// claimsAction reports whether the answer says an action happened.
func claimsAction(text string) bool {
	return claims.MatchString(strings.ToLower(text))
}
