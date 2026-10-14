package haconv

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sker65/lares/internal/config"
	"github.com/sker65/lares/internal/conversation"
	"github.com/sker65/lares/internal/store"
)

type fakeConversation struct {
	got conversation.Request
}

func (f *fakeConversation) Handle(_ context.Context, req conversation.Request) (conversation.Response, error) {
	f.got = req
	return conversation.Response{Text: "Welches Licht?", ConversationID: "c1", ContinueConversation: true}, nil
}

func setup(t *testing.T) (*httptest.Server, *fakeConversation, *store.Store) {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	conv := &fakeConversation{}
	srv := httptest.NewServer(New(Options{
		Token: "secret", Conversation: conv, Store: st, Version: "test",
		Persons: func() map[string]config.Person { return map[string]config.Person{"stefan": {}} },
		Checks: map[string]HealthCheck{
			"llm": func(context.Context) error { return nil },
			"ha":  func(context.Context) error { return errors.New("not connected") },
		},
	}))
	t.Cleanup(srv.Close)
	return srv, conv, st
}

func do(t *testing.T, srv *httptest.Server, method, path, token, body string) (int, map[string]any) {
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
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func TestTokenIsRequired(t *testing.T) {
	srv, _, _ := setup(t)
	for _, tok := range []string{"", "wrong"} {
		if code, _ := do(t, srv, "GET", "/v1/health", tok, ""); code != http.StatusUnauthorized {
			t.Errorf("token %q: status %d", tok, code)
		}
	}
}

func TestHealthReportsDegradedDependencies(t *testing.T) {
	srv, _, _ := setup(t)
	code, body := do(t, srv, "GET", "/v1/health", "secret", "")
	if code != 200 || body["status"] != "degraded" {
		t.Fatalf("health = %d %v", code, body)
	}
	if checks := body["checks"].(map[string]any); checks["llm"] != "ok" || checks["ha"] != "not connected" {
		t.Errorf("checks = %v", checks)
	}
}

func TestConversationIsForwardedAsVoice(t *testing.T) {
	srv, conv, _ := setup(t)
	code, body := do(t, srv, "POST", "/v1/conversation", "secret",
		`{"text":"Licht an","conversation_id":"","device_id":"sat1","language":"de","extra_system_prompt":"Notice X"}`)
	if code != 200 || body["text"] != "Welches Licht?" || body["continue_conversation"] != true || body["conversation_id"] != "c1" {
		t.Fatalf("response = %d %v", code, body)
	}
	if conv.got.Channel != config.ChannelVoice || conv.got.DeviceID != "sat1" || conv.got.Extra != "Notice X" {
		t.Errorf("forwarded request = %+v", conv.got)
	}
	if code, _ := do(t, srv, "POST", "/v1/conversation", "secret", `{"text":"  "}`); code != 400 {
		t.Errorf("empty text status = %d", code)
	}
}

func TestQuietModeAndStatus(t *testing.T) {
	srv, _, _ := setup(t)
	if code, _ := do(t, srv, "PUT", "/v1/quiet_mode", "secret", `{"enabled":true}`); code != 200 {
		t.Fatalf("PUT quiet_mode = %d", code)
	}
	code, body := do(t, srv, "GET", "/v1/status", "secret", "")
	if code != 200 || body["quiet_mode"] != true || body["pending_notices"] != float64(0) {
		t.Fatalf("status = %d %v", code, body)
	}
	if code, _ := do(t, srv, "PUT", "/v1/quiet_mode", "secret", `{}`); code != 400 {
		t.Errorf("missing enabled = %d", code)
	}
}

func TestNoticesAreDeduplicated(t *testing.T) {
	srv, _, _ := setup(t)
	body := `{"text":"Der Wechselrichter meldet einen Fehler.","importance":2,"dedupe_key":"inverter:fault"}`
	code1, r1 := do(t, srv, "POST", "/v1/notices", "secret", body)
	code2, r2 := do(t, srv, "POST", "/v1/notices", "secret", body)
	if code1 != 201 || code2 != 200 || r1["id"] != r2["id"] || r2["created"] != false {
		t.Fatalf("notices = %d %v / %d %v", code1, r1, code2, r2)
	}
	_, status := do(t, srv, "GET", "/v1/status", "secret", "")
	if status["pending_notices"] != float64(1) {
		t.Errorf("pending = %v", status["pending_notices"])
	}
	if code, _ := do(t, srv, "POST", "/v1/notices", "secret", `{"text":"x","importance":7}`); code != 400 {
		t.Errorf("bad importance = %d", code)
	}
}

func TestNoticeAudienceAndConfidentiality(t *testing.T) {
	srv, _, st := setup(t)
	code, body := do(t, srv, "POST", "/v1/notices", "secret", `{"text":"Arzttermin morgen","audience":"stefan","confidentiality":"confidential"}`)
	if code != 201 {
		t.Fatalf("status %d %v", code, body)
	}
	n, _ := st.GetNotice(context.Background(), body["id"].(string))
	if n.Audience != "stefan" || !n.Confidential() {
		t.Errorf("notice = %+v", n)
	}
	if code, _ := do(t, srv, "POST", "/v1/notices", "secret", `{"text":"x","audience":"unbekannt"}`); code != 400 {
		t.Errorf("unknown audience = %d", code)
	}
	if code, _ := do(t, srv, "POST", "/v1/notices", "secret", `{"text":"x","confidentiality":"geheim"}`); code != 400 {
		t.Errorf("unknown confidentiality = %d", code)
	}
}
