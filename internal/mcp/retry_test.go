package mcp

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/clock"
	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/testsupport/fakemcp"
)

// Retries of MCP servers: docs/spec/retry.md, step 5.

type logBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuf) Write(p []byte) (int, error) { l.mu.Lock(); defer l.mu.Unlock(); return l.b.Write(p) }
func (l *logBuf) String() string              { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

// switchable answers with status while it is set, else passes the request
// to a working fake server; it counts the requests it refused.
type switchable struct {
	status  atomic.Int32
	refused atomic.Int32
	srv     *httptest.Server
}

func newSwitchable(t *testing.T, status int) *switchable {
	fake := fakemcp.New("x")
	t.Cleanup(fake.Close)
	t.Cleanup(fake.CloseClientConnections)
	target, _ := url.Parse(fake.Endpoint())
	proxy := httputil.NewSingleHostReverseProxy(target)
	// No connection of the proxy outlives a request: the fake server behind
	// it must close at the end of the test.
	proxy.Transport = &http.Transport{DisableKeepAlives: true}
	sw := &switchable{}
	sw.status.Store(int32(status))
	sw.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if st := sw.status.Load(); st != 0 {
			sw.refused.Add(1)
			w.WriteHeader(int(st))
			return
		}
		r.URL.Path = target.Path
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(sw.srv.Close)
	// A streaming connection the client left open never goes idle; the
	// server would wait for it for ever.
	t.Cleanup(sw.srv.CloseClientConnections)
	return sw
}

// retryEvery is the hosts' retry interval in these tests.
const retryEvery = 20 * time.Millisecond

func runHost(t *testing.T, endpoint string, logs *logBuf) (*Host, *clock.Fake) {
	clk := clock.NewFake(time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC))
	h := New(Options{Logger: slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		RetryInterval: retryEvery, CallTimeout: time.Second, Clock: clk,
		Servers: []config.MCPServer{{Name: "x", Transport: config.TransportHTTP, URL: endpoint, Token: "wrong"}}})
	ctx, cancel := context.WithCancel(context.Background())
	_ = h.Start(ctx)
	done := make(chan struct{})
	go func() { defer close(done); _ = h.Run(ctx) }()
	// Wait for Run before the fake server is closed: its ticker could else
	// connect once more after the sessions were closed, and the server
	// waits for that connection for ever.
	t.Cleanup(func() { cancel(); <-done; h.Close() })
	return h, clk
}

// rounds lets n retry rounds of Run pass. Run waits on two timers while
// idle (the retry and the refresh); during a round only on the refresh.
func rounds(clk *clock.Fake, n int) {
	for range n {
		<-clk.AwaitWaiters(2)
		clk.Advance(retryEvery)
		<-clk.AwaitWaiters(2) // the round is done: its retry is set anew
	}
}

// failures is how often the server's connect failed in a row.
func failures(s *server) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failed
}

func TestRY40_AnMCPServerThatRefusesTheTokenIsNotTriedAgain(t *testing.T) {
	sw := newSwitchable(t, http.StatusUnauthorized)
	logs := &logBuf{}
	h, clk := runHost(t, sw.srv.URL, logs)
	first := sw.refused.Load() // one connect (the SDK asks twice)
	rounds(clk, 10)
	if n := sw.refused.Load(); n != first || failures(h.byName["x"]) != 1 {
		t.Errorf("refused %d requests after %d at the start: tried again", n, first)
	}
	if out := logs.String(); strings.Count(out, "level=ERROR") != 1 || !strings.Contains(out, "refused the token (401") {
		t.Errorf("log = %s", out)
	}
	// Its connection changes: it is tried again, and connects.
	sw.status.Store(0)
	h.Reconnect(context.Background(), "x")
	if !h.byName["x"].connected() {
		t.Error("not connected after its connection changed")
	}
}

func TestRY41_AnUnreachableMCPServerIsTriedWithGrowingWaits(t *testing.T) {
	sw := newSwitchable(t, http.StatusServiceUnavailable)
	logs := &logBuf{}
	h, clk := runHost(t, sw.srv.URL, logs)
	rounds(clk, 16)
	// In 320 ms a fixed 20 ms would be 17 connects; doubling from 20 ms
	// connects at 0, 20, 60, 140 and 300 ms.
	if n := failures(h.byName["x"]); n != 5 {
		t.Errorf("%d connects in 320 ms, want 5", n)
	}
	if out := logs.String(); strings.Count(out, "level=WARN") != 1 {
		t.Errorf("want one warning for the same error, log = %s", out)
	}
}

func TestRY42_AReconnectedMCPServerStartsTheWaitsAnew(t *testing.T) {
	sw := newSwitchable(t, http.StatusServiceUnavailable)
	logs := &logBuf{}
	h, clk := runHost(t, sw.srv.URL, logs)
	rounds(clk, 1) // at 20 ms: the second failure
	s := h.byName["x"]
	if failed := failures(s); failed != 2 {
		t.Fatalf("failed = %d", failed)
	}
	sw.status.Store(0)
	rounds(clk, 2) // at 60 ms: due, and it connects
	s.mu.Lock()
	failed, next := s.failed, s.next
	s.mu.Unlock()
	if !s.connected() || failed != 0 || !next.IsZero() {
		t.Errorf("connected %v, failed %d, next %s", s.connected(), failed, next)
	}
}
