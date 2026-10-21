package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/miraassist/mira/internal/agent"
	"github.com/miraassist/mira/internal/channels/haconv"
	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/conversation"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/tools"
)

// The agents (docs/spec/agenten.md): one runtime for all of them, and the
// internal agents described here.

// newAgents is the runtime the agents run in. Model clients are the app's.
func (a *app) newAgents(registry *tools.Registry, guard *tools.Guard) *agent.Runtime {
	return agent.New(agent.Options{
		Config: a.liveConfig,
		Models: func(name string) (agent.Chatter, config.LLM, bool) {
			if name == "" {
				name = config.MainModel
			}
			c, ok := a.models[name]
			m, known := a.cfg.Models[name]
			return c, m, ok && known
		},
		Tools: registry, Guard: guard, Logger: a.log,
		Background: func(ctx context.Context, cc agent.CallContext, instructions, input string) (agent.Built, error) {
			return a.engine.BackgroundContext(ctx, cc, instructions, input)
		},
		Runs: a.store, Deliver: a.deliverRun,
	})
}

// registerAgents makes the agents callable by name, from Home Assistant
// (mira.agent) and when a stored run is taken up after a restart: Mira's
// own with a fixed description, then those described in YAML, in mira.yaml
// and in the skills (docs/spec/agenten.md, Factory). A YAML agent that does
// not fit is left out and shown as a problem.
func (a *app) registerAgents() {
	for _, spec := range a.engine.Agents() {
		a.agents.Register(spec)
	}
	a.agents.Register(notifyComposeSpec(nil))

	defs := agent.FromConfig(a.cfg.Agents.Defined)
	if a.skills != nil {
		for _, sk := range a.skills.Skills {
			for _, name := range sortedNames(sk.Agents) {
				defs = append(defs, agent.Defined{Name: name, Skill: sk.Name, Def: sk.Agents[name]})
			}
		}
	}
	problems := a.agents.RegisterDefined(defs, a.cfg.Model)
	failed := map[string]bool{}
	for _, p := range problems {
		text := p.Text(a.lang)
		failed[p.Skill+"/"+p.Agent] = true
		a.log.Warn("agent not started", "agent", p.Agent, "skill", p.Skill, "problem", text)
		a.problems = append(a.problems, haconv.Problem{Source: "agent", Name: p.Agent, Text: text})
	}
	for _, d := range defs {
		if d.Skill == "" && agent.Own(d.Name) {
			continue
		}
		if !failed[d.Skill+"/"+d.Name] {
			a.log.Info("agent enabled", "agent", d.Name, "skill", d.Skill)
		}
	}
}

func sortedNames[V any](m map[string]V) []string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// deliverRun hands a finished asynchronous run to its route
// (docs/spec/agenten.md, Delivery): Home Assistant fires mira_agent_done, or
// a person or the house gets a message about it.
func (a *app) deliverRun(ctx context.Context, rec store.AgentRun, cc agent.CallContext) {
	switch cc.Mode.Delivery.Route {
	case agent.ToEvent:
		// The integration fires the event: Mira's user may not (Decided 14, 30).
		if _, err := a.ha.Call(ctx, map[string]any{"type": "mira/agent/done", "call_id": rec.CallID,
			"agent": rec.Agent, "status": rec.Status, "output": rec.Output, "error": rec.Error}); err != nil {
			a.log.Error("could not tell home assistant that a run is done", "call_id", rec.CallID, "error", err)
		}
	case agent.ToMessage:
		if err := a.resultMessage(ctx, rec, cc.Mode.Delivery); err != nil {
			a.log.Error("could not deliver a run's result as a message", "call_id", rec.CallID, "error", err)
		}
	case agent.ToThread:
		if a.threads != nil {
			a.threads.AgentResult(ctx, cc.Mode.Delivery.Thread, cc.Mode.Delivery.Into, rec)
		}
	}
}

// resultMessage tells a person or the house a run's result: notify.compose
// words it (Decided 25), the output goes into the details. When the model
// cannot word it, a fixed sentence says that the result is there.
func (a *app) resultMessage(ctx context.Context, rec store.AgentRun, d agent.Delivery) error {
	t := i18n.Pick(resultCatalog, a.lang)
	receiver := d.Receiver
	if _, ok := a.persons()[receiver]; !ok {
		receiver = store.ReceiverHouse
	}
	about := rec.Output
	if rec.Status != string(agent.Done) {
		about = fmt.Sprintf(t.failed, rec.Error)
	}
	spoken := fmt.Sprintf(t.fixed, rec.Agent)
	text, err := a.notifyCompose(ctx, fmt.Sprintf(t.task, rec.Agent, about), resultSchema)
	var out struct {
		Text string `json:"text"`
	}
	if err == nil && json.Unmarshal([]byte(text), &out) == nil && strings.TrimSpace(out.Text) != "" {
		spoken = strings.TrimSpace(out.Text)
	} else {
		a.log.Warn("model could not word a run's result, using the fixed form", "call_id", rec.CallID, "error", err)
	}
	a.proactiveMu.Lock()
	nf := a.notifier
	a.proactiveMu.Unlock()
	if nf == nil {
		return fmt.Errorf("no notifier is running")
	}
	return nf.Tell(ctx, store.Message{ID: ulid.Make().String(), Source: store.SourceTask,
		DedupeKey: "agent:" + rec.CallID, Importance: 2, Receiver: receiver, Spoken: spoken,
		Details: about}, d.Area)
}

var resultSchema = json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`)

type resultTexts struct{ task, failed, fixed string }

var resultCatalog = map[i18n.Lang]resultTexts{
	i18n.DE: {
		task: "Ein Auftrag, den Mira im Hintergrund erledigt hat (%s), ist fertig. Sag in ein, zwei kurzen Sätzen, " +
			"was dabei herauskam, so wie Mira es jemandem im Haus sagen würde. Antworte als JSON mit \"text\".\n\nErgebnis:\n%s",
		failed: "Es hat nicht geklappt: %s",
		fixed:  "Das Ergebnis von %s ist da; es steht in den Details.",
	},
	i18n.EN: {
		task: "A job Mira did in the background (%s) is finished. Say in one or two short sentences what came " +
			"of it, as Mira would tell someone in the house. Answer as JSON with \"text\".\n\nResult:\n%s",
		failed: "It did not work: %s",
		fixed:  "The result of %s is there; it is in the details.",
	},
}

// isolatedAgent is an internal agent of the isolated profile, used where a
// package asks a model one question with a JSON answer (Ask). It is bound
// late: the runtime is built after the packages that use it.
type isolatedAgent struct {
	a      *app
	name   string
	origin agent.Origin
	limits agent.Limits
}

func (m isolatedAgent) Ask(ctx context.Context, system, instruction string, schema json.RawMessage) (string, error) {
	res, err := m.a.agents.Call(ctx, agent.Spec{Name: m.name, Profile: agent.Isolated,
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
	return isolatedAgent{a: a, name: "mail.read", origin: agent.FromIMAP, limits: mailLimits}
}

func (a *app) mailMatch() isolatedAgent {
	return isolatedAgent{a: a, name: "mail.match", origin: agent.FromIMAP, limits: mailLimits}
}

// mailLimits: a model call that hangs is tried once more after 60 s (a
// local model with a cold prompt takes about 50).
var mailLimits = agent.Limits{AttemptTimeout: 60 * time.Second, Retries: 1}

func (a *app) tasksAssess() isolatedAgent {
	return isolatedAgent{a: a, name: "tasks.assess", origin: agent.FromAutomation}
}

func (a *app) tasksDecide() isolatedAgent {
	return isolatedAgent{a: a, name: "tasks.decide", origin: agent.FromAutomation}
}

func (a *app) tasksPlan() isolatedAgent {
	return isolatedAgent{a: a, name: "tasks.plan", origin: agent.FromCode}
}

func (a *app) profileCompact() isolatedAgent {
	return isolatedAgent{a: a, name: "profile.compact", origin: agent.FromSchedule}
}

// notifyCompose words announcements: several messages as one, or a run's
// result. Background without tools, so the background slot's cache stays
// warm.
func (a *app) notifyCompose(ctx context.Context, instruction string, schema json.RawMessage) (string, error) {
	res, err := a.agents.Call(ctx, notifyComposeSpec(schema), instruction, agent.CallContext{Origin: agent.FromCode})
	return res.Text, err
}

func notifyComposeSpec(schema json.RawMessage) agent.Spec {
	return agent.Spec{Name: "notify.compose", Description: "Words announcements and results for the house.",
		Profile: agent.Background, Output: schema}
}

// agentNames are the agents a skill's target may name: Mira's own with a
// fixed description and those described in YAML (whether these start is
// checked later; one that does not fails like any agent).
func (a *app) agentNames() map[string]bool {
	names := map[string]bool{}
	for _, n := range agent.Callable {
		names[n] = true
	}
	for n := range a.cfg.Agents.Defined {
		names[n] = true
	}
	if a.skills != nil {
		for _, sk := range a.skills.Skills {
			for n := range sk.Agents {
				names[n] = true
			}
		}
	}
	return names
}

// unknownAgent is the first agent a target names that does not exist.
func unknownAgent(t *skills.Target, known map[string]bool) string {
	if t.Agent != "" && !known[t.Agent] {
		return t.Agent
	}
	for _, f := range t.Fields {
		if f.Agent != "" && !known[f.Agent] {
			return f.Agent
		}
	}
	return ""
}

// dispatchForThread starts an agent that fills a field of a thread: an
// asynchronous run in the thread's call context, delivered back into it.
func (a *app) dispatchForThread(ctx context.Context, th store.Thread, into, name, callID string, input json.RawMessage) error {
	area, _ := strings.CutPrefix(th.Place, "area:")
	if area == th.Place {
		area = ""
	}
	_, err := a.agents.CallByName(ctx, name, input, agent.CallContext{CallID: callID, Origin: agent.FromCode,
		Person: th.Person, Place: area, Mode: agent.Mode{Async: true,
			Delivery: agent.Delivery{Route: agent.ToThread, Thread: th.ID, Into: into}}})
	return err
}

// notLocal is, for a skill that must run locally, the first agent it uses
// whose model is not local, with that model and its privacy; "" when all
// fit. The agents a skill uses: its own, those its target names, and for a
// mail skill mail.read and mail.match.
func notLocal(cfg config.Config, sk skills.Skill) (agentName, model, privacy string) {
	if sk.Privacy != config.PrivacyLocal {
		return "", "", ""
	}
	var used []string
	if sk.Mail != nil {
		used = append(used, "mail.read", "mail.match")
	}
	if t := sk.Target; t != nil {
		if t.Agent != "" {
			used = append(used, t.Agent)
		}
		for _, f := range t.Fields {
			if f.Agent != "" {
				used = append(used, f.Agent)
			}
		}
	}
	used = append(used, sortedNames(sk.Agents)...)
	for _, name := range used {
		model := cfg.ModelFor(name)
		if d, ok := sk.Agents[name]; ok {
			model = modelNameOf(d.Model)
		}
		if m, ok := cfg.Model(model); !ok || m.Privacy != config.PrivacyLocal {
			return name, model, m.Privacy
		}
	}
	return "", "", ""
}

func modelNameOf(name string) string {
	if name == "" {
		return config.MainModel
	}
	return name
}
