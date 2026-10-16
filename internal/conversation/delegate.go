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
	return e.toolLoop(ctx, name, e.texts(ctx).delegated, task, "delegate")
}

// RunTask runs a scheduled job of a skill: a background turn on the
// background model that may call read-only tools and ends with a JSON
// object, which it returns.
func (e *Engine) RunTask(ctx context.Context, instruction string) (string, error) {
	ctx = context.WithValue(ctx, delegatingKey{}, true) // no delegate from a job
	text, err := e.toolLoop(ctx, e.cfg.RouteFor(config.ChannelBackground), e.texts(ctx).background, instruction, "task")
	if err != nil {
		return "", err
	}
	return ExtractJSON(text), nil
}

// toolLoop runs one stateless turn on a model: the background prefix (rules,
// house, tools; changes are refused for the background channel), the text as
// layer 7, tool rounds until the model answers.
func (e *Engine) toolLoop(ctx context.Context, modelName, channelContext, text, label string) (string, error) {
	client, model, name := e.modelFor(modelName)
	prefix, err := e.Prefix(ctx, config.ChannelBackground)
	if err != nil {
		return "", err
	}
	turn := prompt.Turn{
		Lang:           e.lang(ctx),
		Now:            e.clock().In(e.loc),
		ChannelContext: channelContext,
		Text:           text,
	}
	built := prompt.Build(prefix, nil, turn)
	msgs := built.Messages
	ts := &tools.TurnState{SessionKey: label, Channel: config.ChannelBackground}
	var prompted, completed int
	for round := 0; ; round++ {
		req := modelRequest(model, msgs)
		req.Tools = built.Tools
		req.Channel = label
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
			e.log.Info(label+" done", "model", name, "rounds", round,
				"prompt_tokens", prompted, "completion_tokens", completed)
			if strings.TrimSpace(msg.Content) == "" {
				return "", errors.New("the model gave no answer")
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
