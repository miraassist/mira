package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/ha/structure"
	"github.com/miraassist/mira/internal/hooks"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/tools"

	"gopkg.in/yaml.v3"
)

// --- fakes -------------------------------------------------------------------

// step is one scripted model answer: tool calls, text, or a hang until the
// call's context ends.
type step struct {
	calls []llm.ToolCall
	text  string
	hang  bool
	err   error // the model fails
}

// model is a scripted model; it counts calls and how many ran at once.
type model struct {
	mu       sync.Mutex
	steps    []step
	reqs     []llm.ChatRequest
	running  atomic.Int32
	maxAtOne atomic.Int32
	delay    time.Duration
}

func (m *model) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	n := m.running.Add(1)
	defer m.running.Add(-1)
	for {
		old := m.maxAtOne.Load()
		if n <= old || m.maxAtOne.CompareAndSwap(old, n) {
			break
		}
	}
	m.mu.Lock()
	m.reqs = append(m.reqs, req)
	var s step
	if len(m.steps) > 0 {
		s, m.steps = m.steps[0], m.steps[1:]
	} else {
		s = step{text: "fertig"}
	}
	m.mu.Unlock()
	if s.hang {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if s.err != nil {
		return nil, s.err
	}
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &llm.ChatResponse{Choices: []llm.Choice{{Message: llm.Message{Role: llm.RoleAssistant, Content: s.text, ToolCalls: s.calls}}},
		Usage: llm.Usage{PromptTokens: 100, CompletionTokens: 10}}, nil
}

func call(name, args string) llm.ToolCall {
	return llm.ToolCall{Function: llm.FunctionCall{Name: name, Arguments: args}}
}

type confirmations struct{}

func (confirmations) PutConfirmation(context.Context, store.Confirmation, time.Duration) error {
	return nil
}

// env is a runtime with a local model "main", a cloud model "smart", a set
// of tools and a log to read.
type env struct {
	rt     *Runtime
	opts   Options
	main   *model
	smart  *model
	cfg    config.Config
	logs   *syncBuffer
	ran    map[string]int
	mu     sync.Mutex
	helper func(ctx context.Context) (Result, error)
	// onTool runs inside every tool of the env, when set.
	onTool func(name string)
	// st keeps the asynchronous runs; delivered gets each finished one.
	st        *store.Store
	now       time.Time
	delivered chan store.AgentRun
}

// syncBuffer is a log the runs' goroutines write to at once.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func (e *env) clock() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.now
}

func (e *env) advance(d time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.now = e.now.Add(d)
}

// restart is a new runtime on the same store, as after the agent started
// again; the agents are registered anew by the caller.
func (e *env) restart(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	e.rt.Close(ctx)
	e.rt = New(e.opts)
}

// waitDelivered is the next finished asynchronous run.
func (e *env) waitDelivered(t *testing.T) store.AgentRun {
	t.Helper()
	select {
	case rec := <-e.delivered:
		return rec
	case <-time.After(5 * time.Second):
		t.Fatal("no run was delivered")
	}
	return store.AgentRun{}
}

func setup(t *testing.T) *env {
	t.Helper()
	e := &env{main: &model{}, smart: &model{}, cfg: config.Default(), logs: &syncBuffer{}, ran: map[string]int{},
		now: time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC), delivered: make(chan store.AgentRun, 20)}
	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "mira.db"), Clock: e.clock})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	e.st = st
	tool := func(name string, readOnly, web, askAfterWeb bool) tools.InternalTool {
		return tools.InternalTool{Name: name, Description: name, Schema: `{"type":"object","properties":{}}`,
			ReadOnly: readOnly, WebContent: web, AskAfterWeb: askAfterWeb,
			Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
				e.mu.Lock()
				e.ran[name]++
				hook := e.onTool
				e.mu.Unlock()
				if hook != nil {
					hook(name)
				}
				return name + " ok", nil
			}}
	}
	helper := tools.InternalTool{Name: "ask_helper", Description: "asks another agent", Schema: `{"type":"object","properties":{}}`,
		ReadOnly: true,
		Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
			res, err := e.helper(ctx)
			return res.Text, err
		}}
	reg := tools.NewRegistry(nil,
		tool("look", true, false, false),               // reads the house
		tool("radio_play", false, false, false),        // acts on the house
		tool("web_page", true, true, false),            // reads the web
		tool(tools.ToolMemorySave, false, false, true), // remembers
		tool("lock_open", false, false, false),         // critical (a household hook)
		helper,
	)
	// lock_open is critical: a household hook asks for it (docs/spec/hooks.md).
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lock_open.star"), []byte(`DESCRIPTION = "lock_open nur mit Ja"
def pre_tool(call, verdict):
    if call.tool.name == "lock_open":
        return confirm()
`), 0o600); err != nil {
		t.Fatal(err)
	}
	guard := tools.NewGuard(i18n.DE, config.Default().Security, confirmations{}, func() structure.Snapshot { return structure.Snapshot{} },
		hooks.Load(dir, i18n.DE, nil), tools.WithRightOf(func(s tools.Spec) string { return RightFor(s).String() }))
	e.cfg.Models["smart"] = config.LLM{BaseURL: "https://api.example.com/v1", Privacy: config.PrivacyCloud, Parallel: 4}
	e.opts = Options{
		Config: func() config.Config { return e.cfg },
		Models: func(name string) (Chatter, config.LLM, bool) {
			switch name {
			case "main":
				return e.main, e.cfg.Main(), true
			case "smart":
				return e.smart, e.cfg.Models["smart"], true
			}
			return nil, config.LLM{}, false
		},
		Tools: reg, Guard: guard,
		Background: func(ctx context.Context, cc CallContext, instructions, input string) (Built, error) {
			lt, err := BuildTools(reg.Defs("", false))
			return Built{Messages: []llm.Message{{Role: llm.RoleSystem, Content: "Hintergrund"},
				{Role: llm.RoleUser, Content: instructions + "\n" + input}}, Tools: lt}, err
		},
		Runs:    st,
		Deliver: func(_ context.Context, rec store.AgentRun, _ CallContext) { e.delivered <- rec },
		Clock:   e.clock,
		Logger:  slog.New(slog.NewTextHandler(e.logs, nil)),
	}
	e.rt = New(e.opts)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		e.rt.Close(ctx)
	})
	return e
}

// background is an agent of the background profile with all tools.
func background(name string, rights Rights) Spec {
	return Spec{Name: name, Profile: Background, Tools: Tools{Kind: AllTools}, Rights: rights, Instructions: "Tu es."}
}

func (e *env) count(name string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.ran[name]
}

// toolResult is what the model got back for its n-th tool call, from the
// request after it.
func toolResult(m *model, n int) string {
	var results []string
	for _, msg := range m.reqs[len(m.reqs)-1].Messages {
		if msg.Role == llm.RoleTool {
			results = append(results, msg.Content)
		}
	}
	if n >= len(results) {
		return ""
	}
	return results[n]
}

// refused reports a "tool refused" line of the agent's run for the tool and
// the right it lacked.
func (e *env) refused(t *testing.T, agent, tool, right string) bool {
	t.Helper()
	for _, l := range e.logLines(t, "tool refused") {
		if strings.Contains(l, "agent="+agent) && strings.Contains(l, "tool="+tool) && strings.Contains(l, "needs="+right) {
			return true
		}
	}
	return false
}

func (e *env) logLines(t *testing.T, msg string) []string {
	t.Helper()
	var out []string
	for _, l := range strings.Split(e.logs.String(), "\n") {
		if strings.Contains(l, `msg="`+msg+`"`) {
			out = append(out, l)
		}
	}
	return out
}

// --- Rights --------------------------------------------------------------------

// AG-01 Effective is what both call context and agent allow
func TestAG01_EffectiveRightsAreTheIntersection(t *testing.T) {
	e := setup(t)
	// Given an agent whose description names the rights "read the house" and "web"
	// And a call context with the rights "read the house" and "act on the house"
	spec := background("a", ReadHouse|Web)
	cc := CallContext{Rights: ReadHouse | ActOnHouse}
	// When the agent runs with this call context (and tries all three)
	e.main.steps = []step{{calls: []llm.ToolCall{call("look", "{}"), call("radio_play", "{}"), call("web_page", "{}")}}, {text: "ok"}}
	if _, err := e.rt.Call(context.Background(), spec, "x", cc); err != nil {
		t.Fatal(err)
	}
	// Then its effective rights are only "read the house"
	if e.count("look") != 1 || e.count("radio_play") != 0 || e.count("web_page") != 0 {
		t.Errorf("ran: %v", e.ran)
	}
}

// AG-02 Without the right "act on the house" an acting action is refused
func TestAG02_ActingWithoutTheRightIsRefused(t *testing.T) {
	e := setup(t)
	// Given a run without the effective right "act on the house"
	// When the model calls an action that changes something in Home Assistant
	e.main.steps = []step{{calls: []llm.ToolCall{call("radio_play", "{}")}}, {text: "ok"}}
	e.rt.Call(context.Background(), background("a", ReadHouse), "x", CallContext{Rights: AllRights})
	// Then the action is not run
	// And the model gets as the result that this run may not act on the house
	if e.count("radio_play") != 0 || !strings.Contains(toolResult(e.main, 0), "act_on_house") {
		t.Errorf("ran %v, result %q", e.ran, toolResult(e.main, 0))
	}
}

// AG-03 A sub-agent gets at most its caller's effective rights
func TestAG03_SubAgentGetsAtMostTheCallersRights(t *testing.T) {
	e := setup(t)
	// Given a run with the effective rights "read the house" and "web"
	// And a second agent whose description names all rights
	var sub Result
	e.helper = func(ctx context.Context) (Result, error) {
		var err error
		sub, err = e.rt.Call(ctx, background("second", AllRights), "y", CallContext{Rights: AllRights})
		return sub, err
	}
	// When the run calls the second agent (which then tries to act and to read the web)
	e.main.steps = []step{{calls: []llm.ToolCall{call("ask_helper", "{}")}},
		{calls: []llm.ToolCall{call("radio_play", "{}"), call("web_page", "{}")}}, {text: "sub ok"}, {text: "ok"}}
	e.rt.Call(context.Background(), background("first", ReadHouse|Web), "x", CallContext{Rights: AllRights})
	// Then that one's call context holds at most "read the house" and "web":
	// the rights refuse acting (before the guard would), reading the web runs
	if e.count("radio_play") != 0 || e.count("web_page") != 1 || !e.refused(t, "second", "radio_play", "act_on_house") {
		t.Errorf("ran: %v\n%s", e.ran, e.logs)
	}
}

// AG-04 Who may not act on the house cannot do so through another agent either
func TestAG04_NoActingThroughAnotherAgent(t *testing.T) {
	e := setup(t)
	// Given a run without the effective right "act on the house"
	// And a second agent that can call actions in Home Assistant
	e.helper = func(ctx context.Context) (Result, error) {
		return e.rt.Call(ctx, background("second", AllRights), "y", CallContext{})
	}
	// When the run calls the second agent and that one's model calls an acting action
	e.main.steps = []step{{calls: []llm.ToolCall{call("ask_helper", "{}")}},
		{calls: []llm.ToolCall{call("radio_play", "{}")}}, {text: "sub"}, {text: "ok"}}
	e.rt.Call(context.Background(), background("first", ReadHouse), "x", CallContext{Rights: AllRights})
	// Then the action is not run, refused by the rights
	if e.count("radio_play") != 0 || !e.refused(t, "second", "radio_play", "act_on_house") {
		t.Errorf("ran: %v\n%s", e.ran, e.logs)
	}
}

// AG-05 Rights do not replace the guard
func TestAG05_RightsDoNotReplaceTheGuard(t *testing.T) {
	e := setup(t)
	// Given a run with the effective right "act on the house"
	// When the model calls a critical action (such as opening a lock)
	e.main.steps = []step{{calls: []llm.ToolCall{call("lock_open", "{}")}}, {text: "ok"}}
	e.rt.Call(context.Background(), background("a", AllRights), "x", CallContext{Rights: AllRights})
	// Then the guard does not just run it: in the background, where nobody
	// can confirm, it refuses
	if e.count("lock_open") != 0 || toolResult(e.main, 0) == "" || strings.Contains(toolResult(e.main, 0), "ok") {
		t.Errorf("ran %v, result %q", e.ran, toolResult(e.main, 0))
	}
}

// AG-08 In the background the rights decide what may change
func TestAG08_InTheBackgroundTheRightsDecide(t *testing.T) {
	e := setup(t)
	// Given a background run with the effective right "act on the house"
	// When the model calls an action that changes something and is not critical
	e.main.steps = []step{{calls: []llm.ToolCall{call("radio_play", "{}")}}, {text: "ok"}}
	e.rt.Call(context.Background(), background("a", ReadHouse|ActOnHouse), "x", CallContext{Rights: AllRights})
	// Then it runs
	if e.count("radio_play") != 1 {
		t.Errorf("ran %v, result %q", e.ran, toolResult(e.main, 0))
	}
}

// AG-06 A sub-agent's third-party text counts for the caller
func TestAG06_SubAgentsThirdPartyTextCountsForTheCaller(t *testing.T) {
	e := setup(t)
	// Given a run without any third-party text involved yet
	e.helper = func(ctx context.Context) (Result, error) {
		return e.rt.Call(ctx, background("reader", ReadHouse|Web), "y", CallContext{})
	}
	// When it calls a sub-agent that reads a web page
	// And the run then wants to keep or pass something on (remember, message)
	e.main.steps = []step{{calls: []llm.ToolCall{call("ask_helper", "{}")}},
		{calls: []llm.ToolCall{call("web_page", "{}")}}, {text: "Seite gelesen"},
		{calls: []llm.ToolCall{call(tools.ToolMemorySave, `{"text":"x"}`)}}, {text: "ok"}}
	res, _ := e.rt.Call(context.Background(), background("first", AllRights), "x", CallContext{Rights: AllRights})
	// Then it counts as after web content in its own run: in the background
	// it is refused
	if e.count(tools.ToolMemorySave) != 0 || !res.Foreign {
		t.Errorf("ran %v, foreign %v", e.ran, res.Foreign)
	}
}

// AG-07 After web content critical actions stay locked
func TestAG07_AfterWebContentCriticalActionsStayLocked(t *testing.T) {
	e := setup(t)
	// Given a run that has read web content
	// When the model then calls a critical action
	e.main.steps = []step{{calls: []llm.ToolCall{call("web_page", "{}")}}, {calls: []llm.ToolCall{call("lock_open", "{}")}}, {text: "ok"}}
	e.rt.Call(context.Background(), background("a", AllRights), "x", CallContext{Rights: AllRights})
	// Then it is not run, not even with a yes
	if e.count("lock_open") != 0 || e.count("web_page") != 1 {
		t.Errorf("ran: %v", e.ran)
	}
}

// --- Depth ---------------------------------------------------------------------

// AG-10 An outside call has depth 0, each sub-agent one more
func TestAG10_OutsideCallDepthZeroSubAgentOneMore(t *testing.T) {
	e := setup(t)
	e.helper = func(ctx context.Context) (Result, error) {
		return e.rt.Call(ctx, Spec{Name: "second", Profile: Isolated}, "y", CallContext{})
	}
	// When Home Assistant calls an agent (which calls another)
	e.main.steps = []step{{calls: []llm.ToolCall{call("ask_helper", "{}")}}, {text: "sub"}, {text: "ok"}}
	e.rt.Call(context.Background(), background("first", ReadHouse), "x", CallContext{Origin: FromAutomation, Rights: ReadHouse})
	// Then its call context has depth 0
	// And an agent it calls has depth 1
	lines := e.logLines(t, "agent done")
	if len(lines) != 2 || !strings.Contains(lines[0], "agent=second") || !strings.Contains(lines[0], "depth=1") ||
		!strings.Contains(lines[1], "agent=first") || !strings.Contains(lines[1], "depth=0") {
		t.Errorf("log:\n%s", strings.Join(lines, "\n"))
	}
}

// AG-11 A call beyond the allowed depth fails
func TestAG11_CallBeyondTheAllowedDepthFails(t *testing.T) {
	e := setup(t)
	// Given the setting agents.max_depth is not set (the default, 3)
	// And a run with depth 3
	var subErr error
	e.helper = func(ctx context.Context) (Result, error) {
		var res Result
		res, subErr = e.rt.Call(ctx, Spec{Name: "second", Profile: Isolated}, "y", CallContext{})
		return res, subErr
	}
	// When it calls another agent
	e.main.steps = []step{{calls: []llm.ToolCall{call("ask_helper", "{}")}}, {text: "ok"}}
	e.rt.Call(context.Background(), background("first", ReadHouse), "x", CallContext{Rights: ReadHouse, Depth: 3})
	// Then the call fails
	// And the reason is that the allowed depth is reached
	if !errors.Is(subErr, ErrDepth) || !strings.Contains(toolResult(e.main, 0), "depth") {
		t.Errorf("sub error %v, result %q", subErr, toolResult(e.main, 0))
	}
}

// AG-12 The allowed depth is a setting
func TestAG12_TheAllowedDepthIsASetting(t *testing.T) {
	e := setup(t)
	// Given agents.max_depth is 1
	e.cfg.Agents.MaxDepth = 1
	// And a run with depth 1
	var subErr error
	e.helper = func(ctx context.Context) (Result, error) {
		var res Result
		res, subErr = e.rt.Call(ctx, Spec{Name: "second", Profile: Isolated}, "y", CallContext{})
		return res, subErr
	}
	// When it calls another agent
	e.main.steps = []step{{calls: []llm.ToolCall{call("ask_helper", "{}")}}, {text: "ok"}}
	e.rt.Call(context.Background(), background("first", ReadHouse), "x", CallContext{Rights: ReadHouse, Depth: 1})
	// Then the call fails
	if !errors.Is(subErr, ErrDepth) {
		t.Errorf("sub error %v", subErr)
	}
}

// --- Synchronous -----------------------------------------------------------------

// AG-20 Synchronously the caller gets the answer
func TestAG20_SynchronouslyTheCallerGetsTheAnswer(t *testing.T) {
	e := setup(t)
	// When an agent is called synchronously with a timeout
	// And it is done before the timeout
	e.main.steps = []step{{text: "Antwort"}}
	res, err := e.rt.Call(context.Background(), Spec{Name: "a", Profile: Isolated, Instructions: "sys"}, "frage",
		CallContext{Mode: Mode{Timeout: time.Second}})
	// Then the caller gets its answer
	if err != nil || res.Text != "Antwort" || res.Status != Done {
		t.Errorf("%+v %v", res, err)
	}
	if msgs := e.main.reqs[0].Messages; len(msgs) != 2 || msgs[0].Content != "sys" || msgs[1].Content != "frage" {
		t.Errorf("isolated context: %+v", msgs)
	}
}

// AG-21 Timeout, and the caller wants to cancel
func TestAG21_TimeoutAndTheCallerCancels(t *testing.T) {
	e := setup(t)
	// Given a synchronous call whose caller wants to cancel at the timeout
	// When the agent is not done by the timeout
	e.main.steps = []step{{hang: true}}
	res, err := e.rt.Call(context.Background(), Spec{Name: "a", Profile: Isolated}, "x",
		CallContext{Mode: Mode{Timeout: 30 * time.Millisecond, OnTimeout: Cancel}})
	// Then the run is cancelled
	// And the caller gets that the time is up
	if !errors.Is(err, ErrTimeout) || res.Status != Cancelled {
		t.Errorf("%+v %v", res, err)
	}
}

// AG-23 An output that does not follow the schema is no result
func TestAG23_OutputNotFollowingTheSchemaIsNoResult(t *testing.T) {
	e := setup(t)
	// Given an agent with a schema for its output
	spec := Spec{Name: "a", Profile: Isolated, Output: json.RawMessage(`{"type":"object","properties":{"day":{"type":"string"},"n":{"type":"integer"}},"required":["day"]}`)}
	// When the model gives an answer that does not follow the schema
	for _, bad := range []string{`{"n":2}`, `{"day":3}`, `{"day":"heute","n":1.5}`, `kein JSON`} {
		e.main.steps = []step{{text: bad}}
		res, err := e.rt.Call(context.Background(), spec, "x", CallContext{})
		// Then the run counts as failed
		if !errors.Is(err, ErrOutput) || res.Status != Failed {
			t.Errorf("%s: %+v %v", bad, res, err)
		}
	}
	e.main.steps = []step{{text: "Hier: {\"day\":\"heute\",\"n\":2}"}}
	if res, err := e.rt.Call(context.Background(), spec, "x", CallContext{}); err != nil || res.Text != `{"day":"heute","n":2}` {
		t.Errorf("good answer: %+v %v", res, err)
	}
}

// AG-24 A hanging model call is retried following the limits
func TestAG24_HangingModelCallIsRetried(t *testing.T) {
	e := setup(t)
	// Given an agent with the limits "60 s per attempt, retry once" (shortened here)
	spec := Spec{Name: "a", Profile: Isolated, Limits: Limits{AttemptTimeout: 30 * time.Millisecond, Retries: 1}}
	// When the first model call has no answer in time
	e.main.steps = []step{{hang: true}, {text: "zweiter Versuch"}}
	res, err := e.rt.Call(context.Background(), spec, "x", CallContext{})
	// Then it is retried once
	// And the run goes on with the answer of the second attempt
	if err != nil || res.Text != "zweiter Versuch" || res.ModelCalls != 2 {
		t.Errorf("%+v %v", res, err)
	}
}

// --- Log -----------------------------------------------------------------------

// AG-50 Every run writes a line "agent done"
func TestAG50_EveryRunWritesALine(t *testing.T) {
	e := setup(t)
	// When a run ends, however it ends
	e.main.steps = []step{{text: "ok"}}
	e.rt.Call(context.Background(), Spec{Name: "good", Profile: Isolated}, "x", CallContext{Origin: FromSchedule})
	e.rt.Call(context.Background(), Spec{Name: "deep", Profile: Isolated}, "x", CallContext{Depth: 9})
	// Then the log has a line "agent done"
	// And it names agent, call ID, depth, origin, mode, status, model, duration, model calls and tokens
	lines := e.logLines(t, "agent done")
	if len(lines) != 2 {
		t.Fatalf("lines: %q", lines)
	}
	for _, f := range []string{"agent=good", "call_id=", "depth=0", "origin=schedule", "mode=sync", "status=done", "model=main",
		"ms=", "model_calls=1", "prompt_tokens=100", "completion_tokens=10"} {
		if !strings.Contains(lines[0], f) {
			t.Errorf("%s missing in %s", f, lines[0])
		}
	}
	if !strings.Contains(lines[1], "status=failed") || !strings.Contains(lines[1], "error=") {
		t.Errorf("failed run: %s", lines[1])
	}
}

// AG-51 Sub-agents can be matched to their caller
func TestAG51_SubAgentsCanBeMatchedToTheirCaller(t *testing.T) {
	e := setup(t)
	e.helper = func(ctx context.Context) (Result, error) {
		return e.rt.Call(ctx, Spec{Name: "second", Profile: Isolated}, "y", CallContext{})
	}
	// When a run calls another agent
	e.main.steps = []step{{calls: []llm.ToolCall{call("ask_helper", "{}")}}, {text: "sub"}, {text: "ok"}}
	res, _ := e.rt.Call(context.Background(), background("first", ReadHouse), "x", CallContext{Rights: ReadHouse})
	// Then the called run's line "agent done" names the calling run's call ID and its depth
	lines := e.logLines(t, "agent done")
	if len(lines) != 2 || !strings.Contains(lines[0], "parent_id="+res.CallID) || !strings.Contains(lines[0], "depth=1") {
		t.Errorf("log:\n%s", strings.Join(lines, "\n"))
	}
}

// --- Model ---------------------------------------------------------------------

// AG-60 An agent in the tool list is a tool
func TestAG60_AnAgentInTheToolListIsATool(t *testing.T) {
	e := setup(t)
	// Given an agent with a second agent in its tool list
	e.rt.Register(Spec{Name: "house.check", Description: "prüft das Haus", Profile: Background,
		Tools: Tools{Kind: ListedTools, Names: []string{"look"}}, Rights: ReadHouse})
	first := background("first", AllRights)
	first.Tools = Tools{Kind: ListedTools, Names: []string{"look", "house.check"}}
	// When its model is called
	e.main.steps = []step{{calls: []llm.ToolCall{call("house_check", `{"input":"Ist alles zu?"}`)}},
		{calls: []llm.ToolCall{call("look", "{}")}}, {text: "alles zu"}, {text: "ok"}}
	e.rt.Call(context.Background(), first, "x", CallContext{Rights: AllRights})
	// Then it sees the second agent as a tool, with name, description and input schema
	var sub *llm.Tool
	for i, tl := range e.main.reqs[0].Tools {
		if tl.Function.Name == "house_check" {
			sub = &e.main.reqs[0].Tools[i]
		}
	}
	if sub == nil || sub.Function.Description != "prüft das Haus" || !strings.Contains(string(sub.Function.Parameters), `"input"`) {
		t.Fatalf("tools offered: %+v", e.main.reqs[0].Tools)
	}
	// and calling it runs the second agent with the input
	if e.count("look") != 1 || !strings.Contains(lastUser(e.main.reqs[1]), "Ist alles zu?") || toolResult(e.main, 0) != "alles zu" {
		t.Errorf("ran %v, sub input %q, result %q", e.ran, lastUser(e.main.reqs[1]), toolResult(e.main, 0))
	}
}

// AG-61 An agent not in the tool list is no tool
func TestAG61_AnAgentNotInTheToolListIsNoTool(t *testing.T) {
	e := setup(t)
	// Given an agent without a second agent in its tool list
	e.rt.Register(Spec{Name: "house.check", Description: "prüft das Haus", Profile: Background})
	first := background("first", AllRights)
	first.Tools = Tools{Kind: ListedTools, Names: []string{"look"}}
	// When its model is called
	e.rt.Call(context.Background(), first, "x", CallContext{Rights: AllRights})
	// Then it does not see the second agent
	for _, tl := range e.main.reqs[0].Tools {
		if tl.Function.Name == "house_check" {
			t.Fatal("the second agent is offered")
		}
	}
}

func lastUser(req llm.ChatRequest) string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == llm.RoleUser {
			return req.Messages[i].Content
		}
	}
	return ""
}

// AG-70 Without an entry for its role an agent takes main
func TestAG70_WithoutAnEntryTheAgentTakesMain(t *testing.T) {
	e := setup(t)
	// Given mira.yaml names no model for an agent
	// When the agent runs
	res, _ := e.rt.Call(context.Background(), Spec{Name: "mail.read", Profile: Isolated}, "x", CallContext{})
	// Then it takes the model main
	if res.Model != "main" || len(e.main.reqs) != 1 || len(e.smart.reqs) != 0 {
		t.Errorf("model %s, main %d, smart %d", res.Model, len(e.main.reqs), len(e.smart.reqs))
	}
}

// AG-71 With an entry an agent takes the model entered
func TestAG71_WithAnEntryTheAgentTakesThatModel(t *testing.T) {
	e := setup(t)
	// Given mira.yaml names the model smart for an agent
	if err := yaml.Unmarshal([]byte("mail.read: { model: smart }"), &e.cfg.Agents); err != nil {
		t.Fatal(err)
	}
	// When the agent runs
	res, _ := e.rt.Call(context.Background(), Spec{Name: "mail.read", Profile: Isolated}, "x", CallContext{})
	// Then it takes the model smart
	if res.Model != "smart" || len(e.smart.reqs) != 1 || len(e.main.reqs) != 0 {
		t.Errorf("model %s, main %d, smart %d", res.Model, len(e.main.reqs), len(e.smart.reqs))
	}
}

// --- Runtime -------------------------------------------------------------------

// runTwo starts two runs at the same moment on a model and waits for both.
func runTwo(e *env, spec Spec) {
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.rt.Call(context.Background(), spec, fmt.Sprint(i), CallContext{})
		}()
	}
	wg.Wait()
}

// AG-80 A local model serves one run at a time
func TestAG80_LocalModelServesOneRunAtATime(t *testing.T) {
	e := setup(t)
	// Given two runs that use the same local model
	e.main.delay = 40 * time.Millisecond
	// When both want to call the model at the same moment
	runTwo(e, Spec{Name: "a", Profile: Isolated})
	// Then the second waits until the first call is answered
	if got := e.main.maxAtOne.Load(); got != 1 || len(e.main.reqs) != 2 {
		t.Errorf("at once %d, calls %d", got, len(e.main.reqs))
	}
}

// AG-81 A cloud model serves several runs at once
func TestAG81_CloudModelServesSeveralRunsAtOnce(t *testing.T) {
	e := setup(t)
	// Given two runs that use the same cloud model
	e.smart.delay = 40 * time.Millisecond
	// When both call the model at the same moment
	runTwo(e, Spec{Name: "a", Model: "smart", Profile: Isolated})
	// Then both calls run at the same time
	if got := e.smart.maxAtOne.Load(); got != 2 {
		t.Errorf("at once %d", got)
	}
}

// --- not scenarios ---------------------------------------------------------------

func TestTextsComplete(t *testing.T) {
	if missing := i18n.Missing(catalog); len(missing) > 0 {
		t.Errorf("texts missing: %v", missing)
	}
}

func TestRightForTools(t *testing.T) {
	cases := map[string]struct {
		spec tools.Spec
		want Rights
	}{
		"ha read":      {tools.Spec{ReadOnly: true}, ReadHouse},
		"ha act":       {tools.Spec{}, ActOnHouse},
		"web":          {tools.Spec{ReadOnly: true, WebContent: true}, Web},
		"memory_save":  {tools.Spec{Internal: true}, Remember},
		"message_send": {tools.Spec{Internal: true}, Messages},
		"sleep_timer":  {tools.Spec{Internal: true}, ActOnHouse},
		"skill_open":   {tools.Spec{Internal: true, ReadOnly: true}, ReadHouse},
	}
	for name, c := range cases {
		c.spec.Name = name
		if got := RightFor(c.spec); got != c.want {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}

// An agent of read-only tools is offered only those; an agent without tools
// still gets the prefix's tools, with tool_choice none (the model's cache).
func TestToolsOffered(t *testing.T) {
	e := setup(t)
	spec := background("a", AllRights)
	spec.Tools = Tools{Kind: ReadOnlyTools}
	e.rt.Call(context.Background(), spec, "x", CallContext{Rights: AllRights})
	var names []string
	for _, tl := range e.main.reqs[0].Tools {
		names = append(names, tl.Function.Name)
	}
	if got := strings.Join(names, ","); got != "ask_helper,look,web_page" {
		t.Errorf("read-only tools offered: %s", got)
	}

	spec.Tools = Tools{Kind: NoTools}
	e.rt.Call(context.Background(), spec, "x", CallContext{Rights: AllRights})
	if req := e.main.reqs[1]; len(req.Tools) != 6 || req.ToolChoice != "none" {
		t.Errorf("no tools: %d tools sent, tool_choice %q", len(req.Tools), req.ToolChoice)
	}
}

// An agent with tools and an output schema sends the answer's form with
// the tools, in every round (Decided 35).
func TestOutputSchemaGoesAlongWithTools(t *testing.T) {
	e := setup(t)
	spec := background("a", AllRights)
	spec.Output = json.RawMessage(`{"type":"object","properties":{"satz":{"type":"string"}},"required":["satz"]}`)
	e.main.steps = []step{{calls: []llm.ToolCall{call("look", "{}")}}, {text: `{"satz":"25 Grad"}`}}
	res, err := e.rt.Call(context.Background(), spec, "x", CallContext{Rights: AllRights})
	if err != nil || res.Text != `{"satz":"25 Grad"}` {
		t.Fatalf("%+v, %v", res, err)
	}
	for i, req := range e.main.reqs {
		if len(req.Tools) == 0 || req.ResponseFormat == nil {
			t.Errorf("request %d: %d tools, response_format %v", i, len(req.Tools), req.ResponseFormat)
		}
	}
}

// MO-05 How many requests at once follows privacy unless set
func TestMO05_RequestsAtOnceFollowPrivacyUnlessSet(t *testing.T) {
	// Given a local model and a cloud model without parallel, and a cloud model with parallel 2
	cfg, err := config.Parse([]byte(`
ha: { url: http://ha, token: t }
models:
  main: { endpoint: http://lokal/v1, model: m, privacy: local }
  smart: { endpoint: https://cloud/v1, model: m, privacy: cloud }
  lite: { endpoint: https://cloud/v1, model: m, privacy: cloud, parallel: 2 }
`), func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	clients := map[string]*model{"main": {delay: 40 * time.Millisecond}, "smart": {delay: 40 * time.Millisecond},
		"lite": {delay: 40 * time.Millisecond}}
	rt := New(Options{Config: func() config.Config { return cfg },
		Models: func(name string) (Chatter, config.LLM, bool) {
			m, ok := cfg.Model(name)
			return clients[name], m, ok
		}})
	// When requests come in at the same time
	var wg sync.WaitGroup
	for name := range clients {
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				rt.Call(context.Background(), Spec{Name: "a", Model: name, Profile: Isolated}, "x", CallContext{})
			}()
		}
	}
	wg.Wait()
	// Then the local one takes 1 at a time, the first cloud one 4, the second 2
	if a, b, c := clients["main"].maxAtOne.Load(), clients["smart"].maxAtOne.Load(), clients["lite"].maxAtOne.Load(); a != 1 || b != 4 || c != 2 {
		t.Errorf("at once: main %d, smart %d, lite %d", a, b, c)
	}
}

// MO-10 An agent without a model takes main
func TestMO10_AnAgentWithoutAModelTakesMain(t *testing.T) {
	e := setup(t)
	// Given Mira's own agent tasks.assess with no entry under agents
	// When it runs
	res, _ := e.rt.Call(context.Background(), Spec{Name: "tasks.assess", Profile: Isolated}, "x", CallContext{})
	// Then it takes main
	if res.Model != "main" || e.main.calls() != 1 {
		t.Errorf("model %q", res.Model)
	}
}

// fallbackEnv has the models main (local), smart and lite (cloud), each a
// scripted model of its own; their fallbacks come from the test.
func fallbackEnv(t *testing.T, models string) (*Runtime, map[string]*model, *syncBuffer) {
	t.Helper()
	cfg, err := config.Parse([]byte("ha: { url: http://ha, token: t }\n"+models), func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	clients := map[string]*model{"main": {}, "smart": {}, "lite": {}}
	logs := &syncBuffer{}
	rt := New(Options{Config: func() config.Config { return cfg },
		Models: func(name string) (Chatter, config.LLM, bool) {
			m, ok := cfg.Model(name)
			return clients[name], m, ok
		}, Logger: slog.New(slog.NewTextHandler(logs, nil))})
	return rt, clients, logs
}

const threeModels = `
models:
  main: { endpoint: http://lokal/v1, model: m, privacy: local, max_tokens: 100 }
  smart: { endpoint: https://cloud/v1, model: m, privacy: cloud, fallback: %s, max_tokens: 900 }
  lite: { endpoint: https://cloud/v1, model: m, privacy: cloud, fallback: main }
`

// MO-30 A failing model passes to its fallback
func TestMO30_AFailingModelPassesToItsFallback(t *testing.T) {
	// Given smart with fallback main
	rt, clients, logs := fallbackEnv(t, fmt.Sprintf(threeModels, "main"))
	// When smart fails after its retries
	clients["smart"].steps = []step{{err: errors.New("503")}}
	clients["main"].steps = []step{{text: "von main"}}
	res, err := rt.Call(context.Background(), Spec{Name: "a", Model: "smart", Profile: Isolated}, "x", CallContext{})
	// Then the call is made on main, with main's settings
	// And the log line names main and that it was a fallback from smart
	if err != nil || res.Text != "von main" || res.Model != "main" || clients["main"].reqs[0].MaxTokens != 100 {
		t.Fatalf("%+v, %v", res, err)
	}
	if !strings.Contains(logs.String(), "model=main") || !strings.Contains(logs.String(), "fallback_from=smart") {
		t.Errorf("log:\n%s", logs)
	}
}

// MO-31 A busy model is waited for, not passed by
func TestMO31_ABusyModelIsWaitedForNotPassedBy(t *testing.T) {
	// Given smart with fallback main and all of smart's requests taken
	rt, clients, _ := fallbackEnv(t, `
models:
  main: { endpoint: http://lokal/v1, model: m, privacy: local }
  smart: { endpoint: https://cloud/v1, model: m, privacy: cloud, parallel: 1, fallback: main }
`)
	clients["smart"].steps = []step{{hang: true}, {text: "endlich"}}
	busy, stop := context.WithCancel(context.Background())
	defer stop()
	go rt.Call(busy, Spec{Name: "a", Model: "smart", Profile: Isolated}, "x", CallContext{})
	time.Sleep(30 * time.Millisecond)
	// When another call for smart comes
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	_, err := rt.Call(ctx, Spec{Name: "b", Model: "smart", Profile: Isolated}, "x", CallContext{})
	// Then it waits for smart
	if err == nil || clients["main"].calls() != 0 {
		t.Errorf("err %v, main called %d times", err, clients["main"].calls())
	}
}

// MO-33 A chain of fallbacks is followed
func TestMO33_AChainOfFallbacksIsFollowed(t *testing.T) {
	// Given smart with fallback lite and lite with fallback main
	rt, clients, _ := fallbackEnv(t, fmt.Sprintf(threeModels, "lite"))
	// When smart and lite fail
	clients["smart"].steps = []step{{err: errors.New("503")}}
	clients["lite"].steps = []step{{err: errors.New("timeout")}}
	clients["main"].steps = []step{{text: "von main"}}
	res, err := rt.Call(context.Background(), Spec{Name: "a", Model: "smart", Profile: Isolated}, "x", CallContext{})
	// Then the call is made on main
	if err != nil || res.Model != "main" || res.FallbackFrom != "smart" || clients["lite"].calls() != 1 {
		t.Errorf("%+v, %v", res, err)
	}
}

// MO-22 A local requirement never falls back to the cloud
func TestMO22_ALocalRequirementNeverFallsBackToTheCloud(t *testing.T) {
	// Given an agent with privacy local on a local model whose fallback is a cloud model
	rt, clients, _ := fallbackEnv(t, `
models:
  main: { endpoint: http://lokal/v1, model: m, privacy: local, fallback: smart }
  smart: { endpoint: https://cloud/v1, model: m, privacy: cloud }
`)
	// When the local model fails
	clients["main"].steps = []step{{err: errors.New("lm studio is down")}}
	_, err := rt.Call(context.Background(), Spec{Name: "a", Privacy: config.PrivacyLocal, Profile: Isolated}, "x", CallContext{})
	// Then the cloud model is not called, and the call fails
	if err == nil || clients["smart"].calls() != 0 {
		t.Errorf("err %v, smart called %d times", err, clients["smart"].calls())
	}
	// Without the requirement the fallback is taken.
	clients["main"].steps = []step{{err: errors.New("lm studio is down")}}
	if res, err := rt.Call(context.Background(), Spec{Name: "b", Profile: Isolated}, "x", CallContext{}); err != nil || res.Model != "smart" {
		t.Errorf("without privacy: %+v, %v", res, err)
	}
}
