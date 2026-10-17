// Package haconv is the HTTP API the Home Assistant integration talks to. The
// integration is a pure forwarder: it posts what the satellite heard and
// speaks what comes back.
package haconv

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/miraassist/mira/internal/channels/telegram"
	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/conversation"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/tasks"
	"github.com/miraassist/mira/internal/trace"
)

// Conversation is the part of the engine the API needs.
type Conversation interface {
	Handle(ctx context.Context, req conversation.Request) (conversation.Response, error)
}

// HealthCheck reports the state of one dependency.
type HealthCheck func(ctx context.Context) error

type Options struct {
	// Eval is eval mode (docs/spec/evals.md): answers carry their trace,
	// and POST /v1/eval/reset empties Mira's state between scenarios.
	Eval         bool
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
	// Persons returns the valid personal receivers of a message. A function,
	// because the web UI can change them while the agent runs.
	Persons func() map[string]config.Person
	// TurnTimeout bounds one conversation request.
	TurnTimeout time.Duration
	// Tasks serves mira.assess and mira.decide.
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
	s.mux.HandleFunc("POST /v1/conversation/reset", s.resetConversation)
	s.mux.HandleFunc("GET /v1/status", s.status)
	s.mux.HandleFunc("PUT /v1/quiet_mode", s.quietMode)
	s.mux.HandleFunc("POST /v1/messages", s.addMessage)
	s.mux.HandleFunc("POST /v1/assess", s.assess)
	s.mux.HandleFunc("POST /v1/decide", s.decide)
	if s.opts.Eval {
		s.mux.HandleFunc("POST /v1/eval/reset", s.evalReset)
	}
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

// decide serves mira.decide: a yes/no answer, no message.
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
	// UserID is the signed-in HA user (the app, its chat); satellites
	// send none.
	UserID string `json:"user_id"`
	// Channel is "voice" (default: satellites and the HA chat) or
	// "telegram": a chat of HA's Telegram integration, passed on by the
	// integration's adapter (docs/spec/kanaele.md).
	Channel string `json:"channel"`
	// Person is who writes on a messenger: the integration matched the
	// chat to a person's channels. Trusted, like everything that comes with
	// the token.
	Person string `json:"person"`
}

type conversationResponse struct {
	Text                 string `json:"text"`
	ConversationID       string `json:"conversation_id"`
	ContinueConversation bool   `json:"continue_conversation"`
	// HTML is the answer for Telegram: Telegram's HTML, split into
	// messages it accepts.
	HTML []string `json:"html,omitempty"`
	// Trace is what happened in the turn, in eval mode only.
	Trace []trace.Event `json:"trace,omitempty"`
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
	channel := config.ChannelVoice
	switch req.Channel {
	case "", string(config.ChannelVoice):
	case string(config.ChannelTelegram):
		channel = config.ChannelTelegram
	default:
		writeError(w, http.StatusBadRequest, "channel must be voice or telegram")
		return
	}
	resp, err := s.opts.Conversation.Handle(ctx, conversation.Request{
		Channel: channel, ConversationID: req.ConversationID, DeviceID: req.DeviceID,
		Language: req.Language, Text: req.Text, Extra: req.ExtraSystemPrompt, HAUser: req.UserID,
		Person: req.Person,
	})
	if err != nil {
		s.log.Error("conversation failed", "error", err)
		writeError(w, http.StatusBadGateway, "the agent could not answer")
		return
	}
	out := conversationResponse{
		Text: resp.Text, ConversationID: resp.ConversationID, ContinueConversation: resp.ContinueConversation,
		Trace: resp.Trace,
	}
	if channel == config.ChannelTelegram {
		for _, part := range telegram.Split(resp.Text, telegram.MaxMessageLen) {
			out.HTML = append(out.HTML, telegram.ToHTML(part))
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// evalReset empties Mira's state between eval scenarios (conversations,
// threads, memory, messages) and puts in the facts a scenario starts with.
// Only in eval mode.
func (s *Server) evalReset(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Facts []struct {
			Owner string `json:"owner"`
			Text  string `json:"text"`
		} `json:"facts"`
	}
	if r.ContentLength != 0 {
		if err := decode(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	ctx := r.Context()
	if err := s.opts.Store.ResetForEval(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for i, f := range req.Facts {
		owner := f.Owner
		if owner == "" {
			owner = store.OwnerHouse
		}
		if _, err := s.opts.Store.SaveFact(ctx, store.Fact{ID: fmt.Sprintf("eval-%d", i), Text: f.Text, Owner: owner, Author: "eval"}); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "reset"})
}

// resetter starts a conversation afresh ("/neu" on Telegram).
type resetter interface {
	ResetSession(ctx context.Context, ch config.Channel, conversationID string) error
}

func (s *Server) resetConversation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Channel        string `json:"channel"`
		ConversationID string `json:"conversation_id"`
	}
	if err := decode(r, &req); err != nil || req.ConversationID == "" {
		writeError(w, http.StatusBadRequest, `body must be {"channel", "conversation_id"}`)
		return
	}
	reset, ok := s.opts.Conversation.(resetter)
	if !ok || (req.Channel != string(config.ChannelTelegram) && req.Channel != string(config.ChannelVoice)) {
		writeError(w, http.StatusBadRequest, "cannot reset that conversation")
		return
	}
	if err := reset.ResetSession(r.Context(), config.Channel(req.Channel), req.ConversationID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"reset": true})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	pending, err := s.opts.Store.CountPendingMessages(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	quiet, err := s.opts.Store.GetBool(r.Context(), store.KeyQuietMode, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pending_messages": pending, "quiet_mode": quiet})
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

type messageRequest struct {
	Text       string `json:"text"`
	Details    string `json:"details"`
	Importance int    `json:"importance"`
	Receiver   string `json:"receiver"`
	DedupeKey  string `json:"dedupe_key"`
	// Confidentiality is "normal" (default) or "confidential".
	Confidentiality string `json:"confidentiality"`
	// ExpiresIn is a Go duration such as "12h". Default 24h.
	ExpiresIn string `json:"expires_in"`
	// Task is the automation that sent it, when it is one of Mira's tasks
	// ({{ this.attributes.id }}): the message then counts as the task's and
	// shows in its log.
	Task string `json:"task"`
}

func (s *Server) addMessage(w http.ResponseWriter, r *http.Request) {
	var req messageRequest
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
	if a := req.Receiver; a != "" && a != store.ReceiverHouse {
		var persons map[string]config.Person
		if s.opts.Persons != nil {
			persons = s.opts.Persons()
		}
		if _, ok := persons[a]; !ok {
			writeError(w, http.StatusBadRequest, "receiver must be house or a person")
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
	source := store.SourceExternal
	if req.Task != "" {
		source = store.SourceTask
	}
	n, created, err := s.opts.Store.PutMessage(r.Context(), store.Message{
		ID: ulid.Make().String(), Source: source, DedupeKey: req.DedupeKey,
		Importance: req.Importance, Receiver: req.Receiver, Spoken: strings.TrimSpace(req.Text),
		Details: req.Details, ExpiresAt: time.Now().Add(ttl), Confidentiality: req.Confidentiality,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Task != "" {
		if err := s.opts.Store.AddAssessment(r.Context(), store.Assessment{ID: ulid.Make().String(), Task: req.Task,
			Kind: store.AssessmentMessage, Goal: req.Text, Importance: req.Importance, Spoken: n.Spoken, MessageID: n.ID}); err != nil {
			s.log.Warn("could not log a task's message", "task", req.Task, "error", err)
		}
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
