package tools

import (
	"context"

	"github.com/miraassist/mira/internal/i18n"
)

const ToolMessagesRead = "messages_read"

// MessageReader reads out what waits for the asker and marks it delivered;
// "" when nothing waits.
type MessageReader func(ctx context.Context, inv Invocation) (string, error)

// MessagesRead is the tool for "Hast du eine Benachrichtigung?". In a room
// whose messages are only shown on a light, this is how they are heard.
func MessagesRead(l i18n.Lang, read MessageReader) InternalTool {
	t := textsFor(l)
	return InternalTool{
		Name:        ToolMessagesRead,
		Description: t.messagesReadDesc,
		Schema:      `{"type":"object","properties":{}}`,
		Run: func(ctx context.Context, inv Invocation) (string, error) {
			text, err := read(ctx, inv)
			if err != nil {
				return "", err
			}
			if text == "" {
				return t.messagesNothing, nil
			}
			return t.messagesReadOut + text, nil
		},
	}
}
