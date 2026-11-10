package updates

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	haws "github.com/miraassist/mira/internal/ha/ws"
)

type fakeHA struct {
	mu       sync.Mutex
	commands []string
	known    string // "marketplace", "hacs" or ""
}

func (f *fakeHA) Call(_ context.Context, cmd map[string]any) (json.RawMessage, error) {
	typ := cmd["type"].(string)
	f.mu.Lock()
	f.commands = append(f.commands, strings.TrimSpace(typ+" "+fmtRepo(cmd)))
	f.mu.Unlock()
	if !strings.HasPrefix(typ, f.known+"/") || f.known == "" {
		return nil, &haws.Error{Command: typ, Code: "unknown_command", Message: "Unknown command."}
	}
	if strings.HasSuffix(typ, "/repositories/list") {
		return json.RawMessage(`[{"id":"7","full_name":"other/repo"},{"id":"1421679440","full_name":"miraassist/mira"}]`), nil
	}
	return json.RawMessage(`{}`), nil
}

func fmtRepo(cmd map[string]any) string {
	if r, ok := cmd["repository"]; ok {
		return r.(string)
	}
	return ""
}

func (f *fakeHA) got() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.commands...)
}

func wait(t *testing.T, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(2 * time.Second); !cond(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatal("timed out")
		}
	}
}

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func TestAnOlderIntegrationMakesTheMarketplaceLookOnce(t *testing.T) {
	ha := &fakeHA{known: "marketplace"}
	logs := &lockedBuf{}
	w := &Watcher{Version: "0.21.0", HA: ha, Log: slog.New(slog.NewTextHandler(logs, nil))}
	w.Seen(context.Background(), "0.20.0")
	w.Seen(context.Background(), "0.20.0")
	wait(t, func() bool { return strings.Contains(logs.String(), "asked for the integration's update") })
	want := []string{"marketplace/repositories/list", "marketplace/repository/refresh 1421679440"}
	if got := ha.got(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("commands = %v, want %v", got, want)
	}
}

func TestBefore202611HACSIsAsked(t *testing.T) {
	ha := &fakeHA{known: "hacs"}
	logs := &lockedBuf{}
	w := &Watcher{Version: "0.21.0", HA: ha, Log: slog.New(slog.NewTextHandler(logs, nil))}
	w.Seen(context.Background(), "v0.20.1")
	wait(t, func() bool { return strings.Contains(logs.String(), "via=hacs") })
	if got := ha.got(); got[len(got)-1] != "hacs/repository/refresh 1421679440" {
		t.Errorf("commands = %v", got)
	}
}

func TestNeitherCommandIsLogged(t *testing.T) {
	ha := &fakeHA{}
	logs := &lockedBuf{}
	w := &Watcher{Version: "0.21.0", HA: ha, Log: slog.New(slog.NewTextHandler(logs, nil))}
	w.Seen(context.Background(), "0.20.0")
	wait(t, func() bool { return strings.Contains(logs.String(), "could not ask for the integration's update") })
}

func TestTheSameOrANewerOrNoIntegrationAsksNothing(t *testing.T) {
	ha := &fakeHA{known: "marketplace"}
	w := &Watcher{Version: "0.21.0", HA: ha, Log: slog.Default()}
	for _, v := range []string{"", "0.21.3", "v0.21.0", "0.22.0", "garbage"} {
		w.Seen(context.Background(), v)
	}
	time.Sleep(30 * time.Millisecond)
	if got := ha.got(); len(got) != 0 {
		t.Errorf("commands = %v", got)
	}
}
