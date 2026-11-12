package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/store"
)

// MessageLister returns the open messages the asker of inv may hear
// (docs/spec/messages.md, Who sees which open messages).
type MessageLister func(ctx context.Context, inv Invocation) ([]store.Message, error)

// MessagesList is the tool for "Welche Nachrichten sind offen?": what the
// asker may hear, with ids for message_ack.
func MessagesList(l i18n.Lang, list MessageLister) InternalTool {
	t := textsFor(l)
	return InternalTool{
		Name:        ToolMessagesList,
		Description: t.messagesListDesc,
		Schema:      `{"type":"object","properties":{}}`,
		ReadOnly:    true,
		Run: func(ctx context.Context, inv Invocation) (string, error) {
			messages, err := list(ctx, inv)
			if err != nil {
				return "", err
			}
			if len(messages) == 0 {
				return t.noMessages, nil
			}
			// One line per entry, with the entry's id: message_ack takes
			// one entry out (docs/spec/messages.md, decision 18).
			var b strings.Builder
			for _, n := range messages {
				if len(n.Entries) == 0 {
					n.Entries = []store.Entry{{ID: n.ID, Importance: n.Importance, Text: n.Spoken, Details: n.Details}}
				}
				for _, e := range n.Entries {
					fmt.Fprintf(&b, t.messageLine, e.ID, e.Importance, n.State, e.Text)
					if e.Details != "" {
						fmt.Fprintf(&b, t.messageDetails, e.Details)
					}
					b.WriteByte('\n')
				}
			}
			return strings.TrimRight(b.String(), "\n"), nil
		},
	}
}
