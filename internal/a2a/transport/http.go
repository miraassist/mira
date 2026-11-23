package transport

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	sdk "github.com/a2aproject/a2a-go/v2/a2a"
)

// StatusError is an answer of a remote agent with an HTTP status Mira reads
// as an error.
type StatusError struct {
	URL    string
	Status int
	// Body is the start of the answer.
	Body string
	// Wait is the wait the agent named (Retry-After).
	Wait time.Duration
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s: %d %s, the answer begins %q", e.URL, e.Status, http.StatusText(e.Status), e.Body)
}

// StatusTransport turns an answer of the remote agent that is no A2A
// answer into a StatusError, so the error says what came: a 408, a 429 or
// a 5xx always, another 4xx unless it carries a JSON-RPC error, which the
// SDK reads itself.
type StatusTransport struct{ Next http.RoundTripper }

func (t StatusTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := t.Next.RoundTrip(r)
	if err != nil || resp.StatusCode < 400 {
		return resp, err
	}
	always := resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests ||
		resp.StatusCode >= 500
	if !always && strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		return resp, nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	e := &StatusError{URL: r.URL.Redacted(), Status: resp.StatusCode, Body: BodyStart(body)}
	if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && secs > 0 {
		e.Wait = time.Duration(secs) * time.Second
	}
	return nil, e
}

// BodyStart is the start of an answer, for an error that names it.
func BodyStart(b []byte) string {
	s := strings.ToValidUTF8(string(b), "?")
	if r := []rune(s); len(r) > 200 {
		return string(r[:200]) + "…"
	}
	return s
}

// Router is Mira's push route (POST /v1/a2a/push/<call id>, E of
// docs/spec/remote-agents.md): it checks the call and its token against
// the open push transports and hands the event to that transport. It
// answers at once and never blocks.
type Router struct {
	log *slog.Logger
	// Ended reports whether a call of that id ended with that token (read
	// from its row by the caller); its late push is answered 200.
	Ended func(r *http.Request, id, token string) bool

	mu    sync.Mutex
	calls map[string]*T
}

// NewRouter makes an empty push route.
func NewRouter(log *slog.Logger, ended func(r *http.Request, id, token string) bool) *Router {
	return &Router{log: log, Ended: ended, calls: map[string]*T{}}
}

// Add lets the pushes of a call through to its transport.
func (rt *Router) Add(id string, t *T) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.calls[id] = t
}

// Remove stops them.
func (rt *Router) Remove(id string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	delete(rt.calls, id)
}

// ServeHTTP takes a push: the call by its id, the token from
// A2A-Notification-Token or Authorization: Bearer (decision 14).
func (rt *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("call")
	token := r.Header.Get("A2A-Notification-Token")
	if token == "" {
		token, _ = strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	rt.mu.Lock()
	t := rt.calls[id]
	rt.mu.Unlock()
	if t == nil || !t.Open() {
		if token != "" && rt.Ended != nil && rt.Ended(r, id, token) {
			rt.log.Info("push for a remote agent's call that has ended; nothing to do", "call_id", id, "from", r.RemoteAddr)
			w.WriteHeader(http.StatusOK)
			return
		}
		rt.log.Warn("push refused: no such call", "call_id", id, "from", r.RemoteAddr)
		http.Error(w, "unknown call or wrong token", http.StatusUnauthorized)
		return
	}
	if want := t.Token(); want == "" || subtle.ConstantTimeCompare([]byte(token), []byte(want)) != 1 {
		rt.log.Warn("push refused: wrong token", "call_id", id, "agent", t.o.Agent, "from", r.RemoteAddr)
		http.Error(w, "unknown call or wrong token", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		rt.log.Warn("push broke off", "call_id", id, "agent", t.o.Agent, "from", r.RemoteAddr, "error", err)
		http.Error(w, "the body could not be read", http.StatusBadRequest)
		return
	}
	var sr sdk.StreamResponse
	if err := json.Unmarshal(body, &sr); err != nil || sr.Event == nil {
		rt.log.Warn("push not readable", "call_id", id, "agent", t.o.Agent, "from", r.RemoteAddr, "error", err,
			"body", BodyStart(body))
		http.Error(w, "the body is no A2A event", http.StatusBadRequest)
		return
	}
	if !t.Deliver(sr.Event) {
		// Closed between the check and now: the call ended.
		rt.log.Info("push for a remote agent's call that has ended; nothing to do", "call_id", id, "from", r.RemoteAddr)
	}
	w.WriteHeader(http.StatusOK)
}
