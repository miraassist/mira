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

	"github.com/miraassist/mira/internal/clock"
	"github.com/miraassist/mira/internal/config"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/testsupport/fakeha"
	"github.com/miraassist/mira/internal/testsupport/fakellm"
	"github.com/miraassist/mira/internal/testsupport/fakemcp"
	"github.com/miraassist/mira/internal/testsupport/logcap"
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
	st, err := store.Open(context.Background(), store.Options{Path: dbPath, Clock: clock.Real().Now})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	raw, _ := json.Marshal(ids)
	if err := st.SetKV(context.Background(), kvExposed, string(raw)); err != nil {
		t.Fatal(err)
	}
}

// startAgent runs the service with cfg until the test ends and returns
// once Mira is up, with her log (at debug) for the test to await.
func startAgent(t *testing.T, cfg config.Config) *logcap.Lines {
	t.Helper()
	lines := &logcap.Lines{}
	cfg.Telemetry.LogLevel = "debug"
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runAgent(ctx, cfg, prepared{logTo: lines}) }()
	t.Cleanup(func() {
		http.DefaultClient.CloseIdleConnections()
		cancel()
		if err := <-done; err != nil {
			t.Errorf("runAgent: %v", err)
		}
	})
	select {
	case <-lines.Await(`msg="mira is up"`):
	case err := <-done:
		t.Fatalf("runAgent ended: %v", err)
	}
	return lines
}

// TestAgentEndToEnd starts the real wiring against fakes and drives one voice
// turn through the HTTP API: satellite → area → tool call → answer.
func TestAgentEndToEnd(t *testing.T) {
	warmed := []chan struct{}{make(chan struct{}), make(chan struct{}), make(chan struct{})}
	model := fakellm.New(
		fakellm.Step{Content: ".", Taken: warmed[0]}, fakellm.Step{Content: ".", Taken: warmed[1]},
		fakellm.Step{Content: ".", Taken: warmed[2]}, // startup warmups
		fakellm.Step{ToolCalls: []llm.ToolCall{fakellm.ToolCall("c1", "ha__HassTurnOn", `{"name":"Küchenlicht"}`)}},
		fakellm.Step{Content: "Das Licht in der Küche ist an."},
	)
	t.Cleanup(model.Close)

	ha := fakeha.New()
	t.Cleanup(ha.Close)
	ha.SetRegistry(
		[]haws.Area{{AreaID: "kueche", Name: "Küche"}},
		[]haws.Device{{ID: "sat-kueche", AreaID: "kueche", Name: "Satellit"}},
		[]haws.Entity{{EntityID: "light.kueche", AreaID: "kueche", Name: "Küchenlicht"}},
	)

	haMCP := fakemcp.HA("Küchenlicht: aus")
	haMCP.Token = ha.Token
	t.Cleanup(haMCP.Close)

	cfg := config.Default()
	cfg.Server = config.Server{Listen: freeAddr(t), Token: "api-token"}
	// main as a local llama.cpp server: a slot per channel, the prompt
	// cache, warmups.
	setMain(&cfg, func(m *config.LLM) {
		m.BaseURL, m.CachePrompt, m.WarmupOnStart, m.WarmupOnPrefixChange = model.BaseURL(), true, true, true
		m.PinSlots = map[config.Channel]int{config.ChannelVoice: 0, config.ChannelTelegram: 1, config.ChannelBackground: 2}
	})
	cfg.Store.Path = filepath.Join(t.TempDir(), "mira.db")
	cfg.HA = config.HA{URL: ha.URL, Token: ha.Token, StructureDebounce: config.Duration(time.Second)}
	cfg.MCPServers = []config.MCPServer{{Name: "ha", Transport: config.TransportHTTP, URL: haMCP.Endpoint(), Token: ha.Token}}
	cfg.Telemetry.LogLevel = "error"
	seedExposed(t, cfg.Store.Path, "light.kueche")

	startAgent(t, cfg)
	base := "http://" + cfg.Server.Listen
	// Let the startup warmups pass before the turn so the script lines up.
	for _, w := range warmed {
		<-w
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
	if !strings.Contains(turn.Messages[0].Content, "Static Context: an overview") {
		t.Errorf("Home Assistant's prompt missing from the system prompt:\n%s", turn.Messages[0].Content)
	}
	if !strings.Contains(turn.Messages[len(turn.Messages)-1].Content, "Satellit im Bereich Küche") {
		t.Error("satellite area not resolved")
	}
	if reqs[3].CachedTokens != reqs[0].PromptTokens {
		t.Errorf("voice turn reused %d tokens, the voice warmup sent %d", reqs[3].CachedTokens, reqs[0].PromptTokens)
	}
}

// An enabled skill that cannot start (a required setting is missing) must not
// keep the agent from starting: it is left out and /v1/status says why, in
// words the user can act on.
func TestBrokenSkillDoesNotStopTheAgent(t *testing.T) {
	model := fakellm.New(fakellm.Step{Content: "."}, fakellm.Step{Content: "."}, fakellm.Step{Content: "."})
	t.Cleanup(model.Close)
	ha := fakeha.New()
	t.Cleanup(ha.Close)
	haMCP := fakemcp.HA("")
	haMCP.Token = ha.Token
	t.Cleanup(haMCP.Close)

	cfg := config.Default()
	cfg.Server = config.Server{Listen: freeAddr(t), Token: "api-token"}
	// main as a local llama.cpp server: a slot per channel, the prompt
	// cache, warmups.
	setMain(&cfg, func(m *config.LLM) {
		m.BaseURL, m.CachePrompt, m.WarmupOnStart, m.WarmupOnPrefixChange = model.BaseURL(), true, true, true
		m.PinSlots = map[config.Channel]int{config.ChannelVoice: 0, config.ChannelTelegram: 1, config.ChannelBackground: 2}
	})
	cfg.Store.Path = filepath.Join(t.TempDir(), "mira.db")
	cfg.HA = config.HA{URL: ha.URL, Token: ha.Token, StructureDebounce: config.Duration(time.Second)}
	cfg.MCPServers = []config.MCPServer{{Name: "ha", Transport: config.TransportHTTP, URL: haMCP.Endpoint(), Token: ha.Token}}
	cfg.Telemetry.LogLevel = "error"
	cfg.Skills.Dirs = []string{"../../internal/skills/testdata/skills"}
	cfg.Skills.Enabled = map[string]map[string]any{"abfall": {}, "gibtsnicht": nil}

	startAgent(t, cfg)
	var status struct {
		Problems []struct{ Source, Name, Text string } `json:"problems"`
	}
	req, _ := http.NewRequest("GET", "http://"+cfg.Server.Listen+"/v1/status", nil)
	req.Header.Set("Authorization", "Bearer api-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || json.Unmarshal(raw, &status) != nil {
		t.Fatalf("status: %d %s", resp.StatusCode, raw)
	}
	if len(status.Problems) != 2 {
		t.Fatalf("problems = %+v", status.Problems)
	}
	got := map[string]string{}
	for _, p := range status.Problems {
		if p.Source != "skill" {
			t.Errorf("source = %q", p.Source)
		}
		got[p.Name] = p.Text
	}
	if !strings.Contains(got["abfall"], "„calendar“ fehlt in der mira.yaml") || !strings.Contains(got["gibtsnicht"], "Es gibt keinen Ordner") {
		t.Errorf("texts = %v", got)
	}
}
