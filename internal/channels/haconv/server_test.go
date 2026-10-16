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

	"github.com/sker65/mira/internal/config"
	"github.com/sker65/mira/internal/conversation"
	"github.com/sker65/mira/internal/store"
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
		Persons: func() map[string]config.Person { return map[string]config.Person{"ben": {}} },
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

// A Telegram chat of HA's integration comes through the adapter with the
// person it belongs to; the answer comes back as Telegram HTML.
func TestConversationFromTelegram(t *testing.T) {
	srv, conv, _ := setup(t)
	code, body := do(t, srv, "POST", "/v1/conversation", "secret",
		`{"text":"Hallo","conversation_id":"4711","language":"de","channel":"telegram","person":"ben"}`)
	if code != 200 || conv.got.Channel != config.ChannelTelegram || conv.got.Person != "ben" || conv.got.ConversationID != "4711" {
		t.Fatalf("response = %d %v, request %+v", code, body, conv.got)
	}
	if html, _ := body["html"].([]any); len(html) != 1 || html[0] != "Welches Licht?" {
		t.Errorf("html = %v", body["html"])
	}
	if code, _ := do(t, srv, "POST", "/v1/conversation", "secret", `{"text":"x","channel":"fax"}`); code != 400 {
		t.Errorf("unknown channel: %d", code)
	}
}

func TestConversationIsForwardedAsVoice(t *testing.T) {
	srv, conv, _ := setup(t)
	code, body := do(t, srv, "POST", "/v1/conversation", "secret",
		`{"text":"Licht an","conversation_id":"","device_id":"sat1","language":"de","extra_system_prompt":"Message X"}`)
	if code != 200 || body["text"] != "Welches Licht?" || body["continue_conversation"] != true || body["conversation_id"] != "c1" {
		t.Fatalf("response = %d %v", code, body)
	}
	if conv.got.Channel != config.ChannelVoice || conv.got.DeviceID != "sat1" || conv.got.Extra != "Message X" {
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
	if code != 200 || body["quiet_mode"] != true || body["pending_messages"] != float64(0) {
		t.Fatalf("status = %d %v", code, body)
	}
	if code, _ := do(t, srv, "PUT", "/v1/quiet_mode", "secret", `{}`); code != 400 {
		t.Errorf("missing enabled = %d", code)
	}
}

func TestMessagesAreDeduplicated(t *testing.T) {
	srv, _, _ := setup(t)
	body := `{"text":"Der Wechselrichter meldet einen Fehler.","importance":2,"dedupe_key":"inverter:fault"}`
	code1, r1 := do(t, srv, "POST", "/v1/messages", "secret", body)
	code2, r2 := do(t, srv, "POST", "/v1/messages", "secret", body)
	if code1 != 201 || code2 != 200 || r1["id"] != r2["id"] || r2["created"] != false {
		t.Fatalf("messages = %d %v / %d %v", code1, r1, code2, r2)
	}
	_, status := do(t, srv, "GET", "/v1/status", "secret", "")
	if status["pending_messages"] != float64(1) {
		t.Errorf("pending = %v", status["pending_messages"])
	}
	if code, _ := do(t, srv, "POST", "/v1/messages", "secret", `{"text":"x","importance":7}`); code != 400 {
		t.Errorf("bad importance = %d", code)
	}
}

func TestMessageReceiverAndConfidentiality(t *testing.T) {
	srv, _, st := setup(t)
	code, body := do(t, srv, "POST", "/v1/messages", "secret", `{"text":"Arzttermin morgen","receiver":"ben","confidentiality":"confidential"}`)
	if code != 201 {
		t.Fatalf("status %d %v", code, body)
	}
	n, _ := st.GetMessage(context.Background(), body["id"].(string))
	if n.Receiver != "ben" || !n.Confidential() {
		t.Errorf("message = %+v", n)
	}
	if code, _ := do(t, srv, "POST", "/v1/messages", "secret", `{"text":"x","receiver":"unbekannt"}`); code != 400 {
		t.Errorf("unknown receiver = %d", code)
	}
	if code, _ := do(t, srv, "POST", "/v1/messages", "secret", `{"text":"x","confidentiality":"geheim"}`); code != 400 {
		t.Errorf("unknown confidentiality = %d", code)
	}
}

// A fixed message from one of Mira's tasks counts as the task's and shows in
// its log.
func TestTaskMessageIsLogged(t *testing.T) {
	srv, _, st := setup(t)
	code, r := do(t, srv, "POST", "/v1/messages", "secret", `{"text":"Um 21:28 ging das Licht an.","importance":1,"task":"mira_licht"}`)
	if code != 201 {
		t.Fatalf("message: %d %v", code, r)
	}
	n, _ := st.GetMessage(context.Background(), r["id"].(string))
	if n.Source != store.SourceTask {
		t.Errorf("source = %s", n.Source)
	}
	log, _ := st.Assessments(context.Background(), "mira_licht", 5)
	if len(log) != 1 || log[0].Kind != store.AssessmentMessage || log[0].MessageID != n.ID {
		t.Errorf("log = %+v", log)
	}
}
