package conversation

import (
	"context"
	"errors"
	"strings"

	"github.com/sker65/mira/internal/config"
	"github.com/sker65/mira/internal/llm"
	"github.com/sker65/mira/internal/prompt"
	"github.com/sker65/mira/internal/tools"
)

type delegatingKey struct{}

// Delegate hands a task to the model routing.delegate names, typically a
// stronger cloud model, and returns its answer. The fast model calls it
// through the delegate tool when a question needs more knowledge or thought
// than it has; the user hears the answer through the fast model.
//
// The stronger model gets a fresh context: the shared prefix (rules, house,
// tools) and the task, no history. It works like a background job: it may
// look things up but never change anything, and it cannot delegate again.
func (e *Engine) Delegate(ctx context.Context, task string) (string, error) {
	if ctx.Value(delegatingKey{}) != nil {
		return "", errors.New("delegate cannot be called from a delegated task")
	}
	if strings.TrimSpace(task) == "" {
		return "", errors.New("empty task")
	}
	name := e.cfg.Routing.Delegate
	if name == "" {
		return "", errors.New("no delegate model configured")
	}
	ctx = context.WithValue(ctx, delegatingKey{}, true)
	client, model, name := e.modelFor(name)

	prefix, err := e.Prefix(ctx, config.ChannelBackground)
	if err != nil {
		return "", err
	}
	turn := prompt.Turn{
		Lang:           e.lang(),
		Now:            e.clock().In(e.loc),
		ChannelContext: e.texts().delegated,
		Text:           task,
	}
	built := prompt.Build(prefix, nil, turn)
	msgs := built.Messages
	ts := &tools.TurnState{SessionKey: "delegate", Channel: config.ChannelBackground}
	var prompted, completed int
	for round := 0; ; round++ {
		req := modelRequest(model, msgs)
		req.Tools = built.Tools
		req.Channel = "delegate"
		req.PrefixHash = built.PrefixHash
		if round >= model.MaxToolRounds {
			req.ToolChoice = "none"
		}
		out, err := client.Chat(ctx, req)
		if err != nil {
			return "", err
		}
		prompted += out.Usage.PromptTokens
		completed += out.Usage.CompletionTokens
		msg := out.Message()
		msg.Role = llm.RoleAssistant
		msg.Content = stripThinking(msg.Content)
		if len(msg.ToolCalls) == 0 || round >= model.MaxToolRounds {
			e.log.Info("delegated task done", "model", name, "rounds", round,
				"prompt_tokens", prompted, "completion_tokens", completed)
			if strings.TrimSpace(msg.Content) == "" {
				return "", errors.New("the delegate model gave no answer")
			}
			return strings.TrimSpace(msg.Content), nil
		}
		ensureCallIDs(msg.ToolCalls, round)
		msgs = append(msgs, msg)
		for _, call := range msg.ToolCalls {
			msgs = append(msgs, llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Name: call.Function.Name,
				Content: e.runTool(ctx, ts, call)})
		}
	}
}
