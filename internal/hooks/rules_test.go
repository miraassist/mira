package hooks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/prompt"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/threads"
	"github.com/miraassist/mira/internal/tools"
)

// Writing rules on request (docs/spec/hooks-schreiben.md).

const denyAll = `DESCRIPTION = "nichts"
def pre_tool(call, verdict):
    return deny("nein")
`

func ruleTool(f fixture, name string) tools.InternalTool {
	for _, t := range f.e.Tools("regel", func() string { return "house" }) {
		if t.Name == name {
			return t
		}
	}
	panic(name)
}

func own(name string) tools.Spec {
	return tools.Spec{ToolDef: prompt.ToolDef{Name: name}, Internal: true}
}

// HW-01 A hook with ACTIVE = False is loaded but not called
func TestHW01_AHookWithActiveFalseIsLoadedButNotCalled(t *testing.T) {
	f := newFixture(t, map[string]string{"aus.star": "ACTIVE = False\n" + denyAll})
	if v := f.g.Check(context.Background(), voice(), turnOn, args(`{"name":"Küchenlicht"}`)); v.Outcome != tools.Allow {
		t.Fatalf("%+v", v)
	}
	var off bool
	for _, i := range f.e.Infos() {
		if i.Name == "aus.star" {
			off = i.Off
		}
	}
	if !off {
		t.Errorf("not shown as off: %+v", f.e.Infos())
	}
}

// HW-02 A hook without ACTIVE is active
func TestHW02_AHookWithoutActiveIsActive(t *testing.T) {
	f := newFixture(t, map[string]string{"an.star": denyAll})
	if v := f.g.Check(context.Background(), voice(), turnOn, args(`{"name":"Küchenlicht"}`)); v.Outcome != tools.Deny {
		t.Fatalf("%+v", v)
	}
}

// HW-03 A changed file applies without a restart
func TestHW03_AChangedFileAppliesWithoutARestart(t *testing.T) {
	f := newFixture(t, map[string]string{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go f.e.Watch(ctx, 20*time.Millisecond)
	outcome := func() tools.Outcome {
		return f.g.Check(context.Background(), voice(), turnOn, args(`{"name":"Küchenlicht"}`)).Outcome
	}
	wait := func(want tools.Outcome) {
		t.Helper()
		for range 200 {
			if outcome() == want {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("still not %v", want)
	}
	if outcome() != tools.Allow {
		t.Fatal("denied before")
	}
	path := filepath.Join(f.e.dir, "neu.star")
	if err := os.WriteFile(path, []byte(denyAll), 0o600); err != nil {
		t.Fatal(err)
	}
	wait(tools.Deny)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	wait(tools.Allow)
}

// HW-04 A broken file while running is reported, the others apply
func TestHW04_ABrokenFileWhileRunningIsReportedTheOthersApply(t *testing.T) {
	f := newFixture(t, map[string]string{"gut.star": logName("gut")})
	if err := os.WriteFile(filepath.Join(f.e.dir, "kaputt.star"), []byte("def x("), 0o600); err != nil {
		t.Fatal(err)
	}
	f.e.Reload()
	if p := f.e.Problems(); len(p) != 1 || p[0].File != "kaputt.star" {
		t.Fatalf("problems %v", p)
	}
	f.logs.Reset()
	f.g.Check(context.Background(), voice(), turnOn, args(`{"name":"Küchenlicht"}`))
	if len(f.lines("hook log")) != 1 {
		t.Errorf("gut.star did not run: %s", f.logs)
	}
}

// A household critical.star switched off does not switch off the default:
// critical.star cannot be switched off (hooks-schreiben.md, decision 8).
func TestCriticalCannotBeSwitchedOff(t *testing.T) {
	f := newFixture(t, map[string]string{"critical.star": `DESCRIPTION = "x"
ACTIVE = False
DOMAINS = []
def critical(e):
    return False
def pre_tool(call, verdict):
    return None
`})
	if v := f.g.Check(context.Background(), voice(), turnOn, args(`{"name":"Haustür"}`)); v.Reason != "default/critical.star" {
		t.Fatalf("%+v", v)
	}
}

// HW-11 Only a known person may change rules
func TestHW11_OnlyAKnownPersonMayChangeRules(t *testing.T) {
	f := newFixture(t, map[string]string{"musik.star": logName("x")})
	sw := args(`{"name":"musik.star","on":false}`)
	if v := f.g.Check(context.Background(), voice(), own(ToolSwitch), sw); v.Outcome != tools.Deny || v.Reason != "system/self.star" {
		t.Fatalf("unknown voice: %+v", v)
	}
	ben := &tools.TurnState{SessionKey: "s", Channel: config.ChannelVoice, Person: "ben", PersonBy: prompt.ByVoice}
	if v := f.g.Check(context.Background(), ben, own(ToolSwitch), sw); v.Outcome != tools.NeedsConfirmation {
		t.Fatalf("ben: %+v", v)
	}
}

// HW-12 Changing a rule always asks
func TestHW12_ChangingARuleAlwaysAsks(t *testing.T) {
	f := newFixture(t, map[string]string{"musik.star": logName("x")})
	anna := &tools.TurnState{SessionKey: "s", Channel: config.ChannelVoice, Person: "anna"}
	v := f.g.Check(context.Background(), anna, own(ToolDelete), args(`{"name":"musik"}`))
	if v.Outcome != tools.NeedsConfirmation || v.Reason != "system/self.star" || f.mem.last == nil {
		t.Fatalf("%+v", v)
	}
	// hook_list only reads.
	list := tools.Spec{ToolDef: prompt.ToolDef{Name: ToolList}, Internal: true, ReadOnly: true}
	if v := f.g.Check(context.Background(), voice(), list, args(`{}`)); v.Outcome != tools.Allow {
		t.Fatalf("list: %+v", v)
	}
}

// HW-13 No rule changes after web content
func TestHW13_NoRuleChangesAfterWebContent(t *testing.T) {
	f := newFixture(t, map[string]string{"musik.star": logName("x")})
	anna := &tools.TurnState{SessionKey: "s", Channel: config.ChannelVoice, Person: "anna"}
	f.g.Observe(anna, search)
	if v := f.g.Check(context.Background(), anna, own(ToolSwitch), args(`{"name":"musik","on":false}`)); v.Outcome != tools.Deny || v.Reason != "system/self.star" {
		t.Fatalf("%+v", v)
	}
}

// HW-14 A system hook and critical.star are never written, switched or deleted
func TestHW14_ASystemHookAndCriticalStarAreNeverWrittenSwitchedOrDeleted(t *testing.T) {
	f := newFixture(t, map[string]string{})
	anna := &tools.TurnState{SessionKey: "s", Channel: config.ChannelVoice, Person: "anna"}
	for _, c := range []struct {
		spec tools.Spec
		args string
	}{{own(ToolDelete), `{"name":"web.star"}`}, {own(ToolSwitch), `{"name":"critical","on":false}`}, {own(ToolSave), `{"name":"self.star"}`}} {
		if v := f.g.Check(context.Background(), anna, c.spec, args(c.args)); v.Outcome != tools.Deny {
			t.Errorf("%s %s: %+v", c.spec.Name, c.args, v)
		}
	}
	// And the tools themselves refuse it, should a call get past.
	if _, err := ruleTool(f, ToolDelete).Run(context.Background(), tools.Invocation{Args: args(`{"name":"web.star"}`)}); err == nil {
		t.Error("deleted web.star")
	}
}

// HW-20 hook_list names the rules
func TestHW20_HookListNamesTheRules(t *testing.T) {
	f := newFixture(t, map[string]string{"aus.star": "ACTIVE = False\n" + denyAll})
	if err := f.e.Save(Rule{File: "tuer.star", Code: "DESCRIPTION = \"Tür zu\"\ndef pre_tool(call, verdict):\n    return None\n"},
		"Anna", "Tür nur mit Stimme", time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	out, _ := ruleTool(f, ToolList).Run(context.Background(), tools.Invocation{})
	if !strings.Contains(out, "aus.star (aus): nichts") || !strings.Contains(out, "tuer.star (an): Tür zu") ||
		!strings.Contains(out, `Gewünscht von Anna am 2026-10-28: "Tür nur mit Stimme"`) {
		t.Fatalf("%s", out)
	}
}

// HW-21 hook_switch changes only the line ACTIVE
func TestHW21_HookSwitchChangesOnlyTheLineActive(t *testing.T) {
	src := "# a comment\nDESCRIPTION = \"x\"\n\ndef pre_tool(call, verdict):\n    return None\n"
	f := newFixture(t, map[string]string{"musik.star": src})
	if _, err := ruleTool(f, ToolSwitch).Run(context.Background(), tools.Invocation{Args: args(`{"name":"musik","on":false}`)}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(f.e.dir, "musik.star"))
	if want := strings.Replace(src, "DESCRIPTION = \"x\"\n", "DESCRIPTION = \"x\"\nACTIVE = False\n", 1); string(got) != want {
		t.Fatalf("got\n%s", got)
	}
	ruleTool(f, ToolSwitch).Run(context.Background(), tools.Invocation{Args: args(`{"name":"musik","on":true}`)})
	got, _ = os.ReadFile(filepath.Join(f.e.dir, "musik.star"))
	if !strings.Contains(string(got), "ACTIVE = True") || strings.Count(string(got), "ACTIVE") != 1 {
		t.Fatalf("got\n%s", got)
	}
}

// ruleThreads is the skill "regel" as shipped, with its thread machinery
// around the hooks; the agent's result is handed in by the test.
type ruleThreads struct {
	m      *threads.Manager
	st     *store.Store
	input  map[string]any // what the agent got
	mu     sync.Mutex
	calls  map[string]string // field -> call id
	thread string
}

func newRuleThreads(t *testing.T, f fixture) *ruleThreads {
	t.Helper()
	list, problems := skills.Load([]string{"../../skills"}, map[string]map[string]any{"regel": nil})
	if len(problems) != 0 || len(list) != 1 || list[0].Target == nil {
		t.Fatalf("skill: %v %v", list, problems)
	}
	if _, ok := list[0].Agents["regel.schreiben"]; !ok {
		t.Fatalf("agents: %v", list[0].Agents)
	}
	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	r := &ruleThreads{calls: map[string]string{}, st: st}
	r.m = threads.New(threads.Options{Lang: i18n.DE, Store: st, Targets: list,
		Checks: map[string]threads.Check{"hook.check": func(_ context.Context, v string) (string, error) { return f.e.CheckRule(v) }},
		Finishers: map[string]threads.Finisher{"hook.save": f.e.SaveFinisher(func() *tools.Guard { return f.g },
			func(p string) string { return map[string]string{"anna": "Anna"}[p] }, nil)},
		Dispatch: func(_ context.Context, th store.Thread, into, agent, callID string, input json.RawMessage) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.calls[into], r.thread = callID, th.ID
			_ = json.Unmarshal(input, &r.input)
			return nil
		},
		AgentWait: 10 * time.Millisecond, InlineWait: 10 * time.Millisecond})
	return r
}

func (r *ruleThreads) agentWrites(output string) {
	r.mu.Lock()
	id, call := r.thread, r.calls[RuleField]
	r.mu.Unlock()
	r.m.AgentResult(context.Background(), id, RuleField, store.AgentRun{CallID: call, Agent: "regel.schreiben", Status: "done", Output: output})
}

const goodRule = `{"file": "unknown_voice.star", "summary": "Ohne bekannte Stimme schließe ich nichts auf.",
"code": "DESCRIPTION = \"Ohne bekannte Stimme schließe ich nichts auf.\"\n\ndef pre_tool(call, verdict):\n    if call.who.person:\n        return None\n    for e in call.targets + call.candidates:\n        if e.domain == \"lock\" and call.tool.name == \"HassTurnOff\":\n            return deny(\"Aufschließen nur, wenn ich weiß, wer spricht.\")\n"}`

// HW-30 A rule from a wish
func TestHW30_ARuleFromAWish(t *testing.T) {
	f := newFixture(t, map[string]string{})
	r := newRuleThreads(t, f)
	ctx := context.Background()
	anna := tools.Invocation{SessionKey: "telegram:1", Channel: config.ChannelTelegram, Person: "anna", Place: "person:anna"}
	wish := "Wenn du nicht weißt, wer spricht, schließt du nichts auf."
	if _, err := r.m.Start(ctx, anna, "regel", wish); err != nil {
		t.Fatal(err)
	}
	if _, err := r.m.Fill(ctx, anna, map[string]string{"wunsch": wish}); err != nil {
		t.Fatal(err)
	}
	// An agent without an input schema reads its input from "input".
	if r.input["input"] != wish {
		t.Fatalf("agent input %v", r.input)
	}
	r.agentWrites(goodRule)
	th, _ := r.st.GetThread(ctx, r.thread)
	if th.State != store.ThreadProposed {
		t.Fatalf("state %s, log %v", th.State, th.Log)
	}
	ts := &tools.TurnState{SessionKey: "telegram:1", Channel: config.ChannelTelegram, Person: "anna", UserText: "Ja"}
	out, err := r.m.Confirm(tools.WithTurn(ctx, ts), anna)
	if err != nil || !strings.Contains(out, "aktiv") || !strings.Contains(out, "Getestet habe ich sie nicht") || !strings.Contains(out, "probier") {
		t.Fatalf("confirm: %q %v", out, err)
	}
	got, err := os.ReadFile(filepath.Join(f.e.dir, "unknown_voice.star"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), "# Written by Mira on ") || !strings.Contains(string(got), "for Anna:\n# \""+wish+"\"") ||
		!strings.Contains(string(got), "ACTIVE = True") {
		t.Fatalf("file\n%s", got)
	}
	// It applies at once.
	if v := f.g.Check(ctx, voice(), turnOff, args(`{"name":"Haustür"}`)); v.Outcome != tools.Deny || v.Reason != "household/unknown_voice.star" {
		t.Fatalf("after saving: %+v", v)
	}
}

// HW-31 A rule that does not load is not proposed
func TestHW31_ARuleThatDoesNotLoadIsNotProposed(t *testing.T) {
	f := newFixture(t, map[string]string{})
	r := newRuleThreads(t, f)
	ctx := context.Background()
	anna := tools.Invocation{SessionKey: "telegram:1", Channel: config.ChannelTelegram, Person: "anna", Place: "person:anna"}
	r.m.Start(ctx, anna, "regel", "x")
	r.m.Fill(ctx, anna, map[string]string{"wunsch": "Kaputt"})
	r.agentWrites(`{"file": "kaputt.star", "summary": "x", "code": "def pre_tool(call, verdict)"}`)
	th, _ := r.st.GetThread(ctx, r.thread)
	if th.State == store.ThreadProposed || th.Fields[RuleField] != "" {
		t.Fatalf("state %s, field %q", th.State, th.Fields[RuleField])
	}
	if entries, _ := os.ReadDir(f.e.dir); len(entries) != 0 {
		t.Errorf("files: %v", entries)
	}
	// The agent's own "cannot" is no rule either.
	if _, err := f.e.CheckRule(`{"error": "Eine Regel kann nichts einschalten."}`); err == nil || !strings.Contains(err.Error(), "einschalten") {
		t.Errorf("error: %v", err)
	}
}

// HW-32 The agent never writes a system hook's file or critical.star
func TestHW32_TheAgentNeverWritesASystemHooksFileOrCriticalStar(t *testing.T) {
	f := newFixture(t, map[string]string{"tuer.star": logName("x")})
	code := `DESCRIPTION = \"x\"\ndef pre_tool(call, verdict):\n    return None\n`
	for _, name := range []string{"web.star", "critical", "Böse Datei.star"} {
		if _, err := f.e.CheckRule(`{"file": "` + name + `", "summary": "x", "code": "` + code + `"}`); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	// A name that is taken gets a number.
	out, err := f.e.CheckRule(`{"file": "tuer.star", "summary": "x", "code": "` + code + `"}`)
	if err != nil || !strings.Contains(out, `"file":"tuer_2.star"`) {
		t.Errorf("taken name: %s %v", out, err)
	}
}

// hook_try says OK or the error, for the agent.
func TestHookTry(t *testing.T) {
	f := newFixture(t, map[string]string{})
	try := ruleTool(f, ToolTry)
	ok, _ := try.Run(context.Background(), tools.Invocation{Args: args(`{"code":"DESCRIPTION = \"x\"\ndef pre_tool(call, verdict):\n    return None\n"}`)})
	bad, _ := try.Run(context.Background(), tools.Invocation{Args: args(`{"code":"DESCRIPTION = \"x\"\ndef pre_tool(call, verdict):\n    return call.nope\n"}`)})
	if !strings.HasPrefix(ok, "OK") || !strings.Contains(bad, "nope") {
		t.Errorf("ok %q, bad %q", ok, bad)
	}
	ref, _ := ruleTool(f, ToolReference).Run(context.Background(), tools.Invocation{})
	if !strings.Contains(ref, "mira_confirm") || !strings.HasSuffix(ref, "house") {
		t.Errorf("reference: %.200s", ref)
	}
}
