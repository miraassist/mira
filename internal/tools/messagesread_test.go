package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/i18n"
)

// docs/spec/messages.md, Delivery: read out on request, the message comes
// whole, and the model is told to keep dates, times, places, amounts,
// deadlines and names exactly, also with several appointments.
func TestMessagesReadKeepsTheFactsExact(t *testing.T) {
	msg := "Ich habe 3 Termine gefunden: Gespräch, Do 19.11.2026 12:00; Gespräch, Fr 20.11.2026 15:00. Soll ich sie eintragen?"
	for lang, words := range map[i18n.Lang][]string{
		i18n.DE: {"Uhrzeit", "Orte", "Beträge", "Fristen", "Namen", "genau so", "mehreren Terminen"},
		i18n.EN: {"times", "places", "amounts", "deadlines", "names", "exactly", "several appointments"},
	} {
		tool := MessagesRead(lang, func(context.Context, Invocation) (string, error) { return msg, nil })
		out, err := tool.Run(context.Background(), Invocation{})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(out, msg) {
			t.Errorf("%s: the message is not passed whole: %q", lang, out)
		}
		for _, w := range words {
			if !strings.Contains(out[:len(out)-len(msg)], w) {
				t.Errorf("%s: the instruction does not name %q: %q", lang, w, out)
			}
		}
	}
}
