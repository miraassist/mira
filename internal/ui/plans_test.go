package ui

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/store"
)

// The running plans, with the room's name instead of its id; anyone with
// the token may read them.
func TestPlans(t *testing.T) {
	h := New(Options{Token: "secret",
		AreaName: func(id string) string { return map[string]string{"kueche": "Küche"}[id] },
		Plans: func(context.Context) ([]Plan, error) {
			return []Plan{{ID: "1", Target: "kochen", Request: "Pfannkuchen", Place: "area:kueche", Plan: "- [x] a\n- [ ] b", Done: 1, Steps: 2},
				{ID: "2", Target: "kochen", Place: "telegram:42"}}, nil
		}})
	srv := httptest.NewServer(h)
	defer srv.Close()
	code, body := call(t, srv, "GET", "/v1/ui/plans", "secret", "")
	if code != 200 || !strings.Contains(body, `"place":"Küche"`) || !strings.Contains(body, `"done":1`) || strings.Contains(body, "telegram") {
		t.Errorf("plans: %d %s", code, body)
	}
	if code, _ := call(t, srv, "GET", "/v1/ui/plans", "", ""); code != 401 {
		t.Errorf("without token: %d", code)
	}
	empty := httptest.NewServer(New(Options{Token: "secret"}))
	defer empty.Close()
	if code, body := call(t, empty, "GET", "/v1/ui/plans", "secret", ""); code != 200 || !strings.Contains(body, `"plans":[]`) {
		t.Errorf("no plans: %d %s", code, body)
	}
}

// endThreads records the threads the page ended; err is what CancelThread
// answers.
type endThreads struct {
	cancelled []string
	err       error
}

func (f *endThreads) ConfirmThread(context.Context, string) (string, error) { return "", nil }
func (f *endThreads) CancelThread(_ context.Context, id string) error {
	if f.err != nil {
		return f.err
	}
	f.cancelled = append(f.cancelled, id)
	return nil
}

// planServer has one running plan "q1"; listErr makes reading the plans fail.
func planServer(t *testing.T, th *endThreads, listErr error) (*httptest.Server, *bytes.Buffer) {
	t.Helper()
	logs := &bytes.Buffer{}
	h := New(Options{Token: "secret", Threads: th, Logger: slog.New(slog.NewTextHandler(logs, nil)),
		Plans: func(context.Context) ([]Plan, error) {
			return []Plan{{ID: "q1", Target: "quiz", Request: "Quiz"}}, listErr
		}})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, logs
}

func TestPE01_ARunningPlanIsEndedFromThePage(t *testing.T) {
	th := &endThreads{}
	srv, logs := planServer(t, th, nil)
	code, body := call(t, srv, "DELETE", "/v1/ui/plans/q1", "secret", "")
	if code != 200 || !strings.Contains(body, `"ended":true`) || !slices.Equal(th.cancelled, []string{"q1"}) {
		t.Fatalf("end: %d %s, cancelled %v", code, body, th.cancelled)
	}
	if !strings.Contains(logs.String(), "plan ended on the page") || !strings.Contains(logs.String(), "target=quiz") {
		t.Errorf("log: %s", logs)
	}
}

func TestPE02_OnlyARunningPlanIsEnded(t *testing.T) {
	th := &endThreads{}
	srv, _ := planServer(t, th, nil)
	// "t1" stands for an open thread that is not a plan: it is not among
	// the running plans, like an id that is no thread at all.
	for _, id := range []string{"t1", "nope"} {
		if code, body := call(t, srv, "DELETE", "/v1/ui/plans/"+id, "secret", ""); code != 404 || !strings.Contains(body, "no such plan") {
			t.Errorf("%s: %d %s", id, code, body)
		}
	}
	// Ended meanwhile: CancelThread finds it no longer open.
	gone := &endThreads{err: store.ErrNotFound}
	srv2, _ := planServer(t, gone, nil)
	if code, _ := call(t, srv2, "DELETE", "/v1/ui/plans/q1", "secret", ""); code != 404 {
		t.Errorf("ended meanwhile: %d", code)
	}
	if len(th.cancelled) != 0 {
		t.Errorf("cancelled %v", th.cancelled)
	}
}

func TestPE03_EveryoneWhoSeesThePlansMayEndOne(t *testing.T) {
	th := &endThreads{}
	srv, _ := planServer(t, th, nil)
	if code, body := asHA(t, srv, "DELETE", "/v1/ui/plans/q1", "u-ben", false, ""); code != 200 || len(th.cancelled) != 1 {
		t.Errorf("non-admin: %d %s", code, body)
	}
}

func TestPE04_AFailureIsSaidNotSwallowed(t *testing.T) {
	for name, tc := range map[string]struct {
		th      *endThreads
		listErr error
	}{
		"reading the plans": {&endThreads{}, errors.New("database is locked")},
		"ending the thread": {&endThreads{err: errors.New("database is locked")}, nil},
	} {
		srv, logs := planServer(t, tc.th, tc.listErr)
		code, body := call(t, srv, "DELETE", "/v1/ui/plans/q1", "secret", "")
		if code != 500 || !strings.Contains(body, "database is locked") {
			t.Errorf("%s: %d %s", name, code, body)
		}
		if !strings.Contains(logs.String(), "could not end a plan on the page") {
			t.Errorf("%s: log %s", name, logs)
		}
	}
}

func TestPE05_WithoutTheTokenNothingIsEnded(t *testing.T) {
	th := &endThreads{}
	srv, _ := planServer(t, th, nil)
	if code, _ := call(t, srv, "DELETE", "/v1/ui/plans/q1", "", ""); code != 401 || len(th.cancelled) != 0 {
		t.Errorf("without token: %d, cancelled %v", code, th.cancelled)
	}
}
