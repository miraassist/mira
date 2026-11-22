// Package a2afake is a fake A2A agent for the tests of Mira's remote agents
// (docs/spec/remote-agents.md, decisions 23 to 25; docs/spec/fake-devices.md):
// no model, deterministic, built with the official Go SDK. One agent; its
// agent card has one skill per behaviour, and the skill a message names in
// its metadata ("skill") picks the behaviour. Its card is served under
// several paths, one per kind of card the tests need (push only, stream only,
// neither, OAuth2, an API key, no login). What it received (card reads,
// messages, credentials, cancels, the pushes it sent) is kept for the tests.
//
// The Go tests start it in the process (httptest.NewServer(fake)); the test
// environment runs it as a container (cmd/a2a-fake, testenv/compose.yaml),
// where /fake/received, /fake/release and /fake/reset do the same over HTTP.
package a2afake

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"iter"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/push"
)

// The skills of the card, one per behaviour.
const (
	// SkillComplete completes at once with the artifact Result.
	SkillComplete = "complete"
	// SkillSlow works until Release (or the fake's Wait), then completes
	// with Result.
	SkillSlow = "slow"
	// SkillAsk asks back (input-required, Question); the answer, a message
	// with the same task id, completes it with AnswerResult and the answer.
	SkillAsk = "ask"
	// SkillAskTwice asks back as SkillAsk; the first answer makes it ask
	// again (Question2), the second completes it as SkillAsk does.
	SkillAskTwice = "ask-twice"
	// SkillLogin needs a login during the work (auth-required, LoginText).
	SkillLogin = "login"
	// SkillFail fails with FailText.
	SkillFail = "fail"
	// SkillReject rejects the task with RejectText.
	SkillReject = "reject"
	// SkillPushTwice completes as SkillComplete and sends every push twice.
	SkillPushTwice = "push-twice"
	// SkillMessage answers with a message instead of a task (Result).
	SkillMessage = "message"
)

// Skills are the card's skills, in its order.
var Skills = []string{SkillComplete, SkillSlow, SkillAsk, SkillAskTwice, SkillLogin, SkillFail, SkillReject, SkillPushTwice, SkillMessage}

// SkillMeta is the key of a message's metadata that names the card's skill.
const SkillMeta = "skill"

// The fixed texts of the behaviours.
const (
	Result       = "Die Übersicht ist fertig."
	Question     = "Wie groß ist die Wohnfläche?"
	Question2    = "Wie alt ist das Haus?"
	AnswerResult = "Die Übersicht für "
	LoginText    = "Ich brauche eine Anmeldung bei der Bibliothek."
	FailText     = "Die Quelle ist nicht erreichbar."
	RejectText   = "Das mache ich nicht."
	NoSkillText  = "Die Nachricht nennt keinen Skill der Karte (metadata skill)."
)

// The cards, by the path they are served under: the agent's base address is
// the fake's address followed by the path (Card "" at the root).
const (
	// CardFull: push and streaming, a bearer token.
	CardFull = ""
	// CardPushOnly: push, no streaming, a bearer token.
	CardPushOnly = "push-only"
	// CardStreamOnly: streaming, no push, a bearer token.
	CardStreamOnly = "stream-only"
	// CardNeither: neither push nor streaming, a bearer token.
	CardNeither = "neither"
	// CardOAuth2: push and streaming, an OAuth2 login (authorization code).
	CardOAuth2 = "oauth2"
	// CardAPIKey: push and streaming, an API key in the header APIKeyHeader.
	CardAPIKey = "api-key"
	// CardOpen: push and streaming, no login.
	CardOpen = "open"
)

// Cards are all cards.
var Cards = []string{CardFull, CardPushOnly, CardStreamOnly, CardNeither, CardOAuth2, CardAPIKey, CardOpen}

// Name is the card's name.
const Name = "Fake-Agent"

// Description is the card's description.
const Description = "Ein Agent für Tests: jeder Skill ist ein Verhalten."

// APIKeyHeader is where CardAPIKey wants the key.
const APIKeyHeader = "X-API-Key"

// DefaultToken is the credential the fake accepts when Options names none.
const DefaultToken = "fake-a2a-token"

// Options configure the fake.
type Options struct {
	// Token is the bearer token, API key or OAuth2 access token the fake
	// accepts; DefaultToken when empty.
	Token string
	// Wait ends SkillSlow after this long; 0: only Release ends it.
	Wait time.Duration
	// Logger; slog.Default() when nil.
	Logger *slog.Logger
}

// Received is one thing the fake received or sent.
type Received struct {
	// Card is the card the request came for (CardFull, CardPushOnly, …).
	Card string `json:"card"`
	// Kind is "card" (the card was read), a JSON-RPC method (SendMessage,
	// SendStreamingMessage, GetTask, CancelTask, …), "cancel" (the task was
	// cancelled), "push" (a push the fake sent) or "completed" (a SkillSlow
	// task completed, its events handed on).
	Kind string `json:"kind"`
	// Auth is the credential that came: the Authorization header, or the
	// API key for CardAPIKey.
	Auth string `json:"auth,omitempty"`
	// Refused: the credential was missing or wrong, the fake answered 401.
	Refused bool `json:"refused,omitempty"`
	// Text is a message's text parts, joined.
	Text string `json:"text,omitempty"`
	// Skill is the skill a message names.
	Skill     string `json:"skill,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	ContextID string `json:"context_id,omitempty"`
	MessageID string `json:"message_id,omitempty"`
	// PushURL and PushToken are the push configuration of a message, or
	// where a push went and with which token.
	PushURL   string `json:"push_url,omitempty"`
	PushToken string `json:"push_token,omitempty"`
	// State is the task's state a push carried.
	State string `json:"state,omitempty"`
}

// Fake is the fake A2A agent; an http.Handler.
type Fake struct {
	token string
	wait  time.Duration
	log   *slog.Logger
	mux   *http.ServeMux

	mu       sync.Mutex
	received []Received
	skillOf  map[a2a.TaskID]string
	gate     chan struct{}
	// waiters wait for a record (Await).
	waiters []waiter
}

type waiter struct {
	match func(Received) bool
	done  chan struct{}
}

// New makes the fake.
func New(o Options) *Fake {
	f := &Fake{token: o.Token, wait: o.Wait, log: o.Logger, mux: http.NewServeMux(),
		skillOf: map[a2a.TaskID]string{}, gate: make(chan struct{})}
	if f.token == "" {
		f.token = DefaultToken
	}
	if f.log == nil {
		f.log = slog.Default()
	}
	handler := a2asrv.NewHandler(f, a2asrv.WithPushNotifications(push.NewInMemoryStore(), &recordingSender{f: f,
		// The fake runs beside Mira in the home network: its pushes go to
		// private addresses, which the SDK's sender refuses by default.
		next: push.NewHTTPPushSender(&push.HTTPSenderConfig{Timeout: 10 * time.Second, AllowPrivateNetworks: true})}))
	rpc := a2asrv.NewJSONRPCHandler(handler)
	for _, card := range Cards {
		prefix := ""
		if card != "" {
			prefix = "/" + card
		}
		f.mux.HandleFunc("GET "+prefix+a2asrv.WellKnownAgentCardPath, func(w http.ResponseWriter, r *http.Request) {
			f.record(Received{Card: card, Kind: "card"})
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(f.Card(card, base(r)+prefix)); err != nil {
				f.log.Error("a2a fake: could not write the card", "card", card, "error", err)
			}
		})
		f.mux.Handle("POST "+prefix+"/a2a", f.guard(card, rpc))
	}
	f.mux.HandleFunc("GET /fake/received", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(f.Received()); err != nil {
			f.log.Error("a2a fake: could not write what it received", "error", err)
		}
	})
	f.mux.HandleFunc("POST /fake/release", func(w http.ResponseWriter, _ *http.Request) { f.Release() })
	f.mux.HandleFunc("POST /fake/reset", func(w http.ResponseWriter, _ *http.Request) { f.Reset() })
	return f
}

func (f *Fake) ServeHTTP(w http.ResponseWriter, r *http.Request) { f.mux.ServeHTTP(w, r) }

// base is the fake's own address as the request reached it.
func base(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// Card is the agent card served at baseURL for a kind of card.
func (f *Fake) Card(card, baseURL string) *a2a.AgentCard {
	c := &a2a.AgentCard{
		Name:                Name,
		Description:         Description,
		Version:             "1.0.0",
		SupportedInterfaces: []*a2a.AgentInterface{a2a.NewAgentInterface(baseURL+"/a2a", a2a.TransportProtocolJSONRPC)},
		DefaultInputModes:   []string{"text/plain"},
		DefaultOutputModes:  []string{"text/plain"},
		Capabilities: a2a.AgentCapabilities{
			PushNotifications: card != CardStreamOnly && card != CardNeither,
			Streaming:         card != CardPushOnly && card != CardNeither,
		},
	}
	for _, s := range Skills {
		c.Skills = append(c.Skills, a2a.AgentSkill{ID: s, Name: s, Description: "Verhalten " + s + ".", Tags: []string{"test"},
			InputModes: []string{"text/plain"}, OutputModes: []string{"text/plain"}})
	}
	const scheme = "login"
	switch card {
	case CardOpen:
		return c
	case CardOAuth2:
		c.SecuritySchemes = a2a.NamedSecuritySchemes{scheme: a2a.OAuth2SecurityScheme{
			Flows: a2a.AuthorizationCodeOAuthFlow{AuthorizationURL: baseURL + "/authorize", TokenURL: baseURL + "/token",
				Scopes: map[string]string{"agent": "Den Agenten nutzen"}},
		}}
	case CardAPIKey:
		c.SecuritySchemes = a2a.NamedSecuritySchemes{scheme: a2a.APIKeySecurityScheme{
			Location: a2a.APIKeySecuritySchemeLocationHeader, Name: APIKeyHeader}}
	default:
		c.SecuritySchemes = a2a.NamedSecuritySchemes{scheme: a2a.HTTPAuthSecurityScheme{Scheme: "Bearer"}}
	}
	c.SecurityRequirements = a2a.SecurityRequirementsOptions{{scheme: {}}}
	return c
}

// guard records a call of the JSON-RPC endpoint and checks its credential
// as its card asks.
func (f *Fake) guard(card string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "the body could not be read", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		rec := callOf(body)
		rec.Card = card
		want := "Bearer " + f.token
		rec.Auth = r.Header.Get("Authorization")
		switch card {
		case CardOpen:
			want = rec.Auth
		case CardAPIKey:
			rec.Auth, want = r.Header.Get(APIKeyHeader), f.token
		}
		if rec.Auth != want {
			rec.Refused = true
			f.record(rec)
			http.Error(w, "the credential is missing or wrong", http.StatusUnauthorized)
			return
		}
		f.record(rec)
		next.ServeHTTP(w, r)
	})
}

// callOf reads what a JSON-RPC call carries: its method, and for a message
// its text, skill, ids and push configuration.
func callOf(body []byte) Received {
	var call struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(body, &call); err != nil {
		return Received{Kind: "unreadable", Text: cut(string(body))}
	}
	rec := Received{Kind: call.Method}
	switch call.Method {
	case "SendMessage", "SendStreamingMessage":
		var req a2a.SendMessageRequest
		if err := json.Unmarshal(call.Params, &req); err != nil || req.Message == nil {
			rec.Text = cut(string(call.Params))
			return rec
		}
		rec.Text = textOf(req.Message.Parts)
		rec.Skill, _ = req.Message.Metadata[SkillMeta].(string)
		rec.TaskID, rec.ContextID, rec.MessageID = string(req.Message.TaskID), req.Message.ContextID, req.Message.ID
		if req.Config != nil && req.Config.PushConfig != nil {
			rec.PushURL, rec.PushToken = req.Config.PushConfig.URL, req.Config.PushConfig.Token
		}
	default:
		var ids struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(call.Params, &ids) == nil {
			rec.TaskID = ids.ID
		}
	}
	return rec
}

func cut(s string) string {
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

func textOf(parts a2a.ContentParts) string {
	var out []string
	for _, p := range parts {
		if t := p.Text(); t != "" {
			out = append(out, t)
		}
	}
	return strings.Join(out, "\n")
}

func (f *Fake) record(r Received) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.received = append(f.received, r)
	kept := f.waiters[:0]
	for _, w := range f.waiters {
		if w.match(r) {
			close(w.done)
			continue
		}
		kept = append(kept, w)
	}
	f.waiters = kept
}

// Await is closed once the fake has recorded something match takes, at
// once when it has already: the signal a test waits for instead of a time.
func (f *Fake) Await(match func(Received) bool) <-chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	done := make(chan struct{})
	for _, r := range f.received {
		if match(r) {
			close(done)
			return done
		}
	}
	f.waiters = append(f.waiters, waiter{match: match, done: done})
	return done
}

// Received is everything the fake received and sent, in order.
func (f *Fake) Received() []Received {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.received)
}

// Reset forgets what was received.
func (f *Fake) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.received = nil
}

// Release ends the work of every SkillSlow task that waits now.
func (f *Fake) Release() {
	f.mu.Lock()
	defer f.mu.Unlock()
	close(f.gate)
	f.gate = make(chan struct{})
}

func (f *Fake) skillFor(id a2a.TaskID, msg *a2a.Message) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.skillOf[id]; ok {
		return s
	}
	s := ""
	if msg != nil {
		s, _ = msg.Metadata[SkillMeta].(string)
	}
	f.skillOf[id] = s
	return s
}

// Execute runs a message by the skill it names (a2asrv.AgentExecutor).
func (f *Fake) Execute(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		skill := f.skillFor(ec.TaskID, ec.Message)
		say := func(text string) *a2a.Message {
			return a2a.NewMessageForTask(a2a.MessageRoleAgent, ec, a2a.NewTextPart(text))
		}
		complete := func(text string) {
			if !yield(a2a.NewArtifactEvent(ec, a2a.NewTextPart(text)), nil) {
				return
			}
			yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateCompleted, nil), nil)
		}
		if skill == SkillMessage {
			yield(a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(Result)), nil)
			return
		}
		if ec.StoredTask != nil {
			// The answer to a question: only SkillAsk and SkillAskTwice ask.
			asked := ec.StoredTask.Status.State == a2a.TaskStateInputRequired
			if skill == SkillAskTwice && asked && ec.StoredTask.Status.Message != nil &&
				textOf(ec.StoredTask.Status.Message.Parts) == Question {
				yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateInputRequired, say(Question2)), nil)
				return
			}
			if (skill == SkillAsk || skill == SkillAskTwice) && asked {
				if !yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateWorking, nil), nil) {
					return
				}
				complete(AnswerResult + textOf(ec.Message.Parts) + ".")
				return
			}
			yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateRejected, say(RejectText)), nil)
			return
		}
		if !yield(a2a.NewSubmittedTask(ec, ec.Message), nil) {
			return
		}
		switch skill {
		case SkillComplete, SkillPushTwice:
			complete(Result)
		case SkillSlow:
			if !yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateWorking, nil), nil) {
				return
			}
			f.mu.Lock()
			gate := f.gate
			f.mu.Unlock()
			var timeout <-chan time.Time
			if f.wait > 0 {
				timeout = time.After(f.wait)
			}
			select {
			case <-ctx.Done():
				return
			case <-gate:
			case <-timeout:
			}
			complete(Result)
			f.record(Received{Kind: "completed", TaskID: string(ec.TaskID)})
		case SkillAsk, SkillAskTwice:
			yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateInputRequired, say(Question)), nil)
		case SkillLogin:
			yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateAuthRequired, say(LoginText)), nil)
		case SkillFail:
			yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateFailed, say(FailText)), nil)
		case SkillReject:
			yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateRejected, say(RejectText)), nil)
		default:
			yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateFailed, say(NoSkillText)), nil)
		}
	}
}

// Cancel cancels a task and records it (a2asrv.AgentExecutor).
func (f *Fake) Cancel(_ context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		f.record(Received{Kind: "cancel", TaskID: string(ec.TaskID), ContextID: ec.ContextID})
		yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateCanceled, nil), nil)
	}
}

// recordingSender sends the SDK's pushes, records each, and sends each twice
// for SkillPushTwice.
type recordingSender struct {
	f    *Fake
	next push.Sender
}

func (s *recordingSender) SendPush(ctx context.Context, config *a2a.PushConfig, event a2a.Event) error {
	times := 1
	state := ""
	if info, ok := event.(a2a.TaskInfoProvider); ok {
		s.f.mu.Lock()
		if s.f.skillOf[info.TaskInfo().TaskID] == SkillPushTwice {
			times = 2
		}
		s.f.mu.Unlock()
	}
	switch e := event.(type) {
	case *a2a.Task:
		state = string(e.Status.State)
	case *a2a.TaskStatusUpdateEvent:
		state = string(e.Status.State)
	}
	for range times {
		s.f.record(Received{Kind: "push", TaskID: string(config.TaskID), PushURL: config.URL, PushToken: config.Token, State: state})
		if err := s.next.SendPush(ctx, config, event); err != nil {
			s.f.log.Warn("a2a fake: a push failed", "url", config.URL, "error", err)
			return err
		}
	}
	return nil
}
