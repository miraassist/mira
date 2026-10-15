package ui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sker65/mira/internal/store"
)

type fakeConfig struct {
	sections map[string]json.RawMessage
	over     map[string]bool
}

func (f *fakeConfig) Section(name string) (json.RawMessage, bool, error) {
	return f.sections[name], f.over[name], nil
}

func (f *fakeConfig) SetSections(_ context.Context, s map[string]json.RawMessage) error {
	for name, raw := range s {
		if strings.Contains(string(raw), "invalid") {
			return errors.New("notifier: quiet_hours invalid")
		}
		f.sections[name], f.over[name] = raw, true
	}
	return nil
}

func (f *fakeConfig) ResetSection(_ context.Context, name string) error {
	f.over[name] = false
	return nil
}

func setup(t *testing.T) (*httptest.Server, *fakeConfig, *store.Store) {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := &fakeConfig{sections: map[string]json.RawMessage{"notifier": json.RawMessage(`{"cooldown":"30m0s"}`)}, over: map[string]bool{}}
	h := New(Options{Token: "secret", Sections: []string{"notifier", "watchers", "persons", "satellites"}, Config: cfg, Store: st,
		Entities:       func() []Entity { return []Entity{{ID: "light.b"}, {ID: "light.a", Name: "A"}} },
		CompactProfile: func(context.Context) (bool, error) { return true, nil }})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, cfg, st
}

func call(t *testing.T, srv *httptest.Server, method, path, token, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return resp.StatusCode, b.String()
}

func TestPageIsPublicAPIIsNot(t *testing.T) {
	srv, _, _ := setup(t)
	if code, body := call(t, srv, "GET", "/ui", "", ""); code != 200 || !strings.Contains(body, "<title>Mira</title>") {
		t.Fatalf("page: %d", code)
	}
	if code, _ := call(t, srv, "GET", "/v1/ui/config", "", ""); code != 401 {
		t.Errorf("config without token: %d", code)
	}
	if code, _ := call(t, srv, "GET", "/v1/ui/config", "wrong", ""); code != 401 {
		t.Errorf("config with a wrong token: %d", code)
	}
}

func TestConfigEditing(t *testing.T) {
	srv, cfg, _ := setup(t)
	code, body := call(t, srv, "GET", "/v1/ui/config", "secret", "")
	if code != 200 || !strings.Contains(body, `"notifier":{"value":{"cooldown":"30m0s"},"overridden":false}`) {
		t.Fatalf("config: %d %s", code, body)
	}
	code, body = call(t, srv, "PUT", "/v1/ui/config", "secret", `{"notifier":{"cooldown":"10m"}}`)
	if code != 200 || !strings.Contains(body, `"overridden":true`) || !cfg.over["notifier"] {
		t.Fatalf("put: %d %s", code, body)
	}
	if code, body := call(t, srv, "PUT", "/v1/ui/config", "secret", `{"notifier":{"quiet_hours":"invalid"}}`); code != 422 || !strings.Contains(body, "quiet_hours") {
		t.Errorf("invalid config: %d %s", code, body)
	}
	if code, _ := call(t, srv, "DELETE", "/v1/ui/config/notifier", "secret", ""); code != 200 || cfg.over["notifier"] {
		t.Errorf("reset: %d", code)
	}
}

func TestNoticesAndMemory(t *testing.T) {
	srv, _, st := setup(t)
	ctx := context.Background()
	st.PutNotice(ctx, store.Notice{ID: "n1", Source: store.SourceExternal, Importance: 2, Spoken: "Test", ExpiresAt: time.Now().Add(time.Hour)})
	st.SaveFact(ctx, store.Fact{ID: "f1", Text: "Stefan trinkt Kaffee."})

	if code, body := call(t, srv, "GET", "/v1/ui/notices", "secret", ""); code != 200 || !strings.Contains(body, `"spoken":"Test"`) {
		t.Fatalf("notices: %d %s", code, body)
	}
	if code, _ := call(t, srv, "POST", "/v1/ui/notices/n1/ack", "secret", ""); code != 200 {
		t.Fatalf("ack: %d", code)
	}
	if n, _ := st.GetNotice(ctx, "n1"); n.State != store.NoticeAcked {
		t.Errorf("state = %s", n.State)
	}
	if code, body := call(t, srv, "GET", "/v1/ui/memory", "secret", ""); code != 200 || !strings.Contains(body, "Stefan trinkt Kaffee.") {
		t.Fatalf("memory: %d %s", code, body)
	}
	if code, _ := call(t, srv, "DELETE", "/v1/ui/memory/f1", "secret", ""); code != 200 {
		t.Fatalf("forget: %d", code)
	}
	if code, body := call(t, srv, "POST", "/v1/ui/memory/compact", "secret", ""); code != 200 || !strings.Contains(body, `"changed":true`) {
		t.Fatalf("compact: %d %s", code, body)
	}
	if code, body := call(t, srv, "GET", "/v1/ui/entities", "secret", ""); code != 200 || strings.Index(body, "light.a") > strings.Index(body, "light.b") {
		t.Errorf("entities not sorted: %s", body)
	}
}
