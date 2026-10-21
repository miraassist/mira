package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/ha/structure"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/tools"
)

// --- fakes -------------------------------------------------------------------

// step is one scripted model answer: tool calls, text, or a hang until the
// call's context ends.
type step struct {
	calls []llm.ToolCall
	text  string
	hang  bool
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
	main   *model
	smart  *model
	cfg    config.Config
	logs   *bytes.Buffer
	ran    map[string]int
	mu     sync.Mutex
	helper func(ctx context.Context) (Result, error)
}

func setup(t *testing.T) *env {
	t.Helper()
	e := &env{main: &model{}, smart: &model{}, cfg: config.Default(), logs: &bytes.Buffer{}, ran: map[string]int{}}
	tool := func(name string, readOnly, web, askAfterWeb bool) tools.InternalTool {
		return tools.InternalTool{Name: name, Description: name, Schema: `{"type":"object","properties":{}}`,
			ReadOnly: readOnly, WebContent: web, AskAfterWeb: askAfterWeb,
			Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
				e.mu.Lock()
				e.ran[name]++
				e.mu.Unlock()
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
		tool("lock_open", false, false, false),         // critical (security.confirm_tools)
		helper,
	)
	sec := config.Default().Security
	sec.ConfirmTools = []string{"lock_open"}
	guard := tools.NewGuard(i18n.DE, sec, confirmations{}, func() structure.Snapshot { return structure.Snapshot{} })
	e.cfg.Models = map[string]config.LLM{"smart": {BaseURL: "https://api.example.com/v1"}}
	e.cfg.LLM.BaseURL = "http://localhost:1234/v1"
	e.rt = New(Options{
		Config: func() config.Config { return e.cfg },
		Models: func(name string) (Chatter, config.LLM, bool) {
			switch name {
			case "main":
				return e.main, e.cfg.LLM, true
			case "smart":
				return e.smart, e.cfg.Models["smart"], true
			}
			return nil, config.LLM{}, false
		},
		Tools: reg, Guard: guard,
		Background: func(ctx context.Context, cc CallContext, instructions, input string) (Built, error) {
			lt, err := BuildTools(reg.Defs(false))
			return Built{Messages: []llm.Message{{Role: llm.RoleSystem, Content: "Hintergrund"},
				{Role: llm.RoleUser, Content: instructions + "\n" + input}}, Tools: lt}, err
		},
		Logger: slog.New(slog.NewTextHandler(e.logs, nil)),
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
	// Then the guard asks first, as before
	if e.count("lock_open") != 0 || toolResult(e.main, 0) == "" || strings.Contains(toolResult(e.main, 0), "ok") {
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
	// Then Mira asks first, as after web content in its own run
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

// AG-70 Without an entry for its role an agent takes main
func TestAG70_WithoutAnEntryTheAgentTakesMain(t *testing.T) {
	e := setup(t)
	// Given routing has nothing for an agent's role
	// When the agent runs
	res, _ := e.rt.Call(context.Background(), Spec{Name: "a", Role: "mail", Profile: Isolated}, "x", CallContext{})
	// Then it takes the model main
	if res.Model != "main" || len(e.main.reqs) != 1 || len(e.smart.reqs) != 0 {
		t.Errorf("model %s, main %d, smart %d", res.Model, len(e.main.reqs), len(e.smart.reqs))
	}
}

// AG-71 With an entry an agent takes the model entered
func TestAG71_WithAnEntryTheAgentTakesThatModel(t *testing.T) {
	e := setup(t)
	// Given routing has the model smart for an agent's role
	e.cfg.Routing.Mail = "smart"
	// When the agent runs
	res, _ := e.rt.Call(context.Background(), Spec{Name: "a", Role: "mail", Profile: Isolated}, "x", CallContext{})
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

func TestIsLocal(t *testing.T) {
	for url, want := range map[string]bool{
		"http://localhost:1234/v1": true, "http://192.168.178.10:8080": true, "http://127.0.0.1": true,
		"http://lmstudio.local:1234": true, "https://generativelanguage.googleapis.com/v1beta/openai": false,
		"https://api.example.com": false, "http://10.0.0.5": true,
	} {
		if got := isLocal(url); got != want {
			t.Errorf("%s: %v", url, got)
		}
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
