package conversation

import (
	"strings"
	"unicode"
)

// expectsReply decides whether a spoken answer waits for the user, so the
// satellite keeps listening. A question mark is the clear case. Local models
// also ask for input with a plain request ("Bitte gib an, welches Licht."),
// which is caught by how the last sentence begins.
//
// The sentence before the last counts too: a model may add a filler after
// its question ("Wie viele Beine hat eine Spinne? Ich warte auf deine
// Antwort.").
func expectsReply(text string) bool {
	text = strings.TrimSpace(text)
	if strings.HasSuffix(text, "?") || strings.HasSuffix(beforeLastSentence(text), "?") {
		return true
	}
	last := strings.ToLower(lastSentence(text))
	last = strings.TrimPrefix(last, "bitte ")
	last = strings.TrimPrefix(last, "please ")
	for _, p := range statementOpeners {
		if strings.HasPrefix(last, p) {
			return false
		}
	}
	for _, p := range replyOpeners {
		if strings.HasPrefix(last, p) {
			return true
		}
	}
	return false
}

// replyOpeners start a sentence that asks the user for something.
var replyOpeners = []string{
	"gib an", "gib mir", "sag mir", "sag bitte", "sag mal", "nenne", "nenn mir", "teile mir", "teil mir",
	"wiederhole", "bestätige", "antworte", "sprich", "verrate mir",
	"welche", "welcher", "welches", "wo ", "wohin", "wann", "wie ", "was ", "wer ", "ob du", "soll ich", "möchtest du", "willst du",
	"tell me", "let me know", "confirm", "which", "where", "when", "what", "who", "should i", "do you want",
}

// statementOpeners look like question words but open a statement.
var statementOpeners = []string{"wie gewünscht", "wie besprochen", "wie gewohnt", "wie immer", "was das betrifft", "wo ich kann"}

// beforeLastSentence is the text up to the end of the sentence before the
// last, with its punctuation; "" for a single sentence.
func beforeLastSentence(text string) string {
	text = strings.TrimRightFunc(text, func(r rune) bool { return unicode.IsPunct(r) || unicode.IsSpace(r) })
	if i := strings.LastIndexAny(text, ".!?\n"); i >= 0 {
		return strings.TrimSpace(text[:i+1])
	}
	return ""
}

// lastSentence returns the final sentence, ignoring trailing punctuation.
func lastSentence(text string) string {
	text = strings.TrimRightFunc(text, func(r rune) bool { return unicode.IsPunct(r) || unicode.IsSpace(r) })
	if i := strings.LastIndexAny(text, ".!?\n"); i >= 0 {
		text = text[i+1:]
	}
	return strings.TrimSpace(text)
}
