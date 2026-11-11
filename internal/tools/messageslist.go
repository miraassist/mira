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
			var b strings.Builder
			for _, n := range messages {
				fmt.Fprintf(&b, t.messageLine, n.ID, n.Importance, n.State, n.Spoken)
				if n.Details != "" {
					fmt.Fprintf(&b, t.messageDetails, n.Details)
				}
				b.WriteByte('\n')
			}
			return strings.TrimRight(b.String(), "\n"), nil
		},
	}
}
