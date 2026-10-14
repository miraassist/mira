package tools

import (
	"strings"
	"unicode"
)

// Affirmative decides, without the model, whether the user's reply approves a
// pending critical action. It is deliberately strict: anything that is not a
// plain yes — a question, a "no", a new request — does not count, and the
// action has to be asked for again.
func Affirmative(text string) bool {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(words) == 0 || len(words) > 8 {
		return false
	}
	for _, w := range words {
		if negations[w] {
			return false
		}
	}
	if strings.HasSuffix(strings.TrimSpace(text), "?") {
		return false
	}
	joined := strings.Join(words, " ")
	for _, p := range affirmativePhrases {
		if joined == p || strings.HasPrefix(joined, p+" ") {
			return true
		}
	}
	return false
}

var negations = map[string]bool{
	"nein": true, "nicht": true, "nee": true, "nö": true, "no": true, "nope": true,
	"not": true, "don": true, "stopp": true, "stop": true, "abbrechen": true, "lieber": true,
	"warte": true, "doch": false,
}

// affirmativePhrases must start the reply. Longer phrases come first only for
// readability; matching is by whole words.
var affirmativePhrases = []string{
	"ja", "jawohl", "jap", "jep", "jo", "klar", "genau", "richtig", "korrekt", "gerne", "gern",
	"ok", "okay", "okey", "sicher", "bestätigt", "bestätige", "ich bestätige",
	"mach", "mach das", "mach es", "mach mal", "tu es", "tu das", "los", "passt",
	"yes", "yep", "yeah", "sure", "confirm", "confirmed", "do it", "go ahead",
}
