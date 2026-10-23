package conversation

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/agent"
	"github.com/miraassist/mira/internal/store"
)

// How long a conversation waits for the delegate before it runs on and its
// result comes as a message (docs/spec/agenten.md, Decided 27).
var (
	delegateWaitVoice    = 5 * time.Second
	delegateWaitTelegram = 30 * time.Second
)

// Delegate runs the agent delegate (docs/spec/agenten.md): a task handed on
// by the conversation, typically to a stronger cloud model, whose answer the
// user hears through the conversation's model. Without routing.delegate it
// takes main.
//
// It gets a fresh context: the background prefix (rules, house, tools) and
// the task, no history. It may look things up but never change anything.
// Only a conversation delegates, as before the agents: a call from inside
// another agent's run is refused. Called by a conversation, it takes its
// call context from it (person, place, third-party text, one deeper). The
// conversation waits 5 seconds at a satellite, 30 on Telegram; then the run
// goes on and its result comes as a message to the person or the room.
func (e *Engine) Delegate(ctx context.Context, task string) (string, error) {
	_, profile, ok := agent.Caller(ctx)
	if ok && profile != agent.Conversation {
		return "", errors.New("delegate cannot be called from an agent's run")
	}
	if strings.TrimSpace(task) == "" {
		return "", errors.New("empty task")
	}
	res, err := e.agents.Call(ctx, e.delegateSpec(ctx), task, e.AgentCall(ctx))
	return e.AgentAnswer(ctx, res, err)
}

// AgentCall is the call context of an agent the conversation calls as a tool
// (delegate, a YAML agent with conversation true): its person and place, one
// deeper. The conversation waits 5 seconds at a satellite, 30 on Telegram;
// then the run goes on and its result comes as a message to the person or
// the room (docs/spec/agenten.md, An agent as a tool in a conversation).
func (e *Engine) AgentCall(ctx context.Context) agent.CallContext {
	cc := agent.CallContext{Origin: agent.FromCode, Rights: agent.AllRights, Lang: string(e.lang(ctx))}
	if conv, ok := agent.Current(ctx); ok {
		caller, _, _ := agent.Caller(ctx)
		wait := delegateWaitVoice
		if caller == "conversation.telegram" {
			wait = delegateWaitTelegram
		}
		receiver := conv.Person
		if receiver == "" {
			receiver = store.ReceiverHouse
		}
		cc.Mode = agent.Mode{Timeout: wait, OnTimeout: agent.RunOn,
			Delivery: agent.Delivery{Route: agent.ToMessage, Receiver: receiver, Area: conv.Place}}
	}
	return cc
}

// AgentAnswer is the tool result of such a call: the agent's answer, or that
// Mira comes back with it.
func (e *Engine) AgentAnswer(ctx context.Context, res agent.Result, err error) (string, error) {
	if err == nil && res.Status == agent.Running {
		return e.texts(ctx).delegateLater, nil
	}
	return res.Text, err
}

func (e *Engine) delegateSpec(ctx context.Context) agent.Spec {
	return agent.Spec{Name: "delegate", Description: "Hands a harder task to a stronger model and returns its answer.",
		Profile: agent.Background, Instructions: e.texts(ctx).delegated,
		Tools: agent.Tools{Kind: agent.AllTools}, Rights: agent.ReadHouse | agent.Web}
}

// RunTask runs the agent skill.task: a scheduled job of a skill, a
// background run that may look things up and ends with a JSON object, which
// it returns. It gets the tools of the skill's MCP server, if it has one.
func (e *Engine) RunTask(ctx context.Context, skill, instruction string) (string, error) {
	res, err := e.agents.Call(ctx, taskSpec(), instruction,
		agent.CallContext{Origin: agent.FromSchedule, Rights: agent.AllRights, Lang: string(e.lang(ctx)), Skill: skill})
	if err != nil {
		return "", err
	}
	return ExtractJSON(res.Text), nil
}

func taskSpec() agent.Spec {
	return agent.Spec{Name: "skill.task", Description: "Runs a skill's job in the background and answers in JSON.",
		Profile: agent.Background, Tools: agent.Tools{Kind: agent.AllTools},
		Rights: agent.ReadHouse | agent.Web}
}

// Agents are the engine's agents as they are called by name (Home
// Assistant's mira.agent, a run taken up after a restart), in the language
// of now.
func (e *Engine) Agents() []agent.Spec {
	return []agent.Spec{e.delegateSpec(context.Background()), taskSpec()}
}
