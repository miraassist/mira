package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/mcp/transport"
	"github.com/miraassist/mira/internal/testsupport/fakemcp"
)

// gatedServer is a fake server with a tool "slow" that holds each call
// until the test lets it go; entered tells when a call reached it, runs
// counts its runs.
type gatedServer struct {
	*fakemcp.Server
	entered chan struct{}
	gate    chan struct{}
	runs    atomic.Int32
}

func newGated(t *testing.T) *gatedServer {
	t.Helper()
	g := &gatedServer{Server: fakemcp.HA("Küche: Licht an"), entered: make(chan struct{}, 4), gate: make(chan struct{})}
	g.AddTool("slow", "Setzt etwas auf die Liste", `{"type":"object","properties":{}}`, false, func(map[string]any) (string, bool) {
		g.runs.Add(1)
		g.entered <- struct{}{}
		<-g.gate
		return "erledigt", false
	})
	t.Cleanup(g.Close)
	// Let held calls go before the server closes.
	t.Cleanup(func() {
		select {
		case <-g.gate:
		default:
			close(g.gate)
		}
	})
	return g
}

func gatedHost(t *testing.T, g *gatedServer) (*Host, *server) {
	t.Helper()
	h := New(Options{Logger: slog.New(slog.DiscardHandler), Servers: []config.MCPServer{
		{Name: "ha", Transport: config.TransportHTTP, URL: g.Endpoint()},
	}})
	if err := h.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	return h, h.byName["ha"]
}

func current(s *server) *transport.Conn {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn
}

type callResult struct {
	res Result
	err error
}

// slowCall starts a call of "slow" and returns once it reached the server.
func slowCall(h *Host, g *gatedServer) chan callResult {
	done := make(chan callResult, 1)
	go func() {
		res, err := h.Call(context.Background(), "ha__slow", json.RawMessage(`{}`))
		done <- callResult{res, err}
	}()
	<-g.entered
	return done
}

// MX-02 A reconnect does not cut a call on the old connection: another call
// finds the session lost and replaces the connection; the call on the old
// one finishes there, its tool ran once, and the old one is closed after.
func TestMX02_AReconnectDoesNotCutACallOnTheOldConnection(t *testing.T) {
	g := newGated(t)
	h, s := gatedHost(t, g)
	old := current(s)
	running := slowCall(h, g)
	// When the server's connection is replaced: another call found the
	// session lost and asks the server's loop for a new one.
	if fresh := s.replaced(context.Background(), old, errors.New("the session is gone")); fresh == nil || fresh == old {
		t.Fatalf("replaced by %v", fresh)
	}
	if res, err := h.Call(context.Background(), "ha__GetLiveContext", nil); err != nil || res.Text != "Küche: Licht an" {
		t.Fatalf("the other call: %+v %v", res, err)
	}
	if current(s) == old {
		t.Fatal("the connection was not replaced")
	}
	if old.Closed() {
		t.Fatal("the old connection was closed under its call")
	}
	// Then the call finishes on the old connection, its tool ran once
	close(g.gate)
	if r := <-running; r.err != nil || r.res.Text != "erledigt" {
		t.Fatalf("the call: %+v %v", r.res, r.err)
	}
	if n := g.runs.Load(); n != 1 {
		t.Errorf("the tool ran %d times", n)
	}
	// And the old connection is closed once the call ended
	if !old.Closed() {
		t.Error("the old connection is still open")
	}
}

// MX-03 Two calls that fail on a lost session connect once.
func TestMX03_TwoCallsThatFailOnALostSessionConnectOnce(t *testing.T) {
	g := newGated(t)
	h, _ := gatedHost(t, g)
	before := g.Inits()
	g.ForgetSessions()
	done := make(chan callResult, 2)
	for range 2 {
		go func() {
			res, err := h.Call(context.Background(), "ha__GetLiveContext", nil)
			done <- callResult{res, err}
		}()
	}
	for range 2 {
		if r := <-done; r.err != nil || r.res.Text != "Küche: Licht an" {
			t.Fatalf("a call: %+v %v", r.res, r.err)
		}
	}
	if n := g.Inits() - before; n != 1 {
		t.Errorf("connected %d times", n)
	}
}

// MX-04 A call on a connection Mira closed is not repeated: the service
// stops while the call runs; whatever its answer, Mira does not connect
// again for it, and its tool ran once.
func TestMX04_ACallOnAConnectionMiraClosedIsNotRepeated(t *testing.T) {
	g := newGated(t)
	h, s := gatedHost(t, g)
	before := g.Inits()
	running := slowCall(h, g)
	// When the Mira service stops and closes it: the SDK's close waits for
	// the call already sent.
	closed := make(chan struct{})
	go func() { h.Close(); close(closed) }()
	<-s.stopping
	close(g.gate)
	<-running
	<-closed
	if c := current(s); c != nil {
		t.Errorf("a connection after the stop: %v", c)
	}
	if n := g.Inits() - before; n != 0 {
		t.Errorf("connected %d times after the stop", n)
	}
	if n := g.runs.Load(); n != 1 {
		t.Errorf("the tool ran %d times", n)
	}
}

// MX-05 A change of the server's connection lets running calls finish, and
// the next call goes to the new connection.
func TestMX05_AChangeOfTheServersConnectionLetsRunningCallsFinish(t *testing.T) {
	g := newGated(t)
	h, s := gatedHost(t, g)
	old := current(s)
	running := slowCall(h, g)
	// When the server's connection changes
	h.Reconnect(context.Background(), "ha")
	fresh := current(s)
	if fresh == nil || fresh == old {
		t.Fatalf("not connected anew: %v", fresh)
	}
	close(g.gate)
	if r := <-running; r.err != nil || r.res.Text != "erledigt" {
		t.Fatalf("the call: %+v %v", r.res, r.err)
	}
	if n := g.runs.Load(); n != 1 {
		t.Errorf("the tool ran %d times", n)
	}
	if !old.Closed() {
		t.Error("the old connection is still open")
	}
	before := fresh.Uses()
	if _, err := h.Call(context.Background(), "ha__GetLiveContext", nil); err != nil || fresh.Uses() != before {
		t.Errorf("the next call: %v", err)
	}
}
