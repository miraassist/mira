package haconv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"

	"github.com/miraassist/mira/internal/excerpt"
	haws "github.com/miraassist/mira/internal/ha/ws"
)

// HA is what the routes to Home Assistant need of Mira's connection
// (docs/spec/ha-api.md); *haws.Client is one.
type HA interface {
	// State reads one entity over Home Assistant's REST interface.
	State(ctx context.Context, entityID string) (json.RawMessage, error)
	CallService(ctx context.Context, domain, service string, data map[string]any, target *haws.Target) error
	CallServiceResponse(ctx context.Context, domain, service string, data map[string]any) (json.RawMessage, error)
	Connected() bool
}

// HAState is Home Assistant's state object as GET /api/states/<entity_id>
// gives it; Mira passes it on unchanged (the type only describes it).
type HAState struct {
	EntityID    string         `json:"entity_id"`
	State       string         `json:"state"`
	Attributes  map[string]any `json:"attributes"`
	LastChanged string         `json:"last_changed"`
	LastUpdated string         `json:"last_updated"`
	Context     map[string]any `json:"context,omitempty"`
}

// HAActionData is an action's data as Home Assistant takes it, with its
// target inside (entity_id, area_id, …).
type HAActionData map[string]any

// HAActionResult is the answer of an action: empty, or Home Assistant's
// response when return_response was asked.
type HAActionResult struct {
	Response json.RawMessage `json:"response,omitempty"`
}

// entityIDPattern is domain.object_id.
var entityIDPattern = regexp.MustCompile(`^[a-z0-9_]+\.[a-z0-9_]+$`)

// haState serves GET /v1/ha/states/{entity_id}: one request to Home
// Assistant's REST interface, no retry (decision 7).
func (s *Server) haState(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("entity_id")
	if !entityIDPattern.MatchString(id) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("%q is not an entity id (domain.object_id)", id))
		return
	}
	if s.opts.HA == nil {
		writeError(w, http.StatusServiceUnavailable, "Home Assistant is not connected")
		return
	}
	raw, err := s.opts.HA.State(r.Context(), id)
	s.noteToken(err)
	switch {
	case err == nil:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(raw)
	case errors.Is(err, haws.ErrNotFound):
		writeError(w, http.StatusNotFound, "Home Assistant has no entity "+id)
	case errors.Is(err, haws.ErrNotAdmin):
		s.log.Error("Home Assistant refused Mira's token; the caller gets 502", "route", "GET /v1/ha/states", "entity", id, "error", err)
		writeError(w, http.StatusBadGateway, "Home Assistant refused Mira's token")
	default:
		s.log.Error("reading an entity from Home Assistant failed; the caller gets 502, nothing is tried again",
			"route", "GET /v1/ha/states", "entity", id, "error", err)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("reading %s from Home Assistant failed: %v", id, err))
	}
}

// noteToken keeps whether Home Assistant refused Mira's token on the last
// call of these routes it answered (decision 10): a refusal raises the
// integration's repair through /v1/status; a call it answered otherwise (the
// state, no such entity, an action done or refused for its data) takes it
// away. A call that did not reach Home Assistant says nothing about the
// token and changes nothing.
func (s *Server) noteToken(err error) {
	var refusal *haws.Error
	switch {
	case errors.Is(err, haws.ErrNotAdmin):
		if !s.haRefused.Swap(true) {
			s.log.Warn("Home Assistant refused Mira's token on a route to Home Assistant; /v1/status reports it for the integration's repair until a call is accepted")
		}
	case err == nil, errors.Is(err, haws.ErrNotFound), errors.As(err, &refusal) && !refusal.Retryable():
		if s.haRefused.Swap(false) {
			s.log.Info("Home Assistant accepts Mira's token again; the repair goes")
		}
	}
}

// haService serves POST /v1/ha/services/{domain}/{service}: one action call
// over Mira's WebSocket connection, no retry (decision 7), no hooks
// (decision 8).
func (s *Server) haService(w http.ResponseWriter, r *http.Request) {
	domain, service := r.PathValue("domain"), r.PathValue("service")
	if domain == "" || service == "" {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("the action %q.%q needs a domain and a service", domain, service))
		return
	}
	action := domain + "." + service
	body, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "the body could not be read: "+err.Error())
		return
	}
	var data map[string]any
	if len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, &data); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("the data of %s is not a JSON object (%v): %s", action, err, excerpt.Bytes(body)))
			return
		}
	}
	if s.opts.HA == nil {
		writeError(w, http.StatusServiceUnavailable, "Home Assistant is not connected")
		return
	}
	if !s.opts.HA.Connected() {
		s.log.Error("calling an action failed: Home Assistant cannot be reached; the caller gets 502",
			"route", "POST /v1/ha/services", "action", action)
		writeError(w, http.StatusBadGateway, "calling "+action+" failed: Home Assistant cannot be reached (Mira's connection is down)")
		return
	}
	_, withResponse := r.URL.Query()["return_response"]
	var out HAActionResult
	if withResponse {
		out.Response, err = s.opts.HA.CallServiceResponse(r.Context(), domain, service, data)
	} else {
		err = s.opts.HA.CallService(r.Context(), domain, service, data, nil)
	}
	s.noteToken(err)
	var refusal *haws.Error
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, out)
	case errors.Is(err, haws.ErrNotAdmin):
		s.log.Error("Home Assistant refused Mira's token; the caller gets 502", "route", "POST /v1/ha/services", "action", action, "error", err)
		writeError(w, http.StatusBadGateway, "Home Assistant refused Mira's token")
	case errors.As(err, &refusal) && !refusal.Retryable():
		// No such action, or data it does not take: the caller's request.
		s.log.Warn("Home Assistant refused an action; the caller gets 400", "action", action, "code", refusal.Code, "message", refusal.Message)
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Home Assistant refused %s: %s (%s)", action, refusal.Message, refusal.Code))
	default:
		s.log.Error("calling an action failed; the caller gets 502, nothing is tried again",
			"route", "POST /v1/ha/services", "action", action, "error", err)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("calling %s failed: %v", action, err))
	}
}
