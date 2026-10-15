// Package haconv is the HTTP API the Home Assistant integration talks to. The
// integration is a pure forwarder: it posts what the satellite heard and
// speaks what comes back.
package haconv

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/sker65/mira/internal/config"
	"github.com/sker65/mira/internal/conversation"
	"github.com/sker65/mira/internal/store"
	"github.com/sker65/mira/internal/tasks"
)

// Conversation is the part of the engine the API needs.
type Conversation interface {
	Handle(ctx context.Context, req conversation.Request) (conversation.Response, error)
}

// HealthCheck reports the state of one dependency.
type HealthCheck func(ctx context.Context) error

type Options struct {
	Token        string
	Conversation Conversation
	Store        *store.Store
	// Checks are run by /v1/health. A failing check makes the agent
	// "degraded", not down: it still answers, just with less.
	Checks  map[string]HealthCheck
	Version string
	// InstanceID identifies this agent; the integration uses it as the
	// config entry's unique id.
	InstanceID string
	Logger     *slog.Logger
	// Persons returns the valid personal audiences of a notice. A function,
	// because the web UI can change them while the agent runs.
	Persons func() map[string]config.Person
	// TurnTimeout bounds one conversation request.
	TurnTimeout time.Duration
	// Tasks serves mira.assess and mira.decide; nil without Home Assistant.
	Tasks Tasks
	// HAUser returns the agent's Home Assistant user id, which the
	// integration lets manage Mira's automations; "" when unknown.
	HAUser func(ctx context.Context) string
}

// Tasks judges for automations (package tasks).
type Tasks interface {
	Assess(ctx context.Context, r tasks.Request) (tasks.Verdict, error)
	Decide(ctx context.Context, r tasks.Request) (tasks.Decision, error)
}

type Server struct {
	opts Options
	log  *slog.Logger
	mux  *http.ServeMux
}

func New(opts Options) *Server {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.TurnTimeout == 0 {
		opts.TurnTimeout = 60 * time.Second
	}
	s := &Server{opts: opts, log: opts.Logger.With("component", "http"), mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /v1/health", s.health)
	s.mux.HandleFunc("POST /v1/conversation", s.conversation)
	s.mux.HandleFunc("GET /v1/status", s.status)
	s.mux.HandleFunc("PUT /v1/quiet_mode", s.quietMode)
	s.mux.HandleFunc("POST /v1/notices", s.addNotice)
	s.mux.HandleFunc("POST /v1/assess", s.assess)
	s.mux.HandleFunc("POST /v1/decide", s.decide)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.opts.Token != "" {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.opts.Token)) != 1 {
			writeError(w, http.StatusUnauthorized, "missing or wrong bearer token")
			return
		}
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	checks := map[string]string{}
	status := "ok"
	for name, check := range s.opts.Checks {
		if err := check(ctx); err != nil {
			checks[name] = err.Error()
			status = "degraded"
		} else {
			checks[name] = "ok"
		}
	}
	out := map[string]any{"status": status, "version": s.opts.Version, "id": s.opts.InstanceID, "checks": checks}
	if s.opts.HAUser != nil {
		if user := s.opts.HAUser(ctx); user != "" {
			out["ha_user_id"] = user
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// assess serves mira.assess: judge, maybe notify, answer the verdict.
func (s *Server) assess(w http.ResponseWriter, r *http.Request) {
	if s.opts.Tasks == nil {
		writeError(w, http.StatusServiceUnavailable, "tasks need Home Assistant")
		return
	}
	var req tasks.Request
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	v, err := s.opts.Tasks.Assess(r.Context(), req)
	if err != nil {
		s.log.Error("assess failed", "error", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// decide serves mira.decide: a yes/no answer, no notice.
func (s *Server) decide(w http.ResponseWriter, r *http.Request) {
	if s.opts.Tasks == nil {
		writeError(w, http.StatusServiceUnavailable, "tasks need Home Assistant")
		return
	}
	var req tasks.Request
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	d, err := s.opts.Tasks.Decide(r.Context(), req)
	if err != nil {
		s.log.Error("decide failed", "error", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, d)
}

type conversationRequest struct {
	Text           string `json:"text"`
	ConversationID string `json:"conversation_id"`
	DeviceID       string `json:"device_id"`
	Language       string `json:"language"`
	AgentID        string `json:"agent_id"`
	// ExtraSystemPrompt is what HA passes for start_conversation and similar
	// flows. It only ever reaches layer 7.
	ExtraSystemPrompt string `json:"extra_system_prompt"`
}

type conversationResponse struct {
	Text                 string `json:"text"`
	ConversationID       string `json:"conversation_id"`
	ContinueConversation bool   `json:"continue_conversation"`
}

func (s *Server) conversation(w http.ResponseWriter, r *http.Request) {
	var req conversationRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.opts.TurnTimeout)
	defer cancel()
	resp, err := s.opts.Conversation.Handle(ctx, conversation.Request{
		Channel: config.ChannelVoice, ConversationID: req.ConversationID, DeviceID: req.DeviceID,
		Language: req.Language, Text: req.Text, Extra: req.ExtraSystemPrompt,
	})
	if err != nil {
		s.log.Error("conversation failed", "error", err)
		writeError(w, http.StatusBadGateway, "the agent could not answer")
		return
	}
	writeJSON(w, http.StatusOK, conversationResponse{
		Text: resp.Text, ConversationID: resp.ConversationID, ContinueConversation: resp.ContinueConversation,
	})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	pending, err := s.opts.Store.CountPendingNotices(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	quiet, err := s.opts.Store.GetBool(r.Context(), store.KeyQuietMode, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pending_notices": pending, "quiet_mode": quiet})
}

func (s *Server) quietMode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decode(r, &req); err != nil || req.Enabled == nil {
		writeError(w, http.StatusBadRequest, `body must be {"enabled": true|false}`)
		return
	}
	if err := s.opts.Store.SetBool(r.Context(), store.KeyQuietMode, *req.Enabled); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"quiet_mode": *req.Enabled})
}

type noticeRequest struct {
	Text       string `json:"text"`
	Details    string `json:"details"`
	Importance int    `json:"importance"`
	Audience   string `json:"audience"`
	DedupeKey  string `json:"dedupe_key"`
	// Confidentiality is "normal" (default) or "confidential".
	Confidentiality string `json:"confidentiality"`
	// ExpiresIn is a Go duration such as "12h". Default 24h.
	ExpiresIn string `json:"expires_in"`
}

func (s *Server) addNotice(w http.ResponseWriter, r *http.Request) {
	var req noticeRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}
	if c := req.Confidentiality; c != "" && c != store.ConfidentialityNormal && c != store.ConfidentialityConfidential {
		writeError(w, http.StatusBadRequest, "confidentiality must be normal or confidential")
		return
	}
	if a := req.Audience; a != "" && a != store.AudienceHousehold {
		var persons map[string]config.Person
		if s.opts.Persons != nil {
			persons = s.opts.Persons()
		}
		if _, ok := persons[a]; !ok {
			writeError(w, http.StatusBadRequest, "audience must be household or a configured person")
			return
		}
	}
	if req.Importance < 0 || req.Importance > 3 {
		writeError(w, http.StatusBadRequest, "importance must be between 0 and 3")
		return
	}
	ttl := 24 * time.Hour
	if req.ExpiresIn != "" {
		d, err := time.ParseDuration(req.ExpiresIn)
		if err != nil || d <= 0 {
			writeError(w, http.StatusBadRequest, "expires_in must be a positive duration like 12h")
			return
		}
		ttl = d
	}
	n, created, err := s.opts.Store.PutNotice(r.Context(), store.Notice{
		ID: ulid.Make().String(), Source: store.SourceExternal, DedupeKey: req.DedupeKey,
		Importance: req.Importance, Audience: req.Audience, Spoken: strings.TrimSpace(req.Text),
		Details: req.Details, ExpiresAt: time.Now().Add(ttl), Confidentiality: req.Confidentiality,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	code := http.StatusOK
	if created {
		code = http.StatusCreated
	}
	writeJSON(w, code, map[string]any{"id": n.ID, "created": created})
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	if err := dec.Decode(v); err != nil {
		return errors.New("invalid JSON body: " + err.Error())
	}
	return nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
