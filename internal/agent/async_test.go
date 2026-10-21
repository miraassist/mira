package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/tools"
)

// worker is a background agent that may read and act.
func worker() Spec {
	return background("worker", ReadHouse|ActOnHouse)
}

var async = CallContext{Rights: AllRights, Mode: Mode{Async: true, Delivery: Delivery{Route: ToEvent}}}

// stored writes a run as the agent left it when it stopped: its record and
// the steps of its rounds.
func (e *env) stored(t *testing.T, callID, agent string, deadline time.Time, steps ...any) {
	t.Helper()
	ctx := context.Background()
	raw, _ := json.Marshal(async)
	if err := e.st.PutAgentRun(ctx, store.AgentRun{CallID: callID, Agent: agent, Status: string(Running), Call: raw,
		Input: "x", StartedAt: e.clock(), Deadline: deadline}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(steps); i += 2 {
		body, _ := json.Marshal(steps[i+1])
		if err := e.st.AddAgentStep(ctx, callID, steps[i].(string), body); err != nil {
			t.Fatal(err)
		}
	}
}

func answer(calls ...llm.ToolCall) llm.Message {
	for i := range calls {
		calls[i].ID = "c" + calls[i].Function.Name
		calls[i].Type = "function"
	}
	return llm.Message{Role: llm.RoleAssistant, ToolCalls: calls}
}

func result(name, text string) llm.Message {
	return llm.Message{Role: llm.RoleTool, ToolCallID: "c" + name, Name: name, Content: text}
}

func started(name string) llm.Message { return llm.Message{ToolCallID: "c" + name, Name: name} }

// toolResults are the tool results in the last request to m.
func toolResults(m *model) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, msg := range m.reqs[len(m.reqs)-1].Messages {
		if msg.Role == llm.RoleTool {
			out = append(out, msg.Name+": "+msg.Content)
		}
	}
	return out
}

func (m *model) calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.reqs)
}

func (e *env) record(t *testing.T, callID string) store.AgentRun {
	t.Helper()
	rec, err := e.rt.Record(context.Background(), callID)
	if err != nil {
		t.Fatalf("record %s: %v", callID, err)
	}
	return rec
}

// AG-22 Timeout, and the caller lets it run on
func TestAG22_TimeoutAndTheCallerLetsItRunOn(t *testing.T) {
	e := setup(t)
	e.main.delay = 300 * time.Millisecond
	// Given a synchronous call whose caller wants it to run on at the timeout
	cc := CallContext{Rights: AllRights, Mode: Mode{Timeout: 50 * time.Millisecond, OnTimeout: RunOn,
		Delivery: Delivery{Route: ToEvent}}}
	// When the agent is not done by the timeout
	res, err := e.rt.Call(context.Background(), worker(), "x", cc)
	// Then the caller gets the call ID
	if err != nil || res.CallID == "" || res.Status != Running {
		t.Fatalf("result %+v, %v", res, err)
	}
	// And the run goes on and is delivered like an asynchronous one
	if rec := e.waitDelivered(t); rec.CallID != res.CallID || rec.Status != string(Done) || rec.Output != "fertig" {
		t.Errorf("delivered %+v", rec)
	}
}

// AG-30 Asynchronously the call ID comes at once
func TestAG30_AsynchronouslyTheCallIDComesAtOnce(t *testing.T) {
	e := setup(t)
	e.main.steps = []step{{hang: true}}
	// When an agent is called asynchronously
	res, err := e.rt.Call(context.Background(), worker(), "x", async)
	// Then the caller gets a call ID at once
	if err != nil || res.CallID == "" || res.Status != Running {
		t.Fatalf("result %+v, %v", res, err)
	}
	// And the run record has the status "running"
	if rec := e.record(t, res.CallID); rec.Status != string(Running) || rec.Agent != "worker" {
		t.Errorf("record %+v", rec)
	}
}

// AG-31 A finished run holds its output
func TestAG31_AFinishedRunHoldsItsOutput(t *testing.T) {
	e := setup(t)
	// Given an asynchronous run of an agent with a schema for its output
	spec := Spec{Name: "rate", Profile: Isolated, Instructions: "Bewerte.",
		Output: json.RawMessage(`{"type":"object","properties":{"note":{"type":"integer"}},"required":["note"]}`)}
	e.main.steps = []step{{text: `{"note": 2}`}}
	res, _ := e.rt.Call(context.Background(), spec, "x", async)
	// When the run is done
	e.waitDelivered(t)
	// Then the run record has the status "done"
	// And it holds the output following the schema
	if rec := e.record(t, res.CallID); rec.Status != string(Done) || rec.Output != `{"note": 2}` {
		t.Errorf("record %+v", rec)
	}
}

// AG-32 A failed run holds its error
func TestAG32_AFailedRunHoldsItsError(t *testing.T) {
	e := setup(t)
	// Given an asynchronous run
	e.main.steps = []step{{text: ""}}
	res, _ := e.rt.Call(context.Background(), worker(), "x", async)
	// When the run fails
	e.waitDelivered(t)
	// Then the run record has the status "failed"
	// And it holds the error
	if rec := e.record(t, res.CallID); rec.Status != string(Failed) || rec.Error == "" {
		t.Errorf("record %+v", rec)
	}
}

// AG-33 A run beyond its time limits has expired
func TestAG33_ARunBeyondItsTimeLimitsHasExpired(t *testing.T) {
	old := maxRunTime
	maxRunTime = 100 * time.Millisecond
	defer func() { maxRunTime = old }()
	e := setup(t)
	// Given an asynchronous run
	e.main.steps = []step{{hang: true}}
	res, _ := e.rt.Call(context.Background(), worker(), "x", async)
	// When it exceeds its time limits
	e.waitDelivered(t)
	// Then the run record has the status "expired"
	if rec := e.record(t, res.CallID); rec.Status != string(Expired) {
		t.Errorf("record %+v", rec)
	}
}

// AG-34 The run record knows its caller
func TestAG34_TheRunRecordKnowsItsCaller(t *testing.T) {
	e := setup(t)
	var sub Result
	e.helper = func(ctx context.Context) (Result, error) {
		var err error
		sub, err = e.rt.Call(ctx, background("sub", ReadHouse), "y", CallContext{Mode: Mode{Async: true}})
		return sub, err
	}
	// When a run calls another agent asynchronously
	e.main.steps = []step{{calls: []llm.ToolCall{call("ask_helper", "{}")}}, {text: "ok"}}
	parent, _ := e.rt.Call(context.Background(), background("first", AllRights), "x", CallContext{Rights: AllRights})
	// Then that one's run record holds the call ID of the calling run
	if rec := e.record(t, sub.CallID); rec.ParentID != parent.CallID || parent.CallID == "" {
		t.Errorf("record %+v, parent %s", rec, parent.CallID)
	}
}

// AG-35 A run can be queried by its call ID
func TestAG35_ARunCanBeQueriedByItsCallID(t *testing.T) {
	e := setup(t)
	// Given an asynchronous run with its call ID
	e.main.steps = []step{{text: "Ergebnis"}}
	res, _ := e.rt.Call(context.Background(), worker(), "x", async)
	e.waitDelivered(t)
	// When the caller asks with the call ID
	rec := e.record(t, res.CallID)
	// Then it gets the run record with the status and, if done, the output
	if rec.Status != string(Done) || rec.Output != "Ergebnis" || rec.EndedAt.IsZero() {
		t.Errorf("record %+v", rec)
	}
}

// AG-36 60 minutes after its end a run record is gone
func TestAG36_SixtyMinutesAfterItsEndARunRecordIsGone(t *testing.T) {
	e := setup(t)
	// Given a run that was done 61 minutes ago
	res, _ := e.rt.Call(context.Background(), worker(), "x", async)
	e.waitDelivered(t)
	e.advance(61 * time.Minute)
	// When someone asks with its call ID
	_, err := e.rt.Record(context.Background(), res.CallID)
	// Then the run no longer exists
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("err = %v", err)
	}
}

// AG-82 An asynchronous run runs in the runtime
func TestAG82_AnAsynchronousRunRunsInTheRuntime(t *testing.T) {
	e := setup(t)
	e.main.steps = []step{{hang: true}}
	// When an agent is called asynchronously
	res, _ := e.rt.Call(context.Background(), worker(), "x", async)
	// Then the runtime runs it in the background
	// And the runtime can cancel it
	if !e.rt.Cancel(res.CallID) {
		t.Fatal("not running")
	}
	if rec := e.waitDelivered(t); rec.Status != string(Cancelled) {
		t.Errorf("record %+v", rec)
	}
}

// AG-84 After every round the new parts are stored
func TestAG84_AfterEveryRoundTheNewPartsAreStored(t *testing.T) {
	e := setup(t)
	// Given an asynchronous run
	e.main.steps = []step{{calls: []llm.ToolCall{call("look", "{}")}}, {text: "ok"}}
	res, _ := e.rt.Call(context.Background(), worker(), "x", async)
	// When a round ends
	e.waitDelivered(t)
	// Then the model's answer and the tool results of that round are in the database
	steps, err := e.st.AgentSteps(context.Background(), res.CallID)
	if err != nil || len(steps) != 3 || steps[0].Kind != stepAnswer || steps[1].Kind != stepStarted ||
		steps[2].Kind != stepResult || !strings.Contains(string(steps[2].Body), "look ok") {
		t.Errorf("steps %+v, %v", steps, err)
	}
}

// AG-85 A run is continued after a restart
func TestAG85_ARunIsContinuedAfterARestart(t *testing.T) {
	e := setup(t)
	// Given an asynchronous run that stored two rounds
	e.stored(t, "r1", "worker", e.clock().Add(time.Hour),
		stepAnswer, answer(call("look", "{}")), stepStarted, started("look"), stepResult, result("look", "Küche 21 Grad"),
		stepAnswer, answer(call("web_page", "{}")), stepStarted, started("web_page"), stepResult, result("web_page", "Seite"))
	// When the agent is restarted
	e.restart(t)
	e.rt.Register(worker())
	if err := e.rt.Resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Then the run goes on after the second round
	if rec := e.waitDelivered(t); rec.Status != string(Done) {
		t.Fatalf("record %+v", rec)
	}
	if got := toolResults(e.main); len(got) != 2 || got[0] != "look: Küche 21 Grad" || got[1] != "web_page: Seite" ||
		e.main.calls() != 1 || e.count("look")+e.count("web_page") != 0 {
		t.Errorf("results %v, %d model calls, ran %v", got, e.main.calls(), e.ran)
	}
}

// AG-86 An interrupted model call is made again
func TestAG86_AnInterruptedModelCallIsMadeAgain(t *testing.T) {
	e := setup(t)
	// Given an asynchronous run that was waiting for the model when the agent stopped
	e.stored(t, "r1", "worker", e.clock().Add(time.Hour),
		stepAnswer, answer(call("look", "{}")), stepStarted, started("look"), stepResult, result("look", "21 Grad"))
	// When the agent starts again
	e.restart(t)
	e.rt.Register(worker())
	e.rt.Resume(context.Background())
	// Then the run makes that model call again
	if rec := e.waitDelivered(t); rec.Status != string(Done) || e.main.calls() != 1 {
		t.Errorf("record %+v, %d model calls", rec, e.main.calls())
	}
}

// AG-87 An interrupted read-only tool is run again
func TestAG87_AnInterruptedReadOnlyToolIsRunAgain(t *testing.T) {
	e := setup(t)
	// Given an asynchronous run with a read-only tool started but without a stored result
	e.stored(t, "r1", "worker", e.clock().Add(time.Hour), stepAnswer, answer(call("look", "{}")), stepStarted, started("look"))
	// When the agent starts again
	e.restart(t)
	e.rt.Register(worker())
	e.rt.Resume(context.Background())
	e.waitDelivered(t)
	// Then the tool is run again
	if got := toolResults(e.main); e.count("look") != 1 || len(got) != 1 || got[0] != "look: look ok" {
		t.Errorf("ran %v, results %v", e.ran, got)
	}
}

// AG-88 An interrupted tool that changes something is not repeated
func TestAG88_AnInterruptedActingToolIsNotRepeated(t *testing.T) {
	e := setup(t)
	// Given an asynchronous run with an acting tool started but without a stored result
	e.stored(t, "r1", "worker", e.clock().Add(time.Hour),
		stepAnswer, answer(call("radio_play", "{}")), stepStarted, started("radio_play"))
	// When the agent starts again
	e.restart(t)
	e.rt.Register(worker())
	e.rt.Resume(context.Background())
	e.waitDelivered(t)
	// Then the tool is not run again
	// And the model gets that its result is unknown because the run was interrupted
	if got := toolResults(e.main); e.count("radio_play") != 0 || len(got) != 1 || !strings.Contains(got[0], "unterbrochen") {
		t.Errorf("ran %v, results %v", e.ran, got)
	}
}

// AG-89 Synchronous calls are not continued
func TestAG89_SynchronousCallsAreNotContinued(t *testing.T) {
	e := setup(t)
	// Given a synchronous run without run-on that was running when the agent stopped
	e.main.steps = []step{{hang: true}}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	res, _ := e.rt.Call(ctx, worker(), "x", CallContext{Rights: AllRights})
	// When the agent starts again
	e.restart(t)
	e.rt.Register(worker())
	e.rt.Resume(context.Background())
	// Then the run is not continued
	if _, err := e.st.GetAgentRun(context.Background(), res.CallID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a synchronous run was stored: %v", err)
	}
	if running, _ := e.st.RunningAgentRuns(context.Background()); len(running) != 0 {
		t.Errorf("runs to continue: %+v", running)
	}
}

// AG-90 The time limits count across a restart
func TestAG90_TheTimeLimitsCountAcrossARestart(t *testing.T) {
	e := setup(t)
	// Given an asynchronous run whose time limit passed while the agent was stopped
	e.stored(t, "r1", "worker", e.clock().Add(time.Hour))
	e.advance(61 * time.Minute)
	// When the agent starts again
	e.restart(t)
	e.rt.Register(worker())
	e.rt.Resume(context.Background())
	// Then the run record has the status "expired"
	// And the run is not continued
	if rec := e.waitDelivered(t); rec.Status != string(Expired) || e.main.calls() != 0 {
		t.Errorf("record %+v, %d model calls", rec, e.main.calls())
	}
}

// AG-91 On shutdown no new round starts
func TestAG91_OnShutdownNoNewRoundStarts(t *testing.T) {
	e := setup(t)
	// Given an asynchronous run between two rounds
	e.main.steps = []step{{calls: []llm.ToolCall{call("look", "{}")}}, {text: "ok"}}
	e.onTool = func(string) {
		// When the agent shuts down
		go e.rt.Close(context.Background())
		<-e.rt.base.Done()
	}
	res, _ := e.rt.Call(context.Background(), worker(), "x", async)
	e.rt.wg.Wait()
	// Then no new round starts
	// And everything up to then is stored
	steps, _ := e.st.AgentSteps(context.Background(), res.CallID)
	rec, _ := e.st.GetAgentRun(context.Background(), res.CallID)
	if e.main.calls() != 1 || len(steps) != 3 || rec.Status != string(Running) {
		t.Errorf("%d model calls, %d steps, status %s", e.main.calls(), len(steps), rec.Status)
	}
}

// AG-92 At most 10 runs at the same time
func TestAG92_AtMostTenRunsAtTheSameTime(t *testing.T) {
	e := setup(t)
	for i := 0; i < 11; i++ {
		e.smart.steps = append(e.smart.steps, step{hang: true})
	}
	spec := worker()
	spec.Model = "smart"
	// Given 10 asynchronous runs are running
	// When an eleventh is called
	var ids []string
	for i := 0; i < 11; i++ {
		res, _ := e.rt.Call(context.Background(), spec, "x", async)
		ids = append(ids, res.CallID)
	}
	time.Sleep(100 * time.Millisecond)
	// Then it waits until one of the 10 ends
	if n := len(e.rt.async); n != maxAsync {
		t.Fatalf("%d runs at once, want %d", n, maxAsync)
	}
	for _, id := range ids {
		e.rt.Cancel(id)
	}
}

// AG-93 After a restart a run goes on with the agent as it is now
func TestAG93_AfterARestartARunGoesOnWithTheAgentAsItIsNow(t *testing.T) {
	e := setup(t)
	// Given an asynchronous run of an agent whose description changed while the agent was stopped
	e.stored(t, "r1", "worker", e.clock().Add(time.Hour))
	e.restart(t)
	changed := worker()
	changed.Instructions = "Mach es jetzt anders."
	e.rt.Register(changed)
	// When the agent starts again
	e.rt.Resume(context.Background())
	e.waitDelivered(t)
	// Then the run goes on with the new description
	if got := lastUser(e.main.reqs[0]); !strings.Contains(got, "Mach es jetzt anders.") {
		t.Errorf("model saw %q", got)
	}
}

// AG-94 After a restart a run of an agent that no longer exists fails
func TestAG94_AfterARestartARunOfAGoneAgentFails(t *testing.T) {
	e := setup(t)
	// Given an asynchronous run of an agent that no longer exists after the restart
	e.stored(t, "r1", "worker", e.clock().Add(time.Hour))
	e.restart(t)
	// When the agent starts again
	e.rt.Resume(context.Background())
	// Then the run record has the status "failed"
	// And the reason is that the agent no longer exists
	if rec := e.waitDelivered(t); rec.Status != string(Failed) || !strings.Contains(rec.Error, "no longer exists") {
		t.Errorf("record %+v", rec)
	}
}

// AG-97 Conversations do not count towards the 10 runs
func TestAG97_ConversationsDoNotCountTowardsTheTenRuns(t *testing.T) {
	e := setup(t)
	for i := 0; i < maxAsync; i++ {
		e.smart.steps = append(e.smart.steps, step{hang: true})
	}
	spec := worker()
	spec.Model = "smart"
	// Given 10 asynchronous runs are running
	var ids []string
	for i := 0; i < maxAsync; i++ {
		res, _ := e.rt.Call(context.Background(), spec, "x", async)
		ids = append(ids, res.CallID)
	}
	defer func() {
		for _, id := range ids {
			e.rt.Cancel(id)
		}
	}()
	time.Sleep(50 * time.Millisecond)
	// When a person talks to Mira
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ctx, done := e.rt.Enter(ctx, Spec{Name: "conversation.voice", Profile: Conversation}, CallContext{}, nil)
	release, err := e.rt.Acquire(ctx, "main", e.cfg.Main())
	// Then the conversation runs without waiting for them
	if err != nil {
		t.Fatalf("the conversation waits: %v", err)
	}
	release()
	done(Result{}, nil)
}

// AG-65 All agents can be called from Home Assistant
func TestAG65_AllAgentsCanBeCalledFromHomeAssistant(t *testing.T) {
	e := setup(t)
	// Given any agent, internal or from YAML
	e.rt.Register(Spec{Name: "rezept", Profile: Isolated, Instructions: "Zerlege das Rezept.",
		Input: json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]}`)})
	e.main.steps = []step{{text: "Schritte"}}
	// When an automation calls mira.agent with its name and a fitting input
	res, err := e.rt.CallByName(context.Background(), "rezept", json.RawMessage(`{"url":"https://example.com"}`),
		CallContext{Origin: FromAutomation})
	// Then the agent runs
	if err != nil || res.Status != Done || res.Text != "Schritte" || !strings.Contains(lastUser(e.main.reqs[0]), "example.com") {
		t.Errorf("%+v, %v", res, err)
	}
}

// AG-67 A call from Home Assistant gets the agent's own rights
func TestAG67_ACallFromHomeAssistantGetsTheAgentsOwnRights(t *testing.T) {
	e := setup(t)
	// Given an agent whose description names the rights "read the house" and "messages"
	spec := background("melder", ReadHouse|Messages)
	e.rt.Register(spec)
	// When an automation calls it with mira.agent
	e.main.steps = []step{{calls: []llm.ToolCall{call("radio_play", "{}")}}, {text: "ok"}}
	e.rt.CallByName(context.Background(), "melder", json.RawMessage(`{"input":"x"}`), CallContext{Origin: FromAutomation})
	// Then the run's effective rights are "read the house" and "messages"
	lines := e.logLines(t, "tool refused")
	if len(lines) != 1 || !strings.Contains(lines[0], "rights=read_house,messages") || e.count("radio_play") != 0 {
		t.Errorf("log %v, ran %v", lines, e.ran)
	}
}

// AG-101 After a restart a run knows again what it had read
func TestAG101_AfterARestartARunKnowsAgainWhatItHadRead(t *testing.T) {
	for _, via := range []string{"web_page", "ask_helper"} {
		t.Run(via, func(t *testing.T) {
			e := setup(t)
			stop := func() {
				go e.rt.Close(context.Background())
				<-e.rt.base.Done()
			}
			// Given an asynchronous run that read third-party text, itself or
			// through a sub-agent, before the agent stopped
			e.helper = func(ctx context.Context) (Result, error) {
				res, err := e.rt.Call(ctx, background("reader", ReadHouse|Web), "y", CallContext{})
				stop()
				return res, err
			}
			e.onTool = func(name string) {
				if name == via {
					stop()
				}
			}
			e.main.steps = []step{{calls: []llm.ToolCall{call(via, "{}")}}}
			if via == "ask_helper" {
				e.main.steps = append(e.main.steps, step{calls: []llm.ToolCall{call("web_page", "{}")}}, step{text: "gelesen"})
			}
			res, _ := e.rt.Call(context.Background(), background("worker", AllRights), "x", async)
			e.rt.wg.Wait()
			if rec, _ := e.st.GetAgentRun(context.Background(), res.CallID); rec.Status != string(Running) {
				t.Fatalf("the run did not stop before the restart: %+v", rec)
			}
			// When the agent starts again and the run goes on
			e.onTool = nil
			e.restart(t)
			e.rt.Register(background("worker", AllRights))
			e.main.steps = []step{{calls: []llm.ToolCall{call(tools.ToolMemorySave, `{"text":"x"}`)}}, {text: "ok"}}
			e.rt.Resume(context.Background())
			rec := e.waitDelivered(t)
			// Then it counts as having read third-party text, as before the restart
			// And what keeps or passes something on is refused, as it would have been
			got := toolResults(e.main)
			if rec.CallID != res.CallID || rec.Status != string(Done) || e.count(tools.ToolMemorySave) != 0 ||
				len(got) == 0 || !strings.HasPrefix(got[len(got)-1], tools.ToolMemorySave+": ") {
				t.Errorf("record %+v, ran %v, results %v", rec, e.ran, got)
			}
		})
	}
}

// A job runs code like a run: the agents it calls hang under its call ID,
// its log line adds up their model calls, it takes one of the places, and
// it can be cancelled by its call ID.
func TestJobRunsLikeARun(t *testing.T) {
	e := setup(t)
	e.main.steps = []step{{text: "Entwurf"}}
	spec := Spec{Name: "tasks.automation", Profile: Isolated, Rights: AllRights}
	err := e.rt.Job(context.Background(), spec, CallContext{CallID: "th1", Rights: AllRights}, func(ctx context.Context) error {
		if n := len(e.rt.async); n != 1 {
			t.Errorf("%d places taken, want 1", n)
		}
		_, err := e.rt.Call(ctx, Spec{Name: "tasks.plan", Profile: Isolated, Instructions: "Plane."}, "x", CallContext{})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	var job, plan string
	for _, l := range e.logLines(t, "agent done") {
		switch {
		case strings.Contains(l, "agent=tasks.automation"):
			job = l
		case strings.Contains(l, "agent=tasks.plan"):
			plan = l
		}
	}
	if !strings.Contains(plan, "parent_id=th1") || !strings.Contains(job, "call_id=th1") ||
		!strings.Contains(job, "status=done") || !strings.Contains(job, "model_calls=1") || !strings.Contains(job, "prompt_tokens=100") {
		t.Errorf("job %q\nplan %q", job, plan)
	}

	started := make(chan struct{})
	go func() {
		<-started
		e.rt.Cancel("th2")
	}()
	err = e.rt.Job(context.Background(), spec, CallContext{CallID: "th2"}, func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	if !errors.Is(err, ErrCancelled) || len(e.rt.async) != 0 {
		t.Errorf("cancel: %v, %d places taken", err, len(e.rt.async))
	}
}
