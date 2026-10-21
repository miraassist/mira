package threads

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/oklog/ulid/v2"

	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
)

// Agents fill a thread's fields, as the user does (docs/spec/agenten.md,
// Continuation): a field of the target names the agent and its input; Mira
// starts it once everything its input names is there, its result is filled
// in when it comes, and the thread goes on from there.

// PlanField is where an agent's result goes for a plan target.
const PlanField = "plan"

// launch is an agent run a thread decided to start: its call id is with the
// thread before it starts, so a quick result finds it.
type launch struct {
	into, agent, callID string
	input               json.RawMessage
}

// dispatch decides which agents of th can start now: their field is empty,
// none of theirs runs, and everything their input names is there. It notes
// their call ids in th; the caller saves th, then starts them.
func (m *Manager) dispatch(th *store.Thread) []launch {
	if m.opts.Dispatch == nil {
		return nil
	}
	sk := m.targets[th.Target]
	var out []launch
	want := func(into, agent string, input map[string]any) {
		if agent == "" || th.Calls[into] != "" {
			return
		}
		raw, ready := agentInput(*th, skills.AgentInput(input))
		if !ready {
			return
		}
		l := launch{into: into, agent: agent, callID: strings.ToLower(ulid.Make().String()), input: raw}
		if th.Calls == nil {
			th.Calls = map[string]string{}
		}
		th.Calls[into] = l.callID
		m.logEntry(th, "mira", fmt.Sprintf(m.texts().agentStarted, agent, into))
		out = append(out, l)
	}
	if sk.Target.Plan {
		if planOf(*th).Plan == "" {
			want(PlanField, sk.Target.Agent, sk.Target.Input)
		}
		return out
	}
	for _, f := range sk.Target.Fields {
		if strings.TrimSpace(th.Fields[f.Name]) == "" {
			want(f.Name, f.Agent, f.Input)
		}
	}
	return out
}

// start starts what dispatch decided, after th was saved. One that does not
// start is a failed agent.
func (m *Manager) start(ctx context.Context, th store.Thread, launches []launch) {
	for _, l := range launches {
		if err := m.opts.Dispatch(ctx, th, l.into, l.agent, l.callID, l.input); err != nil {
			m.log.Error("could not start an agent for a thread", "thread", th.ID, "agent", l.agent, "error", err)
			m.AgentResult(ctx, th.ID, l.into, store.AgentRun{CallID: l.callID, Agent: l.agent, Status: "failed",
				Error: err.Error()})
		}
	}
}

// agentInput fills an input's placeholders: {{ field }} with the thread's
// fields, {{ request }} with what the user asked for. ready is false while a
// field it names is empty.
func agentInput(th store.Thread, input map[string]any) (json.RawMessage, bool) {
	ready := true
	var fill func(v any) any
	fill = func(v any) any {
		switch x := v.(type) {
		case string:
			return placeholder.ReplaceAllStringFunc(x, func(p string) string {
				name := placeholder.FindStringSubmatch(p)[1]
				if name == "request" {
					return th.Request
				}
				val := strings.TrimSpace(th.Fields[name])
				if val == "" {
					ready = false
				}
				return val
			})
		case map[string]any:
			out := make(map[string]any, len(x))
			for k, e := range x {
				out[k] = fill(e)
			}
			return out
		case []any:
			out := make([]any, len(x))
			for i, e := range x {
				out[i] = fill(e)
			}
			return out
		}
		return v
	}
	raw, err := json.Marshal(fill(input))
	return raw, ready && err == nil
}

// errStale: a result for a thread that ended or no longer waits for it.
var errStale = errors.New("stale result")

// AgentResult takes an agent's result into its thread (the route "thread"):
// a field is filled as if the user had given it, a plan is written; a failed
// agent makes the thread ask the user. When then every field is there, the
// finish runs in the background and its answer comes as a message of the
// thread.
func (m *Manager) AgentResult(ctx context.Context, id, into string, rec store.AgentRun) {
	t := m.texts()
	var say string
	var complete bool
	var launches []launch
	th, err := m.change(ctx, id, func(th *store.Thread) error {
		say, complete, launches = "", false, nil
		if !store.ThreadOpen(th.State) || th.Calls[into] != rec.CallID {
			return errStale
		}
		delete(th.Calls, into)
		sk := m.targets[th.Target]
		defer m.extend(th)
		if rec.Status != "done" {
			m.logEntry(th, "mira", fmt.Sprintf(t.agentFailedLog, rec.Agent, into, rec.Error))
			th.State = store.ThreadWaiting
			if into == PlanField {
				say = fmt.Sprintf(t.agentPlanFailed, th.Request)
			} else if f, ok := field(sk.Target, into); ok {
				say = fmt.Sprintf(t.agentAsk, f.Description)
			}
			return nil
		}
		value := strings.TrimSpace(rec.Output)
		if into == PlanField {
			c := planOf(*th)
			c.Plan = value
			c.Foreign = c.Foreign || rec.Foreign
			m.finished(&c)
			setPlan(th, c)
			m.logEntry(th, "mira", fmt.Sprintf(t.planWritten, len(boxes(value))))
			say = fmt.Sprintf(t.agentPlanReady, th.Request, len(boxes(value)))
			return nil
		}
		f, _ := field(sk.Target, into)
		if strings.TrimSpace(th.Fields[into]) != "" {
			// The user answered meanwhile: their answer stands.
			return nil
		}
		if f.Check != "" {
			if check, ok := m.opts.Checks[f.Check]; ok {
				checked, err := check(ctx, value)
				if err != nil {
					m.logEntry(th, "mira", fmt.Sprintf(t.agentFailedLog, rec.Agent, into, err))
					th.State = store.ThreadWaiting
					say = fmt.Sprintf(t.agentAsk, f.Description)
					return nil
				}
				value = checked
			}
		}
		th.Fields[into] = value
		m.logEntry(th, "mira", fmt.Sprintf(t.agentFilled, rec.Agent, into, value))
		launches = m.dispatch(th)
		complete = len(missing(sk.Target, th.Fields)) == 0
		return nil
	})
	if errors.Is(err, errStale) {
		m.log.Info("agent result no longer wanted", "thread", id, "agent", rec.Agent, "call_id", rec.CallID)
		return
	}
	if err != nil {
		m.log.Error("could not take an agent's result into a thread", "thread", id, "error", err)
		return
	}
	m.log.Info("agent result in thread", "thread", id, "agent", rec.Agent, "into", into, "status", rec.Status)
	m.start(ctx, th, launches)
	if say != "" {
		m.tell(ctx, th, say)
	}
	if complete {
		m.finishInBackground(ctx, th)
	}
}

// finishInBackground runs the finish of a thread nobody waits for in a
// conversation: its answer comes as a message of the thread.
func (m *Manager) finishInBackground(ctx context.Context, th store.Thread) {
	fin, err := m.finisher(th.Target)
	if err != nil {
		m.log.Warn("a thread cannot finish", "thread", th.ID, "error", err)
		return
	}
	th.State = store.ThreadWorking
	if err := m.save(ctx, &th); err != nil {
		m.log.Error("could not save a thread", "thread", th.ID, "error", err)
		return
	}
	res := m.runWork(ctx, fin, th)
	if cur, err := m.opts.Store.GetThread(ctx, th.ID); err == nil && cur.State == store.ThreadWorking {
		m.apply(ctx, cur, res, true)
	}
}

// cancelAgents ends the agents still filling a thread that ended.
func (m *Manager) cancelAgents(th store.Thread) {
	if m.opts.Cancel == nil {
		return
	}
	for _, id := range th.Calls {
		m.opts.Cancel(id)
	}
}
