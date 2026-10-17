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

// The first pairing needs no code, the next ones do: the right code hands
// out the token once; a wrong one counts, five spend the code, and it
// expires after 15 minutes. Unpairing makes a new token and opens again.
func TestPairing(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	s, err := Load(ctx, st)
	if err != nil || len(s.Token) != 64 || !s.Open {
		t.Fatalf("token made at the first start, open: %+v %v", s, err)
	}
	now := time.Now()
	if token, err := Pair(ctx, st, "", now); err != nil || token != s.Token {
		t.Fatalf("first pairing without a code: %q %v", token, err)
	}
	if _, err := Pair(ctx, st, "", now); err != ErrBadCode {
		t.Fatal("a second pairing without a code worked")
	}
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

	Save(ctx, st, "http://ha", "ha-token", &LLM{Kind: KindCloud, BaseURL: "u", Model: "m"})
	if err := Unpair(ctx, st); err != nil {
		t.Fatal(err)
	}
	after, _ := Load(ctx, st)
	if !after.Open || after.Token == token || after.HAToken != "" || after.LLM == nil {
		t.Errorf("after unpairing: open, new token, no HA, model kept: %+v", after)
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
	s.LLM.Kind = KindCustom
	if c := Apply(config.Default(), s); c.LLM.SlotPinning || !c.LLM.CachePrompt || !c.LLM.WarmupOnStart {
		t.Error("one's own server: warmup and cache_prompt, no slots")
	}
}

// /v1/setup tries the model with a real request before it keeps anything:
// a wrong key, a model without tools and an unknown model are refused with
// their reason; then the agent starts anew.
func TestSetupEndpoint(t *testing.T) {
	models := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" && r.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(401)
			return
		}
		var req struct {
			Model string `json:"model"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		switch req.Model {
		case "qwen3":
			w.Write([]byte(`{"choices":[{"message":{"tool_calls":[{"id":"1","type":"function","function":{"name":"turn_on","arguments":"{}"}}]}}]}`))
		case "chatty":
			w.Write([]byte(`{"choices":[{"message":{"content":"I turned it on."}}]}`))
		default:
			w.WriteHeader(404)
			w.Write([]byte(`{"error":"model not found"}`))
		}
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
	var reason string
	post := func(path, token string, body any) int {
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", srv.URL+path, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out struct {
			Reason string `json:"reason"`
		}
		json.NewDecoder(resp.Body).Decode(&out)
		reason = out.Reason
		return resp.StatusCode
	}
	if code := post("/v1/setup", "wrong", map[string]any{}); code != 401 {
		t.Errorf("without the token: %d", code)
	}
	for model, want := range map[string]string{"nope": "no_model", "chatty": "no_tools"} {
		if code := post("/v1/setup", "tok", map[string]any{"llm": LLM{Kind: KindCustom, BaseURL: models.URL, Model: model}}); code != 422 || reason != want {
			t.Errorf("%s: %d %q, want %s", model, code, reason, want)
		}
	}
	if code := post("/v1/setup", "tok", map[string]any{"llm": LLM{Kind: KindCloud, BaseURL: models.URL, Model: "qwen3", APIKey: "bad"}}); code != 422 || reason != "bad_key" {
		t.Errorf("wrong key: %d %q", code, reason)
	}
	if code := post("/v1/setup", "tok", map[string]any{"llm": LLM{Kind: KindCloud, BaseURL: "http://127.0.0.1:1", Model: "qwen3"}}); code != 422 || reason != "unreachable" {
		t.Errorf("no server: %d %q", code, reason)
	}
	if restarted.Load() {
		t.Fatal("restarted although nothing was kept")
	}
	if code := post("/v1/setup", "tok", map[string]any{"ha_url": "http://ha:8123", "ha_token": "x",
		"llm": LLM{Kind: KindCloud, BaseURL: models.URL, Model: "qwen3", APIKey: "good"}}); code != 200 {
		t.Fatalf("setup: %d %q", code, reason)
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
