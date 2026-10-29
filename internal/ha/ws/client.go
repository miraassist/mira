package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// ErrNotConnected is returned when a call is attempted while the connection
// is down and the caller did not want to wait.
var ErrNotConnected = errors.New("home assistant websocket not connected")

// ErrNotFound: Home Assistant does not know what was asked for, such as an
// action of an integration that is not installed.
var ErrNotFound = errors.New("not found")

// EventHandler receives events of a subscription. It runs on the read loop,
// so it must not block; anything slow belongs on its own goroutine.
type EventHandler func(Event)

// subscription is a live or pending subscription. It is kept across
// reconnects and re-issued on the new connection.
type subscription struct {
	// command is the full subscribe command minus the id, so it can be
	// replayed verbatim after a reconnect.
	command map[string]any
	handler EventHandler
	// cancelled is set (under Client.mu) once the caller unsubscribed, so a
	// reconnect that is replaying subscriptions concurrently drops it.
	cancelled bool
}

type Options struct {
	// URL is the Home Assistant base URL, http(s) or ws(s).
	URL   string
	Token string
	// CallTimeout bounds a single command.
	CallTimeout time.Duration
	// ReconnectMin and ReconnectMax bound the backoff between attempts.
	ReconnectMin time.Duration
	ReconnectMax time.Duration
	Logger       *slog.Logger
	HTTPClient   *http.Client
	// OnConnect runs after every successful authentication, including after a
	// reconnect. The structure cache uses it to rebuild, because registry
	// changes may have happened while the connection was down.
	OnConnect func(ctx context.Context)
}

type Client struct {
	url         string
	token       string
	callTimeout time.Duration
	minBackoff  time.Duration
	maxBackoff  time.Duration
	log         *slog.Logger
	httpClient  *http.Client
	onConnect   func(context.Context)

	// sendMu keeps taking an id and sending it together: Home Assistant
	// refuses an id lower than one it has seen (id_reuse), so two calls
	// must not send in the opposite order of their ids. Taken before mu.
	sendMu    sync.Mutex
	mu        sync.Mutex
	conn      *websocket.Conn
	nextID    int
	pending   map[int]chan incoming
	subs      map[int]*subscription
	ready     chan struct{} // closed while connected, replaced on disconnect
	connected bool
}

func New(opts Options) (*Client, error) {
	wsURL, err := websocketURL(opts.URL)
	if err != nil {
		return nil, err
	}
	if opts.CallTimeout == 0 {
		opts.CallTimeout = 15 * time.Second
	}
	if opts.ReconnectMin == 0 {
		opts.ReconnectMin = time.Second
	}
	if opts.ReconnectMax == 0 {
		opts.ReconnectMax = 30 * time.Second
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Client{
		url:         wsURL,
		token:       opts.Token,
		callTimeout: opts.CallTimeout,
		minBackoff:  opts.ReconnectMin,
		maxBackoff:  opts.ReconnectMax,
		log:         opts.Logger.With("component", "ha-ws"),
		httpClient:  opts.HTTPClient,
		onConnect:   opts.OnConnect,
		nextID:      1,
		pending:     map[int]chan incoming{},
		subs:        map[int]*subscription{},
		ready:       make(chan struct{}),
	}, nil
}

// websocketURL turns a Home Assistant base URL into its /api/websocket
// endpoint, accepting http, https, ws and wss spellings.
func websocketURL(base string) (string, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return "", fmt.Errorf("home assistant url is empty")
	}
	switch {
	case strings.HasPrefix(base, "https://"):
		base = "wss://" + strings.TrimPrefix(base, "https://")
	case strings.HasPrefix(base, "http://"):
		base = "ws://" + strings.TrimPrefix(base, "http://")
	case strings.HasPrefix(base, "ws://"), strings.HasPrefix(base, "wss://"):
	default:
		return "", fmt.Errorf("home assistant url %q has no scheme", base)
	}
	if strings.HasSuffix(base, "/api/websocket") {
		return base, nil
	}
	return base + "/api/websocket", nil
}

// Run keeps the connection up until ctx is cancelled. It returns only when
// ctx ends; connection failures are retried with backoff.
func (c *Client) Run(ctx context.Context) error {
	backoff := c.minBackoff
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := c.session(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, errAuthFailed) {
			// A bad token will not fix itself; retrying forever only hides it.
			return err
		}
		c.log.Warn("home assistant connection lost, reconnecting", "error", err, "in", backoff)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > c.maxBackoff {
			backoff = c.maxBackoff
		}
	}
}

var errAuthFailed = errors.New("home assistant rejected the access token")

// session runs one connection from dial to disconnect.
func (c *Client) session(ctx context.Context) error {
	dialCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	conn, _, err := websocket.Dial(dialCtx, c.url, &websocket.DialOptions{HTTPClient: c.httpClient})
	cancel()
	if err != nil {
		return fmt.Errorf("dial %s: %w", c.url, err)
	}
	// Registry listings are large; the default read limit is too small.
	conn.SetReadLimit(32 << 20)
	defer conn.CloseNow()

	if err := c.authenticate(ctx, conn); err != nil {
		return err
	}

	c.mu.Lock()
	c.conn = conn
	c.connected = true
	ready := c.ready
	// Re-issue every subscription on the new connection. The handlers stay,
	// so callers never notice a reconnect beyond a gap in events.
	resubscribe := make([]*subscription, 0, len(c.subs))
	for _, sub := range c.subs {
		resubscribe = append(resubscribe, sub)
	}
	c.subs = map[int]*subscription{}
	c.mu.Unlock()
	close(ready) // unblock everyone waiting for a connection

	c.log.Info("connected to home assistant", "url", c.url, "subscriptions", len(resubscribe))

	// The read loop has to be running before anything is sent: every command
	// waits for a reply that only the read loop can deliver.
	readErr := make(chan error, 1)
	go func() { readErr <- c.readLoop(ctx, conn) }()

	for _, sub := range resubscribe {
		if err := c.resubscribe(ctx, sub); err != nil {
			c.log.Error("could not restore subscription", "error", err)
		}
	}
	if c.onConnect != nil {
		go c.onConnect(ctx)
	}

	err = <-readErr

	c.mu.Lock()
	c.conn = nil
	c.connected = false
	c.ready = make(chan struct{})
	// Fail every in-flight call rather than let it hang until its timeout.
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.mu.Unlock()
	return err
}

func (c *Client) authenticate(ctx context.Context, conn *websocket.Conn) error {
	authCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	var hello incoming
	if err := readJSON(authCtx, conn, &hello); err != nil {
		return fmt.Errorf("read auth challenge: %w", err)
	}
	if hello.Type != typeAuthRequired {
		return fmt.Errorf("unexpected first frame %q", hello.Type)
	}
	if err := writeJSON(authCtx, conn, map[string]any{
		"type": typeAuth, "access_token": c.token,
	}); err != nil {
		return fmt.Errorf("send auth: %w", err)
	}
	var result incoming
	if err := readJSON(authCtx, conn, &result); err != nil {
		return fmt.Errorf("read auth result: %w", err)
	}
	switch result.Type {
	case typeAuthOK:
		return nil
	case typeAuthInvalid:
		return fmt.Errorf("%w: %s", errAuthFailed, errorText(result.Error))
	default:
		return fmt.Errorf("unexpected auth response %q", result.Type)
	}
}

func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		var msg incoming
		if err := readJSON(ctx, conn, &msg); err != nil {
			return err
		}
		switch msg.Type {
		case typeResult, typePong:
			c.mu.Lock()
			ch, ok := c.pending[msg.ID]
			delete(c.pending, msg.ID)
			c.mu.Unlock()
			if ok {
				ch <- msg
				close(ch)
			}
		case typeEvent:
			c.mu.Lock()
			sub, ok := c.subs[msg.ID]
			c.mu.Unlock()
			if !ok {
				continue
			}
			var ev Event
			if err := json.Unmarshal(msg.Event, &ev); err != nil {
				c.log.Warn("undecodable event", "error", err)
				continue
			}
			ev.Raw = msg.Event
			sub.handler(ev)
		default:
			c.log.Debug("ignoring frame", "type", msg.Type)
		}
	}
}

// WaitReady blocks until the client is authenticated or ctx ends.
func (c *Client) WaitReady(ctx context.Context) error {
	c.mu.Lock()
	ready := c.ready
	connected := c.connected
	c.mu.Unlock()
	if connected {
		return nil
	}
	select {
	case <-ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Connected reports the current connection state.
func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// Call sends a command and waits for its result, and for a connection first
// if there is none.
func (c *Client) Call(ctx context.Context, command map[string]any) (json.RawMessage, error) {
	if err := c.WaitReady(ctx); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotConnected, err)
	}
	return c.callNow(ctx, command)
}

// callNow is Call without waiting for a connection: with none, it fails
// immediately.
func (c *Client) callNow(ctx context.Context, command map[string]any) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, c.callTimeout)
	defer cancel()

	c.sendMu.Lock()
	c.mu.Lock()
	conn := c.conn
	if conn == nil {
		c.mu.Unlock()
		c.sendMu.Unlock()
		return nil, ErrNotConnected
	}
	id := c.nextID
	c.nextID++
	ch := make(chan incoming, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	frame := make(map[string]any, len(command)+1)
	for k, v := range command {
		frame[k] = v
	}
	frame["id"] = id

	err := writeJSON(ctx, conn, frame)
	c.sendMu.Unlock()
	if err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("send %v: %w", command["type"], err)
	}

	select {
	case msg, ok := <-ch:
		if !ok {
			return nil, ErrNotConnected // connection dropped while waiting
		}
		if msg.Success != nil && !*msg.Success {
			if msg.Error != nil && msg.Error.Code == "not_found" {
				return nil, fmt.Errorf("home assistant rejected %v: %w: %s", command["type"], ErrNotFound, errorText(msg.Error))
			}
			return nil, fmt.Errorf("home assistant rejected %v: %s", command["type"], errorText(msg.Error))
		}
		return msg.Result, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("%v timed out: %w", command["type"], ctx.Err())
	}
}

// Subscribe registers a handler for an event type and returns a function that
// cancels the subscription. An empty eventType subscribes to all events.
func (c *Client) Subscribe(ctx context.Context, eventType string, handler EventHandler) (func(), error) {
	cmd := map[string]any{"type": CmdSubscribeEvents}
	if eventType != "" {
		cmd["event_type"] = eventType
	}
	return c.subscribeCommand(ctx, cmd, handler)
}

func (c *Client) subscribeCommand(ctx context.Context, cmd map[string]any, handler EventHandler) (func(), error) {
	sub := &subscription{command: cmd, handler: handler}
	if _, err := c.issueSubscribe(ctx, sub); err != nil {
		return nil, err
	}
	cancel := func() {
		// The id changes on every reconnect, so the subscription is found by
		// identity, not by the id it was first issued under.
		c.mu.Lock()
		sub.cancelled = true
		current, found := 0, false
		for sid, s := range c.subs {
			if s == sub {
				current, found = sid, true
				delete(c.subs, sid)
				break
			}
		}
		c.mu.Unlock()
		// Best effort, and never waiting for a connection: if it is gone, the
		// subscription went with it. Shutdown must not stall here.
		if !found {
			return
		}
		unsubCtx, done := context.WithTimeout(context.Background(), 2*time.Second)
		defer done()
		_, _ = c.callNow(unsubCtx, map[string]any{"type": CmdUnsubscribeEvents, "subscription": current})
	}
	return cancel, nil
}

func (c *Client) issueSubscribe(ctx context.Context, sub *subscription) (int, error) {
	if err := c.WaitReady(ctx); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrNotConnected, err)
	}
	ctx, cancel := context.WithTimeout(ctx, c.callTimeout)
	defer cancel()

	c.sendMu.Lock()
	c.mu.Lock()
	conn := c.conn
	if conn == nil {
		c.mu.Unlock()
		c.sendMu.Unlock()
		return 0, ErrNotConnected
	}
	if sub.cancelled {
		c.mu.Unlock()
		c.sendMu.Unlock()
		return 0, nil
	}
	id := c.nextID
	c.nextID++
	ch := make(chan incoming, 1)
	c.pending[id] = ch
	// Register the handler before the result arrives: Home Assistant may send
	// the first event immediately after acknowledging the subscription.
	c.subs[id] = sub
	c.mu.Unlock()

	frame := make(map[string]any, len(sub.command)+1)
	for k, v := range sub.command {
		frame[k] = v
	}
	frame["id"] = id

	err := writeJSON(ctx, conn, frame)
	c.sendMu.Unlock()
	if err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		delete(c.subs, id)
		c.mu.Unlock()
		return 0, err
	}
	select {
	case msg, ok := <-ch:
		if !ok {
			c.mu.Lock()
			delete(c.subs, id)
			c.mu.Unlock()
			return 0, ErrNotConnected
		}
		if msg.Success != nil && !*msg.Success {
			c.mu.Lock()
			delete(c.subs, id)
			c.mu.Unlock()
			return 0, fmt.Errorf("subscribe failed: %s", errorText(msg.Error))
		}
		return id, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		delete(c.subs, id)
		c.mu.Unlock()
		return 0, ctx.Err()
	}
}

func (c *Client) resubscribe(ctx context.Context, sub *subscription) error {
	_, err := c.issueSubscribe(ctx, sub)
	return err
}

// CallService invokes a Home Assistant service.
func (c *Client) CallService(ctx context.Context, domain, service string, data map[string]any, target *Target) error {
	cmd := map[string]any{"type": CmdCallService, "domain": domain, "service": service}
	if len(data) > 0 {
		cmd["service_data"] = data
	}
	if target != nil {
		cmd["target"] = target
	}
	_, err := c.Call(ctx, cmd)
	return err
}

// CallServiceResponse invokes a Home Assistant action that answers, and
// returns its response.
func (c *Client) CallServiceResponse(ctx context.Context, domain, service string, data map[string]any) (json.RawMessage, error) {
	cmd := map[string]any{"type": CmdCallService, "domain": domain, "service": service, "return_response": true}
	if len(data) > 0 {
		cmd["service_data"] = data
	}
	raw, err := c.Call(ctx, cmd)
	if err != nil {
		return nil, err
	}
	var result struct {
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decode %s.%s: %w", domain, service, err)
	}
	return result.Response, nil
}

// Areas, Devices and Entities read the registries.
func (c *Client) Areas(ctx context.Context) ([]Area, error) {
	return callList[Area](ctx, c, CmdAreaRegistryList)
}

func (c *Client) Devices(ctx context.Context) ([]Device, error) {
	return callList[Device](ctx, c, CmdDeviceRegistryList)
}

func (c *Client) Entities(ctx context.Context) ([]Entity, error) {
	return callList[Entity](ctx, c, CmdEntityRegistryList)
}

// Services lists the actions Home Assistant offers: domain -> action ->
// the names of its fields.
func (c *Client) Services(ctx context.Context) (map[string]map[string][]string, error) {
	raw, err := c.Call(ctx, map[string]any{"type": "get_services"})
	if err != nil {
		return nil, err
	}
	var all map[string]map[string]struct {
		Fields map[string]json.RawMessage `json:"fields"`
	}
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, fmt.Errorf("decode get_services: %w", err)
	}
	out := make(map[string]map[string][]string, len(all))
	for domain, services := range all {
		out[domain] = make(map[string][]string, len(services))
		for name, svc := range services {
			fields := make([]string, 0, len(svc.Fields))
			for f := range svc.Fields {
				fields = append(fields, f)
			}
			out[domain][name] = fields
		}
	}
	return out, nil
}

func callList[T any](ctx context.Context, c *Client, cmdType string) ([]T, error) {
	raw, err := c.Call(ctx, map[string]any{"type": cmdType})
	if err != nil {
		return nil, err
	}
	var out []T
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode %s: %w", cmdType, err)
	}
	return out, nil
}

// Ping checks a live connection.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.Call(ctx, map[string]any{"type": CmdPing})
	return err
}

func errorText(e *wsError) string {
	if e == nil {
		return "unknown error"
	}
	return fmt.Sprintf("%s (%s)", e.Message, e.Code)
}

func readJSON(ctx context.Context, conn *websocket.Conn, v any) error {
	_, data, err := conn.Read(ctx)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func writeJSON(ctx context.Context, conn *websocket.Conn, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, data)
}

// Endpoint is the resolved WebSocket URL, exposed for diagnostics and tests.
func (c *Client) Endpoint() string { return c.url }

// CurrentUser returns the id of the user the agent is logged in as. The
// Mira integration lets only this user manage Mira's automations.
func (c *Client) CurrentUser(ctx context.Context) (string, error) {
	raw, err := c.Call(ctx, map[string]any{"type": "auth/current_user"})
	if err != nil {
		return "", err
	}
	var u struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &u); err != nil {
		return "", err
	}
	return u.ID, nil
}
