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
		tool := MessagesRead(lang, func(context.Context, Invocation) (Reading, error) { return Reading{Text: msg}, nil })
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

// docs/spec/messages.md, MS-10: with entries that go on in a skill, the model
// says what waits and asks what to start with; each lead with its thread.
func TestMS10_DifferentKindsAreNamedAndTheUserChooses(t *testing.T) {
	tool := MessagesRead(i18n.DE, func(context.Context, Invocation) (Reading, error) {
		return Reading{Text: "Der Gasverbrauch ist hoch.", Leads: []Lead{
			{Line: "Elternbrief, 2 Termine", Skill: "postfach", Thread: "t1"},
			{Line: "Oma, heute 14:12", Skill: "anrufbeantworter"}}}, nil
	})
	out, err := tool.Run(context.Background(), Invocation{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"womit du anfangen sollst", "thread_take", "- Elternbrief, 2 Termine (Skill postfach, Vorgang t1)",
		"- Oma, heute 14:12 (Skill anrufbeantworter)", "Der Gasverbrauch ist hoch."} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	nothing := MessagesRead(i18n.DE, func(context.Context, Invocation) (Reading, error) { return Reading{}, nil })
	if out, _ := nothing.Run(context.Background(), Invocation{}); out != "Keine neuen Nachrichten." {
		t.Errorf("nothing: %q", out)
	}
}
