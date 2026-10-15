package prompt

import (
	"encoding/json"
	"strings"

	"github.com/sker65/mira/internal/config"
	"github.com/sker65/mira/internal/i18n"
	"github.com/sker65/mira/internal/llm"
)

// The seven layers, from stable to volatile. The numbers are the ones used in
// telemetry and in the plan.
const (
	LayerAgent     = 1 // role, rules, safety — changes on release
	LayerStyle     = 2 // per-channel style — fixed per channel
	LayerTools     = 3 // tool definitions — changes with the MCP servers
	LayerStructure = 4 // HA areas, devices, exposed entities — no states
	LayerProfile   = 5 // compacted user profile
	LayerHistory   = 6 // previous turns, append-only
	LayerTurn      = 7 // this request: time, channel context, memories, text
)

// StyleFor returns the layer 2 block of a channel.
func StyleFor(l i18n.Lang, ch config.Channel) string {
	t := textsFor(l)
	switch ch {
	case config.ChannelVoice:
		return t.styleVoice
	case config.ChannelBackground:
		return t.styleBackground
	default:
		return t.styleTelegram
	}
}

// AgentLayer returns layer 1, with the configured persona appended. The
// persona is part of the cached prefix, so it must be static text.
func AgentLayer(l i18n.Lang, persona string) string {
	agent := textsFor(l).agent
	persona = strings.TrimSpace(persona)
	if persona == "" {
		return agent
	}
	return agent + "\n\n" + persona
}

// Prefix holds layers 1 to 5: everything that is expected to stay
// byte-identical between requests of the same channel.
type Prefix struct {
	// Lang is the language of the section headers.
	Lang      i18n.Lang
	Agent     string
	Style     string
	Tools     []llm.Tool
	Structure string
	Profile   string
	// Examples follow the system message: a fixed sample exchange that shows
	// the model how tools are used here. See Examples.
	Examples []llm.Message
}

// SystemText renders layers 1, 4 and 5 into the single system message.
//
// Layer 3 is not here: tool definitions travel in the request's tools field,
// where the server's chat template renders them into the prompt — after the
// system text, for the Qwen and Llama templates. Layer 2 is not here either:
// the channel style follows the tools and the examples as its own message.
// That way every channel shares the same start, about nine thousand tokens
// of rules, house and tools, and a server that picks slots by common prefix
// (LM Studio does) computes it once instead of once per channel.
func (p Prefix) SystemText() string {
	t := textsFor(p.Lang)
	var b strings.Builder
	b.WriteString(p.Agent)
	if s := strings.TrimSpace(p.Structure); s != "" {
		b.WriteString("\n\n")
		b.WriteString(t.headerStructure)
		b.WriteString("\n")
		b.WriteString(s)
	}
	if s := strings.TrimSpace(p.Profile); s != "" {
		b.WriteString("\n\n")
		b.WriteString(t.headerProfile)
		b.WriteString("\n")
		b.WriteString(s)
	}
	return b.String()
}

// StyleMessages is layer 2 as it is sent: the channel style and its fixed
// acknowledgement, placed after the shared start. A user message needs an
// answer before the history continues, or the template would merge it with
// the first history turn.
func (p Prefix) StyleMessages() []llm.Message {
	if strings.TrimSpace(p.Style) == "" {
		return nil
	}
	return []llm.Message{
		{Role: llm.RoleUser, Content: p.Style},
		{Role: llm.RoleAssistant, Content: textsFor(p.Lang).styleAck},
	}
}

// Hash is the fingerprint of layers 1 to 5. Two requests that should hit the
// same cached prefix must carry the same hash; a changed hash is the signal
// that the cache was broken, deliberately or not.
func (p Prefix) Hash() string {
	layers := []string{p.Agent, p.Style, renderTools(p.Tools), p.Structure, p.Profile}
	if p.Lang != "" && p.Lang != i18n.Default {
		layers = append(layers, string(p.Lang))
	}
	if len(p.Examples) > 0 {
		layers = append(layers, renderExamples(p.Examples))
	}
	return hashLayers(layers...)
}

// Messages is the stable start of every request: the system message, the
// sample exchange (both shared by all channels) and the channel style.
func (p Prefix) Messages() []llm.Message {
	msgs := append([]llm.Message{p.SystemMessage()}, p.Examples...)
	return append(msgs, p.StyleMessages()...)
}

// SharedHash fingerprints the part all channels have in common: everything
// but the channel style.
func (p Prefix) SharedHash() string {
	q := p
	q.Style = ""
	return q.Hash()
}

func renderExamples(msgs []llm.Message) string {
	if len(msgs) == 0 {
		return ""
	}
	raw, _ := json.Marshal(msgs)
	return string(raw)
}

// SystemMessage is the chat message carrying the stable layers.
func (p Prefix) SystemMessage() llm.Message {
	return llm.Message{Role: llm.RoleSystem, Content: p.SystemText()}
}
