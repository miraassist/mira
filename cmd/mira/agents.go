package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/miraassist/mira/internal/agent"
	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/conversation"
	"github.com/miraassist/mira/internal/tools"
)

// The agents (docs/spec/agenten.md): one runtime for all of them, and the
// internal agents described here.

// newAgents is the runtime the agents run in. Model clients are the app's.
func (a *app) newAgents(registry *tools.Registry, guard *tools.Guard) *agent.Runtime {
	return agent.New(agent.Options{
		Config: a.liveConfig,
		Models: func(name string) (agent.Chatter, config.LLM, bool) {
			if name == "" || name == config.MainModel {
				return a.llm, a.cfg.LLM, true
			}
			c, ok := a.models[name]
			m, known := a.cfg.Models[name]
			return c, m, ok && known
		},
		Tools: registry, Guard: guard, Logger: a.log,
	})
}

// isolatedAgent is an internal agent of the isolated profile, used where a
// package asks a model one question with a JSON answer (Ask). It is bound
// late: the runtime is built after the packages that use it.
type isolatedAgent struct {
	a      *app
	name   string
	role   string
	origin agent.Origin
	limits agent.Limits
}

func (m isolatedAgent) Ask(ctx context.Context, system, instruction string, schema json.RawMessage) (string, error) {
	res, err := m.a.agents.Call(ctx, agent.Spec{Name: m.name, Role: m.role, Profile: agent.Isolated,
		Instructions: system, Output: schema, Limits: m.limits}, instruction, agent.CallContext{Origin: m.origin})
	if err != nil {
		return "", err
	}
	if len(schema) == 0 {
		// A text answer that holds JSON (a task draft) comes without
		// prose around it, as before.
		return conversation.ExtractJSON(res.Text), nil
	}
	return res.Text, nil
}

// AssessIsolated is Ask, for the profile compaction.
func (m isolatedAgent) AssessIsolated(ctx context.Context, system, instruction string, schema json.RawMessage) (string, error) {
	return m.Ask(ctx, system, instruction, schema)
}

// The isolated internal agents.
func (a *app) mailRead() isolatedAgent {
	return isolatedAgent{a: a, name: "mail.read", role: "mail", origin: agent.FromIMAP, limits: mailLimits}
}

func (a *app) mailMatch() isolatedAgent {
	return isolatedAgent{a: a, name: "mail.match", role: "mail", origin: agent.FromIMAP, limits: mailLimits}
}

// mailLimits: a model call that hangs is tried once more after 60 s (a
// local model with a cold prompt takes about 50).
var mailLimits = agent.Limits{AttemptTimeout: 60 * time.Second, Retries: 1}

func (a *app) tasksAssess() isolatedAgent {
	return isolatedAgent{a: a, name: "tasks.assess", role: "background", origin: agent.FromAutomation}
}

func (a *app) tasksDecide() isolatedAgent {
	return isolatedAgent{a: a, name: "tasks.decide", role: "background", origin: agent.FromAutomation}
}

func (a *app) tasksPlan() isolatedAgent {
	return isolatedAgent{a: a, name: "tasks.plan", role: "tasks", origin: agent.FromCode}
}

func (a *app) profileCompact() isolatedAgent {
	return isolatedAgent{a: a, name: "profile.compact", role: "background", origin: agent.FromSchedule}
}
