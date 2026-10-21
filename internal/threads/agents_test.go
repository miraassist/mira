package threads

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/tools"
)

// started is an agent a thread started.
type started struct {
	th          store.Thread
	into, agent string
	callID      string
	input       map[string]any
}

// agentEnv has a target "geburtstag" with two fields agents fill and a plan
// target "kochplan" an agent writes; the agents it starts are recorded.
type agentEnv struct {
	*env
	mu        sync.Mutex
	starts    []started
	cancelled []string
}

func setupAgents(t *testing.T) *agentEnv {
	t.Helper()
	e := &agentEnv{env: setup(t)}
	geburtstag := skills.Skill{Name: "geburtstag", Description: "Kindergeburtstag planen", Body: "Plane die Feier.",
		Target: &skills.Target{Finish: skills.Finish{Name: "fake"}, Fields: []skills.TargetField{
			{Name: "gaeste", Description: "wie viele Kinder"},
			{Name: "datum", Description: "der Tag"},
			{Name: "wetter", Description: "die Wettervorhersage für den Tag", Agent: "wetter", Check: "upper",
				Input: map[string]any{"tag": "{{ datum }}"}},
			{Name: "spiele", Description: "Spielideen", Agent: "spielideen"},
		}}}
	kochplan := skills.Skill{Name: "kochplan", Description: "Kochen nach Rezept", Body: "Koch mit Plan.",
		Target: &skills.Target{Plan: true, Agent: "rezept", Input: map[string]any{"url": "{{ request }}"}}}
	opts := e.m.opts
	opts.Targets = []skills.Skill{geburtstag, kochplan}
	opts.Checks = map[string]Check{"upper": func(_ context.Context, v string) (string, error) { return strings.ToUpper(v), nil }}
	opts.Dispatch = func(_ context.Context, th store.Thread, into, agent, callID string, input json.RawMessage) error {
		var in map[string]any
		json.Unmarshal(input, &in)
		e.mu.Lock()
		defer e.mu.Unlock()
		e.starts = append(e.starts, started{th: th, into: into, agent: agent, callID: callID, input: in})
		return nil
	}
	opts.Cancel = func(id string) bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.cancelled = append(e.cancelled, id)
		return true
	}
	e.m = New(opts)
	return e
}

func (e *agentEnv) startedFor(into string) (started, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, s := range e.starts {
		if s.into == into {
			return s, true
		}
	}
	return started{}, false
}

// done delivers an agent's result as the runtime does.
func (e *agentEnv) done(s started, output string) {
	e.m.AgentResult(context.Background(), s.th.ID, s.into, store.AgentRun{CallID: s.callID, Agent: s.agent,
		Status: "done", Output: output})
}

func (e *agentEnv) birthday(t *testing.T) (tools.Invocation, store.Thread) {
	t.Helper()
	inv, _ := e.turn(t, "voice:a")
	inv.Person = "anna"
	if _, err := e.m.Start(context.Background(), inv, "geburtstag", "Plane Tims Geburtstag"); err != nil {
		t.Fatal(err)
	}
	th, err := e.m.current(context.Background(), inv)
	if err != nil {
		t.Fatal(err)
	}
	return inv, th
}

func (e *agentEnv) thread(t *testing.T, id string) store.Thread {
	t.Helper()
	th, err := e.st.GetThread(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return th
}

func logged(th store.Thread, text string) bool {
	for _, l := range th.Log {
		if strings.Contains(l.Text, text) {
			return true
		}
	}
	return false
}

// AG-42 An agent's result fills a field of the thread
func TestAG42_AnAgentsResultFillsAFieldOfTheThread(t *testing.T) {
	e := setupAgents(t)
	// Given a thread whose target has a field an agent fills
	_, th := e.birthday(t)
	s, ok := e.startedFor("spiele")
	if !ok {
		t.Fatal("the agent did not start")
	}
	// When the agent is done
	e.done(s, "Topfschlagen")
	// Then its result is in the field, checked as if the user had given it
	// And the log says which agent filled it
	th = e.thread(t, th.ID)
	if th.Fields["spiele"] != "Topfschlagen" || !logged(th, "Agent spielideen: spiele = Topfschlagen") || th.Calls["spiele"] != "" {
		t.Errorf("fields %v, calls %v, log %+v", th.Fields, th.Calls, th.Log)
	}
}

// AG-43 An agent starts once its input is there
func TestAG43_AnAgentStartsOnceItsInputIsThere(t *testing.T) {
	e := setupAgents(t)
	// Given a field whose agent takes another field as input
	inv, th := e.birthday(t)
	if _, ok := e.startedFor("wetter"); ok {
		t.Fatal("started before its input was there")
	}
	// When the user gives that other field
	if _, err := e.m.Fill(context.Background(), inv, map[string]string{"datum": "2026-11-01"}); err != nil {
		t.Fatal(err)
	}
	// Then the agent is started with it, in the thread's call context
	s, ok := e.startedFor("wetter")
	if !ok || s.agent != "wetter" || s.input["tag"] != "2026-11-01" || s.th.Person != "anna" || s.th.Place != "area:wohnzimmer" ||
		e.thread(t, th.ID).Calls["wetter"] != s.callID {
		t.Errorf("started %+v", s)
	}
}

// AG-44 Several agents run at once, the last result finishes
func TestAG44_SeveralAgentsRunAtOnceTheLastResultFinishes(t *testing.T) {
	e := setupAgents(t)
	// Given a thread with two fields filled by agents and the other fields given
	inv, th := e.birthday(t)
	e.m.Fill(context.Background(), inv, map[string]string{"datum": "2026-11-01", "gaeste": "8"})
	// When both agents run
	spiele, _ := e.startedFor("spiele")
	wetter, ok := e.startedFor("wetter")
	// Then they run at the same time
	if !ok || len(e.thread(t, th.ID).Calls) != 2 {
		t.Fatalf("calls %v", e.thread(t, th.ID).Calls)
	}
	e.done(spiele, "Topfschlagen")
	if len(e.fin.works) != 0 {
		t.Fatal("finished before the last result")
	}
	// And when the second result is there, the finish runs and its answer comes as a message of the thread
	e.done(wetter, "sonnig")
	if n := e.next(t); n.ThreadID != th.ID || n.Spoken != "Ab wann ist es ungewöhnlich?" {
		t.Errorf("message %+v", n)
	}
	if got := e.thread(t, th.ID); got.Fields["wetter"] != "SONNIG" || got.State != store.ThreadWaiting {
		t.Errorf("thread %+v", got)
	}
}

// AG-45 A failed agent makes the thread ask the user
func TestAG45_AFailedAgentMakesTheThreadAskTheUser(t *testing.T) {
	e := setupAgents(t)
	// Given a field an agent fills
	_, th := e.birthday(t)
	s, _ := e.startedFor("spiele")
	// When the agent fails
	e.m.AgentResult(context.Background(), th.ID, "spiele", store.AgentRun{CallID: s.callID, Agent: "spielideen",
		Status: "failed", Error: "kein Modell"})
	// Then the log says so
	// And the thread asks the user for that field, as a message of the thread
	got := e.thread(t, th.ID)
	if !logged(got, "Agent spielideen konnte spiele nicht ermitteln: kein Modell") || got.State != store.ThreadWaiting {
		t.Errorf("thread %+v", got)
	}
	if n := e.next(t); n.ThreadID != th.ID || !strings.Contains(n.Spoken, "Spielideen") {
		t.Errorf("message %+v", n)
	}
}

// AG-46 Ending the thread cancels its agents
func TestAG46_EndingTheThreadCancelsItsAgents(t *testing.T) {
	e := setupAgents(t)
	// Given a thread with an agent still running
	inv, _ := e.birthday(t)
	s, _ := e.startedFor("spiele")
	// When the user cancels the thread
	if _, err := e.m.Cancel(context.Background(), inv); err != nil {
		t.Fatal(err)
	}
	// Then the agent's run is cancelled
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.cancelled) != 1 || e.cancelled[0] != s.callID {
		t.Errorf("cancelled %v", e.cancelled)
	}
}

// AG-47 An agent writes a plan
func TestAG47_AnAgentWritesAPlan(t *testing.T) {
	e := setupAgents(t)
	// Given a plan target that names an agent
	inv, _ := e.turn(t, "voice:a")
	// When the thread starts and the agent is done
	if _, err := e.m.Start(context.Background(), inv, "kochplan", "https://example.com/pfannkuchen"); err != nil {
		t.Fatal(err)
	}
	s, ok := e.startedFor(PlanField)
	if !ok || s.input["url"] != "https://example.com/pfannkuchen" {
		t.Fatalf("started %+v", s)
	}
	e.m.AgentResult(context.Background(), s.th.ID, PlanField, store.AgentRun{CallID: s.callID, Agent: "rezept",
		Status: "done", Output: "- [ ] Teig rühren\n- [ ] Backen", Foreign: true})
	// Then its result is the thread's plan, marked foreign if the agent read third-party text
	th := e.thread(t, s.th.ID)
	if c := planOf(th); c.Plan != "- [ ] Teig rühren\n- [ ] Backen" || !c.Foreign {
		t.Errorf("plan %+v", c)
	}
	// And the user gets a message that the plan is there
	if n := e.next(t); n.ThreadID != th.ID || !strings.Contains(n.Spoken, "2 Schritte") {
		t.Errorf("message %+v", n)
	}
}

// AG-48 While an agent works the model does not ask for its field
func TestAG48_WhileAnAgentWorksTheModelDoesNotAskForItsField(t *testing.T) {
	e := setupAgents(t)
	// Given a field an agent is filling
	_, th := e.birthday(t)
	// When the model sees the thread
	status := e.m.status(e.thread(t, th.ID))
	// Then the field shows that an agent fills it
	if !strings.Contains(status, "spiele – Spielideen: (ermittelt gerade Agent spielideen") {
		t.Errorf("status:\n%s", status)
	}
	for _, line := range strings.Split(status, "\n") {
		if strings.HasPrefix(line, "Fehlt noch") && strings.Contains(line, "spiele") {
			t.Errorf("the model is told to ask for it: %s", line)
		}
	}
	if !strings.Contains(status, "Fehlt noch: gaeste, datum") {
		t.Errorf("the fields for the user are not asked for:\n%s", status)
	}
}
