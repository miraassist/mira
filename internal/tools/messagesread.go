package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/miraassist/mira/internal/i18n"
)

const ToolMessagesRead = "messages_read"

// Reading is what waits for the asker (docs/spec/messages.md, Kinds of
// messages, step 6): the messages to read out as one text, and the entries
// of kinds with a skill, each with its thread.
type Reading struct {
	Text  string
	Leads []Lead
}

// Lead is an entry that goes on in a skill, in its thread ("" for none).
type Lead struct {
	Line, Skill, Thread string
}

// MessageReader reads what waits for the asker and marks it delivered; an
// empty Reading when nothing waits.
type MessageReader func(ctx context.Context, inv Invocation) (Reading, error)

// MessagesRead is the tool for "Hab ich neue Nachrichten?". With only
// messages to read out, the model says them; with entries that go on in a
// skill, it says what waits and asks what to start with (decision 8).
func MessagesRead(l i18n.Lang, read MessageReader) InternalTool {
	t := textsFor(l)
	return InternalTool{
		Name:        ToolMessagesRead,
		Description: t.messagesReadDesc,
		Schema:      `{"type":"object","properties":{}}`,
		Run: func(ctx context.Context, inv Invocation) (string, error) {
			r, err := read(ctx, inv)
			if err != nil {
				return "", err
			}
			switch {
			case r.Text == "" && len(r.Leads) == 0:
				return t.messagesNothing, nil
			case len(r.Leads) == 0:
				return t.messagesReadOut + r.Text, nil
			}
			var b strings.Builder
			b.WriteString(t.messagesChoose)
			for _, ld := range r.Leads {
				if ld.Thread != "" {
					fmt.Fprintf(&b, t.leadLine, ld.Line, ld.Skill, ld.Thread)
				} else {
					fmt.Fprintf(&b, t.leadLineNoThread, ld.Line, ld.Skill)
				}
			}
			if r.Text != "" {
				b.WriteString(t.messagesAlso + r.Text)
			}
			return strings.TrimRight(b.String(), "\n"), nil
		},
	}
}
