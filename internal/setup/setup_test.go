package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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

// Pairing sets up in the same step, or does nothing. The first pairing
// needs no code, the next ones do: the right code works once; a wrong one
// counts, five spend the code, and it expires after 15 minutes. Unpairing
// makes a new token and opens again.
func TestPairing(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	s, err := Load(ctx, st)
	if err != nil || len(s.Token) != 64 || !s.Open {
		t.Fatalf("token made at the first start, open: %+v %v", s, err)
	}
	now := time.Now()
	llm := &LLM{Kind: KindCloud, BaseURL: "u", Model: "m"}
	pair := func(code string, at time.Time) (string, error) {
		return Pair(ctx, st, code, at, "http://ha", "ha-token", llm)
	}
	if _, err := Pair(ctx, st, "", now, "", "", llm); err == nil {
		t.Fatal("paired without Home Assistant's access")
	}
	if s, _ := Load(ctx, st); !s.Open || s.HAURL != "" {
		t.Fatal("a refused pairing left something behind")
	}
	token, err := pair("", now)
	if err != nil || token != s.Token {
		t.Fatalf("first pairing without a code: %q %v", token, err)
	}
	if s, _ := Load(ctx, st); s.Open || !s.Paired || s.HAToken != "ha-token" || s.LLM == nil {
		t.Fatalf("paired and set up at once: %+v", s)
	}
	if _, err := pair("", now); err != ErrBadCode {
		t.Fatal("a second pairing without a code worked")
	}
	code, _ := NewCode(ctx, st, now)
	if _, err := pair("000000x", now); err != ErrBadCode {
		t.Errorf("wrong code: %v", err)
	}
	if got, err := pair(code[:3]+" "+code[3:], now); err != nil || got != token {
		t.Fatalf("right code: %q %v", got, err)
	}
	if _, err := pair(code, now); err != ErrBadCode {
		t.Error("a code works only once")
	}
	code, _ = NewCode(ctx, st, now)
	for i := 0; i < 5; i++ {
		pair("bad", now)
	}
	if _, err := pair(code, now); err != ErrBadCode {
		t.Error("five wrong tries must spend the code")
	}
	code, _ = NewCode(ctx, st, now)
	if _, err := pair(code, now.Add(16*time.Minute)); err != ErrBadCode {
		t.Error("an expired code worked")
	}

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
	if m := cfg.Main(); m.BaseURL != "https://cloud/v1" || len(m.PinSlots) != 0 || m.CachePrompt || m.WarmupOnStart {
		t.Errorf("cloud model: %+v", m)
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
	if m := Apply(config.Default(), s).Main(); len(m.PinSlots) != 0 || !m.CachePrompt || !m.WarmupOnStart {
		t.Error("one's own server: warmup and cache_prompt, no slots")
	}
}

// MO-40 The integration's model becomes main
func TestMO40_TheIntegrationsModelBecomesMain(t *testing.T) {
	// Given the integration set a model from a provider
	file := config.Default()
	file.Models["smart"] = config.LLM{BaseURL: "https://other", Model: "x", Privacy: config.PrivacyCloud}
	s := Settings{LLM: &LLM{Kind: KindCloud, BaseURL: "https://provider/v1", Model: "flash", APIKey: "k"}}
	// When Mira starts
	cfg := Apply(file, s)
	// Then main is that model, with privacy cloud, over what mira.yaml says for main
	if m := cfg.Main(); m.BaseURL != "https://provider/v1" || m.Model != "flash" || m.APIKey != "k" ||
		m.Privacy != config.PrivacyCloud || m.Parallel != 4 {
		t.Errorf("main %+v", m)
	}
	if _, ok := cfg.Models["smart"]; !ok || file.Main().Model == "flash" {
		t.Error("the file's other models must stay, and the file's config must not change")
	}
}

// MO-41 An own server from the integration is local
func TestMO41_AnOwnServerFromTheIntegrationIsLocal(t *testing.T) {
	// Given the integration set a model on an own server
	s := Settings{LLM: &LLM{Kind: KindCustom, BaseURL: "http://lmstudio:1234/v1", Model: "qwen"}}
	// When Mira starts
	// Then main has privacy local
	if m := Apply(config.Default(), s).Main(); m.Privacy != config.PrivacyLocal || m.Parallel != 1 {
		t.Errorf("main %+v", m)
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

// Before pairing the form may list and try models without a token; the
// pairing at the end sets everything up at once and restarts. After it,
// those routes want the token.
func TestPairEndpoint(t *testing.T) {
	models := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Write([]byte(`{"data":[{"id":"qwen3"}]}`))
			return
		}
		w.Write([]byte(`{"choices":[{"message":{"tool_calls":[{"id":"1"}]}}]}`))
	}))
	t.Cleanup(models.Close)
	ctx := context.Background()
	st := newStore(t)
	s, _ := Load(ctx, st)
	var restarted atomic.Bool
	h := &Handler{Store: st, Token: s.Token, ID: "id1", Version: "t", Pairing: true, SetupMode: true,
		Restart: func() { restarted.Store(true) }, Log: slog.Default()}
	mux := http.NewServeMux()
	h.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	post := func(path string, body any) (int, map[string]any) {
		raw, _ := json.Marshal(body)
		resp, err := http.Post(srv.URL+path, "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	llm := LLM{Kind: KindCustom, BaseURL: models.URL, Model: "qwen3"}
	if resp, err := http.Get(srv.URL + "/v1/setup/providers"); err != nil || resp.StatusCode != 200 {
		t.Errorf("providers before pairing: %v %v", resp, err)
	} else {
		var out struct {
			Providers []struct{ Key, Name, BaseURL string }
		}
		json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if len(out.Providers) == 0 || out.Providers[0].Key != "openai" {
			t.Errorf("providers: %+v", out)
		}
	}
	if code, _ := post("/v1/setup/models", map[string]any{"base_url": models.URL}); code != 200 {
		t.Errorf("models before pairing: %d", code)
	}
	if code, _ := post("/v1/setup/probe", map[string]any{"llm": llm}); code != 200 {
		t.Errorf("probe before pairing: %d", code)
	}
	if code, _ := post("/v1/pair", map[string]any{"code": ""}); code != 422 {
		t.Errorf("pairing without Home Assistant: %d", code)
	}
	if code, _ := post("/v1/pair", map[string]any{"ha_url": "http://ha", "ha_token": "x"}); code != 422 {
		t.Errorf("pairing without any model: %d", code)
	}
	code, out := post("/v1/pair", map[string]any{"ha_url": "http://ha", "ha_token": "x", "llm": llm})
	if code != 200 || out["token"] != s.Token {
		t.Fatalf("pairing: %d %v", code, out)
	}
	time.Sleep(400 * time.Millisecond)
	if !restarted.Load() {
		t.Error("no restart after pairing")
	}
	if code, _ := post("/v1/setup/models", map[string]any{"base_url": models.URL}); code != 401 {
		t.Errorf("models after pairing without the token: %d", code)
	}
}

// Before the first pairing the model routes are open, but they must not read
// other servers' answers out to the caller, follow a redirect, take a
// non-web address, or be called without limit.
func TestOpenRoutesAreHarmless(t *testing.T) {
	var inner atomic.Int32
	secret := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inner.Add(1)
		w.Write([]byte("internal-secret-value"))
	}))
	t.Cleanup(secret.Close)
	srvModels := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/leak/models":
			w.WriteHeader(500)
			w.Write([]byte("<html>internal-secret-value</html>"))
		case "/err/models":
			w.WriteHeader(404)
			w.Write([]byte(`{"error":{"message":"model not loaded"}}`))
		case "/redirect/models":
			http.Redirect(w, r, secret.URL, http.StatusFound)
		}
	}))
	t.Cleanup(srvModels.Close)

	st := newStore(t)
	h := &Handler{Store: st, Token: "tok", ID: "id", Version: "t", Pairing: true, SetupMode: true, Log: slog.Default()}
	mux := http.NewServeMux()
	h.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	models := func(base string) (int, string) {
		raw, _ := json.Marshal(map[string]string{"base_url": base})
		resp, err := http.Post(srv.URL+"/v1/setup/models", "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out bytes.Buffer
		out.ReadFrom(resp.Body)
		return resp.StatusCode, out.String()
	}
	if code, body := models(srvModels.URL + "/leak"); code != 422 || strings.Contains(body, "internal-secret-value") {
		t.Errorf("a foreign answer was read out: %d %s", code, body)
	}
	if code, body := models(srvModels.URL + "/err"); code != 422 || !strings.Contains(body, "model not loaded") {
		t.Errorf("the model server's own error message must come through: %d %s", code, body)
	}
	if code, _ := models(srvModels.URL + "/redirect"); code != 422 || inner.Load() != 0 {
		t.Errorf("a redirect was followed: %d, %d calls", code, inner.Load())
	}
	if code, body := models("file:///etc/passwd"); code != 422 || !strings.Contains(body, "http") {
		t.Errorf("a non-web address was taken: %d %s", code, body)
	}
	// 4 calls so far; the 20 a minute are used up, then 429.
	var last int
	for i := 0; i < 20; i++ {
		last, _ = models(srvModels.URL + "/err")
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("no limit on the open routes: %d", last)
	}
}

// The proof shows the token without sending it: the same token and nonce
// give the same answer, another token does not.
func TestProve(t *testing.T) {
	h := &Handler{Store: newStore(t), Token: "tok", ID: "id", Version: "t", Log: slog.Default()}
	mux := http.NewServeMux()
	h.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	prove := func(body string) (int, string) {
		resp, err := http.Post(srv.URL+"/v1/prove", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out struct {
			Proof string `json:"proof"`
		}
		json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out.Proof
	}
	nonce := "0123456789abcdef0123"
	code, proof := prove(`{"nonce":"` + nonce + `"}`)
	if code != 200 || proof != Proof("tok", nonce) || proof == Proof("other", nonce) || strings.Contains(proof, "tok") {
		t.Errorf("proof: %d %q", code, proof)
	}
	// The integration computes the same in Python; the vector is shared
	// with tests/ha/test_config_flow.py.
	if want := "8d3326e40cf47d6e108e1d121286a33260f495bcf4d3ceab92a10e467a745b19"; proof != want {
		t.Errorf("proof %s, the integration expects %s", proof, want)
	}
	if code, _ := prove(`{"nonce":"short"}`); code != 400 {
		t.Errorf("short nonce: %d", code)
	}
}
