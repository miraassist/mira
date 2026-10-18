// Package conversation is the agent loop: it turns one user message into one
// answer, calling tools in between, for every channel alike.
//
// A turn builds the prompt from the seven layers, asks the model, runs the
// tool calls it makes through the security guard, and repeats until the model
// answers in plain text or the round limit is hit. The transcript is
// append-only and stores the rendered layer 7 exactly as it was sent, so the
// next turn's prompt starts with the bytes the server already has cached.
package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/ha/structure"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/prompt"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/telemetry"
	"github.com/miraassist/mira/internal/tools"
	mtrace "github.com/miraassist/mira/internal/trace"
)

// Chatter is the part of the LLM client the engine uses.
type Chatter interface {
	Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error)
	Warmup(ctx context.Context, req llm.ChatRequest) error
}

// Request is one user message from any channel.
type Request struct {
	Channel config.Channel
	// ConversationID identifies the session within the channel: HA's
	// conversation_id for voice, the chat id for Telegram. Empty starts a
	// new voice session with a generated id.
	ConversationID string
	// DeviceID is the satellite a voice request came from.
	DeviceID string
	Language string
	Text     string
	// Extra is passed-through context such as HA's extra_system_prompt. It
	// only ever lands in layer 7.
	Extra string
	// HAUser is the Home Assistant user behind a request from the HA app or
	// its chat; satellites send none.
	HAUser string
	// Person is who writes, named by the Home Assistant integration for a
	// messenger (Telegram through HA): it matched the chat to a person.
	Person string
}

// Response is the answer to a Request.
type Response struct {
	Text           string
	ConversationID string
	// ContinueConversation asks a voice satellite to keep listening because
	// the answer was a question.
	ContinueConversation bool
	PrefixHash           string
	ToolRounds           int
	PromptTokens         int
	CachedTokens         int
	// Model is the name of the model that answered. MorePromptTokens and
	// CompletionTokens complete the usage of the turn, which is what a cloud
	// model bills.
	Model            string
	MorePromptTokens int
	CompletionTokens int
	// Trace is what happened in the turn, in eval mode only.
	Trace []mtrace.Event
}

// Languages is package language as the engine needs it.
type Languages interface {
	// Turn is the language of a turn, remembered when Home Assistant sent
	// one.
	Turn(ctx context.Context, sent, area, person string) i18n.Lang
	// Last is the language last spoken, for what has no turn.
	Last(ctx context.Context) i18n.Lang
}

type Options struct {
	Config config.Config
	// LLM is the main model. Models holds the further ones by name, as in
	// config.Models; config.Routing says which serves what.
	LLM       Chatter
	Models    map[string]Chatter
	Store     *store.Store
	Tools     *tools.Registry
	Guard     *tools.Guard
	Structure func() structure.Snapshot
	// StructurePrompt, when it returns text, replaces the structure block in
	// layer 4: Home Assistant's own Assist prompt.
	StructurePrompt func() string
	// Afterword returns the messages waiting for an area as a closing
	// sentence, marking them heard, or "" when none wait.
	Afterword func(ctx context.Context, area string) string
	// Thread links the turn to its thread ("Vorgang", package threads) and
	// renders it for layer 7. followedUp are the messages this turn answers.
	Thread func(ctx context.Context, sess store.Session, followedUp []string) string
	// Identify says who is speaking: a person key, or "" for anonymous.
	Identify func(ctx context.Context, req Request, sess store.Session) string
	// Languages says which language applies (docs/spec/sprache.md); tests
	// without it get the default language.
	Languages Languages
	// Persona returns the persona as it is now, which the web UI can
	// change; without it the config's applies.
	Persona   func() string
	Telemetry *telemetry.Provider
	Logger    *slog.Logger
	// Clock and Location set the time the model is told. Tests pin both.
	Clock    func() time.Time
	Location *time.Location
}

type Engine struct {
	cfg       config.Config
	models    map[string]Chatter
	store     *store.Store
	tools     *tools.Registry
	guard     *tools.Guard
	structure func() structure.Snapshot
	assist    func() string
	afterword func(ctx context.Context, area string) string
	thread    func(ctx context.Context, sess store.Session, followedUp []string) string
	identify  func(ctx context.Context, req Request, sess store.Session) string
	persona   func() string
	languages Languages
	tel       *telemetry.Provider
	log       *slog.Logger
	clock     func() time.Time
	loc       *time.Location

	// One turn per channel at a time: each channel owns one llama.cpp slot,
	// and interleaving two sessions on a slot would evict each other's cache
	// mid-turn.
	locks map[config.Channel]*sync.Mutex

	// lastPrefix is the hash of each channel's last request start, to log
	// when it changes (ctxlog.go).
	ctxMu      sync.Mutex
	lastPrefix map[config.Channel]string

	warm chan string
}

func New(opts Options) *Engine {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Telemetry == nil {
		opts.Telemetry = telemetry.NewTestProvider()
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	if opts.Location == nil {
		opts.Location = time.Local
	}
	if opts.Structure == nil {
		opts.Structure = func() structure.Snapshot { return structure.Snapshot{} }
	}
	e := &Engine{
		cfg:        opts.Config,
		models:     map[string]Chatter{config.MainModel: opts.LLM},
		store:      opts.Store,
		tools:      opts.Tools,
		guard:      opts.Guard,
		structure:  opts.Structure,
		assist:     opts.StructurePrompt,
		afterword:  opts.Afterword,
		thread:     opts.Thread,
		identify:   opts.Identify,
		persona:    opts.Persona,
		languages:  opts.Languages,
		tel:        opts.Telemetry,
		log:        opts.Logger.With("component", "conversation"),
		clock:      opts.Clock,
		loc:        opts.Location,
		locks:      map[config.Channel]*sync.Mutex{},
		lastPrefix: map[config.Channel]string{},
		warm:       make(chan string, 1),
	}
	for name, m := range opts.Models {
		e.models[name] = m
	}
	for _, ch := range []config.Channel{config.ChannelVoice, config.ChannelTelegram, config.ChannelBackground} {
		e.locks[ch] = &sync.Mutex{}
	}
	return e
}

// SessionKey is how a (channel, conversation id) pair is stored.
func SessionKey(ch config.Channel, conversationID string) string {
	return string(ch) + ":" + conversationID
}

// Handle runs one turn.
func (e *Engine) Handle(ctx context.Context, req Request) (Response, error) {
	if strings.TrimSpace(req.Text) == "" {
		return Response{}, errors.New("empty message")
	}
	lock, ok := e.locks[req.Channel]
	if !ok {
		return Response{}, fmt.Errorf("unknown channel %q", req.Channel)
	}
	lock.Lock()
	defer lock.Unlock()

	if req.ConversationID == "" {
		req.ConversationID = ulid.Make().String()
	}
	key := SessionKey(req.Channel, req.ConversationID)

	ctx, span := e.tel.Tracer.Start(ctx, "invoke_agent mira", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	span.SetAttributes(
		telemetry.AttrGenAIOperationName.String("invoke_agent"),
		telemetry.AttrChannel.String(string(req.Channel)),
		telemetry.AttrGenAIConversationID.String(key),
	)

	var rec *mtrace.Recorder
	if e.cfg.Server.Eval {
		ctx, rec = mtrace.With(ctx)
	}
	resp, err := e.turn(ctx, key, req)
	resp.Trace = rec.Events()
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		e.log.Error("turn failed", "session", key, "error", err)
		return Response{}, err
	}
	span.SetAttributes(
		telemetry.AttrPrefixHash.String(resp.PrefixHash),
		telemetry.AttrToolRounds.Int(resp.ToolRounds),
	)
	return resp, nil
}

// threadContext is the turn's thread for layer 7, if any.
func (e *Engine) threadContext(ctx context.Context, sess store.Session, followedUp []string) string {
	if e.thread == nil {
		return ""
	}
	return e.thread(ctx, sess, followedUp)
}

func (e *Engine) turn(ctx context.Context, key string, req Request) (Response, error) {
	turnBegan := time.Now()
	now := e.clock()
	snap := e.structure()
	if !snap.ExposureKnown {
		// What Assist may see comes from the integration (docs/spec/ha-anbindung.md).
		// Until it has sent that, no tool runs: Home Assistant's MCP server would
		// act on what is exposed there, and the guard cannot know those names.
		e.log.Warn("turn refused: the list of what Assist may see has not arrived", "session", key)
		lang := e.lang(ctx)
		if e.languages != nil {
			lang = e.languages.Turn(ctx, req.Language, "", "")
		}
		return Response{ConversationID: req.ConversationID, Text: i18n.Pick(catalog, lang).waitingForHA}, nil
	}

	sess, err := e.openSession(ctx, key, req, snap)
	if err != nil {
		return Response{}, err
	}
	e.compactIfLong(ctx, key, sess)
	history, err := e.history(ctx, key)
	if err != nil {
		return Response{}, err
	}

	person := ""
	if e.identify != nil {
		person = e.identify(ctx, req, sess)
	}
	if e.languages != nil {
		ctx = i18n.WithLang(ctx, e.languages.Turn(ctx, req.Language, sess.AreaID, person))
	}
	ts := &tools.TurnState{SessionKey: key, Channel: req.Channel, UserText: req.Text, Person: person, Area: sess.AreaID}
	messageText, followedUp := e.messageContext(ctx, req.DeviceID)
	turn := prompt.Turn{
		Lang:           e.lang(ctx),
		Now:            now.In(e.loc),
		ChannelContext: e.channelContext(ctx, req, sess, snap),
		Person:         person,
		PersonProfile:  e.personProfile(ctx, person),
		Memories:       e.memories(ctx, req.Text, person),
		MessageContext: messageText,
		Thread:         e.threadContext(ctx, sess, followedUp),
		Extra:          req.Extra,
		Text:           req.Text,
	}
	if c, err := e.store.TakeConfirmation(ctx, key); err == nil {
		ts.Pending = &c
		turn.PendingConfirmation = c.Summary
	} else if !errors.Is(err, store.ErrNotFound) {
		return Response{}, err
	}

	prefix, err := e.Prefix(ctx, req.Channel)
	if err != nil {
		return Response{}, err
	}
	client, model, modelName := e.modelFor(e.cfg.RouteFor(req.Channel))
	built := prompt.Build(prefix, history, turn)
	msgs := built.Messages
	added := []llm.Message{msgs[len(msgs)-1]} // the rendered layer 7
	e.logContext(ctx, key, req.Channel, prefix, built, history, turn, added[0].Content)
	toolCount, toolChars := toolsSize(built.Tools)

	resp := Response{ConversationID: req.ConversationID, PrefixHash: built.PrefixHash}
	resp.Model = modelName
	maxRounds := model.MaxToolRounds
	var final llm.Message
	nudged, forced, force := false, false, false
	for round := 0; ; round++ {
		chatReq := e.chatRequest(req.Channel, msgs, built)
		switch {
		case round >= maxRounds:
			// Out of rounds: the model has to answer with what it has.
			chatReq.ToolChoice = "none"
		case force:
			chatReq.ToolChoice = "required"
			force = false
		}
		began := time.Now()
		out, err := client.Chat(ctx, chatReq)
		if err != nil {
			e.log.Error("model round failed", "session", key, "round", round, "model", modelName,
				"ms", time.Since(began).Milliseconds(), "error", err)
			return Response{}, err
		}
		e.logRound(key, round, modelName, time.Since(began), out, len(chatReq.Messages), messagesSize(chatReq.Messages), toolCount, toolChars)
		mtrace.AddRound(ctx, modelName, time.Since(began), out.Usage.PromptTokens+out.Usage.CompletionTokens)
		resp.CompletionTokens += out.Usage.CompletionTokens
		if round == 0 {
			resp.PromptTokens, resp.CachedTokens = out.Usage.PromptTokens, out.CachedTokens()
			e.compactForContext(ctx, key, out.Usage.PromptTokens, model)
		} else {
			resp.MorePromptTokens += out.Usage.PromptTokens
		}
		msg := out.Message()
		msg.Role = llm.RoleAssistant
		msg.Content = stripThinking(msg.Content)
		if len(msg.ToolCalls) == 0 && resp.ToolRounds == 0 && round < maxRounds && unbacked(msg.Content) {
			if !nudged {
				// Announced or claimed an action, or reported a state,
				// without any tool: give the model one more chance. Neither
				// message reaches the history.
				nudged = true
				e.log.Warn("answer without tool call, asking again", "session", key)
				msgs = append(msgs, msg, llm.Message{Role: llm.RoleUser, Content: e.texts(ctx).unbackedNudge})
				continue
			}
			if !forced {
				// Still made up: this time it must call a tool. The local
				// model sometimes answers from imagination twice in a row.
				forced, force = true, true
				e.log.Warn("answer still without tool call, requiring one", "session", key)
				continue
			}
			if reportsState(msg.Content) {
				e.log.Warn("answer still reports states without tool call", "session", key)
				msg.Content = e.texts(ctx).notLookedUp
			} else if claimsAction(msg.Content) {
				e.log.Warn("answer still claims an action without tool call", "session", key)
				msg.Content = e.texts(ctx).notDone
			}
		}
		if len(msg.ToolCalls) == 0 || round >= maxRounds {
			msg.ToolCalls = nil
			final = msg
			break
		}

		resp.ToolRounds++
		ensureCallIDs(msg.ToolCalls, round)
		msgs = append(msgs, msg)
		added = append(added, msg)
		for _, call := range msg.ToolCalls {
			result := e.runTool(ctx, ts, call)
			tm := llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Name: call.Function.Name, Content: result}
			msgs = append(msgs, tm)
			added = append(added, tm)
		}
	}

	if strings.TrimSpace(final.Content) == "" {
		final.Content = e.texts(ctx).fallback
	}
	// Messages waiting for this room follow the answer ("Licht ist an.
	// Übrigens: ..."): whoever talks to the satellite is listening anyway.
	// Not after a question, which the next turn completes, and not when the
	// user just asked for the messages.
	if req.Channel == config.ChannelVoice && sess.AreaID != "" && e.afterword != nil &&
		!expectsReply(final.Content) && !ts.ConfirmationRequested() && !calledTool(added, tools.ToolMessagesRead) {
		if extra := e.afterword(ctx, sess.AreaID); extra != "" {
			final.Content = strings.TrimSpace(final.Content) + " " + extra
		}
	}
	added = append(added, final)
	if err := e.store.AppendChatMessages(ctx, key, toStore(added)...); err != nil {
		return Response{}, fmt.Errorf("store transcript: %w", err)
	}

	// Whoever answered right after an announcement on this satellite has
	// heard it; that is the acknowledgement the plan asks for.
	for _, id := range followedUp {
		if err := e.store.SetMessageState(ctx, id, store.MessageAcked); err != nil {
			e.log.Warn("could not acknowledge message", "message", id, "error", err)
		}
	}

	resp.Text = strings.TrimSpace(final.Content)
	// A satellite keeps listening after a question, and always after a
	// confirmation request: the user's "yes" must reach this session.
	resp.ContinueConversation = req.Channel == config.ChannelVoice &&
		(expectsReply(resp.Text) || ts.ConfirmationRequested())
	e.log.Info("turn done",
		"session", key, "model", resp.Model, "rounds", resp.ToolRounds, "prefix_hash", resp.PrefixHash,
		"prompt_tokens", resp.PromptTokens, "cached_tokens", resp.CachedTokens,
		"more_prompt_tokens", resp.MorePromptTokens, "completion_tokens", resp.CompletionTokens,
		"ms", time.Since(turnBegan).Milliseconds())
	return resp, nil
}

// logRound is one call to the model: how long it took and what it cost. With
// the tool times it shows where the time of a turn went.
func (e *Engine) logRound(key string, round int, model string, took time.Duration, out *llm.ChatResponse, messages, messageChars, tools, toolChars int) {
	e.log.Info("model round", "session", key, "round", round, "model", model, "ms", took.Milliseconds(),
		"messages", messages, "message_chars", messageChars, "tools", tools, "tool_chars", toolChars,
		"prompt_tokens", out.Usage.PromptTokens, "cached_tokens", out.CachedTokens(),
		"completion_tokens", out.Usage.CompletionTokens)
}

func (e *Engine) chatRequest(ch config.Channel, msgs []llm.Message, built prompt.Request) llm.ChatRequest {
	_, model, _ := e.modelFor(e.cfg.RouteFor(ch))
	r := modelRequest(model, msgs)
	r.Tools = built.Tools
	r.Channel = string(ch)
	r.PrefixHash = built.PrefixHash
	if slot := e.cfg.SlotFor(ch); slot >= 0 {
		r.IDSlot = llm.IntPtr(slot)
	}
	return r
}

// modelRequest is a request with a model's own settings: llama.cpp's
// cache_prompt only where it is switched on, reasoning_effort only where set.
func modelRequest(model config.LLM, msgs []llm.Message) llm.ChatRequest {
	r := llm.ChatRequest{
		Messages:        msgs,
		Temperature:     model.Temperature,
		MaxTokens:       model.MaxTokens,
		ReasoningEffort: model.ReasoningEffort,
	}
	if model.CachePrompt {
		r.CachePrompt = llm.BoolPtr(true)
	}
	return r
}

// modelFor returns the client and settings of a model by name. A name the
// config validation let through always exists; main is the fallback.
func (e *Engine) modelFor(name string) (Chatter, config.LLM, string) {
	if name == "" {
		name = config.MainModel
	}
	m, ok := e.cfg.Model(name)
	c := e.models[name]
	if !ok || c == nil {
		name, m, c = config.MainModel, e.cfg.LLM, e.models[config.MainModel]
	}
	return c, m, name
}

// runTool executes one call through the guard and returns what the model
// reads back. Failures are reported to the model as text, never swallowed:
// a model that does not see the error will claim success.
func (e *Engine) runTool(ctx context.Context, ts *tools.TurnState, call llm.ToolCall) string {
	name := call.Function.Name
	ctx, span := e.tel.Tracer.Start(ctx, "execute_tool "+name, trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	span.SetAttributes(
		telemetry.AttrGenAIOperationName.String("execute_tool"),
		telemetry.AttrGenAIToolName.String(name),
	)
	server, _ := prompt.SplitQualifiedName(name)
	span.SetAttributes(telemetry.AttrToolServer.String(server))
	record := func(outcome string) { e.tel.RecordToolCall(ctx, server, name, outcome) }

	spec, ok := e.tools.Lookup(name)
	if !ok {
		record("unknown")
		return e.texts(ctx).errorPrefix + fmt.Sprintf(e.texts(ctx).unknownTool, name)
	}
	args := json.RawMessage(strings.TrimSpace(call.Function.Arguments))
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	if !json.Valid(args) {
		record("bad_arguments")
		return e.texts(ctx).errorPrefix + e.texts(ctx).invalidJSONArgs
	}

	mtrace.Add(ctx, mtrace.Tool, name, string(args))
	verdict := e.guard.Check(ctx, ts, spec, args)
	if verdict.Outcome != tools.Allow {
		mtrace.Add(ctx, mtrace.Blocked, name, verdict.Reason)
		span.SetAttributes(telemetry.AttrToolBlocked.String(verdict.Reason))
		record(verdict.Outcome.String())
		e.log.Warn("tool call blocked",
			"session", ts.SessionKey, "tool", name, "outcome", verdict.Outcome.String(),
			"reason", verdict.Reason, "targets", verdict.Targets)
		return verdict.Message
	}

	began := time.Now()
	text, isError, err := e.tools.Execute(ctx, tools.Invocation{
		Name: name, Args: args, SessionKey: ts.SessionKey, Channel: ts.Channel, Person: ts.Person, Area: ts.Area,
	})
	e.guard.Observe(ts, spec)
	e.log.Info("tool done", "session", ts.SessionKey, "tool", name, "ms", time.Since(began).Milliseconds(),
		"error", err != nil || isError)
	switch {
	case err != nil:
		record("error")
		span.SetStatus(codes.Error, err.Error())
		return e.texts(ctx).errorPrefix + err.Error()
	case isError:
		record("tool_error")
		if prefix := e.texts(ctx).errorPrefix; !strings.HasPrefix(text, strings.TrimSpace(prefix)) {
			text = prefix + text
		}
		return text
	}
	record("ok")
	if verdict.Reason == "confirmed" {
		e.log.Info("confirmed critical tool call executed", "session", ts.SessionKey, "tool", name, "targets", verdict.Targets)
	}
	if strings.TrimSpace(text) == "" {
		return "OK"
	}
	return text
}

func (e *Engine) openSession(ctx context.Context, key string, req Request, snap structure.Snapshot) (store.Session, error) {
	existing, err := e.store.GetSession(ctx, key)
	switch {
	case err == nil && existing.ExpiresAt.Before(e.clock()):
		// The session ran out. Starting over is the documented behaviour,
		// not a cache accident: the history is gone on purpose.
		if err := e.store.DeleteSession(ctx, key); err != nil {
			return store.Session{}, err
		}
		e.log.Info("session expired, starting fresh", "session", key)
	case err != nil && !errors.Is(err, store.ErrNotFound):
		return store.Session{}, err
	}
	areaID := ""
	if req.DeviceID != "" {
		areaID = snap.DeviceArea[req.DeviceID]
	}
	return e.store.UpsertSession(ctx, store.Session{
		Key: key, Channel: string(req.Channel), ConversationID: req.ConversationID,
		DeviceID: req.DeviceID, AreaID: areaID, Language: req.Language,
	}, e.cfg.SessionTTL(req.Channel).Duration())
}

// calledTool reports whether a tool was called in these messages.
func calledTool(msgs []llm.Message, name string) bool {
	for _, m := range msgs {
		for _, c := range m.ToolCalls {
			if c.Function.Name == name {
				return true
			}
		}
	}
	return false
}

func ensureCallIDs(calls []llm.ToolCall, round int) {
	for i := range calls {
		if calls[i].ID == "" {
			calls[i].ID = fmt.Sprintf("call_%d_%d", round, i)
		}
		if calls[i].Type == "" {
			calls[i].Type = "function"
		}
	}
}

var thinkBlock = regexp.MustCompile(`(?s)<think>.*?</think>`)

// stripThinking removes reasoning blocks some models emit inline.
func stripThinking(s string) string {
	return strings.TrimSpace(thinkBlock.ReplaceAllString(s, ""))
}

// ResetSession drops a session so the next message starts a new one.
func (e *Engine) ResetSession(ctx context.Context, ch config.Channel, conversationID string) error {
	lock, ok := e.locks[ch]
	if !ok {
		return fmt.Errorf("unknown channel %q", ch)
	}
	lock.Lock()
	defer lock.Unlock()
	return e.store.DeleteSession(ctx, SessionKey(ch, conversationID))
}
