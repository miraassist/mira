package mcp

import (
	"context"
	"errors"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/mcp/transport"
	"github.com/miraassist/mira/internal/prompt"
	"github.com/miraassist/mira/internal/retry"
)

// The logical session of a server (docs/spec/mcp-connections.md): one loop
// per server alone changes which connection is current, its tools, its
// errors and waits. Calls do not go through it: they take the current
// connection (transport.Conn) and use it; only a call that failed on a lost
// session asks the loop for another one.

type server struct {
	cfg config.MCPServer
	h   *Host
	in  inbox
	// stopping is closed when the loop begins to stop, stopped when it
	// ended (Close).
	stopping, stopped chan struct{}

	// Only the loop writes these; readers take mu.
	mu      sync.Mutex
	conn    *transport.Conn
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
	// setup are the server's setup tools as it lists them, for the page
	// only (docs/spec/skill-programs.md).
	setup map[string]prompt.ToolDef
}

// inbox is a loop's queue: never full, so nobody waits to put something in.
type inbox struct {
	mu    sync.Mutex
	items []any
	ready chan struct{}
}

func (q *inbox) put(v any) {
	q.mu.Lock()
	q.items = append(q.items, v)
	q.mu.Unlock()
	select {
	case q.ready <- struct{}{}:
	default:
	}
}

func (q *inbox) take() []any {
	q.mu.Lock()
	defer q.mu.Unlock()
	items := q.items
	q.items = nil
	return items
}

// The loop's inputs.
type (
	// connectIn connects a server that has no connection (Start, Run,
	// Wake), or anew after its connection changed (fresh, Reconnect).
	connectIn struct {
		ctx         context.Context
		wake, fresh bool
		reply       chan error
	}
	// replaceIn: a call failed on a lost session (decision 6).
	replaceIn struct {
		ctx    context.Context
		failed *transport.Conn
		cause  error
		reply  chan *transport.Conn
	}
	// endedIn: a connection ended (the server dropped it).
	endedIn struct{ conn *transport.Conn }
	// refreshIn lists the tools again; reply may be nil.
	refreshIn struct {
		ctx   context.Context
		reply chan struct{}
	}
	stopIn struct{}
)

// errAlready: the server has a connection; errFatal: its last error heals
// only by a change of its connection or config.
var (
	errAlready = errors.New("already connected")
	errFatal   = errors.New("not tried: its error is fatal")
)

func newServer(h *Host, cfg config.MCPServer) *server {
	s := &server{cfg: cfg, h: h, in: inbox{ready: make(chan struct{}, 1)}, stopping: make(chan struct{}), stopped: make(chan struct{})}
	go s.run()
	return s
}

func (s *server) run() {
	defer close(s.stopped)
	for range s.in.ready {
		for _, v := range s.in.take() {
			if s.step(v) {
				return
			}
		}
	}
}

// step handles one input; true when the loop ends.
func (s *server) step(v any) bool {
	switch v := v.(type) {
	case connectIn:
		v.reply <- s.connectStep(v)
	case replaceIn:
		v.reply <- s.replace(v)
	case endedIn:
		s.mu.Lock()
		current := s.conn == v.conn
		if current {
			s.conn = nil
		}
		s.mu.Unlock()
		v.conn.Retire()
		if current {
			s.h.log.Warn("mcp server disconnected", "server", s.cfg.Name)
		}
	case refreshIn:
		s.refreshTools(v.ctx)
		if v.reply != nil {
			v.reply <- struct{}{}
		}
	case stopIn:
		close(s.stopping)
		s.mu.Lock()
		c := s.conn
		s.conn = nil
		s.mu.Unlock()
		if c != nil {
			c.Close()
		}
		return true
	}
	return false
}

func (s *server) connectStep(v connectIn) error {
	s.mu.Lock()
	current := s.conn
	fatal := s.lastErr != nil && !retry.Retryable(s.lastErr)
	switch {
	case v.fresh:
		// Its connection changed: the old one is retired, not closed, so
		// calls on it finish (decision 8).
		s.conn = nil
		s.tools, s.missing, s.lastErr, s.failed, s.next = nil, nil, nil, 0, time.Time{}
	case current != nil:
		s.mu.Unlock()
		return errAlready
	case v.wake && fatal:
		s.mu.Unlock()
		return errFatal
	case v.wake:
		s.failed, s.next = 0, time.Time{}
	}
	s.mu.Unlock()
	if v.fresh && current != nil {
		current.Retire()
	}
	return s.connect(v.ctx)
}

// replace gives a call that failed on a lost session a connection: the
// current one when another call replaced the failed one already; else the
// failed one is retired and the server connected once (decision 6). nil
// when the server stays away.
func (s *server) replace(v replaceIn) *transport.Conn {
	s.mu.Lock()
	current := s.conn
	if current != nil && current != v.failed {
		s.mu.Unlock()
		return current
	}
	s.conn = nil
	s.mu.Unlock()
	if current != nil {
		current.Retire()
	}
	s.h.log.Warn("mcp call failed on its session, connecting anew", "server", s.cfg.Name, "error", v.cause)
	if err := s.connect(v.ctx); err != nil {
		s.h.log.Warn("mcp server unreachable", "server", s.cfg.Name, "error", err)
		s.h.rebuild()
		return nil
	}
	s.h.rebuild()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn
}

// connect opens a connection and lists its tools; in the loop only.
func (s *server) connect(ctx context.Context) error {
	h := s.h
	client := sdk.NewClient(&sdk.Implementation{Name: "mira", Version: h.version}, &sdk.ClientOptions{
		Logger: h.log,
		ToolListChangedHandler: func(ctx context.Context, _ *sdk.ToolListChangedRequest) {
			h.log.Info("mcp server announced a tool list change", "server", s.cfg.Name)
			s.in.put(refreshIn{ctx: context.WithoutCancel(ctx)})
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
	c := transport.New(session)
	s.mu.Lock()
	s.conn = c
	s.tools, s.missing, s.unlisted = tools, missingTools(s.cfg, tools), unlisted
	s.lastErr, s.failed, s.next = nil, 0, time.Time{}
	s.mu.Unlock()
	h.log.Info("mcp server connected", "server", s.cfg.Name, "tools", len(tools))
	// Notice a dropped connection so Run can reconnect.
	go func() {
		<-c.Ended()
		s.in.put(endedIn{conn: c})
	}()
	return nil
}

// refreshTools lists the tools again on the current connection.
func (s *server) refreshTools(ctx context.Context) {
	session, release, _, ok := s.use()
	if !ok {
		return
	}
	defer release()
	cctx, cancel := context.WithTimeout(ctx, s.h.callTimeout)
	defer cancel()
	tools, unlisted, err := s.h.listTools(cctx, s.cfg, session)
	if err != nil {
		s.h.log.Warn("could not refresh tool list", "server", s.cfg.Name, "error", err)
		return
	}
	s.mu.Lock()
	s.tools, s.missing, s.unlisted = tools, missingTools(s.cfg, tools), unlisted
	s.mu.Unlock()
	s.h.rebuild()
}

// use takes the current connection for one call; false when there is none.
func (s *server) use() (*sdk.ClientSession, func(), *transport.Conn, bool) {
	for range 3 {
		s.mu.Lock()
		c := s.conn
		s.mu.Unlock()
		if c == nil {
			return nil, nil, nil, false
		}
		if session, release, ok := c.Use(); ok {
			return session, release, c, true
		}
		// Retired meanwhile: the current one again.
	}
	return nil, nil, nil, false
}

// ask puts a request and waits for its reply, or for the loop's end.
func ask[T any](s *server, put func(chan T) any) (T, bool) {
	reply := make(chan T, 1)
	s.in.put(put(reply))
	select {
	case v := <-reply:
		return v, true
	case <-s.stopped:
		var zero T
		return zero, false
	}
}

func (s *server) connectNow(ctx context.Context, wake, fresh bool) error {
	err, ok := ask(s, func(r chan error) any { return connectIn{ctx: ctx, wake: wake, fresh: fresh, reply: r} })
	if !ok {
		return errors.New("the mcp host is closed")
	}
	return err
}

func (s *server) replaced(ctx context.Context, failed *transport.Conn, cause error) *transport.Conn {
	c, _ := ask(s, func(r chan *transport.Conn) any {
		return replaceIn{ctx: context.WithoutCancel(ctx), failed: failed, cause: cause, reply: r}
	})
	return c
}

func (s *server) refreshNow(ctx context.Context) {
	ask(s, func(r chan struct{}) any { return refreshIn{ctx: ctx, reply: r} })
}

// stop closes its connection and ends the loop; it waits for that.
func (s *server) stop() {
	s.in.put(stopIn{})
	<-s.stopped
}

func (s *server) connected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn != nil
}

// retryable: a call that failed on a lost session, on a connection Mira did
// not close herself (decision 7).
func retryable(err error, c *transport.Conn) bool {
	return lostSession(err) && !c.ClosedByUs()
}
