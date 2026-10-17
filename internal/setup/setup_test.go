package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// The right code hands out the token once; a wrong one counts, five spend
// the code, and it expires after 15 minutes.
func TestPairing(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	s, err := Load(ctx, st)
	if err != nil || len(s.Token) != 64 {
		t.Fatalf("token made at the first start: %q %v", s.Token, err)
	}
	now := time.Now()
	code, _ := NewCode(ctx, st, now)
	if _, err := Pair(ctx, st, "000000x", now); err != ErrBadCode {
		t.Errorf("wrong code: %v", err)
	}
	token, err := Pair(ctx, st, code[:3]+" "+code[3:], now)
	if err != nil || token != s.Token {
		t.Fatalf("right code: %q %v", token, err)
	}
	if _, err := Pair(ctx, st, code, now); err != ErrBadCode {
		t.Error("a code works only once")
	}
	if s, _ := Load(ctx, st); !s.Paired || s.Token != token {
		t.Error("not paired, or the token changed")
	}

	code, _ = NewCode(ctx, st, now)
	for i := 0; i < 5; i++ {
		Pair(ctx, st, "bad", now)
	}
	if _, err := Pair(ctx, st, code, now); err != ErrBadCode {
		t.Error("five wrong tries must spend the code")
	}
	code, _ = NewCode(ctx, st, now)
	if _, err := Pair(ctx, st, code, now.Add(16*time.Minute)); err != ErrBadCode {
		t.Error("an expired code worked")
	}
}

// What setup sent lands in the config: the token when the file has none,
// Home Assistant with its MCP server, the model with what suits its kind.
func TestApply(t *testing.T) {
	s := Settings{Token: "agent-token", HAURL: "http://ha:8123/", HAToken: "ha-token",
		LLM: &LLM{Kind: KindCloud, BaseURL: "https://cloud/v1/", Model: "m", APIKey: "k"}}
	cfg := Apply(config.Default(), s)
	if cfg.Server.Token != "agent-token" || cfg.HA.URL != "http://ha:8123" || cfg.HA.Token != "ha-token" {
		t.Errorf("server/ha: %+v %+v", cfg.Server, cfg.HA)
	}
	if len(cfg.MCPServers) != 1 || cfg.MCPServers[0].URL != "http://ha:8123/api/mcp/assist" || cfg.MCPServers[0].Token != "ha-token" {
		t.Errorf("mcp: %+v", cfg.MCPServers)
	}
	if cfg.LLM.BaseURL != "https://cloud/v1" || cfg.LLM.SlotPinning || cfg.LLM.CachePrompt || cfg.LLM.WarmupOnStart {
		t.Errorf("cloud model: %+v", cfg.LLM)
	}
	if !Ready(cfg, false, s) || Ready(config.Default(), false, Settings{}) {
		t.Error("ready")
	}
	file := config.Default()
	file.Server.Token = "from-file"
	if Apply(file, s).Server.Token != "from-file" {
		t.Error("a token in mira.yaml must win")
	}
	s.LLM.Kind = KindLlamaCpp
	if c := Apply(config.Default(), s); !c.LLM.SlotPinning || !c.LLM.WarmupOnStart {
		t.Error("llama.cpp keeps slots and warmup")
	}
}

// /v1/setup checks the model with its server before it keeps anything,
// then the agent starts anew.
func TestSetupEndpoint(t *testing.T) {
	models := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":"qwen3"},{"id":"gemma"}]}`))
	}))
	t.Cleanup(models.Close)
	st := newStore(t)
	var restarted atomic.Bool
	h := &Handler{Store: st, Token: "tok", ID: "id1", Version: "t", Pairing: true, SetupMode: true,
		Restart: func() { restarted.Store(true) }, Log: slog.Default()}
	mux := http.NewServeMux()
	h.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	post := func(path, token string, body any) int {
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", srv.URL+path, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := post("/v1/setup", "wrong", map[string]any{}); code != 401 {
		t.Errorf("without the token: %d", code)
	}
	if code := post("/v1/setup", "tok", map[string]any{"llm": LLM{Kind: KindLMStudio, BaseURL: models.URL, Model: "nope"}}); code != 422 {
		t.Errorf("unknown model: %d", code)
	}
	if code := post("/v1/setup", "tok", map[string]any{"ha_url": "http://ha:8123", "ha_token": "x",
		"llm": LLM{Kind: KindLMStudio, BaseURL: models.URL, Model: "qwen3"}}); code != 200 {
		t.Fatalf("setup: %d", code)
	}
	time.Sleep(400 * time.Millisecond)
	if !restarted.Load() {
		t.Error("no restart after setup")
	}
	s, _ := Load(context.Background(), st)
	if s.HAURL != "http://ha:8123" || s.LLM == nil || s.LLM.Model != "qwen3" {
		t.Errorf("kept: %+v", s)
	}
	resp, _ := http.Get(srv.URL + "/")
	if resp.StatusCode != 200 {
		t.Errorf("setup page: %d", resp.StatusCode)
	}
}
