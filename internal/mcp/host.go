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
	"sort"
	"strings"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/sker65/lares/internal/config"
	"github.com/sker65/lares/internal/prompt"
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
}

type server struct {
	cfg config.MCPServer

	mu      sync.Mutex
	session *sdk.ClientSession
	tools   []Tool // last known, kept while disconnected
	lastErr error
}

// Host owns the server connections.
type Host struct {
	log         *slog.Logger
	servers     []*server
	byName      map[string]*server
	onChange    func([]Tool)
	readOnly    map[string]bool
	retry       time.Duration
	callTimeout time.Duration
	connect     Connector
	version     string

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
	if opts.Connector == nil {
		opts.Connector = DialTransport
	}
	if opts.Version == "" {
		opts.Version = "dev"
	}
	h := &Host{
		log:         opts.Logger.With("component", "mcp"),
		byName:      map[string]*server{},
		onChange:    opts.OnChange,
		readOnly:    map[string]bool{},
		retry:       opts.RetryInterval,
		callTimeout: opts.CallTimeout,
		connect:     opts.Connector,
		version:     opts.Version,
		toolIndex:   map[string]Tool{},
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
			if err := h.connectServer(ctx, s); err != nil {
				h.log.Warn("mcp server unreachable", "server", s.cfg.Name, "error", err)
			}
		}()
	}
	wg.Wait()
	h.rebuild()

	if len(h.servers) == 0 {
		return nil
	}
	for _, s := range h.servers {
		if s.connected() {
			return nil
		}
	}
	return fmt.Errorf("none of %d mcp servers is reachable", len(h.servers))
}

// Run retries unreachable servers until ctx ends.
func (h *Host) Run(ctx context.Context) error {
	t := time.NewTicker(h.retry)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			h.closeAll()
			return ctx.Err()
		case <-t.C:
			changed := false
			for _, s := range h.servers {
				if s.connected() {
					continue
				}
				if err := h.connectServer(ctx, s); err != nil {
					h.log.Debug("mcp server still unreachable", "server", s.cfg.Name, "error", err)
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
	client := sdk.NewClient(&sdk.Implementation{Name: "lares", Version: h.version}, &sdk.ClientOptions{
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
		s.mu.Lock()
		s.lastErr = err
		s.mu.Unlock()
		return err
	}
	tools, err := h.listTools(cctx, s.cfg, session)
	if err != nil {
		_ = session.Close()
		s.mu.Lock()
		s.lastErr = err
		s.mu.Unlock()
		return err
	}
	s.mu.Lock()
	s.session = session
	s.tools = tools
	s.lastErr = nil
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

func (h *Host) refresh(ctx context.Context, s *server) {
	s.mu.Lock()
	session := s.session
	s.mu.Unlock()
	if session == nil {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, h.callTimeout)
	defer cancel()
	tools, err := h.listTools(cctx, s.cfg, session)
	if err != nil {
		h.log.Warn("could not refresh tool list", "server", s.cfg.Name, "error", err)
		return
	}
	s.mu.Lock()
	s.tools = tools
	s.mu.Unlock()
	h.rebuild()
}

func (h *Host) listTools(ctx context.Context, cfg config.MCPServer, session *sdk.ClientSession) ([]Tool, error) {
	var out []Tool
	for t, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("list tools: %w", err)
		}
		schema, err := json.Marshal(t.InputSchema)
		if err != nil || string(schema) == "null" {
			schema = []byte(`{"type":"object","properties":{}}`)
		}
		def := prompt.ToolDef{Server: cfg.Name, Name: t.Name, Description: t.Description, Schema: schema}
		readOnly := cfg.ReadOnly || h.readOnly[def.QualifiedName()] ||
			(t.Annotations != nil && t.Annotations.ReadOnlyHint)
		out = append(out, Tool{ToolDef: def, ReadOnly: readOnly, WebContent: cfg.WebContent})
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

	cctx, cancel := context.WithTimeout(ctx, h.callTimeout)
	defer cancel()
	res, err := session.CallTool(cctx, &sdk.CallToolParams{Name: toolName, Arguments: arguments})
	if err != nil {
		return Result{}, fmt.Errorf("call %s: %w", qualified, err)
	}
	return Result{Text: flatten(res), IsError: res.IsError}, nil
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
}

func (h *Host) Status() []Status {
	out := make([]Status, 0, len(h.servers))
	for _, s := range h.servers {
		s.mu.Lock()
		st := Status{Name: s.cfg.Name, Connected: s.session != nil, Tools: len(s.tools)}
		if s.lastErr != nil {
			st.Error = s.lastErr.Error()
		}
		s.mu.Unlock()
		out = append(out, st)
	}
	return out
}

// DialTransport connects with the transport the config names.
func DialTransport(ctx context.Context, client *sdk.Client, cfg config.MCPServer) (*sdk.ClientSession, error) {
	var transport sdk.Transport
	switch cfg.Transport {
	case config.TransportHTTP:
		hc := &http.Client{}
		if cfg.Token != "" {
			hc.Transport = bearer{token: cfg.Token, next: http.DefaultTransport}
		}
		transport = &sdk.StreamableClientTransport{Endpoint: cfg.URL, HTTPClient: hc}
	case config.TransportStdio:
		cmd := exec.Command(cfg.Command, cfg.Args...)
		cmd.Env = append(os.Environ(), cfg.Env...)
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
