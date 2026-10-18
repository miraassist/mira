package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/testsupport/fakeha"
	"github.com/miraassist/mira/internal/testsupport/fakellm"
	"github.com/miraassist/mira/internal/testsupport/fakemcp"
)

func freeAddr(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

// seedExposed keeps what the integration would have sent: the entities
// exposed to Assist.
func seedExposed(t *testing.T, dbPath string, ids ...string) {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{Path: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	raw, _ := json.Marshal(ids)
	if err := st.SetKV(context.Background(), kvExposed, string(raw)); err != nil {
		t.Fatal(err)
	}
}

// TestAgentEndToEnd starts the real wiring against fakes and drives one voice
// turn through the HTTP API: satellite → area → tool call → answer.
func TestAgentEndToEnd(t *testing.T) {
	model := fakellm.New(
		fakellm.Step{Content: "."}, fakellm.Step{Content: "."}, fakellm.Step{Content: "."}, // startup warmups
		fakellm.Step{ToolCalls: []llm.ToolCall{fakellm.ToolCall("c1", "ha__HassTurnOn", `{"name":"Küchenlicht"}`)}},
		fakellm.Step{Content: "Das Licht in der Küche ist an."},
	)
	defer model.Close()

	ha := fakeha.New()
	defer ha.Close()
	ha.SetRegistry(
		[]haws.Area{{AreaID: "kueche", Name: "Küche"}},
		[]haws.Device{{ID: "sat-kueche", AreaID: "kueche", Name: "Satellit"}},
		[]haws.Entity{{EntityID: "light.kueche", AreaID: "kueche", Name: "Küchenlicht"}},
	)

	haMCP := fakemcp.HA("Küchenlicht: aus")
	haMCP.Token = ha.Token
	defer haMCP.Close()

	cfg := config.Default()
	cfg.Server = config.Server{Listen: freeAddr(t), Token: "api-token"}
	cfg.LLM.BaseURL = model.BaseURL()
	cfg.Store.Path = filepath.Join(t.TempDir(), "mira.db")
	cfg.HA = config.HA{URL: ha.URL, Token: ha.Token, StructureDebounce: config.Duration(time.Second)}
	cfg.MCPServers = []config.MCPServer{{Name: "ha", Transport: config.TransportHTTP, URL: haMCP.Endpoint(), Token: ha.Token}}
	cfg.Telemetry.LogLevel = "error"
	seedExposed(t, cfg.Store.Path, "light.kueche")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runAgent(ctx, cfg, prepared{}) }()
	defer func() {
		http.DefaultClient.CloseIdleConnections()
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("runAgent: %v", err)
			}
		case <-time.After(15 * time.Second):
			t.Error("agent did not shut down")
		}
	}()

	base := "http://" + cfg.Server.Listen
	deadline := time.Now().Add(10 * time.Second)
	for {
		req, _ := http.NewRequest("GET", base+"/v1/health", nil)
		req.Header.Set("Authorization", "Bearer api-token")
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("agent did not come up")
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Let the startup warmups pass before the turn so the script lines up.
	for i := 0; len(model.Requests()) < 3; i++ {
		if i > 200 {
			t.Fatalf("warmups = %d", len(model.Requests()))
		}
		time.Sleep(20 * time.Millisecond)
	}

	body := `{"text":"Mach das Licht an","device_id":"sat-kueche","language":"de"}`
	req, _ := http.NewRequest("POST", base+"/v1/conversation", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer api-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var out map[string]any
	json.Unmarshal(raw, &out)
	if resp.StatusCode != 200 || out["text"] != "Das Licht in der Küche ist an." || out["conversation_id"] == "" {
		t.Fatalf("conversation = %d %s", resp.StatusCode, raw)
	}

	calls := haMCP.Calls()
	if len(calls) != 1 || calls[0].Tool != "HassTurnOn" {
		t.Fatalf("mcp calls = %+v", calls)
	}
	reqs := model.Requests()
	turn := reqs[3].Request
	if !strings.Contains(turn.Messages[0].Content, "light.kueche — Küchenlicht") {
		t.Errorf("structure block missing from the system prompt:\n%s", turn.Messages[0].Content)
	}
	if !strings.Contains(turn.Messages[len(turn.Messages)-1].Content, "Satellit im Bereich Küche") {
		t.Error("satellite area not resolved")
	}
	if reqs[3].CachedTokens != reqs[0].PromptTokens {
		t.Errorf("voice turn reused %d tokens, the voice warmup sent %d", reqs[3].CachedTokens, reqs[0].PromptTokens)
	}
}
