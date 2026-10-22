package conversation

import (
	"context"
	"strings"

	"github.com/miraassist/mira/internal/agent"
	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/prompt"
	"github.com/miraassist/mira/internal/store"
)

// BackgroundContext builds an agent's background profile
// (docs/spec/agenten.md, Context profiles): the background prefix, so the
// slot's cache stays warm, and the agent's instructions and input as layer 7.
// It takes no lock: the runtime limits the access to each model.
func (e *Engine) BackgroundContext(ctx context.Context, cc agent.CallContext, instructions, input string) (agent.Built, error) {
	prefix, err := e.Prefix(ctx, config.ChannelBackground)
	if err != nil {
		return agent.Built{}, err
	}
	lang := e.lang(ctx)
	if cc.Lang != "" {
		lang = i18n.Lang(cc.Lang)
	}
	var text []string
	for _, part := range []string{instructions, input} {
		if part = strings.TrimSpace(part); part != "" {
			text = append(text, part)
		}
	}
	turn := prompt.Turn{
		Lang:           lang,
		Now:            e.clock().In(e.loc),
		ChannelContext: e.channelContext(ctx, Request{Channel: config.ChannelBackground}, store.Session{}, e.structure()),
		Text:           strings.Join(text, "\n\n"),
	}
	built := prompt.Build(prefix, nil, turn)
	b := agent.Built{Messages: built.Messages, Tools: built.Tools, PrefixHash: built.PrefixHash}
	// A run for a skill gets the tools of its MCP server, after the shared
	// ones (docs/spec/skill-mcp.md).
	if cc.Skill != "" {
		more, err := prompt.BuildTools(e.tools.SkillDefs(cc.Skill))
		if err != nil {
			return agent.Built{}, err
		}
		b.Tools = append(b.Tools, more...)
	}
	return b, nil
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
