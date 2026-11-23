package updates

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"

	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/testsupport/logcap"
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

func TestAnOlderIntegrationMakesTheMarketplaceLookOnce(t *testing.T) {
	ha := &fakeHA{known: "marketplace"}
	log, logs := logcap.Logger()
	w := &Watcher{Version: "0.21.0", HA: ha, Log: log}
	w.Seen(context.Background(), "0.20.0")
	w.Seen(context.Background(), "0.20.0")
	<-logs.Await("asked for the integration's update")
	want := []string{"marketplace/repositories/list", "marketplace/repository/refresh 1421679440"}
	if got := ha.got(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("commands = %v, want %v", got, want)
	}
}

func TestAPatchBehindAsksToo(t *testing.T) {
	ha := &fakeHA{known: "marketplace"}
	log, logs := logcap.Logger()
	w := &Watcher{Version: "0.22.1", HA: ha, Log: log}
	w.Seen(context.Background(), "0.22.0")
	<-logs.Await("asked for the integration's update")
}

func TestBefore202611HACSIsAsked(t *testing.T) {
	ha := &fakeHA{known: "hacs"}
	log, logs := logcap.Logger()
	w := &Watcher{Version: "0.21.0", HA: ha, Log: log}
	w.Seen(context.Background(), "v0.20.1")
	<-logs.Await("via=hacs")
	if got := ha.got(); got[len(got)-1] != "hacs/repository/refresh 1421679440" {
		t.Errorf("commands = %v", got)
	}
}

func TestNeitherCommandIsLogged(t *testing.T) {
	ha := &fakeHA{}
	log, logs := logcap.Logger()
	w := &Watcher{Version: "0.21.0", HA: ha, Log: log}
	w.Seen(context.Background(), "0.20.0")
	<-logs.Await("could not ask for the integration's update")
}

func TestTheSameOrANewerOrNoIntegrationAsksNothing(t *testing.T) {
	ha := &fakeHA{known: "marketplace"}
	w := &Watcher{Version: "0.21.0", HA: ha, Log: slog.Default()}
	for _, v := range []string{"", "0.21.3", "v0.21.0", "0.21", "0.22.0", "garbage"} {
		w.Seen(context.Background(), v) // nothing to ask is decided before anything runs
	}
	if got := ha.got(); len(got) != 0 {
		t.Errorf("commands = %v", got)
	}
}
