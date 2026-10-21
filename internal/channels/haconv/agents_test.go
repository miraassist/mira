package haconv

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/agent"
	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/store"
)

// answerModel answers with its text, or waits until the call ends when it
// hangs.
type answerModel struct {
	mu    sync.Mutex
	text  string
	hang  bool
	calls int
}

func (m *answerModel) Chat(ctx context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	m.mu.Lock()
	m.calls++
	text, hang := m.text, m.hang
	m.mu.Unlock()
	if hang {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return &llm.ChatResponse{Choices: []llm.Choice{{Message: llm.Message{Role: llm.RoleAssistant, Content: text}}}}, nil
}

// agentServer is the API with a runtime that knows the agent "zusammenfassen"
// (text in, JSON out).
func agentServer(t *testing.T, m *answerModel) (*httptest.Server, chan store.AgentRun) {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	delivered := make(chan store.AgentRun, 4)
	rt := agent.New(agent.Options{
		Config: config.Default,
		Models: func(string) (agent.Chatter, config.LLM, bool) { return m, config.LLM{}, true },
		Runs:   st,
		Deliver: func(_ context.Context, rec store.AgentRun, cc agent.CallContext) {
			if cc.Mode.Delivery.Route == agent.ToEvent {
				delivered <- rec
			}
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		rt.Close(ctx)
	})
	rt.Register(agent.Spec{Name: "zusammenfassen", Profile: agent.Isolated, Instructions: "Fasse zusammen.",
		Output: []byte(`{"type":"object","properties":{"kurz":{"type":"string"}},"required":["kurz"]}`)})
	srv := httptest.NewServer(New(Options{Token: "secret", Store: st, Version: "test", Agents: rt}))
	t.Cleanup(srv.Close)
	return srv, delivered
}

func waitRun(t *testing.T, delivered chan store.AgentRun) store.AgentRun {
	t.Helper()
	select {
	case rec := <-delivered:
		return rec
	case <-time.After(5 * time.Second):
		t.Fatal("no run delivered")
	}
	return store.AgentRun{}
}

// AG-62 From Home Assistant synchronously with an answer
func TestAG62_FromHomeAssistantSynchronouslyWithAnAnswer(t *testing.T) {
	srv, _ := agentServer(t, &answerModel{text: `{"kurz":"Alles ruhig."}`})
	// When an automation calls the action mira.agent synchronously with an agent and its input
	code, out := do(t, srv, "POST", "/v1/agent", "secret", `{"agent":"zusammenfassen","input":{"input":"Ein langer Text."}}`)
	// Then the automation gets the agent's answer in its response_variable
	got, _ := out["output"].(map[string]any)
	if code != http.StatusOK || out["status"] != "done" || got["kurz"] != "Alles ruhig." {
		t.Errorf("%d %v", code, out)
	}
}

// AG-63 From Home Assistant asynchronously
func TestAG63_FromHomeAssistantAsynchronously(t *testing.T) {
	srv, delivered := agentServer(t, &answerModel{text: `{"kurz":"Fertig."}`})
	// When an automation calls the action mira.agent asynchronously
	code, out := do(t, srv, "POST", "/v1/agent", "secret", `{"agent":"zusammenfassen","input":{"input":"x"},"mode":"async"}`)
	// Then it gets the call ID
	if code != http.StatusOK || out["call_id"] == "" || out["status"] != "running" {
		t.Fatalf("%d %v", code, out)
	}
	// And the result comes as the event mira_agent_done
	if rec := waitRun(t, delivered); rec.CallID != out["call_id"] || rec.Status != "done" {
		t.Errorf("delivered %+v", rec)
	}
}

// AG-64 Query a run from Home Assistant
func TestAG64_QueryARunFromHomeAssistant(t *testing.T) {
	srv, delivered := agentServer(t, &answerModel{text: `{"kurz":"Fertig."}`})
	// Given a call ID of a run
	_, started := do(t, srv, "POST", "/v1/agent", "secret", `{"agent":"zusammenfassen","input":{"input":"x"},"mode":"async"}`)
	waitRun(t, delivered)
	// When an automation calls the action mira.agent_result with this call ID
	code, out := do(t, srv, "POST", "/v1/agent/result", "secret", `{"call_id":"`+started["call_id"].(string)+`"}`)
	// Then it gets the run record
	got, _ := out["output"].(map[string]any)
	if code != http.StatusOK || out["status"] != "done" || got["kurz"] != "Fertig." || out["ended_at"] == "" {
		t.Errorf("%d %v", code, out)
	}
}

// AG-66 An unknown agent
func TestAG66_AnUnknownAgent(t *testing.T) {
	srv, _ := agentServer(t, &answerModel{})
	// When an automation calls mira.agent with a name that does not exist
	code, out := do(t, srv, "POST", "/v1/agent", "secret", `{"agent":"gibtsnicht","input":{"input":"x"}}`)
	// Then the call fails with that reason
	if code != http.StatusNotFound || out["error"] == nil {
		t.Errorf("%d %v", code, out)
	}
}

// AG-68 Cancelling a run from Home Assistant
func TestAG68_CancellingARunFromHomeAssistant(t *testing.T) {
	m := &answerModel{hang: true}
	srv, delivered := agentServer(t, m)
	// Given an asynchronous run with its call ID
	_, started := do(t, srv, "POST", "/v1/agent", "secret", `{"agent":"zusammenfassen","input":{"input":"x"},"mode":"async"}`)
	time.Sleep(50 * time.Millisecond)
	// When an automation calls mira.agent_cancel with this call ID
	code, out := do(t, srv, "POST", "/v1/agent/cancel", "secret", `{"call_id":"`+started["call_id"].(string)+`"}`)
	// Then the run is cancelled
	// And no further round starts
	rec := waitRun(t, delivered)
	m.mu.Lock()
	calls := m.calls
	m.mu.Unlock()
	if code != http.StatusOK || out["cancelled"] != true || rec.Status != "cancelled" || calls != 1 {
		t.Errorf("%d %v, record %+v, %d model calls", code, out, rec, calls)
	}
}

// AG-98 The defaults of mira.agent
func TestAG98_TheDefaultsOfMiraAgent(t *testing.T) {
	// When an automation calls mira.agent with only agent and input
	cc, err := agentRequest{Agent: "zusammenfassen", Input: []byte(`{"input":"x"}`)}.callContext()
	// Then the call is synchronous with a timeout of 60 seconds
	// And at the timeout it is cancelled
	if err != nil || cc.Mode.Async || cc.Mode.Timeout != 60*time.Second || cc.Mode.OnTimeout != agent.Cancel ||
		cc.Origin != agent.FromAutomation {
		t.Errorf("%+v, %v", cc, err)
	}
}
