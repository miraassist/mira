package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/prompt"
	"github.com/miraassist/mira/internal/store"
)

// Assess runs one stateless request on the background slot: the background
// prefix (so the slot's cache stays warm), the instruction as layer 7, no
// tool use, and an answer constrained to the given JSON schema. Watchers use
// it to rate an event and word the message.
func (e *Engine) Assess(ctx context.Context, instruction string, schema json.RawMessage) (string, error) {
	lock := e.locks[config.ChannelBackground]
	lock.Lock()
	defer lock.Unlock()

	prefix, err := e.Prefix(ctx, config.ChannelBackground)
	if err != nil {
		return "", err
	}
	turn := prompt.Turn{
		Lang:           e.lang(ctx),
		Now:            e.clock().In(e.loc),
		ChannelContext: e.channelContext(ctx, Request{Channel: config.ChannelBackground}, store.Session{}, e.structure()),
		Text:           instruction,
	}
	built := prompt.Build(prefix, nil, turn)
	req := e.chatRequest(config.ChannelBackground, built.Messages, built)
	req.ToolChoice = "none"
	if len(schema) > 0 {
		req.ResponseFormat = map[string]any{
			"type":        "json_schema",
			"json_schema": map[string]any{"name": "assessment", "schema": schema},
		}
	}
	client, _, _ := e.modelFor(e.cfg.RouteFor(config.ChannelBackground))
	out, err := client.Chat(ctx, req)
	if err != nil {
		return "", err
	}
	text := stripThinking(out.Message().Content)
	if text == "" {
		return "", errors.New("empty assessment")
	}
	return ExtractJSON(text), nil
}

// ExtractJSON returns the outermost JSON object in s. Models that ignore the
// response format still tend to wrap the object in prose or a code fence.
func ExtractJSON(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end < start {
		return s
	}
	return s[start : end+1]
}
