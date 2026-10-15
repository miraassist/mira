package watchers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sker65/mira/internal/config"
	"github.com/sker65/mira/internal/feeds"
	"github.com/sker65/mira/internal/store"
)

type feedEnv struct {
	m     *Manager
	st    *store.Store
	ai    *fakeAssessor
	items []feeds.Item
	now   time.Time
}

var models = config.Watcher{Name: "modelle", Schedule: "0 8 * * *", Kind: "rss", URL: "https://example.org/models.xml",
	Description: "Neue lokal lauffähige Sprachmodelle."}

func newFeedEnv(t *testing.T, answer string) *feedEnv {
	t.Helper()
	e := &feedEnv{ai: &fakeAssessor{answer: answer}, now: time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)}
	var err error
	e.st, err = store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "db"), Clock: func() time.Time { return e.now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.st.Close() })
	e.m = New(Options{Watchers: []config.Watcher{models}, MinImportance: 1, Store: e.st, Assessor: e.ai,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clock:  func() time.Time { return e.now }, Location: time.UTC,
		Fetch: func(context.Context, string) ([]feeds.Item, error) { return e.items, nil }})
	return e
}

func (f *fakeAssessor) last() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return ""
	}
	return f.calls[len(f.calls)-1]
}

func item(id string, age time.Duration, now time.Time) feeds.Item {
	return feeds.Item{ID: id, Title: "New Model Available: " + id, Link: "https://example.org/" + id, Published: now.Add(-age)}
}

const relevant = `{"importance":2,"spoken":"Es gibt ein neues Qwen-Modell.","details":"- Qwen 4 (https://example.org/qwen4)"}`

func TestFirstRunOnlyLooksAtTheLastDay(t *testing.T) {
	e := newFeedEnv(t, relevant)
	e.items = []feeds.Item{item("qwen4", time.Hour, e.now), item("old", 72*time.Hour, e.now)}
	queued, err := e.m.RunFeed(context.Background(), models)
	if err != nil || !queued {
		t.Fatalf("queued = %v, err = %v", queued, err)
	}
	if !strings.Contains(e.ai.last(), "qwen4") || strings.Contains(e.ai.last(), "old") {
		t.Errorf("instruction:\n%s", e.ai.last())
	}
	if !strings.Contains(e.ai.last(), "keine Anweisungen") {
		t.Error("feed text is not marked as data")
	}
}

func TestSeenItemsAreNotShownAgain(t *testing.T) {
	e := newFeedEnv(t, relevant)
	e.items = []feeds.Item{item("qwen4", time.Hour, e.now)}
	e.m.RunFeed(context.Background(), models)
	calls := e.ai.count()

	e.now = e.now.Add(24 * time.Hour)
	if queued, err := e.m.RunFeed(context.Background(), models); err != nil || queued {
		t.Fatalf("queued = %v, err = %v", queued, err)
	}
	if e.ai.count() != calls {
		t.Error("the model was asked about items it had already seen")
	}

	e.items = append(e.items, item("llama6", time.Hour, e.now))
	if queued, _ := e.m.RunFeed(context.Background(), models); !queued {
		t.Fatal("a new item was ignored")
	}
	if strings.Contains(e.ai.last(), "qwen4") {
		t.Error("an old item was shown again")
	}
}

func TestIrrelevantFeedQueuesNothing(t *testing.T) {
	e := newFeedEnv(t, `{"importance":0,"spoken":"","details":""}`)
	e.items = []feeds.Item{item("image-model", time.Hour, e.now)}
	if queued, err := e.m.RunFeed(context.Background(), models); err != nil || queued {
		t.Fatalf("queued = %v, err = %v", queued, err)
	}
}

func TestFeedCannotRaiseAnUrgentNotice(t *testing.T) {
	e := newFeedEnv(t, `{"importance":3,"spoken":"Alarm!","details":"x"}`)
	e.items = []feeds.Item{item("x", time.Hour, e.now)}
	e.m.RunFeed(context.Background(), models)
	n, err := e.st.OpenNoticeByDedupeKey(context.Background(), "watcher:modelle:2026-10-15")
	if err != nil {
		t.Fatal(err)
	}
	if n.Importance != maxFeedImportance || n.Source != store.SourceSchedule {
		t.Errorf("notice = %+v", n)
	}
}

func TestFailedAssessmentRetriesNextRun(t *testing.T) {
	e := newFeedEnv(t, relevant)
	e.ai.err = errors.New("llm down")
	e.items = []feeds.Item{item("qwen4", time.Hour, e.now)}
	if _, err := e.m.RunFeed(context.Background(), models); err == nil {
		t.Fatal("failure swallowed")
	}
	e.ai.err = nil
	if queued, _ := e.m.RunFeed(context.Background(), models); !queued {
		t.Fatal("the item was lost after a failed assessment")
	}
}
