// Package transport is one connection to an MCP server
// (docs/spec/mcp-connections.md): the SDK's client session over HTTP or
// stdio, with its own state (open, retired, closed) and the calls that use
// it. It is closed only when nobody uses it any more, or at the stop of the
// service. It knows nothing of the host's servers, tools or prompts (MX-01).
package transport

import (
	"sync"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Conn is one connection.
type Conn struct {
	session *sdk.ClientSession

	mu      sync.Mutex
	uses    int
	retired bool
	closed  bool
	// byUs: Mira closed it (the stop of the service); a call that failed on
	// it is not repeated (decision 7).
	byUs  bool
	ended chan struct{}
}

// New wraps a connected session; Ended is closed once the session ends,
// by the server or by Mira.
func New(session *sdk.ClientSession) *Conn {
	c := &Conn{session: session, ended: make(chan struct{})}
	go func() {
		_ = session.Wait()
		close(c.ended)
	}()
	return c
}

// Use takes the connection for one call: the session and the release to
// call when the call is over. False when it is retired or closed: the
// caller takes the server's current connection instead.
func (c *Conn) Use() (session *sdk.ClientSession, release func(), ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.retired || c.closed {
		return nil, nil, false
	}
	c.uses++
	var once sync.Once
	return c.session, func() { once.Do(c.release) }, true
}

func (c *Conn) release() {
	c.mu.Lock()
	c.uses--
	closeNow := c.retired && c.uses == 0 && !c.closed
	if closeNow {
		c.closed = true
	}
	c.mu.Unlock()
	if closeNow {
		_ = c.session.Close()
	}
}

// Retire takes the connection out of use: no new call gets it, and it is
// closed when its last call ended (decision 4).
func (c *Conn) Retire() {
	c.mu.Lock()
	c.retired = true
	closeNow := c.uses == 0 && !c.closed
	if closeNow {
		c.closed = true
	}
	c.mu.Unlock()
	if closeNow {
		_ = c.session.Close()
	}
}

// Close closes it at once, at the stop of the service: calls on it fail and
// are not repeated (ClosedByUs).
func (c *Conn) Close() {
	c.mu.Lock()
	c.retired, c.byUs = true, true
	already := c.closed
	c.closed = true
	c.mu.Unlock()
	if !already {
		_ = c.session.Close()
	}
}

// ClosedByUs reports whether Mira closed it at once (Close).
func (c *Conn) ClosedByUs() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.byUs
}

// Ended is closed once the session ended.
func (c *Conn) Ended() <-chan struct{} { return c.ended }

// Uses is how many calls use it now (for tests).
func (c *Conn) Uses() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.uses
}

// Closed reports whether it is closed.
func (c *Conn) Closed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}
