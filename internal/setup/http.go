package setup

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/store"
)

// Handler serves pairing and setup (docs/spec/einrichtung.md):
//
//	GET  /v1/pair          open: the agent's id, whether it pairs without a
//	                       code (never paired) or a code waits, its model
//	POST /v1/pair          open: {code, rejoin, ha_url, ha_token, llm} ->
//	                       {token}; pairs and sets up in one step, then
//	                       restarts; the first time code is "", after an
//	                       unpair the same Home Assistant sends rejoin
//	POST /v1/unpair        token: Home Assistant lets go; the agent restarts
//	POST /v1/setup         token: {ha_url, ha_token, llm}; a trial request
//	                       to the model first, then the agent restarts
//	POST /v1/setup/models  token, or open while never paired:
//	                       {base_url, api_key} -> the models there
//	POST /v1/setup/probe   token, or open while never paired: {llm}; the
//	                       trial request alone
//	POST /v1/prove         open: {nonce} -> {proof}; shows the agent has
//	                       the token without sending it, before Home
//	                       Assistant follows it to a new address
//
// Errors about the model carry a reason the integration shows: unreachable,
// bad_key, no_model, no_tools, server_error.
//
// In setup mode (nothing set up yet) it also serves a page saying what to do.
type Handler struct {
	Store   *store.Store
	Token   string // the token the agent runs with
	ID      string
	Version string
	// Pairing is allowed when the token was made by the agent, not
	// written into mira.yaml.
	Pairing bool
	// SetupMode: Home Assistant or the model is missing; the agent waits.
	SetupMode bool
	// Model: the model set (in the file or by setup); Home Assistant then
	// need not ask for one.
	Model string
	// Restart applies new settings: the agent starts anew.
	Restart func()
	HTTP    *http.Client
	Clock   func() time.Time
	Log     *slog.Logger

	gate gate
}

// gate limits calls made without the token: at most openPerMinute a minute
// and openParallel at once.
type gate struct {
	mu    sync.Mutex
	times []time.Time
	busy  int
}

const (
	openPerMinute = 20
	openParallel  = 2
)

func (g *gate) enter(now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	keep := g.times[:0]
	for _, t := range g.times {
		if now.Sub(t) < time.Minute {
			keep = append(keep, t)
		}
	}
	g.times = keep
	if len(g.times) >= openPerMinute || g.busy >= openParallel {
		return false
	}
	g.times = append(g.times, now)
	g.busy++
	return true
}

func (g *gate) leave() {
	g.mu.Lock()
	g.busy--
	g.mu.Unlock()
}

func (h *Handler) now() time.Time {
	if h.Clock != nil {
		return h.Clock()
	}
	return time.Now()
}

// Register adds the routes to a mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/pair", h.status)
	mux.HandleFunc("POST /v1/pair", h.pair)
	mux.HandleFunc("POST /v1/prove", h.prove)
	mux.HandleFunc("POST /v1/unpair", h.auth(h.unpair))
	mux.HandleFunc("POST /v1/setup", h.auth(h.setup))
	mux.HandleFunc("GET /v1/setup/providers", h.authOrOpen(h.providers))
	mux.HandleFunc("POST /v1/setup/models", h.authOrOpen(h.models))
	mux.HandleFunc("POST /v1/setup/probe", h.authOrOpen(h.probeOnly))
	if h.SetupMode {
		mux.HandleFunc("GET /v1/health", h.health)
		mux.HandleFunc("GET /", h.page)
	}
}

func (h *Handler) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if h.Token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(h.Token)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing or wrong bearer token"})
			return
		}
		next(w, r)
	}
}

// authOrOpen lets the setup form choose a model before pairing: open while
// the agent was never paired (pairing comes last, with everything chosen),
// else with the token.
func (h *Handler) authOrOpen(next http.HandlerFunc) http.HandlerFunc {
	authed := h.auth(next)
	return func(w http.ResponseWriter, r *http.Request) {
		if h.open(r.Context()) {
			// These routes make the agent send requests to an address the
			// caller names, so the few that are open are rationed.
			if !h.gate.enter(h.now()) {
				writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many requests; wait a minute"})
				return
			}
			defer h.gate.leave()
			next(w, r)
			return
		}
		authed(w, r)
	}
}

func (h *Handler) pending(ctx context.Context) bool {
	if !h.Pairing {
		return false
	}
	code, err := PendingCode(ctx, h.Store)
	return err == nil && code != ""
}

// open: the agent was never paired, so the first pairing needs no code.
func (h *Handler) open(ctx context.Context) bool {
	if !h.Pairing {
		return false
	}
	s, err := Load(ctx, h.Store)
	return err == nil && s.Open
}

// rejoin: the Home Assistant that let go may pair again without a code.
func (h *Handler) rejoin(ctx context.Context) bool {
	if !h.Pairing {
		return false
	}
	s, err := Load(ctx, h.Store)
	return err == nil && s.Rejoin
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"id": h.ID, "version": h.Version, "pairs": h.Pairing, "open": h.open(r.Context()),
		"pairing": h.pending(r.Context()), "setup_mode": h.SetupMode, "model": h.Model,
		"rejoin": h.rejoin(r.Context()),
	})
}

// health in setup mode: up, but waiting for setup.
func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "setup", "id": h.ID, "version": h.Version, "open": h.open(r.Context()),
		"pairing": h.pending(r.Context()), "model": h.Model,
	})
}

func (h *Handler) pair(w http.ResponseWriter, r *http.Request) {
	if !h.Pairing {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "this agent's token is set in mira.yaml; enter it instead of a code"})
		return
	}
	var req struct {
		Code    string `json:"code"`
		Rejoin  string `json:"rejoin"`
		HAURL   string `json:"ha_url"`
		HAToken string `json:"ha_token"`
		LLM     *LLM   `json:"llm"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": `body must be {"code", "ha_url", "ha_token", "llm"}`})
		return
	}
	if req.HAURL == "" || req.HAToken == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "pairing needs ha_url and ha_token"})
		return
	}
	if l := req.LLM; l != nil && (l.BaseURL == "" || l.Model == "") {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "the model needs an address and a name"})
		return
	}
	if req.LLM == nil && h.Model == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "Mira has no model yet; choose one"})
		return
	}
	h.contextOf(r.Context(), req.LLM)
	token, err := Pair(r.Context(), h.Store, req.Code, req.Rejoin, req.HAURL, req.HAToken, req.LLM)
	if errors.Is(err, ErrBadCode) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.Log.Info("paired with Home Assistant and set up; starting anew", "model", req.LLM != nil)
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
	if h.Restart != nil {
		time.AfterFunc(300*time.Millisecond, h.Restart)
	}
}

// prove answers a nonce with an HMAC under the token. Home Assistant
// computes the same with the token it keeps; an agent that merely announces
// the same id cannot.
func (h *Handler) prove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Nonce string `json:"nonce"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req); err != nil || len(req.Nonce) < 16 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": `body must be {"nonce"}, at least 16 characters`})
		return
	}
	if h.Token == "" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "this agent has no token"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"proof": Proof(h.Token, req.Nonce)})
}

// Proof is the answer to a nonce: HMAC-SHA256 under the token, in hex.
func Proof(token, nonce string) string {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte("mira-prove:" + nonce))
	return hex.EncodeToString(mac.Sum(nil))
}

// probeOnly is the trial request alone, for the setup form's model step.
func (h *Handler) probeOnly(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LLM *LLM `json:"llm"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&req); err != nil || req.LLM == nil ||
		req.LLM.BaseURL == "" || req.LLM.Model == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": `body must be {"llm": {"kind", "base_url", "model", "api_key"}}`})
		return
	}
	if err := h.probe(r.Context(), *req.LLM); err != nil {
		writeModelError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) unpair(w http.ResponseWriter, r *http.Request) {
	if !h.Pairing {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "this agent's token is set in mira.yaml"})
		return
	}
	code, err := Unpair(r.Context(), h.Store)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.Log.Info("Home Assistant let go of Mira; waiting to be paired again. The same Home Assistant pairs without a code; another one needs this pairing code", "code", ShowCode(code))
	writeJSON(w, http.StatusOK, map[string]string{"status": "restarting"})
	if h.Restart != nil {
		time.AfterFunc(300*time.Millisecond, h.Restart)
	}
}

func (h *Handler) setup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		HAURL   string `json:"ha_url"`
		HAToken string `json:"ha_token"`
		LLM     *LLM   `json:"llm"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if (req.HAURL == "") != (req.HAToken == "") {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "ha_url and ha_token come together"})
		return
	}
	if l := req.LLM; l != nil {
		if l.BaseURL == "" || l.Model == "" {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "the model needs an address and a name"})
			return
		}
		if err := h.probe(r.Context(), *l); err != nil {
			writeModelError(w, err)
			return
		}
	}
	h.contextOf(r.Context(), req.LLM)
	if err := Save(r.Context(), h.Store, req.HAURL, req.HAToken, req.LLM); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.Log.Info("set up from Home Assistant; starting anew", "ha", req.HAURL != "", "model", req.LLM != nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "restarting"})
	if h.Restart != nil {
		time.AfterFunc(300*time.Millisecond, h.Restart)
	}
}

// providers are the cloud providers the setup offers.
func (h *Handler) providers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"providers": llm.Providers})
}

func (h *Handler) models(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BaseURL string `json:"base_url"`
		APIKey  string `json:"api_key"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&req); err != nil || req.BaseURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": `body must be {"base_url", "api_key"}`})
		return
	}
	models, err := h.list(r.Context(), req.BaseURL, req.APIKey)
	if err != nil {
		writeModelError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

// ModelError is what went wrong with a model server, with a reason the
// integration turns into a sentence.
type ModelError struct {
	Reason string // unreachable, bad_key, no_model, no_tools, server_error
	Detail string
}

func (e *ModelError) Error() string { return e.Reason + ": " + e.Detail }

func writeModelError(w http.ResponseWriter, err error) {
	var me *ModelError
	if !errors.As(err, &me) {
		me = &ModelError{Reason: "server_error", Detail: err.Error()}
	}
	writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": me.Detail, "reason": me.Reason})
}

// call sends one request to a model server and sorts out what failed.
func (h *Handler) call(ctx context.Context, method, baseURL, path, apiKey string, body any, timeout time.Duration) ([]byte, error) {
	if u, err := url.Parse(baseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, &ModelError{Reason: "unreachable", Detail: "only http and https addresses are used"}
	}
	client := h.HTTP
	if client == nil {
		// A redirect is never followed: the address is the one the caller named.
		client = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(baseURL, "/")+path, rd)
	if err != nil {
		return nil, &ModelError{Reason: "unreachable", Detail: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	// Bearer for the OpenAI-compatible API; Anthropic's /models also wants
	// its own header (llm.SetAuth).
	llm.SetAuth(req, apiKey)
	resp, err := client.Do(req)
	if err != nil {
		return nil, &ModelError{Reason: "unreachable", Detail: err.Error()}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusOK:
		return raw, nil
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, &ModelError{Reason: "bad_key", Detail: statusText(resp.StatusCode, raw)}
	case resp.StatusCode == http.StatusNotFound:
		return nil, &ModelError{Reason: "no_model", Detail: statusText(404, raw)}
	default:
		return nil, &ModelError{Reason: "server_error", Detail: statusText(resp.StatusCode, raw)}
	}
}

// statusText is the status, and the model server's own error message if it
// sent one in the usual form ({"error": "…"} or {"error": {"message": "…"}}).
// Nothing else of the body: this call can be made without the token before
// the first pairing, and must not read other servers' answers out to a caller.
func statusText(status int, raw []byte) string {
	out := fmt.Sprintf("HTTP %d", status)
	if msg := errorMessage(raw); msg != "" {
		out += " " + msg
	}
	return out
}

func errorMessage(raw []byte) string {
	var one struct {
		Error any `json:"error"`
	}
	if json.Unmarshal(raw, &one) != nil {
		var many []struct {
			Error any `json:"error"`
		}
		if json.Unmarshal(raw, &many) != nil || len(many) == 0 {
			return ""
		}
		one = many[0]
	}
	msg := ""
	switch e := one.Error.(type) {
	case string:
		msg = e
	case map[string]any:
		msg, _ = e["message"].(string)
	}
	msg = strings.Join(strings.Fields(msg), " ")
	if r := []rune(msg); len(r) > 200 {
		msg = string(r[:200]) + "…"
	}
	return msg
}

// probeTimeout leaves room for a local server to load the model first.
const probeTimeout = 3 * time.Minute

// probe is a real, small request: the model must answer and call a tool,
// since Mira does everything through tools. It warms a local model too.
// Probe tries a model with a real request, as the setup does; what fails
// is a *ModelError with its reason. Mira's page checks a model with it
// before it saves it.
func Probe(ctx context.Context, l LLM) error { return (&Handler{}).probe(ctx, l) }

func (h *Handler) probe(ctx context.Context, l LLM) error {
	req := map[string]any{
		"model": l.Model,
		"messages": []map[string]string{
			{"role": "system", "content": "You control a home. Use the tools to act."},
			{"role": "user", "content": "Turn on the kitchen light."},
		},
		"tools": []map[string]any{{"type": "function", "function": map[string]any{
			"name": "turn_on", "description": "Turn on a device by its name.",
			"parameters": map[string]any{"type": "object", "properties": map[string]any{
				"name": map[string]any{"type": "string"}}, "required": []string{"name"}},
		}}},
		"max_tokens": 1024,
	}
	if l.Kind != KindCloud {
		req["cache_prompt"] = true
	}
	raw, err := h.call(ctx, http.MethodPost, l.BaseURL, "/chat/completions", l.APIKey, req, probeTimeout)
	if err != nil {
		var me *ModelError
		if errors.As(err, &me) && me.Reason == "server_error" && mentionsModel(me.Detail) {
			me.Reason = "no_model"
		}
		return err
	}
	var resp struct {
		Choices []struct {
			Message struct {
				ToolCalls []json.RawMessage `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || len(resp.Choices) == 0 {
		return &ModelError{Reason: "server_error", Detail: "no answer in the reply"}
	}
	if len(resp.Choices[0].Message.ToolCalls) == 0 {
		return &ModelError{Reason: "no_tools", Detail: l.Model}
	}
	return nil
}

func mentionsModel(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, "model")
}

// contextOf asks the model's server once, at setup, how much context the
// model has, and keeps it with the model (docs/spec/modelle.md, Context
// size); a server that does not say leaves it to the default.
func (h *Handler) contextOf(ctx context.Context, l *LLM) {
	if l == nil {
		return
	}
	n, from := llm.ContextSize(ctx, h.HTTP, l.BaseURL, l.APIKey, l.Model)
	l.ContextTokens = n
	h.Log.Info("context size of the model", "model", l.Model, "tokens", n, "from", from)
}

// list asks an OpenAI-compatible server for its models.
func (h *Handler) list(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	raw, err := h.call(ctx, http.MethodGet, baseURL, "/models", apiKey, nil, 15*time.Second)
	if err != nil {
		return nil, err
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, &ModelError{Reason: "server_error", Detail: "not an OpenAI-compatible list of models"}
	}
	out := make([]string, 0, len(body.Data))
	for _, m := range body.Data {
		out = append(out, m.ID)
	}
	return out, nil
}

// page shows, while nothing is set up, what to do and the code.
func (h *Handler) page(w http.ResponseWriter, r *http.Request) {
	// The code itself is only in the log: whoever can read that may pair.
	shown := ""
	if h.pending(r.Context()) {
		shown = codeHint
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprintf(w, setupPage, shown)
}

const codeHint = `<p>Diese Mira war schon mit einem Home Assistant gekoppelt. Ein anderes Home Assistant braucht den <b>Kopplungscode</b>: Er steht im Protokoll von Mira (<code>docker logs mira</code>, oder in Home Assistant im Protokoll der Mira-App).</p>
<p><small>This Mira was paired before. Another Home Assistant needs the pairing code from Mira's log.</small></p>`

const setupPage = `<!doctype html><html lang="de"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Mira – Einrichtung</title>
<style>body{font-family:system-ui,sans-serif;max-width:34rem;margin:3rem auto;padding:0 1rem;line-height:1.5;color:#222}
ol{padding-left:1.2rem}small{color:#666}
@media (prefers-color-scheme:dark){body{background:#111;color:#eee}small{color:#aaa}}</style></head><body>
<h1>Mira wartet auf die Einrichtung</h1>
%s
<ol><li>In Home Assistant: <b>Einstellungen → Geräte &amp; Dienste</b>. Mira steht unter <b>Entdeckt</b> → <b>Hinzufügen</b> (sonst: Integration hinzufügen → Mira).</li>
<li>Das KI-Modell (LLM) wählen.</li><li>Bestätigen, welche Assistenten Mira verwenden. Fertig.</li></ol>
<p><small>Mira is waiting to be set up: in Home Assistant, add Mira under Settings → Devices &amp; services, choose the AI model (LLM), and confirm the assistants.</small></p>
</body></html>`

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
