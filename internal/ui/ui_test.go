package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sker65/mira/internal/store"
	"github.com/sker65/mira/internal/tasks"
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
	h := New(Options{Token: "secret", Sections: []string{"notifier", "persons", "satellites"}, Config: cfg, Store: st,
		Entities:       func() []Entity { return []Entity{{ID: "light.b"}, {ID: "light.a", Name: "A"}} },
		CompactProfile: func(context.Context) (bool, error) { return true, nil },
		Persons:        func() []string { return []string{"anna", "ben"} },
		PersonOfHAUser: func(_ context.Context, user string) string { return map[string]string{"u-anna": "anna"}[user] }})
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
	st.SaveFact(ctx, store.Fact{ID: "f1", Text: "Ben trinkt Kaffee."})

	if code, body := call(t, srv, "GET", "/v1/ui/notices", "secret", ""); code != 200 || !strings.Contains(body, `"spoken":"Test"`) {
		t.Fatalf("notices: %d %s", code, body)
	}
	if code, _ := call(t, srv, "POST", "/v1/ui/notices/n1/ack", "secret", ""); code != 200 {
		t.Fatalf("ack: %d", code)
	}
	if n, _ := st.GetNotice(ctx, "n1"); n.State != store.NoticeAcked {
		t.Errorf("state = %s", n.State)
	}
	// Done notices stay in the history, with their state.
	if _, body := call(t, srv, "GET", "/v1/ui/notices", "secret", ""); !strings.Contains(body, `"state":"acked"`) {
		t.Errorf("acked notice left the list: %s", body)
	}
	if code, body := call(t, srv, "GET", "/v1/ui/memory", "secret", ""); code != 200 || !strings.Contains(body, "Ben trinkt Kaffee.") {
		t.Fatalf("memory: %d %s", code, body)
	}
	// Every person has a memory card, even an empty one.
	if _, body := call(t, srv, "GET", "/v1/ui/memory", "secret", ""); !strings.Contains(body, `"owner":"anna","name":"anna","profile":"","facts":[]`) {
		t.Errorf("anna's empty memory missing: %s", body)
	}
	if code, _ := call(t, srv, "PUT", "/v1/ui/memory/f1", "secret", `{"text":"Ben trinkt Tee."}`); code != 200 {
		t.Fatalf("edit: %d", code)
	}
	if _, body := call(t, srv, "GET", "/v1/ui/memory", "secret", ""); !strings.Contains(body, "Ben trinkt Tee.") || strings.Contains(body, "Kaffee") {
		t.Fatalf("edited fact: %s", body)
	}
	if code, _ := call(t, srv, "PUT", "/v1/ui/memory/nope", "secret", `{"text":"x"}`); code != 404 {
		t.Fatalf("edit unknown: %d", code)
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

func TestGuideIsPublic(t *testing.T) {
	srv, _, _ := setup(t)
	for lang, want := range map[string]string{"de": "# Mira – Anleitung", "en": "# Mira – Guide"} {
		code, body := call(t, srv, "GET", "/ui/guide/"+lang, "", "")
		if code != 200 || !strings.HasPrefix(body, want) {
			t.Errorf("%s: %d %.40q", lang, code, body)
		}
	}
}

// asHA calls like the Home Assistant integration's proxy does: with the
// token and the signed-in HA user.
func asHA(t *testing.T, srv *httptest.Server, method, path, user string, admin bool, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set(HeaderHAUser, user)
	if admin {
		req.Header.Set(HeaderHAAdmin, "1")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// Inside Home Assistant everyone, admins too, sees the house's memory and
// their own; only admins see the rest of the page.
func TestMemoryInsideHomeAssistant(t *testing.T) {
	srv, _, st := setup(t)
	ctx := context.Background()
	st.SaveFact(ctx, store.Fact{ID: "h1", Text: "Die Tonne steht hinter der Garage."})
	st.SaveFact(ctx, store.Fact{ID: "a1", Owner: "anna", Text: "Anna mag keinen Koriander."})
	st.SaveFact(ctx, store.Fact{ID: "b1", Owner: "ben", Text: "Ben hat Chorprobe."})

	for _, admin := range []bool{false, true} {
		code, body := asHA(t, srv, "GET", "/v1/ui/memory", "u-anna", admin, "")
		if code != 200 || !strings.Contains(body, "Tonne") || !strings.Contains(body, "Koriander") || strings.Contains(body, "Chorprobe") {
			t.Fatalf("admin=%v: %d %s", admin, code, body)
		}
		if !strings.Contains(body, `"you":"anna"`) {
			t.Errorf("admin=%v: own person missing: %s", admin, body)
		}
		if strings.Contains(body, `"can_compact":true`) != admin {
			t.Errorf("admin=%v: can_compact in %s", admin, body)
		}
	}
	// A HA user without a person sees the house only, and is told why.
	if _, body := asHA(t, srv, "GET", "/v1/ui/memory", "u-other", false, ""); strings.Contains(body, "Koriander") || !strings.Contains(body, `"unknown_person":true`) {
		t.Errorf("unknown user: %s", body)
	}
	// Ben's facts are out of reach, as if they did not exist.
	if code, _ := asHA(t, srv, "PUT", "/v1/ui/memory/b1", "u-anna", true, `{"text":"x"}`); code != 404 {
		t.Errorf("edit ben's fact: %d", code)
	}
	if code, _ := asHA(t, srv, "DELETE", "/v1/ui/memory/b1", "u-anna", true, ""); code != 404 {
		t.Errorf("forget ben's fact: %d", code)
	}
	if code, _ := asHA(t, srv, "PUT", "/v1/ui/memory/a1", "u-anna", false, `{"text":"Anna mag Koriander doch."}`); code != 200 {
		t.Errorf("edit own fact: %d", code)
	}
	if code, _ := asHA(t, srv, "DELETE", "/v1/ui/memory/h1", "u-anna", false, ""); code != 200 {
		t.Errorf("forget a house fact: %d", code)
	}
	// The rest of the page is for admins.
	for _, path := range []string{"/v1/ui/config", "/v1/ui/notices", "/v1/ui/tasks"} {
		if code, _ := asHA(t, srv, "GET", path, "u-anna", false, ""); code != 403 {
			t.Errorf("%s for a non-admin: %d", path, code)
		}
	}
	if code, _ := asHA(t, srv, "POST", "/v1/ui/memory/compact", "u-anna", false, ""); code != 403 {
		t.Errorf("compact for a non-admin: %d", code)
	}
	if code, _ := asHA(t, srv, "GET", "/v1/ui/config", "u-anna", true, ""); code != 200 {
		t.Errorf("config for an admin: %d", code)
	}
}

// Guide files come in ranges, as Safari wants for videos.
func TestGuideFilesInRanges(t *testing.T) {
	srv, _, _ := setup(t)
	req, _ := http.NewRequest("GET", srv.URL+"/ui/guide/img/aufgaben-liste-de.png", nil)
	req.Header.Set("Range", "bytes=0-7")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || resp.Header.Get("Content-Type") != "image/png" ||
		!strings.HasPrefix(resp.Header.Get("Content-Range"), "bytes 0-7/") || resp.ContentLength != 8 {
		t.Errorf("%d %q %q %d", resp.StatusCode, resp.Header.Get("Content-Type"), resp.Header.Get("Content-Range"), resp.ContentLength)
	}
}

// fakeTasks plays the task manager: one automation.
type fakeTasks struct {
	items   []tasks.Item
	enabled map[string]bool
}

func (f *fakeTasks) List(context.Context) ([]tasks.Item, error) { return f.items, nil }

// fakeThreads records what the UI confirmed or cancelled.
type fakeThreads struct{ confirmed, cancelled []string }

func (f *fakeThreads) ConfirmThread(_ context.Context, id string) (string, error) {
	f.confirmed = append(f.confirmed, id)
	return "Angelegt.", nil
}
func (f *fakeThreads) CancelThread(_ context.Context, id string) error {
	f.cancelled = append(f.cancelled, id)
	return nil
}
func (f *fakeTasks) SetEnabled(_ context.Context, id string, on bool) error {
	if id != "mira_a" {
		return store.ErrNotFound
	}
	f.enabled[id] = on
	return nil
}
func (f *fakeTasks) Delete(_ context.Context, id string) error {
	if id != "mira_a" {
		return store.ErrNotFound
	}
	return nil
}
func (f *fakeTasks) Try(context.Context, string) (tasks.TryResult, error) {
	return tasks.TryResult{Verdicts: []tasks.Verdict{{Importance: 2, Spoken: "Viel Wasser."}}}, nil
}
func (f *fakeTasks) DescribeTry(r tasks.TryResult) string { return r.Verdicts[0].Spoken }

func TestTasksAPI(t *testing.T) {
	srv, _, st := setup(t)
	if code, _ := call(t, srv, "GET", "/v1/ui/tasks", "secret", ""); code != 503 {
		t.Errorf("without Home Assistant: %d", code)
	}
	ft := &fakeTasks{enabled: map[string]bool{}, items: []tasks.Item{{ID: "mira_a", Title: "Licht", State: "on", EditURL: "http://ha/x"}}}
	fth := &fakeThreads{}
	st.PutThread(context.Background(), store.Thread{ID: "p1", Target: "aufgabe", Place: "area:bad", State: store.ThreadProposed,
		Request: "Achte auf das Wasser", Log: []store.ThreadEntry{{Who: "mira", Text: "Vorschlag …"}}, ExpiresAt: time.Now().Add(time.Hour)})
	h := New(Options{Token: "secret", Store: st, Tasks: ft, Threads: fth, Config: &fakeConfig{sections: map[string]json.RawMessage{}, over: map[string]bool{}}})
	srv = httptest.NewServer(h)
	defer srv.Close()

	code, body := call(t, srv, "GET", "/v1/ui/tasks", "secret", "")
	if code != 200 || !strings.Contains(body, `"edit_url":"http://ha/x"`) || !strings.Contains(body, `"request":"Achte auf das Wasser"`) {
		t.Fatalf("list: %d %s", code, body)
	}
	if code, _ := call(t, srv, "POST", "/v1/ui/tasks/p1/activate", "secret", ""); code != 200 || len(fth.confirmed) != 1 {
		t.Errorf("confirm a planned task: %d", code)
	}
	if code, _ := call(t, srv, "POST", "/v1/ui/tasks/mira_a/pause", "secret", ""); code != 200 || ft.enabled["mira_a"] {
		t.Errorf("pause: %d", code)
	}
	if code, _ := call(t, srv, "POST", "/v1/ui/tasks/mira_a/activate", "secret", ""); code != 200 || !ft.enabled["mira_a"] {
		t.Errorf("resume: %d", code)
	}
	if code, body := call(t, srv, "POST", "/v1/ui/tasks/mira_a/try", "secret", ""); code != 200 || !strings.Contains(body, "Viel Wasser.") {
		t.Errorf("try: %d %s", code, body)
	}
	st.AddAssessment(context.Background(), store.Assessment{ID: "a1", Task: "mira_a", Kind: store.AssessmentAssess, Goal: "g",
		Importance: 2, Spoken: "Viel Wasser.", NoticeID: "n1"})
	if code, body := call(t, srv, "GET", "/v1/ui/tasks/mira_a/log", "secret", ""); code != 200 || !strings.Contains(body, `"notified":true`) {
		t.Errorf("log: %d %s", code, body)
	}
	if code, _ := call(t, srv, "POST", "/v1/ui/tasks/nope/pause", "secret", ""); code != 404 {
		t.Errorf("pause unknown: %d", code)
	}
	if code, _ := call(t, srv, "DELETE", "/v1/ui/tasks/mira_a", "secret", ""); code != 200 {
		t.Errorf("delete: %d", code)
	}
}

// The history comes a page at a time, newest first; open=1 leaves out what
// is done.
func TestNoticesArePaged(t *testing.T) {
	srv, _, st := setup(t)
	ctx := context.Background()
	for i := 0; i < 25; i++ {
		id := fmt.Sprintf("n%02d", i)
		st.PutNotice(ctx, store.Notice{ID: id, Source: store.SourceExternal, DedupeKey: id, Importance: 1,
			Audience: store.AudienceHousehold, Spoken: "Meldung " + id})
	}
	st.SetNoticeState(ctx, "n00", store.NoticeAcked)
	st.SetNoticeState(ctx, "n01", store.NoticeDelivered)
	var r struct {
		Items []struct{ ID, State string } `json:"items"`
		More  bool                         `json:"more"`
	}
	_, body := call(t, srv, "GET", "/v1/ui/notices", "secret", "")
	json.Unmarshal([]byte(body), &r)
	if len(r.Items) != 20 || !r.More {
		t.Fatalf("page 0: %d items, more %v", len(r.Items), r.More)
	}
	_, body = call(t, srv, "GET", "/v1/ui/notices?page=1", "secret", "")
	r.Items, r.More = nil, false
	json.Unmarshal([]byte(body), &r)
	if len(r.Items) != 5 || r.More {
		t.Fatalf("page 1: %d items, more %v", len(r.Items), r.More)
	}
	_, body = call(t, srv, "GET", "/v1/ui/notices?page=1&open=1", "secret", "")
	r.Items = nil
	json.Unmarshal([]byte(body), &r)
	if len(r.Items) != 3 {
		t.Errorf("open page 1: %d items", len(r.Items))
	}
}
