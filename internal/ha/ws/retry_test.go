package ws_test

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	haws "github.com/miraassist/mira/internal/ha/ws"
)

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// RY-43, docs/spec/retry.md step 5.
func TestRY43_HomeAssistantsConnectionWaitsGrowUpToTheLongest(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close() // nothing listens: every dial is refused
	logs := &syncBuf{}
	c, err := haws.New(haws.Options{URL: "ws://" + addr + "/api/websocket", Token: "t",
		Logger:       slog.New(slog.NewTextHandler(logs, nil)),
		ReconnectMin: 10 * time.Millisecond, ReconnectMax: 40 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	c.Run(ctx)
	var waits []string
	for _, line := range strings.Split(logs.String(), "\n") {
		if i := strings.Index(line, " in="); i >= 0 {
			waits = append(waits, strings.Fields(line[i+4:])[0])
		}
	}
	if len(waits) < 5 || waits[0] != "10ms" || waits[1] != "20ms" || waits[2] != "40ms" || waits[3] != "40ms" || waits[4] != "40ms" {
		t.Errorf("waits = %v, want 10ms 20ms 40ms 40ms …", waits)
	}
}
