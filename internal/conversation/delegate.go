package conversation

import (
	"context"
	"errors"
	"strings"

	"github.com/miraassist/mira/internal/agent"
)

// Delegate runs the agent delegate (docs/spec/agenten.md): a task handed on
// by the conversation, typically to a stronger cloud model, whose answer the
// user hears through the conversation's model. Without routing.delegate it
// takes main.
//
// It gets a fresh context: the background prefix (rules, house, tools) and
// the task, no history. It may look things up but never change anything.
// Only a conversation delegates; until the conversations are agents too
// (step 5), a call from inside an agent's run is refused.
func (e *Engine) Delegate(ctx context.Context, task string) (string, error) {
	if agent.InRun(ctx) {
		return "", errors.New("delegate cannot be called from an agent's run")
	}
	if strings.TrimSpace(task) == "" {
		return "", errors.New("empty task")
	}
	res, err := e.agents.Call(ctx, agent.Spec{Name: "delegate", Role: "delegate", Profile: agent.Background,
		Instructions: e.texts(ctx).delegated, Tools: agent.Tools{Kind: agent.AllTools},
		Rights: agent.ReadHouse | agent.Web},
		task, agent.CallContext{Origin: agent.FromAgent, Depth: 1, Rights: agent.AllRights, Lang: string(e.lang(ctx))})
	return res.Text, err
}

// RunTask runs the agent skill.task: a scheduled job of a skill, a
// background run that may look things up and ends with a JSON object, which
// it returns.
func (e *Engine) RunTask(ctx context.Context, instruction string) (string, error) {
	res, err := e.agents.Call(ctx, agent.Spec{Name: "skill.task", Role: "background", Profile: agent.Background,
		Tools: agent.Tools{Kind: agent.AllTools}, Rights: agent.ReadHouse | agent.Web},
		instruction, agent.CallContext{Origin: agent.FromSchedule, Rights: agent.AllRights, Lang: string(e.lang(ctx))})
	if err != nil {
		return "", err
	}
	return ExtractJSON(res.Text), nil
}
