package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"regexp"
	"strings"
	"sync"
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
// instructions and the input. readOnly limits the tools to those that only
// read.
type BackgroundContext func(ctx context.Context, cc CallContext, instructions, input string, readOnly bool) ([]llm.Message, []llm.Tool, error)

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
	Logger     *slog.Logger
}

// Runtime runs agents: synchronously in the caller's goroutine for now
// (docs/spec/agenten.md, Runtime). It limits the access to each model.
type Runtime struct {
	o   Options
	log *slog.Logger

	mu    sync.Mutex
	slots map[string]chan struct{}
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
	return &Runtime{o: o, log: o.Logger.With("component", "agent"), slots: map[string]chan struct{}{}}
}

// run is a running call, as the calls made inside it find it in ctx.
type run struct {
	agent  string
	cc     CallContext
	rights Rights
	ts     *tools.TurnState
}

type runKey struct{}

func current(ctx context.Context) *run {
	r, _ := ctx.Value(runKey{}).(*run)
	return r
}

// Call runs an agent synchronously. A call made inside a run (an agent as a
// tool of another) takes its call context from that run: one deeper, the
// parent's call ID, at most the parent's effective rights.
func (r *Runtime) Call(ctx context.Context, spec Spec, input string, cc CallContext) (Result, error) {
	began := time.Now()
	cfg := r.o.Config()
	parent := current(ctx)
	if parent != nil {
		cc = inherit(parent, cc)
	}
	if cc.CallID == "" {
		cc.CallID = strings.ToLower(ulid.Make().String())
	}
	res := Result{CallID: cc.CallID}
	t := i18n.Pick(catalog, lang(cc))
	res.Model = spec.Model
	if res.Model == "" {
		res.Model = cfg.RoleModel(spec.Role)
	}

	finish := func(status Status, err error) (Result, error) {
		res.Status = status
		// Third-party text read by this run counts for its caller.
		if res.Foreign && parent != nil {
			parent.ts.ForeignContext = true
		}
		r.logRun(spec, cc, res, time.Since(began), err)
		return res, err
	}
	if cc.Depth > cfg.Agents.MaxDepth {
		return finish(Failed, ErrDepth)
	}
	client, model, ok := r.o.Models(res.Model)
	if !ok {
		return finish(Failed, fmt.Errorf("no model %q", res.Model))
	}

	if !cc.Mode.Async && cc.Mode.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cc.Mode.Timeout)
		defer cancel()
	}
	me := &run{agent: spec.Name, cc: cc, rights: cc.Rights.And(spec.Rights),
		ts: &tools.TurnState{SessionKey: sessionKey(cc), Channel: config.ChannelBackground, Person: cc.Person,
			Area: cc.Place, DeviceID: cc.DeviceID, ForeignContext: cc.Foreign}}
	ctx = context.WithValue(ctx, runKey{}, me)

	msgs, offered, err := r.build(ctx, spec, input, cc)
	if err != nil {
		return finish(Failed, err)
	}
	maxRounds := spec.Limits.MaxRounds
	if maxRounds == 0 {
		maxRounds = model.MaxToolRounds
	}
	if maxRounds == 0 {
		maxRounds = 3
	}
	local := isLocal(model.BaseURL)

	for round := 0; ; round++ {
		req := llm.ChatRequest{Messages: msgs, Temperature: model.Temperature, MaxTokens: model.MaxTokens,
			ReasoningEffort: model.ReasoningEffort, Channel: string(config.ChannelBackground)}
		if len(offered) > 0 {
			req.Tools = offered
			if round >= maxRounds {
				req.ToolChoice = "none"
			}
		} else if len(spec.Output) > 0 {
			req.ResponseFormat = map[string]any{"type": "json_schema",
				"json_schema": map[string]any{"name": "result", "schema": spec.Output}}
		}
		out, err := r.chat(ctx, client, res.Model, local, req, spec.Limits, &res)
		res.Foreign = me.ts.Foreign()
		if err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return finish(Cancelled, ErrTimeout)
			}
			if ctx.Err() != nil {
				return finish(Cancelled, ctx.Err())
			}
			return finish(Failed, err)
		}
		msg := out.Message()
		msg.Role = llm.RoleAssistant
		msg.Content = stripThinking(msg.Content)
		if len(msg.ToolCalls) == 0 || round >= maxRounds {
			text := strings.TrimSpace(msg.Content)
			if text == "" {
				return finish(Failed, ErrNoAnswer)
			}
			if len(spec.Output) > 0 {
				text = extractJSON(text)
				if err := checkSchema(spec.Output, json.RawMessage(text)); err != nil {
					res.Text = text
					return finish(Failed, fmt.Errorf("%w: %v", ErrOutput, err))
				}
			}
			res.Text = text
			return finish(Done, nil)
		}
		for i := range msg.ToolCalls {
			if msg.ToolCalls[i].ID == "" {
				msg.ToolCalls[i].ID = fmt.Sprintf("call_%d_%d", round, i)
			}
			if msg.ToolCalls[i].Type == "" {
				msg.ToolCalls[i].Type = "function"
			}
		}
		msgs = append(msgs, msg)
		for _, call := range msg.ToolCalls {
			msgs = append(msgs, llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Name: call.Function.Name,
				Content: r.runTool(ctx, me, call, t)})
		}
	}
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
func (r *Runtime) build(ctx context.Context, spec Spec, input string, cc CallContext) ([]llm.Message, []llm.Tool, error) {
	switch spec.Profile {
	case Isolated:
		return []llm.Message{
			{Role: llm.RoleSystem, Content: spec.Instructions},
			{Role: llm.RoleUser, Content: input},
		}, nil, nil
	case Background:
		if r.o.Background == nil {
			return nil, nil, fmt.Errorf("%w: no background context", ErrProfile)
		}
		msgs, all, err := r.o.Background(ctx, cc, spec.Instructions, input, spec.Tools.Kind == ReadOnlyTools)
		if err != nil {
			return nil, nil, err
		}
		return msgs, r.offer(spec.Tools, all), nil
	}
	return nil, nil, ErrProfile
}

// offer narrows the tools to what the agent's description names.
func (r *Runtime) offer(want Tools, all []llm.Tool) []llm.Tool {
	switch want.Kind {
	case NoTools:
		return nil
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

// BuildTools turns tool definitions into what a model request takes, for a
// BackgroundContext.
func BuildTools(defs []prompt.ToolDef) ([]llm.Tool, error) { return prompt.BuildTools(defs) }

// runTool runs one tool call of a run: the right it needs first, then the
// guard, as for every tool.
func (r *Runtime) runTool(ctx context.Context, me *run, call llm.ToolCall, t texts) string {
	name := call.Function.Name
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
