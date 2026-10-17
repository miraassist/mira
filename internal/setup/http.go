package setup

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/store"
)

// Handler serves pairing and setup (docs/spec/einrichtung.md):
//
//	GET  /v1/pair          open: the agent's id and whether a code waits
//	POST /v1/pair          open: {code} -> {token}
//	POST /v1/setup         token: {ha_url, ha_token, llm}; the agent restarts
//	POST /v1/setup/models  token: {base_url, api_key} -> the models there
//
// In setup mode (nothing set up yet) it also serves a page with the code.
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
	// Restart applies new settings: the agent starts anew.
	Restart func()
	HTTP    *http.Client
	Clock   func() time.Time
	Log     *slog.Logger
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
	mux.HandleFunc("POST /v1/setup", h.auth(h.setup))
	mux.HandleFunc("POST /v1/setup/models", h.auth(h.models))
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

func (h *Handler) pending(ctx context.Context) bool {
	if !h.Pairing {
		return false
	}
	code, err := PendingCode(ctx, h.Store, h.now())
	return err == nil && code != ""
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"id": h.ID, "version": h.Version, "pairing": h.pending(r.Context()), "setup_mode": h.SetupMode,
	})
}

// health in setup mode: up, but waiting for setup.
func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "setup", "id": h.ID, "version": h.Version, "pairing": h.pending(r.Context()),
	})
}

func (h *Handler) pair(w http.ResponseWriter, r *http.Request) {
	if !h.Pairing {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "this agent's token is set in mira.yaml; enter it instead of a code"})
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": `body must be {"code": "123456"}`})
		return
	}
	token, err := Pair(r.Context(), h.Store, req.Code, h.now())
	if errors.Is(err, ErrBadCode) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.Log.Info("paired with Home Assistant")
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
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
		models, err := h.list(r.Context(), l.BaseURL, l.APIKey)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "the model server does not answer: " + err.Error()})
			return
		}
		if len(models) > 0 && !contains(models, l.Model) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": fmt.Sprintf("the server has no model %q", l.Model)})
			return
		}
	}
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
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

// list asks an OpenAI-compatible server for its models.
func (h *Handler) list(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	client := h.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(body.Data))
	for _, m := range body.Data {
		out = append(out, m.ID)
	}
	return out, nil
}

// page shows, while nothing is set up, what to do and the code.
func (h *Handler) page(w http.ResponseWriter, r *http.Request) {
	code, _ := PendingCode(r.Context(), h.Store, h.now())
	if !h.Pairing {
		code = ""
	}
	shown := "–"
	if len(code) == 6 {
		shown = code[:3] + " " + code[3:]
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprintf(w, setupPage, html.EscapeString(shown))
}

const setupPage = `<!doctype html><html lang="de"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Mira – Einrichtung</title>
<style>body{font-family:system-ui,sans-serif;max-width:34rem;margin:3rem auto;padding:0 1rem;line-height:1.5;color:#222}
.code{font-size:2.6rem;font-weight:700;letter-spacing:.15em;margin:.4rem 0 1rem}ol{padding-left:1.2rem}small{color:#666}
@media (prefers-color-scheme:dark){body{background:#111;color:#eee}small{color:#aaa}}</style></head><body>
<h1>Mira wartet auf die Einrichtung</h1>
<p>Kopplungscode:</p><div class="code">%s</div>
<ol><li>In Home Assistant: <b>Einstellungen → Geräte &amp; Dienste</b>. Mira steht unter <b>Entdeckt</b> (sonst: Integration hinzufügen → Mira).</li>
<li>Den Kopplungscode eingeben.</li><li>Das Sprachmodell wählen. Fertig.</li></ol>
<p><small>Der Code gilt 15 Minuten. Einen neuen bekommst du, wenn du Mira neu startest, oder mit <code>mira pair</code>.<br>
Mira is waiting to be set up: in Home Assistant, add Mira under Settings → Devices &amp; services and enter the pairing code above.</small></p>
</body></html>`

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
