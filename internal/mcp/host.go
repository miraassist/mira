// Package mcp is the service's MCP host: it connects to every configured MCP
// server, aggregates their tools under "<server>__<tool>" names and executes
// calls the model makes.
//
// Home Assistant is one server among several. A server that is temporarily
// unreachable keeps its last known tools in the list on purpose: dropping and
// re-adding them would change prompt layer 3 and throw away the prompt cache
// for every channel, only to restore the same bytes a minute later.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/miraassist/mira/internal/clock"
	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/prompt"
	"github.com/miraassist/mira/internal/retry"
)

// ErrUnknownTool is returned for a name no connected server offers.
var ErrUnknownTool = errors.New("unknown tool")

// ErrServerDown is returned when the tool exists but its server is not
// connected right now.
var ErrServerDown = errors.New("mcp server not connected")

// Tool is one aggregated tool with the facts the policy needs about it.
type Tool struct {
	prompt.ToolDef
	// ReadOnly is true when the tool cannot change anything: the server is
	// configured read-only or the tool says so in its annotations.
	ReadOnly bool
	// WebContent is true when the result is untrusted third-party text.
	WebContent bool
	// Skill is the skill whose server offers the tool; its tools are offered
	// only once the skill is opened (docs/spec/skill-mcp.md).
	Skill string
	// AgentsOnly: the skill's server is not for the conversation; only the
	// skill's agents and schedules get the tool.
	AgentsOnly bool
	// Confirm: the server's entry names the tool under confirm; it runs only
	// after the user's yes (default/skill_confirm.star).
	Confirm bool
}

// Result is the outcome of a tool call, flattened to what the model reads.
type Result struct {
	Text    string
	IsError bool
}

// Connector opens a session to one server. The default dials the configured
// transport; tests substitute in-memory transports.
type Connector func(ctx context.Context, client *sdk.Client, cfg config.MCPServer) (*sdk.ClientSession, error)

type Options struct {
	Servers []config.MCPServer
	Logger  *slog.Logger
	// OnChange runs after the aggregated tool list changed, with the new
	// list. It does not run for a reconnect that brings back the same tools.
	OnChange func([]Tool)
	// ReadOnlyTools are qualified tool names that count as read-only even
	// though their server does not say so (Home Assistant does not annotate
	// GetLiveContext, for example).
	ReadOnlyTools []string
	// RetryInterval is how often unreachable servers are retried by Run.
	RetryInterval time.Duration
	// CallTimeout bounds a single tool call.
	CallTimeout time.Duration
	Connector   Connector
	// Version is reported to the servers as the client version.
	Version string
	// Devices are the voice satellites' device ids. A server with
	// device_context is asked for its tools as each of them would get them,
	// and the lists are joined: Home Assistant's timers, for one, exist only
	// for a satellite that has them.
	Devices func() []string
	// RefreshInterval is how often those lists are fetched again, since
	// Home Assistant does not announce that a satellite came back with
	// timers. 0 means every five minutes.
	RefreshInterval time.Duration
	// Connections give the servers with token: oauth their token
	// (docs/spec/oauth.md); without them such a server is not connected.
	Connections Connections
	// IntentServer is the server of Home Assistant's Assist API: an intent
	// answer of it where nothing succeeded but an area is an error
	// (docs/spec/intent-results.md). "" for none.
	IntentServer string
	// Clock is what the waits between connects and the refresh wait on;
	// nil is the real one. A call's CallTimeout is a context deadline of
	// the connection and stays real time.
	Clock clock.Clock
}

// Connections are the skills' logins at services, as the MCP host needs
// them.
type Connections interface {
	// Connected reports whether the skill has a connection to use.
	Connected(skill string) bool
	// Transport adds the skill's token to the requests it allows.
	Transport(skill string, next http.RoundTripper) http.RoundTripper
}

// ErrNoConnection: a server with token: oauth whose skill is not connected;
// it is not tried.
var ErrNoConnection = retry.Fatal(errors.New("the skill is not connected"))

// TokenRefused is a server's 401 or 403: its token is refused, and only a
// change of its connection or config can help (docs/spec/retry.md, row 6).
type TokenRefused struct {
	Status int
	URL    string
}

func (e *TokenRefused) Error() string {
	return fmt.Sprintf("%s refused the token (%d %s)", e.URL, e.Status, http.StatusText(e.Status))
}

// Retryable says no: the same token is refused again.
func (e *TokenRefused) Retryable() bool { return false }

// refusals turns a 401 or 403 into a TokenRefused, which the SDK passes on
// to the caller as the error of the request.
type refusals struct{ next http.RoundTripper }

func (r refusals) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := r.next.RoundTrip(req)
	if err == nil && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
		resp.Body.Close()
		return nil, &TokenRefused{Status: resp.StatusCode, URL: req.URL.Redacted()}
	}
	return resp, err
}

// deviceMeta is the key Home Assistant's MCP server reads the requesting
// satellite from.
const deviceMeta = "io.home-assistant/device_id"

type deviceKey struct{}

// WithDevice marks the calls made with ctx as coming from that satellite.
func WithDevice(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, deviceKey{}, id)
}

func deviceOf(ctx context.Context) string {
	id, _ := ctx.Value(deviceKey{}).(string)
	return id
}

// Host owns the server connections.
type Host struct {
	log          *slog.Logger
	servers      []*server
	byName       map[string]*server
	onChange     func([]Tool)
	readOnly     map[string]bool
	retry        time.Duration
	callTimeout  time.Duration
	intentServer string
	connect      Connector
	version      string
	devices      func() []string
	refreshEvery time.Duration
	clock        clock.Clock
	conns        Connections

	// mu guards the aggregated view.
	mu        sync.RWMutex
	tools     []Tool
	toolIndex map[string]Tool
	signature string
}

func New(opts Options) *Host {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.RetryInterval == 0 {
		opts.RetryInterval = 30 * time.Second
	}
	if opts.CallTimeout == 0 {
		opts.CallTimeout = 30 * time.Second
	}
	custom := opts.Connector != nil
	if !custom {
		opts.Connector = DialTransport
	}
	if opts.Version == "" {
		opts.Version = "dev"
	}
	if opts.RefreshInterval == 0 {
		opts.RefreshInterval = 5 * time.Minute
	}
	h := &Host{
		log:          opts.Logger.With("component", "mcp"),
		byName:       map[string]*server{},
		onChange:     opts.OnChange,
		readOnly:     map[string]bool{},
		retry:        opts.RetryInterval,
		callTimeout:  opts.CallTimeout,
		intentServer: opts.IntentServer,
		connect:      opts.Connector,
		version:      opts.Version,
		devices:      opts.Devices,
		refreshEvery: opts.RefreshInterval,
		clock:        clock.Or(opts.Clock),
		toolIndex:    map[string]Tool{},
		conns:        opts.Connections,
	}
	if !custom {
		h.connect = h.dial
	}
	for _, name := range opts.ReadOnlyTools {
		h.readOnly[name] = true
	}
	for _, cfg := range opts.Servers {
		if cfg.Disabled {
			continue
		}
		s := newServer(h, cfg)
		h.servers = append(h.servers, s)
		h.byName[cfg.Name] = s
	}
	return h
}

// Start connects to every server once. Unreachable servers are logged and
// left to Run; the service must come up even when the search server is down.
// It returns an error only if no server could be reached at all while some
// were configured.
func (h *Host) Start(ctx context.Context) error {
	var wg sync.WaitGroup
	for _, s := range h.servers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.connectNow(ctx, false, false) // a failure is logged where it is kept
		}()
	}
	wg.Wait()
	h.rebuild()

	tried := 0
	for _, s := range h.servers {
		if s.connected() {
			return nil
		}
		s.mu.Lock()
		if !errors.Is(s.lastErr, ErrNoConnection) {
			tried++
		}
		s.mu.Unlock()
	}
	if tried == 0 {
		return nil
	}
	return fmt.Errorf("none of %d mcp servers is reachable", tried)
}

// Run retries unreachable servers until ctx ends, each with waits doubling
// from the retry interval up to 30 minutes; one whose token is refused, or
// whose skill is not connected, waits for its connection to change
// (Reconnect, a new config). docs/spec/retry.md, row 6.
func (h *Host) Run(ctx context.Context) error {
	// The retry is a timer set anew once a round is done: a slow connect
	// does not pile up rounds.
	t := h.clock.NewTimer(h.retry)
	defer t.Stop()
	refresh := h.clock.NewTicker(h.refreshEvery)
	defer refresh.Stop()
	for {
		select {
		case <-ctx.Done():
			h.closeAll()
			return ctx.Err()
		case <-refresh.C():
			h.Refresh(ctx)
		case <-t.C():
			changed := false
			now := h.clock.Now()
			for _, s := range h.servers {
				if s.connected() {
					continue
				}
				s.mu.Lock()
				// A dropped session has no error: tried at once.
				due := (s.lastErr == nil || retry.Retryable(s.lastErr)) && !now.Before(s.next)
				s.mu.Unlock()
				if !due {
					continue
				}
				if err := s.connectNow(ctx, false, false); err != nil {
					continue
				}
				h.log.Info("mcp server reconnected", "server", s.cfg.Name)
				changed = true
			}
			if changed {
				h.rebuild()
			}
			t.Reset(h.retry)
		}
	}
}

// policy is the waits between connects of a server (docs/spec/retry.md,
// row 6).
func (h *Host) policy() retry.Policy {
	return retry.Policy{First: h.retry, Max: 30 * time.Minute, Growth: retry.Exponential}
}

// failedConnect keeps a failed connect of s and logs it: a warning at the
// first failure and when the error changes, an error once for a refused
// token, nothing for a skill without connection (its page says so).
func (h *Host) failedConnect(s *server, err error) {
	s.mu.Lock()
	before := s.lastErr
	s.lastErr = err
	s.failed++
	wait := h.policy().Wait(s.failed)
	s.next = h.clock.Now().Add(wait)
	failed := s.failed
	s.mu.Unlock()
	switch {
	case errors.Is(err, ErrNoConnection):
	case !retry.Retryable(err):
		if before == nil || before.Error() != err.Error() {
			h.log.Error("mcp server refused the token; not tried again until its connection changes",
				"server", s.cfg.Name, "error", err)
		}
	case before == nil || before.Error() != err.Error():
		h.log.Warn("mcp server unreachable, trying again", "server", s.cfg.Name, "attempt", failed,
			"wait", wait.String(), "error", err)
	default:
		h.log.Debug("mcp server still unreachable", "server", s.cfg.Name, "attempt", failed,
			"wait", wait.String(), "error", err)
	}
}

// Close ends every session. Run calls it on shutdown; callers that never
// started Run (tests, the check command) call it directly.
func (h *Host) Close() { h.closeAll() }

func (h *Host) closeAll() {
	for _, s := range h.servers {
		s.stop()
	}
}

// lostSession reports a call that failed on the way, not in the tool: a
// remote server over HTTP that restarted has forgotten the session, and
// nothing told the client. A timeout is not one of them: the server may
// have done the work, and doing it twice is worse than failing.
func lostSession(err error) bool {
	if err == nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return false
	}
	var rpc *jsonrpc.Error
	return !errors.As(err, &rpc) // an answer from the server is no lost session
}

// refresh lists the tools of a server again, in its loop.
func (h *Host) refresh(ctx context.Context, s *server) { s.refreshNow(ctx) }

// DeviceTools makes sure a satellite's own tools are in the list before its
// turn: when listing them failed last time, the servers that take the
// satellite into account list again; an error says they still cannot
// (docs/spec/fallbacks.md, row 6). A device no server failed for is fine.
func (h *Host) DeviceTools(ctx context.Context, device string) error {
	if device == "" {
		return nil
	}
	for _, s := range h.servers {
		s.mu.Lock()
		_, failed := s.unlisted[device]
		s.mu.Unlock()
		if !failed {
			continue
		}
		h.refresh(ctx, s)
		s.mu.Lock()
		err, still := s.unlisted[device]
		s.mu.Unlock()
		if still {
			return fmt.Errorf("the tools of %s for satellite %s: %w", s.cfg.Name, device, err)
		}
	}
	return nil
}

// missingTools are the tools a server's configuration names that it does
// not offer.
func missingTools(cfg config.MCPServer, tools []Tool) []string {
	have := map[string]bool{}
	for _, t := range tools {
		have[t.Name] = true
	}
	var out []string
	for _, name := range cfg.Tools {
		if !have[name] {
			out = append(out, name)
		}
	}
	return out
}

// Refresh lists again the tools of the servers that depend on the satellite
// (device_context). A satellite that came back with timers, or a new one,
// shows in the list; without a satellite known, nothing is asked.
func (h *Host) Refresh(ctx context.Context) {
	for _, s := range h.servers {
		if s.cfg.WantsDevice() && s.connected() {
			h.refresh(ctx, s)
		}
	}
}

// listTools asks for the tools. For a server that takes the satellite into
// account, once without one and once as each satellite would get them; the
// lists are joined, so nothing a satellite is offered is lost.
func (h *Host) listTools(ctx context.Context, cfg config.MCPServer, session *sdk.ClientSession) ([]Tool, map[string]error, error) {
	out, err := h.listOne(ctx, cfg, session, "")
	if err != nil || !cfg.WantsDevice() || h.devices == nil {
		return out, nil, err
	}
	var unlisted map[string]error
	have := map[string]bool{}
	for _, t := range out {
		have[t.Name] = true
	}
	for _, device := range h.devices() {
		more, err := h.listOne(ctx, cfg, session, device)
		if err != nil {
			h.log.Warn("could not list the tools for a satellite; its turns list them again", "server", cfg.Name,
				"device", device, "error", err)
			if unlisted == nil {
				unlisted = map[string]error{}
			}
			unlisted[device] = err
			continue
		}
		for _, t := range more {
			if !have[t.Name] {
				have[t.Name] = true
				out = append(out, t)
			}
		}
	}
	return out, unlisted, nil
}

func (h *Host) listOne(ctx context.Context, cfg config.MCPServer, session *sdk.ClientSession, device string) ([]Tool, error) {
	var out []Tool
	var params *sdk.ListToolsParams
	if device != "" {
		params = &sdk.ListToolsParams{Meta: sdk.Meta{deviceMeta: device}}
	}
	for t, err := range session.Tools(ctx, params) {
		if err != nil {
			return nil, fmt.Errorf("list tools: %w", err)
		}
		schema, err := json.Marshal(t.InputSchema)
		if err != nil || string(schema) == "null" {
			schema = []byte(`{"type":"object","properties":{}}`)
		}
		def := prompt.ToolDef{Server: cfg.Name, Name: t.Name, Description: t.Description, Schema: schema}
		// A setup tool is for the page; a model gets it only when tools
		// names it as well (status) (docs/spec/skill-programs.md).
		if cfg.IsSetup(t.Name) {
			h.keepSetup(cfg.Name, def)
			if !slices.Contains(cfg.Tools, t.Name) {
				continue
			}
		}
		if len(cfg.Tools) > 0 && !slices.Contains(cfg.Tools, t.Name) {
			continue // not among the tools Mira takes from this server
		}
		readOnly := cfg.ReadOnly || slices.Contains(cfg.ReadOnlyTools, t.Name) || h.readOnly[def.QualifiedName()] ||
			(t.Annotations != nil && t.Annotations.ReadOnlyHint)
		out = append(out, Tool{ToolDef: def, ReadOnly: readOnly, WebContent: cfg.WebContent, Skill: cfg.Skill,
			AgentsOnly: cfg.Skill != "" && !cfg.ForConversation(), Confirm: slices.Contains(cfg.Confirm, t.Name)})
	}
	return out, nil
}

// rebuild recomputes the aggregated list and fires OnChange if it differs.
func (h *Host) rebuild() {
	var all []Tool
	for _, s := range h.servers {
		s.mu.Lock()
		all = append(all, s.tools...)
		s.mu.Unlock()
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Server != all[j].Server {
			return all[i].Server < all[j].Server
		}
		return all[i].Name < all[j].Name
	})
	sig := signature(all)

	h.mu.Lock()
	changed := sig != h.signature
	h.tools = all
	h.signature = sig
	h.toolIndex = make(map[string]Tool, len(all))
	for _, t := range all {
		h.toolIndex[t.QualifiedName()] = t
	}
	h.mu.Unlock()

	if changed && h.onChange != nil {
		h.onChange(append([]Tool(nil), all...))
	}
}

// signature is a cheap identity of the tool list, including schemas and the
// policy flags, so any change that matters is noticed.
func signature(tools []Tool) string {
	var b strings.Builder
	for _, t := range tools {
		fmt.Fprintf(&b, "%s\x1f%s\x1f%s\x1f%t%t%t\x1e", t.QualifiedName(), t.Description, t.Schema, t.ReadOnly, t.WebContent, t.Confirm)
	}
	return b.String()
}

// Tools returns the aggregated list, sorted by (server, name).
func (h *Host) Tools() []Tool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return append([]Tool(nil), h.tools...)
}

// Lookup returns the tool behind a qualified name.
func (h *Host) Lookup(qualified string) (Tool, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	t, ok := h.toolIndex[qualified]
	return t, ok
}

// Call executes a tool by its qualified name. A tool-level failure (the
// server ran the tool and it reported an error) comes back as a Result with
// IsError set; err is reserved for the call not happening at all.
func (h *Host) Call(ctx context.Context, qualified string, args json.RawMessage) (Result, error) {
	serverName, toolName := prompt.SplitQualifiedName(qualified)
	s, ok := h.byName[serverName]
	if !ok {
		return Result{}, fmt.Errorf("%w: %s", ErrUnknownTool, qualified)
	}
	if _, ok := h.Lookup(qualified); !ok {
		return Result{}, fmt.Errorf("%w: %s", ErrUnknownTool, qualified)
	}
	return h.callOn(ctx, s, qualified, toolName, args)
}

// CallServer calls a tool of a server by the server's own tool name, also one
// its configuration does not offer to the model (tools): for Mira's own code
// with fixed steps, never for a model (the inbox reads a document and marks
// it read, docs/spec/inbox.md).
func (h *Host) CallServer(ctx context.Context, serverName, toolName string, args json.RawMessage) (Result, error) {
	s, ok := h.byName[serverName]
	if !ok {
		return Result{}, fmt.Errorf("%w: %s", ErrUnknownTool, serverName+prompt.ServerSeparator+toolName)
	}
	return h.callOn(ctx, s, serverName+prompt.ServerSeparator+toolName, toolName, args)
}

func (h *Host) callOn(ctx context.Context, s *server, qualified, toolName string, args json.RawMessage) (Result, error) {
	serverName := s.cfg.Name
	if !s.connected() {
		return Result{}, fmt.Errorf("%w: %s", ErrServerDown, serverName)
	}

	var arguments map[string]any
	if len(args) > 0 && string(args) != "null" {
		if err := json.Unmarshal(args, &arguments); err != nil {
			return Result{}, fmt.Errorf("arguments of %s are not a JSON object: %w", qualified, err)
		}
	}
	if arguments == nil {
		arguments = map[string]any{}
	}

	params := &sdk.CallToolParams{Name: toolName, Arguments: arguments}
	if device := deviceOf(ctx); device != "" && s.cfg.WantsDevice() {
		params.Meta = sdk.Meta{deviceMeta: device}
	}
	call := func(session *sdk.ClientSession) (*sdk.CallToolResult, error) {
		// real time: a bound of a call to an MCP server or of its process ending; no test waits for it
		cctx, cancel := context.WithTimeout(ctx, h.callTimeout)
		defer cancel()
		return session.CallTool(cctx, params)
	}
	res, err := onConn(ctx, s, call)
	if err != nil {
		return Result{}, fmt.Errorf("call %s: %w", qualified, err)
	}
	out := Result{Text: flatten(res), IsError: res.IsError}
	if serverName == h.intentServer && !out.IsError {
		if failed := actedOnNothing(out.Text); len(failed) > 0 {
			out.IsError = true
			h.log.Warn("home assistant acted on nothing", "tool", qualified, "failed", failed)
		}
	}
	return out, nil
}

// onConn runs one request on the server's current connection; failed on a
// lost session (not on a connection Mira closed, decision 7), it is repeated
// once on the connection the server's loop gives (decision 6).
func onConn[T any](ctx context.Context, s *server, do func(*sdk.ClientSession) (T, error)) (T, error) {
	session, release, c, ok := s.use()
	if !ok {
		var zero T
		return zero, fmt.Errorf("%w: %s", ErrServerDown, s.cfg.Name)
	}
	res, err := do(session)
	release()
	if !retryable(err, c) {
		return res, err
	}
	fresh := s.replaced(ctx, c, err)
	if fresh == nil {
		return res, err
	}
	session, release, ok = fresh.Use()
	if !ok {
		return res, err
	}
	defer release()
	return do(session)
}

// actedOnNothing is the failed entities of an intent answer of Home
// Assistant that is action_done although no entity succeeded: Home Assistant
// counts a named area or floor as a success before any call
// (docs/spec/intent-results.md). nil for any other answer.
func actedOnNothing(text string) []string {
	var a struct {
		ResponseType string `json:"response_type"`
		Data         struct {
			Success []struct {
				Type string `json:"type"`
			} `json:"success"`
			Failed []struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"failed"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(text)), &a) != nil || a.ResponseType != "action_done" || len(a.Data.Failed) == 0 {
		return nil
	}
	for _, s := range a.Data.Success {
		if s.Type == "entity" {
			return nil
		}
	}
	out := make([]string, 0, len(a.Data.Failed))
	for _, f := range a.Data.Failed {
		id := f.ID
		if id == "" {
			id = f.Name
		}
		out = append(out, id)
	}
	return out
}

// Prompt fetches a prompt template from one server and returns its text
// content. Home Assistant serves its Assist instructions and the overview of
// exposed entities this way. The text names tools as the server knows them;
// they are rewritten to the qualified names the model sees.
func (h *Host) Prompt(ctx context.Context, serverName, name string) (string, error) {
	return h.PromptAs(ctx, serverName, name, "")
}

// PromptAs is Prompt as a satellite would get it: Home Assistant adds the
// area it is in and whether it can start timers. "" asks without a device.
// A name "" takes the server's first prompt: Home Assistant serves one per
// LLM API and names it after the API, which it renames ("Assist" until
// 2026.10, "Device control" since 2026.11).
func (h *Host) PromptAs(ctx context.Context, serverName, name, device string) (string, error) {
	s, ok := h.byName[serverName]
	if !ok {
		return "", fmt.Errorf("unknown mcp server %q", serverName)
	}
	if !s.connected() {
		return "", fmt.Errorf("%w: %s", ErrServerDown, serverName)
	}
	if name == "" {
		list, err := onConn(ctx, s, func(session *sdk.ClientSession) (*sdk.ListPromptsResult, error) {
			// real time: a bound of a call to an MCP server or of its process ending; no test waits for it
			cctx, cancel := context.WithTimeout(ctx, h.callTimeout)
			defer cancel()
			return session.ListPrompts(cctx, nil)
		})
		if err != nil {
			return "", fmt.Errorf("list prompts of %s: %w", serverName, err)
		}
		if len(list.Prompts) == 0 {
			return "", fmt.Errorf("%s serves no prompt", serverName)
		}
		name = list.Prompts[0].Name
	}
	params := &sdk.GetPromptParams{Name: name}
	if device != "" && s.cfg.WantsDevice() {
		params.Meta = sdk.Meta{deviceMeta: device}
	}
	res, err := onConn(ctx, s, func(session *sdk.ClientSession) (*sdk.GetPromptResult, error) {
		// real time: a bound of a call to an MCP server or of its process ending; no test waits for it
		cctx, cancel := context.WithTimeout(ctx, h.callTimeout)
		defer cancel()
		return session.GetPrompt(cctx, params)
	})
	s.mu.Lock()
	tools := s.tools
	s.mu.Unlock()
	if err != nil {
		return "", fmt.Errorf("get prompt %s from %s: %w", name, serverName, err)
	}
	var parts []string
	for _, m := range res.Messages {
		if t, ok := m.Content.(*sdk.TextContent); ok {
			parts = append(parts, t.Text)
		}
	}
	return QualifyToolNames(strings.Join(parts, "\n\n"), tools), nil
}

// QualifyToolNames replaces bare tool names in a server's text with the
// qualified names: "intent__HassTurnOn" becomes "ha__intent__HassTurnOn".
// Only whole names are replaced, and a name already qualified stays as it is.
func QualifyToolNames(text string, tools []Tool) string {
	if len(tools) == 0 {
		return text
	}
	byName := make(map[string]string, len(tools))
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		byName[t.Name] = t.QualifiedName()
		names = append(names, regexp.QuoteMeta(t.Name))
	}
	// Longest first, so a name that is a prefix of another does not win.
	sort.Slice(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })
	re := regexp.MustCompile(`(^|[^\w])(` + strings.Join(names, "|") + `)\b`)
	return re.ReplaceAllStringFunc(text, func(m string) string {
		sub := re.FindStringSubmatch(m)
		return sub[1] + byName[sub[2]]
	})
}

// flatten turns MCP content into text. Structured content is preferred when
// there is no text, since some servers only fill that.
func flatten(res *sdk.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		switch v := c.(type) {
		case *sdk.TextContent:
			parts = append(parts, v.Text)
		case *sdk.ImageContent:
			parts = append(parts, "[Bild, "+v.MIMEType+"]")
		case *sdk.AudioContent:
			parts = append(parts, "[Audio, "+v.MIMEType+"]")
		case *sdk.EmbeddedResource:
			if v.Resource != nil && v.Resource.Text != "" {
				parts = append(parts, v.Resource.Text)
			}
		case *sdk.ResourceLink:
			parts = append(parts, v.URI)
		}
	}
	if len(parts) == 0 && res.StructuredContent != nil {
		if b, err := json.Marshal(res.StructuredContent); err == nil {
			parts = append(parts, string(b))
		}
	}
	return strings.Join(parts, "\n")
}

// Status describes each server for diagnostics.
type Status struct {
	Name      string
	Connected bool
	Tools     int
	Error     string
	// Skill is the skill whose server it is; Missing are the tools it was
	// asked for and does not have.
	Skill   string
	Missing []string
	// OAuth: its token is the skill's connection; while there is none it
	// is not tried (NoConnection).
	OAuth        bool
	NoConnection bool
}

func (h *Host) Status() []Status {
	out := make([]Status, 0, len(h.servers))
	for _, s := range h.servers {
		s.mu.Lock()
		st := Status{Name: s.cfg.Name, Connected: s.conn != nil, Tools: len(s.tools), Skill: s.cfg.Skill,
			Missing: append([]string(nil), s.missing...), OAuth: s.cfg.OAuth,
			NoConnection: errors.Is(s.lastErr, ErrNoConnection)}
		if s.lastErr != nil {
			st.Error = s.lastErr.Error()
		}
		s.mu.Unlock()
		out = append(out, st)
	}
	return out
}

// stdioKeep is what a program Mira starts gets of her environment: where to
// find things, the locale and the proxy settings. Nothing else, above all not
// her own secrets (Home Assistant's token, her access token, model keys); the
// config's env list hands over what a server needs.
var stdioKeep = map[string]bool{
	"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "TZ": true,
	"LANG": true, "LANGUAGE": true, "TMPDIR": true, "TEMP": true, "TMP": true,
	"SSL_CERT_FILE": true, "SSL_CERT_DIR": true, "NODE_EXTRA_CA_CERTS": true,
	"HTTP_PROXY": true, "HTTPS_PROXY": true, "NO_PROXY": true,
	"http_proxy": true, "https_proxy": true, "no_proxy": true,
}

// StdioEnv is the environment of a server started over stdio: the few
// variables in stdioKeep (and LC_*, XDG_*) from parent, then the config's own.
func StdioEnv(parent, extra []string) []string {
	var out []string
	for _, kv := range parent {
		name, _, _ := strings.Cut(kv, "=")
		if stdioKeep[name] || strings.HasPrefix(name, "LC_") || strings.HasPrefix(name, "XDG_") {
			out = append(out, kv)
		}
	}
	return append(out, extra...)
}

// dial is the default connector: a server with token: oauth gets its
// skill's connection, the others DialTransport.
func (h *Host) dial(ctx context.Context, client *sdk.Client, cfg config.MCPServer) (*sdk.ClientSession, error) {
	if !cfg.OAuth {
		return DialTransport(ctx, client, cfg)
	}
	if h.conns == nil || !h.conns.Connected(cfg.Skill) {
		return nil, ErrNoConnection
	}
	hc := &http.Client{Transport: refusals{h.conns.Transport(cfg.Skill, http.DefaultTransport)}}
	return client.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: cfg.URL, HTTPClient: hc}, nil)
}

// Wake tries a server that is unreachable at once and lets its waits start
// anew; one that is connected keeps its session, and one whose error is
// fatal (a refused token) is left alone until its connection or config
// changes (docs/spec/retry.md, row 6). Home Assistant's connection wakes its
// server, so the prompt is there again without waiting for the next of the
// growing waits (docs/spec/context.md, 1.2).
func (h *Host) Wake(ctx context.Context, name string) {
	s := h.byName[name]
	if s == nil {
		return
	}
	if err := s.connectNow(ctx, true, false); err != nil {
		return // connected already, fatal, or kept and logged by failedConnect
	}
	h.log.Info("mcp server reconnected", "server", s.cfg.Name, "reason", "home assistant connected")
	h.rebuild()
}

// Reconnect connects a skill's server anew after its connection changed:
// connected, it is connected at once; disconnected or expired, its tools
// are dropped. The old connection is retired: calls on it finish
// (docs/spec/mcp-connections.md, decision 8).
func (h *Host) Reconnect(ctx context.Context, name string) {
	s := h.byName[name]
	if s == nil {
		return
	}
	_ = s.connectNow(ctx, false, true) // a failure is logged where it is kept
	h.rebuild()
}

// DialTransport connects with the transport the config names.
func DialTransport(ctx context.Context, client *sdk.Client, cfg config.MCPServer) (*sdk.ClientSession, error) {
	var transport sdk.Transport
	switch cfg.Transport {
	case config.TransportHTTP:
		hc := &http.Client{Transport: refusals{http.DefaultTransport}}
		if cfg.Token != "" {
			hc.Transport = refusals{bearer{token: cfg.Token, next: http.DefaultTransport}}
		}
		transport = &sdk.StreamableClientTransport{Endpoint: cfg.URL, HTTPClient: hc}
	case config.TransportStdio:
		cmd := exec.Command(cfg.Command, cfg.Args...)
		cmd.Env = StdioEnv(os.Environ(), cfg.Env)
		cmd.Dir = cfg.Dir
		cmd.Stderr = os.Stderr
		t, err := stdioTransport(cmd, slog.Default().With("component", "mcp", "server", cfg.Name))
		if err != nil {
			return nil, fmt.Errorf("start %s: %w", cfg.Command, err)
		}
		transport = t
	default:
		return nil, fmt.Errorf("unknown transport %q", cfg.Transport)
	}
	return client.Connect(ctx, transport, nil)
}

type bearer struct {
	token string
	next  http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.next.RoundTrip(r)
}

// keepSetup remembers a setup tool's definition for the page.
func (h *Host) keepSetup(serverName string, def prompt.ToolDef) {
	s := h.byName[serverName]
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.setup == nil {
		s.setup = map[string]prompt.ToolDef{}
	}
	s.setup[def.Name] = def
}

// SetupTool is a setup tool of a server as the server listed it: its
// description and input schema, for the page's form. False while the
// server has not listed it (not connected, or an older program).
func (h *Host) SetupTool(serverName, tool string) (prompt.ToolDef, bool) {
	s := h.byName[serverName]
	if s == nil {
		return prompt.ToolDef{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	def, ok := s.setup[tool]
	return def, ok
}

// CallSetup calls a setup tool of a server, for an admin on the page; a
// tool that is not among the server's setup is refused.
func (h *Host) CallSetup(ctx context.Context, serverName, tool string, args json.RawMessage) (Result, error) {
	s := h.byName[serverName]
	if s == nil || !s.cfg.IsSetup(tool) {
		return Result{}, fmt.Errorf("%w: %s", ErrUnknownTool, serverName+prompt.ServerSeparator+tool)
	}
	return h.callOn(ctx, s, serverName+prompt.ServerSeparator+tool, tool, args)
}
