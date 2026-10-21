package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/prompt"
	"github.com/miraassist/mira/internal/tools"
)

// Chatter is a model client.
type Chatter interface {
	Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error)
}

// BackgroundContext builds what the model sees in the background profile:
// the background prefix (rules, house, tools) and, as the turn, the
// instructions and the input.
type BackgroundContext func(ctx context.Context, cc CallContext, instructions, input string) (Built, error)

// Built is a profile's context as its builder gives it to the runtime.
type Built struct {
	Messages []llm.Message
	// Tools are all the tools of the prefix. An agent without tools gets
	// them too, with tool_choice none: they are part of the prompt the
	// model has cached.
	Tools []llm.Tool
	// PrefixHash and IDSlot go into the request as in a conversation turn.
	PrefixHash string
	IDSlot     *int
}

type Options struct {
	// Config is read at each call (routing, models, agents.max_depth).
	Config func() config.Config
	// Models resolves a model name to its client and settings; ok is false
	// for a name it does not know.
	Models func(name string) (Chatter, config.LLM, bool)
	// Tools and Guard run the tools an agent calls.
	Tools *tools.Registry
	Guard *tools.Guard
	// Background builds the background profile; without it, agents of that
	// profile cannot run.
	Background BackgroundContext
	// Runs stores the asynchronous runs; without it, none can be started.
	Runs RunStore
	// Deliver hands a finished asynchronous run to its route.
	Deliver Deliverer
	// Clock is the time for run records; tests set it.
	Clock  func() time.Time
	Logger *slog.Logger
}

// Runtime runs agents (docs/spec/agenten.md, Runtime): synchronous ones in
// the caller's goroutine, asynchronous ones in goroutines of their own
// (async.go). It limits the access to each model.
type Runtime struct {
	o   Options
	log *slog.Logger

	mu      sync.Mutex
	slots   map[string]chan struct{}
	agents  map[string]Spec
	cancels map[string]context.CancelCauseFunc

	// async holds a place per asynchronous run; base ends them all on
	// shutdown.
	async    chan struct{}
	base     context.Context
	stop     context.CancelFunc
	stopping atomic.Bool
	wg       sync.WaitGroup
}

// Requests at the same time per model (docs/spec/agenten.md, Decided 26).
const (
	localSlots = 1
	cloudSlots = 4
)

func New(o Options) *Runtime {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	base, stop := context.WithCancel(context.Background())
	return &Runtime{o: o, log: o.Logger.With("component", "agent"), slots: map[string]chan struct{}{},
		agents: map[string]Spec{}, cancels: map[string]context.CancelCauseFunc{},
		async: make(chan struct{}, maxAsync), base: base, stop: stop}
}

// Register makes an agent known by its name: an agent that names it in its
// tool list gets it as a tool.
func (r *Runtime) Register(spec Spec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents[spec.Name] = spec
}

// Caller is the agent whose run ctx belongs to, and its profile; ok is false
// outside a run.
func Caller(ctx context.Context) (name string, profile Profile, ok bool) {
	p := current(ctx)
	if p == nil {
		return "", 0, false
	}
	return p.agent, p.profile, true
}

// Current is the call context of the run ctx belongs to; ok is false
// outside a run.
func Current(ctx context.Context) (cc CallContext, ok bool) {
	p := current(ctx)
	if p == nil {
		return CallContext{}, false
	}
	return p.cc, true
}

// Enter starts a run its caller drives itself: a conversation turn, which
// stays in package conversation (docs/spec/agenten.md, Context profiles). The
// calls made inside it inherit from it as from any run; ts is the turn's
// guard state. done logs the run as every run is logged; the caller fills
// in the result.
func (r *Runtime) Enter(ctx context.Context, spec Spec, cc CallContext, ts *tools.TurnState) (context.Context, func(Result, error)) {
	began := time.Now()
	if parent := current(ctx); parent != nil {
		cc = inherit(parent, cc)
	}
	if cc.CallID == "" {
		cc.CallID = strings.ToLower(ulid.Make().String())
	}
	me := &run{agent: spec.Name, profile: spec.Profile, cc: cc, rights: cc.Rights.And(spec.Rights), ts: ts}
	return context.WithValue(ctx, runKey{}, me), func(res Result, err error) {
		res.CallID = cc.CallID
		if res.Status == "" {
			res.Status = Done
			if err != nil {
				res.Status = Failed
			}
		}
		r.logRun(spec, cc, res, time.Since(began), err)
	}
}

// Acquire waits for a free request slot of a model, for a caller that talks
// to the model itself (a conversation turn); release frees it.
func (r *Runtime) Acquire(ctx context.Context, name string, model config.LLM) (release func(), err error) {
	return r.acquire(ctx, name, isLocal(model.BaseURL))
}

// run is a running call, as the calls made inside it find it in ctx.
type run struct {
	agent   string
	profile Profile
	cc      CallContext
	rights  Rights
	ts      *tools.TurnState
	// subs are the agents offered as tools, by tool name.
	subs map[string]Spec
}

type runKey struct{}

func current(ctx context.Context) *run {
	r, _ := ctx.Value(runKey{}).(*run)
	return r
}

// Call runs an agent. A call made inside a run (an agent as a tool of
// another) takes its call context from that run: one deeper, the parent's
// call ID, at most the parent's effective rights. The mode of the call
// context says how the caller waits: synchronously (in the caller's
// goroutine), asynchronously (the call ID comes back at once, status
// Running), or synchronously with run-on (the answer if it comes in time,
// else the call ID and status Running; the result is delivered later).
func (r *Runtime) Call(ctx context.Context, spec Spec, input string, cc CallContext) (Result, error) {
	parent := current(ctx)
	if parent != nil {
		cc = inherit(parent, cc)
	}
	if cc.CallID == "" {
		cc.CallID = strings.ToLower(ulid.Make().String())
	}
	if cc.Mode.Async || (cc.Mode.OnTimeout == RunOn && cc.Mode.Timeout > 0) {
		return r.start(ctx, parent, spec, input, cc)
	}
	began := time.Now()
	finish := func(res Result, status Status, err error) (Result, error) {
		res.Status = status
		// Third-party text read by this run counts for its caller.
		if res.Foreign && parent != nil {
			parent.ts.ForeignContext = true
		}
		r.logRun(spec, cc, res, time.Since(began), err)
		return res, err
	}
	if cc.Mode.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cc.Mode.Timeout)
		defer cancel()
	}
	ex, ctx, err := r.prepare(ctx, spec, input, cc)
	if err != nil {
		return finish(ex.res, Failed, err)
	}
	status, err := r.loop(ctx, ex)
	if status == Cancelled && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		err = ErrTimeout
	}
	return finish(ex.res, status, err)
}

// execution is a run as the loop drives it.
type execution struct {
	spec    Spec
	me      *run
	res     Result
	client  Chatter
	model   config.LLM
	local   bool
	b       Built
	offered []llm.Tool
	msgs    []llm.Message
	// round is the next model call's round; maxRounds bounds the tool rounds.
	round, maxRounds int
	t                texts
	// saved stores what each round adds; nil for a synchronous run.
	saved *saver
}

// prepare sets a run up: model, guard state, what the model sees, tools.
func (r *Runtime) prepare(ctx context.Context, spec Spec, input string, cc CallContext) (*execution, context.Context, error) {
	cfg := r.o.Config()
	ex := &execution{spec: spec, res: Result{CallID: cc.CallID}, t: i18n.Pick(catalog, lang(cc))}
	ex.res.Model = spec.Model
	if ex.res.Model == "" {
		ex.res.Model = cfg.RoleModel(spec.Role)
	}
	if cc.Depth > cfg.Agents.MaxDepth {
		return ex, ctx, ErrDepth
	}
	client, model, ok := r.o.Models(ex.res.Model)
	if !ok {
		return ex, ctx, fmt.Errorf("no model %q", ex.res.Model)
	}
	ex.client, ex.model, ex.local = client, model, isLocal(model.BaseURL)
	ex.me = &run{agent: spec.Name, profile: spec.Profile, cc: cc, rights: cc.Rights.And(spec.Rights),
		ts: &tools.TurnState{SessionKey: sessionKey(cc), Channel: config.ChannelBackground, Person: cc.Person,
			Area: cc.Place, DeviceID: cc.DeviceID, ForeignContext: cc.Foreign, ByRights: true}}
	ctx = context.WithValue(ctx, runKey{}, ex.me)

	b, offered, err := r.build(ctx, spec, input, cc)
	if err != nil {
		return ex, ctx, err
	}
	if spec.Profile != Isolated {
		ex.me.subs = r.subAgents(spec.Tools)
		for _, name := range slices.Sorted(maps.Keys(ex.me.subs)) {
			offered = append(offered, ex.me.subs[name].asTool())
		}
	}
	ex.b, ex.offered, ex.msgs = b, offered, b.Messages
	ex.maxRounds = spec.Limits.MaxRounds
	if ex.maxRounds == 0 {
		ex.maxRounds = model.MaxToolRounds
	}
	if ex.maxRounds == 0 {
		ex.maxRounds = 3
	}
	return ex, ctx, nil
}

// loop runs the model rounds until the answer. It returns Done, Failed, or
// Cancelled when ctx ended; the caller tells why ctx ended.
func (r *Runtime) loop(ctx context.Context, ex *execution) (Status, error) {
	spec, model := ex.spec, ex.model
	for ; ; ex.round++ {
		// No new round once the run is to stop (cancelled, expired, shutdown).
		if ctx.Err() != nil {
			return Cancelled, ctx.Err()
		}
		req := llm.ChatRequest{Messages: ex.msgs, Temperature: model.Temperature, MaxTokens: model.MaxTokens,
			ReasoningEffort: model.ReasoningEffort, Channel: string(config.ChannelBackground),
			PrefixHash: ex.b.PrefixHash, IDSlot: ex.b.IDSlot}
		if spec.Profile == Background && model.CachePrompt {
			req.CachePrompt = llm.BoolPtr(true)
		}
		if len(ex.offered) > 0 {
			req.Tools = ex.offered
			if ex.round >= ex.maxRounds {
				req.ToolChoice = "none"
			}
		} else {
			if len(ex.b.Tools) > 0 {
				req.Tools, req.ToolChoice = ex.b.Tools, "none"
			}
			if len(spec.Output) > 0 {
				req.ResponseFormat = map[string]any{"type": "json_schema",
					"json_schema": map[string]any{"name": "result", "schema": spec.Output}}
			}
		}
		out, err := r.chat(ctx, ex.client, ex.res.Model, ex.local, req, spec.Limits, &ex.res)
		ex.res.Foreign = ex.me.ts.Foreign()
		if err != nil {
			if ctx.Err() != nil {
				return Cancelled, ctx.Err()
			}
			return Failed, err
		}
		msg := out.Message()
		msg.Role = llm.RoleAssistant
		msg.Content = stripThinking(msg.Content)
		if len(msg.ToolCalls) == 0 || ex.round >= ex.maxRounds {
			text := strings.TrimSpace(msg.Content)
			if text == "" {
				return Failed, ErrNoAnswer
			}
			if len(spec.Output) > 0 {
				text = extractJSON(text)
				if err := checkSchema(spec.Output, json.RawMessage(text)); err != nil {
					ex.res.Text = text
					return Failed, fmt.Errorf("%w: %v", ErrOutput, err)
				}
			}
			ex.res.Text = text
			return Done, nil
		}
		for i := range msg.ToolCalls {
			if msg.ToolCalls[i].ID == "" {
				msg.ToolCalls[i].ID = fmt.Sprintf("call_%d_%d", ex.round, i)
			}
			if msg.ToolCalls[i].Type == "" {
				msg.ToolCalls[i].Type = "function"
			}
		}
		ex.msgs = append(ex.msgs, msg)
		ex.saved.add(ctx, stepAnswer, msg)
		for _, call := range msg.ToolCalls {
			ex.runTool(ctx, r, call)
		}
	}
}

// runTool runs one tool call of the run and adds its result; an
// asynchronous run stores that it started and what came back.
func (ex *execution) runTool(ctx context.Context, r *Runtime, call llm.ToolCall) {
	ex.saved.add(ctx, stepStarted, llm.Message{ToolCallID: call.ID, Name: call.Function.Name})
	ex.addResult(ctx, call, r.runTool(ctx, ex.me, call, ex.t))
}

func (ex *execution) addResult(ctx context.Context, call llm.ToolCall, text string) {
	m := llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Name: call.Function.Name, Content: text}
	ex.msgs = append(ex.msgs, m)
	ex.saved.add(ctx, stepResult, m)
}

// inherit is the call context of a call made inside the run p.
func inherit(p *run, cc CallContext) CallContext {
	cc.ParentID = p.cc.CallID
	cc.Depth = p.cc.Depth + 1
	cc.Origin = FromAgent
	cc.Rights = p.rights
	cc.Foreign = cc.Foreign || p.ts.Foreign()
	if cc.Person == "" {
		cc.Person = p.cc.Person
	}
	if cc.Place == "" {
		cc.Place = p.cc.Place
	}
	if cc.Lang == "" {
		cc.Lang = p.cc.Lang
	}
	if cc.SessionKey == "" {
		cc.SessionKey = p.cc.SessionKey
	}
	if cc.DeviceID == "" {
		cc.DeviceID = p.cc.DeviceID
	}
	return cc
}

func sessionKey(cc CallContext) string {
	if cc.SessionKey != "" {
		return cc.SessionKey
	}
	return "agent:" + cc.CallID
}

func lang(cc CallContext) i18n.Lang {
	if cc.Lang == string(i18n.EN) {
		return i18n.EN
	}
	return i18n.DE
}

// build is what the model sees, after the profile, and the tools it is
// offered.
func (r *Runtime) build(ctx context.Context, spec Spec, input string, cc CallContext) (Built, []llm.Tool, error) {
	switch spec.Profile {
	case Isolated:
		return Built{Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: spec.Instructions},
			{Role: llm.RoleUser, Content: input},
		}}, nil, nil
	case Background:
		if r.o.Background == nil {
			return Built{}, nil, fmt.Errorf("%w: no background context", ErrProfile)
		}
		b, err := r.o.Background(ctx, cc, spec.Instructions, input)
		if err != nil {
			return Built{}, nil, err
		}
		return b, r.offer(spec.Tools, b.Tools), nil
	}
	return Built{}, nil, ErrProfile
}

// offer narrows the tools to what the agent's description names.
func (r *Runtime) offer(want Tools, all []llm.Tool) []llm.Tool {
	switch want.Kind {
	case NoTools:
		return nil
	case ReadOnlyTools:
		var out []llm.Tool
		for _, t := range all {
			if s, ok := r.o.Tools.Lookup(t.Function.Name); ok && s.ReadOnly {
				out = append(out, t)
			}
		}
		return out
	case ListedTools:
		var out []llm.Tool
		for _, t := range all {
			for _, name := range want.Names {
				if t.Function.Name == name {
					out = append(out, t)
				}
			}
		}
		return out
	}
	return all
}

// subAgents are the registered agents a tool list names, by tool name.
func (r *Runtime) subAgents(want Tools) map[string]Spec {
	if want.Kind != ListedTools {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var out map[string]Spec
	for _, name := range want.Names {
		if sub, ok := r.agents[name]; ok {
			if out == nil {
				out = map[string]Spec{}
			}
			out[toolName(name)] = sub
		}
	}
	return out
}

// toolName is an agent's name as a tool: tool names have no dots.
func toolName(agent string) string { return strings.ReplaceAll(agent, ".", "_") }

// textInput is the input schema of an agent that takes plain text.
var textInput = json.RawMessage(`{"type":"object","properties":{"input":{"type":"string"}},"required":["input"]}`)

// asTool is the agent as a calling model sees it: name, description and
// input schema.
func (s Spec) asTool() llm.Tool {
	schema := s.Input
	if len(schema) == 0 {
		schema = textInput
	}
	return llm.Tool{Type: "function", Function: llm.ToolFunction{Name: toolName(s.Name), Description: s.Description,
		Parameters: schema}}
}

// callSub runs an agent called as a tool, inside the calling run; its answer
// is the tool result.
func (r *Runtime) callSub(ctx context.Context, sub Spec, args string, t texts) string {
	raw := json.RawMessage(args)
	if len(raw) == 0 || !json.Valid(raw) {
		return t.badArgs
	}
	input, err := inputFor(sub, raw)
	if err != nil {
		return t.errorPrefix + err.Error()
	}
	res, err := r.Call(ctx, sub, input, CallContext{})
	if err != nil {
		return t.errorPrefix + err.Error()
	}
	return res.Text
}

// BuildTools turns tool definitions into what a model request takes, for a
// BackgroundContext.
func BuildTools(defs []prompt.ToolDef) ([]llm.Tool, error) { return prompt.BuildTools(defs) }

// runTool runs one tool call of a run: the right it needs first, then the
// guard, as for every tool.
func (r *Runtime) runTool(ctx context.Context, me *run, call llm.ToolCall, t texts) string {
	name := call.Function.Name
	if sub, ok := me.subs[name]; ok {
		return r.callSub(ctx, sub, call.Function.Arguments, t)
	}
	spec, ok := r.o.Tools.Lookup(name)
	if !ok {
		return fmt.Sprintf(t.unknownTool, name)
	}
	if need := RightFor(spec); !me.rights.Has(need) {
		r.log.Info("tool refused", "agent", me.agent, "call_id", me.cc.CallID, "tool", name, "needs", need, "rights", me.rights)
		return fmt.Sprintf(t.noRight, need)
	}
	args := json.RawMessage(call.Function.Arguments)
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	if !json.Valid(args) {
		return t.badArgs
	}
	if r.o.Guard != nil {
		if v := r.o.Guard.Check(ctx, me.ts, spec, args); v.Outcome != tools.Allow {
			r.log.Info("tool blocked", "call_id", me.cc.CallID, "tool", name, "reason", v.Reason)
			return v.Message
		}
	}
	text, isError, err := r.o.Tools.Execute(ctx, tools.Invocation{Name: name, Args: args, SessionKey: me.ts.SessionKey,
		Channel: me.ts.Channel, Person: me.cc.Person, Area: me.cc.Place, DeviceID: me.cc.DeviceID, Foreign: me.ts.Foreign()})
	if r.o.Guard != nil {
		r.o.Guard.Observe(me.ts, spec)
	}
	if err != nil {
		return t.errorPrefix + err.Error()
	}
	if isError && !strings.HasPrefix(text, strings.TrimSpace(t.errorPrefix)) {
		text = t.errorPrefix + text
	}
	return text
}

// chat makes one model call within the model's slots, tried again after the
// limits when it fails or hangs.
func (r *Runtime) chat(ctx context.Context, client Chatter, model string, local bool, req llm.ChatRequest, lim Limits, res *Result) (*llm.ChatResponse, error) {
	var err error
	for attempt := 0; attempt <= lim.Retries; attempt++ {
		var out *llm.ChatResponse
		out, err = r.attempt(ctx, client, model, local, req, lim.AttemptTimeout, res)
		if err == nil {
			return out, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if attempt < lim.Retries {
			r.log.Warn("model call failed, trying again", "model", model, "error", err)
		}
	}
	return nil, err
}

func (r *Runtime) attempt(ctx context.Context, client Chatter, model string, local bool, req llm.ChatRequest, timeout time.Duration, res *Result) (*llm.ChatResponse, error) {
	release, err := r.acquire(ctx, model, local)
	if err != nil {
		return nil, err
	}
	defer release()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	began := time.Now()
	out, err := client.Chat(ctx, req)
	res.ModelCalls++
	res.ModelTime += time.Since(began)
	if err != nil {
		return nil, err
	}
	llm.Count(ctx, model, time.Since(began), out)
	res.PromptTokens += out.Usage.PromptTokens
	res.Completion += out.Usage.CompletionTokens
	return out, nil
}

// acquire waits for a free slot of a model: a local model serves one
// request at a time, a cloud model a few.
func (r *Runtime) acquire(ctx context.Context, model string, local bool) (func(), error) {
	r.mu.Lock()
	slot, ok := r.slots[model]
	if !ok {
		n := cloudSlots
		if local {
			n = localSlots
		}
		slot = make(chan struct{}, n)
		r.slots[model] = slot
	}
	r.mu.Unlock()
	select {
	case slot <- struct{}{}:
		return func() { <-slot }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// isLocal: a model whose address is in the own network (localhost, a
// private or link-local address, a name of the local network).
func isLocal(base string) bool {
	u, err := url.Parse(base)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	switch {
	case host == "localhost", strings.HasSuffix(host, ".local"), strings.HasSuffix(host, ".lan"),
		strings.HasSuffix(host, ".home"), strings.HasSuffix(host, ".internal"), host == "host.docker.internal":
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
	}
	return false
}

// logRun writes the one line every run writes (docs/spec/agenten.md, Log).
func (r *Runtime) logRun(spec Spec, cc CallContext, res Result, took time.Duration, err error) {
	attrs := []any{"agent", spec.Name, "call_id", cc.CallID, "parent_id", cc.ParentID, "depth", cc.Depth,
		"origin", cc.Origin, "mode", cc.Mode.String(), "status", res.Status, "model", res.Model,
		"ms", took.Milliseconds(), "model_calls", res.ModelCalls, "model_ms", res.ModelTime.Milliseconds(),
		"prompt_tokens", res.PromptTokens, "completion_tokens", res.Completion}
	if err != nil {
		r.log.Warn("agent done", append(attrs, "error", err)...)
		return
	}
	r.log.Info("agent done", attrs...)
}

var thinkBlock = regexp.MustCompile(`(?s)<think>.*?</think>`)

func stripThinking(s string) string { return strings.TrimSpace(thinkBlock.ReplaceAllString(s, "")) }

// extractJSON is the outermost JSON object of s: models that ignore the
// response format still tend to wrap it in prose or a code fence.
func extractJSON(s string) string {
	start, end := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if start < 0 || end < start {
		return s
	}
	return s[start : end+1]
}
