package haconv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/conversation"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/store/storetest"
	"github.com/miraassist/mira/internal/tasks"
	"github.com/miraassist/mira/internal/watch"
)

type fakeConversation struct {
	got conversation.Request
	err error
}

func (f *fakeConversation) Handle(_ context.Context, req conversation.Request) (conversation.Response, error) {
	f.got = req
	if f.err != nil {
		return conversation.Response{}, f.err
	}
	if req.Stream != nil {
		req.Stream(conversation.Piece{Text: "Welches "})
		req.Stream(conversation.Piece{Text: "Licht?"})
	}
	return conversation.Response{Text: "Welches Licht?", ConversationID: "c1", ContinueConversation: true}, nil
}

func setup(t *testing.T) (*httptest.Server, *fakeConversation, *store.Store) {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{Path: storetest.Path(t)})
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

// A model that failed for its key: Mira's sentence is the turn's answer
// (docs/spec/models.md, When a model fails, MO-50); any other failure stays
// the error Home Assistant answers with its own sentence (MO-53).
func TestAFailedModelIsSaidAsTheAnswer(t *testing.T) {
	srv, conv, _ := setup(t)
	conv.err = &conversation.TurnError{Say: "Ich erreiche mein KI-Modell gerade nicht.", Err: errors.New("connection refused")}
	code, body := do(t, srv, "POST", "/v1/conversation", "secret", `{"text":"Licht an","conversation_id":"c7","language":"de"}`)
	if code != 200 || body["text"] != "Ich erreiche mein KI-Modell gerade nicht." || body["continue_conversation"] == true {
		t.Fatalf("response = %d %v", code, body)
	}
	conv.err = errors.New("llm server returned 500")
	if code, _ := do(t, srv, "POST", "/v1/conversation", "secret", `{"text":"Licht an"}`); code != 502 {
		t.Errorf("another failure: %d", code)
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

type fakeTasks struct {
	got   tasks.Request
	err   error
	check tasks.CheckCall
	ans   tasks.CheckAnswer
}

func (f *fakeTasks) Check(_ context.Context, c tasks.CheckCall) (tasks.CheckAnswer, error) {
	f.check = c
	return f.ans, f.err
}

func (f *fakeTasks) Assess(context.Context, tasks.Request) (tasks.Verdict, error) {
	return tasks.Verdict{}, nil
}

func (f *fakeTasks) Decide(_ context.Context, r tasks.Request) (tasks.Decision, error) {
	f.got = r
	return tasks.Decision{Answer: true, Reason: "Niemand ist zu Hause."}, f.err
}

// mira.decide as the integration sends it: the action's fields as they
// are, the answer back as {answer, reason}.
func TestDecideAnswersYesOrNo(t *testing.T) {
	ft := &fakeTasks{}
	srv := httptest.NewServer(New(Options{Token: "secret", Tasks: ft, Version: "test"}))
	t.Cleanup(srv.Close)

	code, out := do(t, srv, "POST", "/v1/decide", "secret",
		`{"question":"Ist jemand zu Hause?","data":[{"entity_id":"person.anna","view":"state"}],"task":"mira_heizung"}`)
	if code != http.StatusOK || out["answer"] != true || out["reason"] != "Niemand ist zu Hause." {
		t.Fatalf("%d %v", code, out)
	}
	if ft.got.Question != "Ist jemand zu Hause?" || ft.got.Task != "mira_heizung" ||
		len(ft.got.Data) != 1 || ft.got.Data[0].EntityID != "person.anna" {
		t.Errorf("request = %+v", ft.got)
	}

	ft.err = errors.New("question is required")
	if code, out := do(t, srv, "POST", "/v1/decide", "secret", `{}`); code != http.StatusBadRequest || out["error"] == nil {
		t.Errorf("a failed decision: %d %v", code, out)
	}

	bare := httptest.NewServer(New(Options{Token: "secret", Version: "test"}))
	t.Cleanup(bare.Close)
	if code, _ := do(t, bare, "POST", "/v1/decide", "secret", `{"question":"q"}`); code != http.StatusServiceUnavailable {
		t.Errorf("without Home Assistant: %d", code)
	}
}

// A voice turn asked with stream: one line per piece, then the whole answer
// as without it (docs/spec/streaming.md).
func TestConversationStreamsLines(t *testing.T) {
	srv, _, _ := setup(t)
	req, _ := http.NewRequest("POST", srv.URL+"/v1/conversation",
		strings.NewReader(`{"text":"Mach das Licht an","conversation_id":"c1","stream":true}`))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if resp.Header.Get("Content-Type") != "application/x-ndjson" || len(lines) != 3 {
		t.Fatalf("%s: %q", resp.Header.Get("Content-Type"), raw)
	}
	if lines[0] != `{"delta":"Welches "}` || lines[1] != `{"delta":"Licht?"}` {
		t.Fatalf("pieces %q", lines[:2])
	}
	var last map[string]any
	if err := json.Unmarshal([]byte(lines[2]), &last); err != nil || last["text"] != "Welches Licht?" ||
		last["conversation_id"] != "c1" || last["continue_conversation"] != true {
		t.Fatalf("last line %q", lines[2])
	}
}

// ST-09: an integration that does not ask for the stream gets one JSON
// object, as before.
func TestST09_AnOlderIntegrationGetsTheWholeAnswer(t *testing.T) {
	srv, conv, _ := setup(t)
	code, out := do(t, srv, "POST", "/v1/conversation", "secret", `{"text":"Mach das Licht an","conversation_id":"c1"}`)
	if code != http.StatusOK || out["text"] != "Welches Licht?" || conv.got.Stream != nil {
		t.Fatalf("%d %v", code, out)
	}
}

// Telegram is never streamed, also when asked (decision 6).
func TestTelegramIsNotStreamed(t *testing.T) {
	srv, conv, _ := setup(t)
	code, out := do(t, srv, "POST", "/v1/conversation", "secret", `{"text":"Hallo","conversation_id":"4711","channel":"telegram","stream":true}`)
	if code != http.StatusOK || out["text"] != "Welches Licht?" || conv.got.Stream != nil {
		t.Fatalf("%d %v", code, out)
	}
}

// endedConversation answers like the engine when its caller ended the turn.
type endedConversation struct{}

func (endedConversation) Handle(ctx context.Context, _ conversation.Request) (conversation.Response, error) {
	<-ctx.Done()
	return conversation.Response{}, ctx.Err()
}

// ST-18: a turn Home Assistant ended is no error of the service either.
func TestST18_AnEndedTurnIsNoConversationError(t *testing.T) {
	logs := &strings.Builder{}
	h := New(Options{Token: "secret", Conversation: endedConversation{}, Version: "test",
		Logger: slog.New(slog.NewTextHandler(logs, nil))})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("POST", "/v1/conversation", strings.NewReader(`{"text":"Erzähl was","channel":"voice"}`)).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer secret")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if strings.Contains(logs.String(), "conversation failed") {
		t.Errorf("logged as an error:\n%s", logs)
	}
}

// receiver "persons": a private message of the house, each call its own;
// refused while no person has a channel (docs/spec/group-watch.md, decision 20).
func TestMessageForEveryPerson(t *testing.T) {
	srv, _, _ := setup(t)
	if code, body := do(t, srv, "POST", "/v1/messages", "secret", `{"text":"x","receiver":"persons"}`); code != 400 ||
		!strings.Contains(fmt.Sprint(body["error"]), "no person has a channel") {
		t.Errorf("without channels: %d %v", code, body)
	}
	st2, err := store.Open(context.Background(), store.Options{Path: storetest.Path(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st2.Close() })
	srv2 := httptest.NewServer(New(Options{Token: "secret", Store: st2, Version: "test",
		Persons: func() map[string]config.Person {
			return map[string]config.Person{"ben": {Channels: []string{"notify.ben"}}, "anna": {}}
		}}))
	t.Cleanup(srv2.Close)
	body := `{"text":"Niemand ist zu Hause, aber das Fenster ist offen.","receiver":"persons","importance":2,"dedupe_key":"watch:mira_x","task":"mira_x"}`
	code, r := do(t, srv2, "POST", "/v1/messages", "secret", body)
	code2, r2 := do(t, srv2, "POST", "/v1/messages", "secret", body)
	if code != 201 || code2 != 201 || r["id"] == r2["id"] {
		t.Fatalf("%d %v / %d %v", code, r, code2, r2)
	}
	n, _ := st2.GetMessage(context.Background(), r["id"].(string))
	if n.Kind != store.KindPrivate || n.Receiver != store.ReceiverHouse || n.Importance != 2 || n.Source != store.SourceTask {
		t.Errorf("message %+v", n)
	}
}

// mira.check as the integration sends it: the action's fields, the result
// back; a request naming what does not exist is 400, Home Assistant failing
// to render 502, both with the reason.
func TestCheckRoute(t *testing.T) {
	ft := &fakeTasks{ans: tasks.CheckAnswer{CheckResult: watch.CheckResult{Scope: "openings", Checked: 9,
		Open: []watch.Member{{ID: "binary_sensor.fenster_bad", Name: "Fenster Bad", State: "on", Area: "Bad"}}}, MessageID: "m1"}}
	srv := httptest.NewServer(New(Options{Token: "secret", Tasks: ft, Version: "test"}))
	t.Cleanup(srv.Close)
	code, body := do(t, srv, "POST", "/v1/check", "secret", `{"scope":"openings","floors":["dachgeschoss"],"receiver":"anna","notify":false}`)
	if code != 200 || body["checked"] != 9.0 || body["message_id"] != "m1" || fmt.Sprint(body["open"]) == "[]" {
		t.Fatalf("%d %v", code, body)
	}
	if ft.check.Scope != "openings" || ft.check.Floors[0] != "dachgeschoss" || ft.check.Receiver != "anna" || ft.check.Notify == nil || *ft.check.Notify {
		t.Errorf("call %+v", ft.check)
	}
	ft.err = watch.ErrBadCheck{Reason: `eine Etage "turm" gibt es nicht`}
	if code, body := do(t, srv, "POST", "/v1/check", "secret", `{"floors":["turm"]}`); code != 400 || !strings.Contains(fmt.Sprint(body["error"]), "turm") {
		t.Errorf("%d %v", code, body)
	}
	ft.err = errors.New("home assistant rejected render_template: boom (template_error)")
	if code, body := do(t, srv, "POST", "/v1/check", "secret", `{}`); code != 502 || !strings.Contains(fmt.Sprint(body["error"]), "boom") {
		t.Errorf("%d %v", code, body)
	}
}
