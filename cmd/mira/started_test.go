package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	haws "github.com/miraassist/mira/internal/ha/ws"
)

type firing struct {
	calls []map[string]any
	err   error
}

func (f *firing) Call(_ context.Context, cmd map[string]any) (json.RawMessage, error) {
	f.calls = append(f.calls, cmd)
	return json.RawMessage(`{}`), f.err
}

func TestBE22_MiraSaysSheStarted(t *testing.T) {
	ha := &firing{}
	log := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	s := startSignal{fire: func(ctx context.Context) { fireStarted(ctx, ha, "abc", "0.22.0", log) }}
	ctx := context.Background()
	s.up(ctx, true, false) // Home Assistant connected, the API not yet listening
	if len(ha.calls) != 0 {
		t.Fatal("fired before the API listens: the integration would find no Mira")
	}
	s.up(ctx, false, true)
	s.up(ctx, true, false) // a reconnect
	if len(ha.calls) != 1 {
		t.Fatalf("calls = %v", ha.calls)
	}
	c := ha.calls[0]
	data, _ := c["event_data"].(map[string]any)
	if c["type"] != "fire_event" || c["event_type"] != "mira_started" || data["id"] != "abc" || data["version"] != "0.22.0" {
		t.Errorf("call = %v", c)
	}
}

func TestBE23_AnEventHomeAssistantRefusesIsLoggedAndLeftToThePoll(t *testing.T) {
	var buf bytes.Buffer
	ha := &firing{err: &haws.Error{Command: "fire_event", Code: "unauthorized", Message: "Unauthorized"}}
	fireStarted(context.Background(), ha, "abc", "0.22.0", slog.New(slog.NewTextHandler(&buf, nil)))
	out := buf.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "next poll") || !strings.Contains(out, "unauthorized") {
		t.Errorf("log = %s", out)
	}
}
