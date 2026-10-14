package prompt

import (
	"github.com/sker65/lares-agent/internal/llm"
)

// Request is the assembled prompt, ready to be handed to the LLM client.
type Request struct {
	Messages []llm.Message
	Tools    []llm.Tool
	// PrefixHash is the fingerprint of layers 1 to 5, carried into telemetry.
	PrefixHash string
}

// Build assembles the final message list in layer order.
//
// The system message holds layers 1, 2, 4 and 5; the tools field holds layer
// 3; history is layer 6 and is appended unchanged; the rendered turn is layer
// 7 and is always last. Nothing is inserted between history and the turn, so
// the cached prefix extends as far as the end of the history.
func Build(p Prefix, history []llm.Message, turn Turn) Request {
	msgs := make([]llm.Message, 0, len(history)+2)
	msgs = append(msgs, p.SystemMessage())
	msgs = append(msgs, history...)
	msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: turn.Render()})
	return Request{Messages: msgs, Tools: p.Tools, PrefixHash: p.Hash()}
}

// BuildWithUserText is Build for callers that already have the rendered turn
// text, such as a replay of a stored history.
func BuildWithUserText(p Prefix, history []llm.Message, userText string) Request {
	msgs := make([]llm.Message, 0, len(history)+2)
	msgs = append(msgs, p.SystemMessage())
	msgs = append(msgs, history...)
	msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: userText})
	return Request{Messages: msgs, Tools: p.Tools, PrefixHash: p.Hash()}
}

// WarmupRequest is the smallest request that still carries the full cached
// prefix, used to populate a slot's KV cache before the first real turn.
func WarmupRequest(p Prefix) Request {
	return Request{
		Messages:   []llm.Message{p.SystemMessage()},
		Tools:      p.Tools,
		PrefixHash: p.Hash(),
	}
}
