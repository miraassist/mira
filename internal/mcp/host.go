// Package mcp is the agent's MCP host: it connects to every configured MCP
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

type server struct {
	cfg config.MCPServer

	mu      sync.Mutex
	session *sdk.ClientSession
	tools   []Tool // last known, kept while disconnected
	lastErr error
	// failed counts the failed connects since it last worked; next is when
	// Run tries again (docs/spec/retry.md, row 6).
	failed int
	next   time.Time
	// missing are the tools the configuration names that the server does
	// not have.
	missing []string
	// unlisted are the satellites whose tools could not be listed last
	// time, with why (docs/spec/fallbacks.md, row 6).
	unlisted map[string]error
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
	connect      Connector
	version      string
	devices      func() []string
	refreshEvery time.Duration
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
		connect:      opts.Connector,
		version:      opts.Version,
		devices:      opts.Devices,
		refreshEvery: opts.RefreshInterval,
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
		s := &server{cfg: cfg}
		h.servers = append(h.servers, s)
		h.byName[cfg.Name] = s
	}
	return h
}

// Start connects to every server once. Unreachable servers are logged and
// left to Run; the agent must come up even when the search server is down.
// It returns an error only if no server could be reached at all while some
// were configured.
func (h *Host) Start(ctx context.Context) error {
	var wg sync.WaitGroup
	for _, s := range h.servers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = h.connectServer(ctx, s) // a failure is logged where it is kept
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
	t := time.NewTicker(h.retry)
	defer t.Stop()
	refresh := time.NewTicker(h.refreshEvery)
	defer refresh.Stop()
	for {
		select {
		case <-ctx.Done():
			h.closeAll()
			return ctx.Err()
		case <-refresh.C:
			h.Refresh(ctx)
		case <-t.C:
			changed := false
			now := time.Now()
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
				if err := h.connectServer(ctx, s); err != nil {
					continue
				}
				h.log.Info("mcp server reconnected", "server", s.cfg.Name)
				changed = true
			}
			if changed {
				h.rebuild()
			}
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
	s.next = time.Now().Add(wait)
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
		s.mu.Lock()
		if s.session != nil {
			_ = s.session.Close()
			s.session = nil
		}
		s.mu.Unlock()
	}
}

func (s *server) connected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.session != nil
}

func (h *Host) connectServer(ctx context.Context, s *server) error {
	client := sdk.NewClient(&sdk.Implementation{Name: "mira", Version: h.version}, &sdk.ClientOptions{
		Logger: h.log,
		ToolListChangedHandler: func(ctx context.Context, _ *sdk.ToolListChangedRequest) {
			h.log.Info("mcp server announced a tool list change", "server", s.cfg.Name)
			go h.refresh(context.WithoutCancel(ctx), s)
		},
	})
	cctx, cancel := context.WithTimeout(ctx, h.callTimeout)
	defer cancel()
	session, err := h.connect(cctx, client, s.cfg)
	if err != nil {
		h.failedConnect(s, err)
		return err
	}
	tools, unlisted, err := h.listTools(cctx, s.cfg, session)
	if err != nil {
		_ = session.Close()
		h.failedConnect(s, err)
		return err
	}
	s.mu.Lock()
	s.session = session
	s.tools, s.missing, s.unlisted = tools, missingTools(s.cfg, tools), unlisted
	s.lastErr, s.failed, s.next = nil, 0, time.Time{}
	s.mu.Unlock()
	h.log.Info("mcp server connected", "server", s.cfg.Name, "tools", len(tools))

	// Notice a dropped connection so Run can reconnect.
	go func() {
		_ = session.Wait()
		s.mu.Lock()
		if s.session == session {
			s.session = nil
		}
		s.mu.Unlock()
		if ctx.Err() == nil {
			h.log.Warn("mcp server disconnected", "server", s.cfg.Name)
		}
	}()
	return nil
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

// reconnect drops a session that failed a call and connects anew, for the
// one repeat of that call. It returns nil when the server stays away; Run
// then keeps trying as for any server that is down.
func (h *Host) reconnect(ctx context.Context, s *server, failed *sdk.ClientSession, cause error) *sdk.ClientSession {
	s.mu.Lock()
	if s.session == failed {
		s.session = nil
	}
	s.mu.Unlock()
	_ = failed.Close()
	h.log.Warn("mcp call failed on its session, connecting anew", "server", s.cfg.Name, "error", cause)
	if err := h.connectServer(context.WithoutCancel(ctx), s); err != nil {
		h.log.Warn("mcp server unreachable", "server", s.cfg.Name, "error", err)
		h.rebuild()
		return nil
	}
	h.rebuild()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.session
}

func (h *Host) refresh(ctx context.Context, s *server) {
	s.mu.Lock()
	session := s.session
	s.mu.Unlock()
	if session == nil {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, h.callTimeout)
	defer cancel()
	tools, unlisted, err := h.listTools(cctx, s.cfg, session)
	if err != nil {
		h.log.Warn("could not refresh tool list", "server", s.cfg.Name, "error", err)
		return
	}
	s.mu.Lock()
	s.tools, s.missing, s.unlisted = tools, missingTools(s.cfg, tools), unlisted
	s.mu.Unlock()
	h.rebuild()
}

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
		if len(cfg.Tools) > 0 && !slices.Contains(cfg.Tools, t.Name) {
			continue // not among the tools Mira takes from this server
		}
		schema, err := json.Marshal(t.InputSchema)
		if err != nil || string(schema) == "null" {
			schema = []byte(`{"type":"object","properties":{}}`)
		}
		def := prompt.ToolDef{Server: cfg.Name, Name: t.Name, Description: t.Description, Schema: schema}
		readOnly := cfg.ReadOnly || h.readOnly[def.QualifiedName()] ||
			(t.Annotations != nil && t.Annotations.ReadOnlyHint)
		out = append(out, Tool{ToolDef: def, ReadOnly: readOnly, WebContent: cfg.WebContent, Skill: cfg.Skill,
			AgentsOnly: cfg.Skill != "" && !cfg.ForConversation()})
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
		fmt.Fprintf(&b, "%s\x1f%s\x1f%s\x1f%t%t\x1e", t.QualifiedName(), t.Description, t.Schema, t.ReadOnly, t.WebContent)
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
	s.mu.Lock()
	session := s.session
	s.mu.Unlock()
	if session == nil {
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
	cctx, cancel := context.WithTimeout(ctx, h.callTimeout)
	defer cancel()
	res, err := session.CallTool(cctx, params)
	if lostSession(err) {
		if fresh := h.reconnect(ctx, s, session, err); fresh != nil {
			rctx, rcancel := context.WithTimeout(ctx, h.callTimeout)
			defer rcancel()
			res, err = fresh.CallTool(rctx, params)
		}
	}
	if err != nil {
		return Result{}, fmt.Errorf("call %s: %w", qualified, err)
	}
	return Result{Text: flatten(res), IsError: res.IsError}, nil
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
	s.mu.Lock()
	session, tools := s.session, s.tools
	s.mu.Unlock()
	if session == nil {
		return "", fmt.Errorf("%w: %s", ErrServerDown, serverName)
	}
	cctx, cancel := context.WithTimeout(ctx, h.callTimeout)
	defer cancel()
	if name == "" {
		list, err := session.ListPrompts(cctx, nil)
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
	res, err := session.GetPrompt(cctx, params)
	if lostSession(err) {
		if fresh := h.reconnect(ctx, s, session, err); fresh != nil {
			rctx, rcancel := context.WithTimeout(ctx, h.callTimeout)
			defer rcancel()
			res, err = fresh.GetPrompt(rctx, params)
		}
	}
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
		st := Status{Name: s.cfg.Name, Connected: s.session != nil, Tools: len(s.tools), Skill: s.cfg.Skill,
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
	if s == nil || s.connected() {
		return
	}
	s.mu.Lock()
	fatal := s.lastErr != nil && !retry.Retryable(s.lastErr)
	if !fatal {
		s.failed, s.next = 0, time.Time{}
	}
	s.mu.Unlock()
	if fatal {
		return
	}
	if err := h.connectServer(ctx, s); err != nil {
		return // kept and logged by failedConnect
	}
	h.log.Info("mcp server reconnected", "server", s.cfg.Name, "reason", "home assistant connected")
	h.rebuild()
}

// Reconnect connects a skill's server anew after its connection changed:
// connected, it is connected at once; disconnected or expired, its session
// and tools are dropped.
func (h *Host) Reconnect(ctx context.Context, name string) {
	s := h.byName[name]
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.session != nil {
		_ = s.session.Close()
		s.session = nil
	}
	s.tools, s.missing, s.lastErr, s.failed, s.next = nil, nil, nil, 0, time.Time{}
	s.mu.Unlock()
	_ = h.connectServer(ctx, s) // a failure is logged where it is kept
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
