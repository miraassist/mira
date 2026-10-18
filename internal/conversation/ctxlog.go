package conversation

import (
	"context"
	"encoding/json"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/prompt"
)

// What the log says about the context (docs/system.md): every call to the
// model with the size of the request, and every turn with what went into it
// that changes from turn to turn. The long, stable start (rules, tools,
// house) shows only when it changes, with its parts' sizes, since a changed
// start is also a broken cache.

// messagesSize is the characters in messages: texts, names and the arguments
// of tool calls.
func messagesSize(msgs []llm.Message) int {
	n := 0
	for _, m := range msgs {
		n += len(m.Content) + len(m.Name)
		for _, c := range m.ToolCalls {
			n += len(c.Function.Name) + len(c.Function.Arguments)
		}
	}
	return n
}

// toolsSize is the number of tool definitions and the characters they take
// as they are sent.
func toolsSize(defs []llm.Tool) (count, chars int) {
	raw, _ := json.Marshal(defs)
	return len(defs), len(raw)
}

// turnsIn counts the turns in a history: each starts with a user message.
func turnsIn(msgs []llm.Message) int {
	n := 0
	for _, m := range msgs {
		if m.Role == llm.RoleUser {
			n++
		}
	}
	return n
}

// logContext says what this turn's request is made of, apart from the stable
// start: how much history, how large the part for this turn, and which of its
// optional pieces are there. When the start differs from the channel's last
// turn, it says that and what it consists of.
func (e *Engine) logContext(ctx context.Context, key string, ch config.Channel, prefix prompt.Prefix, built prompt.Request, history []llm.Message, turn prompt.Turn, layer7 string) {
	attrs := []any{
		"session", key, "channel", string(ch),
		"history_turns", turnsIn(history), "history_messages", len(history), "history_chars", messagesSize(history),
		"turn_chars", len(layer7),
	}
	for _, p := range []struct {
		name string
		n    int
	}{
		{"memories", len(turn.Memories)},
		{"person_profile_chars", len(turn.PersonProfile)},
		{"message_context_chars", len(turn.MessageContext)},
		{"thread_chars", len(turn.Thread)},
		{"extra_chars", len(turn.Extra)},
	} {
		if p.n > 0 {
			attrs = append(attrs, p.name, p.n)
		}
	}
	e.ctxMu.Lock()
	last, seen := e.lastPrefix[ch]
	changed := !seen || last != built.PrefixHash
	e.lastPrefix[ch] = built.PrefixHash
	e.ctxMu.Unlock()
	if changed {
		tools, toolChars := toolsSize(built.Tools)
		attrs = append(attrs, "prefix_changed", true, "prefix_hash", built.PrefixHash, "prefix_was", last,
			"system_chars", len(prefix.SystemText()), "style_chars", len(prefix.Style),
			"examples_messages", len(prefix.Examples), "tools", tools, "tools_chars", toolChars)
	}
	e.log.Info("context built", attrs...)
}
