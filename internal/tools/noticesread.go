package tools

import (
	"context"

	"github.com/sker65/mira/internal/i18n"
)

const ToolNoticesRead = "notices_read"

// NoticeReader reads out what waits for the asker and marks it delivered;
// "" when nothing waits.
type NoticeReader func(ctx context.Context, inv Invocation) (string, error)

// NoticesRead is the tool for "Hast du eine Benachrichtigung?". In a room
// whose notices are only shown on a light, this is how they are heard.
func NoticesRead(l i18n.Lang, read NoticeReader) InternalTool {
	t := textsFor(l)
	return InternalTool{
		Name:        ToolNoticesRead,
		Description: t.noticesReadDesc,
		Schema:      `{"type":"object","properties":{}}`,
		Run: func(ctx context.Context, inv Invocation) (string, error) {
			text, err := read(ctx, inv)
			if err != nil {
				return "", err
			}
			if text == "" {
				return t.noticesNothing, nil
			}
			return t.noticesReadOut + text, nil
		},
	}
}
