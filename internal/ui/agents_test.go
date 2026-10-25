package ui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/agent"
	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/store"
)

// storeRun writes a run as the runtime does: its record, its steps, its end.
func storeRun(t *testing.T, st *store.Store, id, agentName string, started time.Time, done bool, steps ...any) {
	t.Helper()
	ctx := context.Background()
	call, _ := json.Marshal(agent.CallContext{CallID: id, Origin: agent.FromAutomation, Rights: agent.ReadHouse | agent.Web,
		Mode: agent.Mode{Async: true, Delivery: agent.Delivery{Route: agent.ToEvent}}})
	if err := st.PutAgentRun(ctx, store.AgentRun{CallID: id, Agent: agentName, Status: "running", Call: call,
		Input: "Wie warm ist es?", StartedAt: started, Deadline: started.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(steps); i += 2 {
		body, _ := json.Marshal(steps[i+1])
		if err := st.AddAgentStep(ctx, id, steps[i].(string), body); err != nil {
			t.Fatal(err)
		}
	}
	if done {
		if err := st.EndAgentRun(ctx, id, "done", `{"satz":"25 Grad"}`, "", true,
			store.AgentUsage{Model: "smart", ModelCalls: 2, PromptTokens: 800, CompletionTokens: 40}); err != nil {
			t.Fatal(err)
		}
	}
}

// AG-C1 The stored runs are listed
func TestAGC1_TheStoredRunsAreListed(t *testing.T) {
	srv, _, st := setup(t)
	// Given two asynchronous runs, one done and one running
	storeRun(t, st, "r-done", "wetterlage", time.Now().Add(-2*time.Minute), true)
	storeRun(t, st, "r-running", "rezept", time.Now().Add(-5*time.Minute), false)
	// When an admin opens the tab Agenten on Mira's page
	code, body := asHA(t, srv, "GET", "/v1/ui/agents", "u-admin", true, "")
	// Then both are listed, the running one first, with agent, status, start, duration, model, model calls and tokens
	var out struct{ Runs []Run }
	json.Unmarshal([]byte(body), &out)
	if code != 200 || len(out.Runs) != 2 || out.Runs[0].CallID != "r-running" || out.Runs[0].Status != "running" {
		t.Fatalf("%d %s", code, body)
	}
	done := out.Runs[1]
	if done.Agent != "wetterlage" || done.Model != "smart" || done.ModelCalls != 2 || done.PromptTokens != 800 ||
		done.Started == "" || done.Ended == "" || done.DurationMS <= 0 || done.Route != "event" {
		t.Errorf("done run %+v", done)
	}
}

// AG-C2 A run shows its rounds
func TestAGC2_ARunShowsItsRounds(t *testing.T) {
	srv, _, st := setup(t)
	// Given a done run that called a tool and read a web page
	answer := llm.Message{Role: llm.RoleAssistant, Content: "Ich schaue nach.", ToolCalls: []llm.ToolCall{
		{ID: "c1", Type: "function", Function: llm.FunctionCall{Name: "web_page", Arguments: `{"url":"https://wetter"}`}}}}
	storeRun(t, st, "r1", "wetterlage", time.Now().Add(-time.Minute), true,
		"answer", answer, "started", llm.Message{ToolCallID: "c1", Name: "web_page"},
		"result", llm.Message{Role: llm.RoleTool, ToolCallID: "c1", Name: "web_page", Content: "25 Grad"},
		"state", map[string]bool{"web": true})
	// When the admin opens it
	code, body := asHA(t, srv, "GET", "/v1/ui/agents/r1", "u-admin", true, "")
	// Then its input, call context, the model's answers with the tool calls, the results, the change of what its guard
	// knows and its output are shown in order
	var d RunDetail
	json.Unmarshal([]byte(body), &d)
	if code != 200 || d.Input != "Wie warm ist es?" || d.Call.Origin != "automation" || d.Call.Rights != "read_house,web" ||
		d.Call.Delivery != "event" || d.Output != `{"satz":"25 Grad"}` || !d.Foreign {
		t.Fatalf("%d %+v", code, d)
	}
	var kinds []string
	for _, s := range d.Steps {
		kinds = append(kinds, s.Kind)
	}
	if strings.Join(kinds, ",") != "answer,started,result,state" || !strings.Contains(string(d.Steps[0].Body), "web_page") {
		t.Errorf("steps %s", body)
	}
	if code, _ := asHA(t, srv, "GET", "/v1/ui/agents/gibtsnicht", "u-admin", true, ""); code != 404 {
		t.Errorf("unknown run: %d", code)
	}
}

// AG-C3 Only admins see the tab
func TestAGC3_OnlyAdminsSeeTheTab(t *testing.T) {
	srv, _, st := setup(t)
	storeRun(t, st, "r1", "wetterlage", time.Now(), true)
	// Given a user of Home Assistant who is no admin
	// When they open Mira's page
	// Then there is no tab Agenten, and the runs cannot be read
	for _, path := range []string{"/v1/ui/agents", "/v1/ui/agents/r1"} {
		if code, _ := asHA(t, srv, "GET", path, "u-anna", false, ""); code != 403 {
			t.Errorf("%s for a user who is no admin: %d", path, code)
		}
	}
	_, page := call(t, srv, "GET", "/ui", "", "")
	if !strings.Contains(page, `if (!isAdmin) TABS = ["home", "me", "messages", "plans", "memory", "guide"];`) || !strings.Contains(page, `"agents"`) {
		t.Error("the page does not keep the tab Agenten for admins")
	}
}
