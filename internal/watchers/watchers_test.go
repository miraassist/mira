package watchers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sker65/lares/internal/config"
	haws "github.com/sker65/lares/internal/ha/ws"
	"github.com/sker65/lares/internal/store"
	"github.com/sker65/lares/internal/testsupport/fakeha"
)

type fakeAssessor struct {
	mu     sync.Mutex
	answer string
	err    error
	calls  []string
}

func (f *fakeAssessor) Assess(_ context.Context, instruction string, schema json.RawMessage) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, instruction)
	if !json.Valid(schema) {
		return "", errors.New("invalid schema")
	}
	return f.answer, f.err
}

func (f *fakeAssessor) count() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.calls) }

var inverter = config.Watcher{
	Name:        "inverter_fault",
	Trigger:     &config.StateTrigger{EntityID: []string{"sensor.inverter_status"}, To: []string{"fault", "error"}},
	DedupeKey:   "inverter:fault",
	Description: "Der Wechselrichter der Solaranlage meldet einen Fehler.",
}

var faultEvent = Event{EntityID: "sensor.inverter_status", FriendlyName: "Wechselrichter Status",
	From: "ok", To: "fault", Attributes: map[string]any{"error_code": 42, "friendly_name": "x"}}

const goodAnswer = `{"importance":2,"spoken":"Der Wechselrichter meldet seit drei Uhr einen Fehler.","details":"Status fault, Fehlercode 42, seit 03:00.","expires_in_hours":24}`

func newManager(t *testing.T, a Assessor) (*Manager, *store.Store) {
	t.Helper()
	now := time.Date(2026, 10, 14, 3, 0, 0, 0, time.UTC)
	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "db"), Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	m := New(Options{Store: st, Assessor: a, MinImportance: 1, Watchers: []config.Watcher{inverter},
		Clock: func() time.Time { return now }, Location: time.UTC, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	return m, st
}

func TestAssessmentFieldsAreStored(t *testing.T) {
	a := &fakeAssessor{answer: goodAnswer}
	m, _ := newManager(t, a)
	n, stored, err := m.Handle(context.Background(), inverter, faultEvent)
	if err != nil || !stored {
		t.Fatalf("Handle = %v, %v", stored, err)
	}
	if n.Importance != 2 || n.DedupeKey != "inverter:fault" || n.Source != store.SourceHATrigger ||
		!strings.HasPrefix(n.Spoken, "Der Wechselrichter") || !strings.Contains(n.Details, "42") {
		t.Fatalf("notice = %+v", n)
	}
	if n.ExpiresAt.Sub(n.CreatedAt) != 24*time.Hour {
		t.Errorf("expiry = %v", n.ExpiresAt.Sub(n.CreatedAt))
	}
	instr := a.calls[0]
	for _, want := range []string{"Bedeutung: Der Wechselrichter", "sensor.inverter_status", "vorher: ok", "jetzt: fault", "error_code=42"} {
		if !strings.Contains(instr, want) {
			t.Errorf("instruction lacks %q:\n%s", want, instr)
		}
	}
	if strings.Contains(instr, "friendly_name=") {
		t.Error("UI attributes leaked into the instruction")
	}
}

// Plan acceptance: a flapping sensor does not create duplicates.
func TestFlappingSensorCreatesOneNotice(t *testing.T) {
	a := &fakeAssessor{answer: goodAnswer}
	m, st := newManager(t, a)
	ctx := context.Background()
	first, _, _ := m.Handle(ctx, inverter, faultEvent)
	for i := 0; i < 5; i++ {
		if _, stored, err := m.Handle(ctx, inverter, faultEvent); err != nil || stored {
			t.Fatalf("flap %d stored a notice (%v)", i, err)
		}
	}
	if a.count() != 1 {
		t.Errorf("assessed %d times; flaps must not cost LLM calls", a.count())
	}
	open, _ := st.ListNotices(ctx, store.NoticeFilter{States: []string{store.NoticePending, store.NoticeDelivered}})
	if len(open) != 1 {
		t.Fatalf("open notices = %d", len(open))
	}

	// Once acknowledged, a new fault is news again.
	st.SetNoticeState(ctx, first.ID, store.NoticeAcked)
	if _, stored, _ := m.Handle(ctx, inverter, faultEvent); !stored {
		t.Fatal("fault after acknowledgement was dropped")
	}
}

func TestFallbackWhenTheModelFails(t *testing.T) {
	for name, a := range map[string]*fakeAssessor{
		"error":   {err: errors.New("llm down")},
		"garbage": {answer: "Das ist wichtig!"},
		"empty":   {answer: `{"importance":2,"spoken":"","details":"","expires_in_hours":5}`},
	} {
		t.Run(name, func(t *testing.T) {
			m, _ := newManager(t, a)
			n, stored, err := m.Handle(context.Background(), inverter, faultEvent)
			if err != nil || !stored {
				t.Fatalf("Handle = %v, %v", stored, err)
			}
			if n.Spoken != inverter.Description || n.Importance != 2 || !strings.Contains(n.Details, `"ok" zu "fault"`) {
				t.Fatalf("fallback notice = %+v", n)
			}
		})
	}
}

func TestImportanceThresholdAndOverride(t *testing.T) {
	low := &fakeAssessor{answer: `{"importance":0,"spoken":"x","details":"y","expires_in_hours":1}`}
	m, _ := newManager(t, low)
	if _, stored, err := m.Handle(context.Background(), inverter, faultEvent); err != nil || stored {
		t.Fatalf("unimportant event stored: %v %v", stored, err)
	}

	three := 3
	w := inverter
	w.Importance = &three
	w.Confidentiality = store.ConfidentialityConfidential
	w.ExpiresIn = config.Duration(6 * time.Hour)
	m2, _ := newManager(t, low)
	n, stored, _ := m2.Handle(context.Background(), w, faultEvent)
	if !stored || n.Importance != 3 || !n.Confidential() || n.ExpiresAt.Sub(n.CreatedAt) != 6*time.Hour {
		t.Fatalf("override not applied: %+v", n)
	}
}

func TestRunSubscribesTriggersOverTheWebSocket(t *testing.T) {
	srv := fakeha.New()
	defer srv.Close()
	client, err := haws.New(haws.Options{URL: srv.URL, Token: "test-token", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go client.Run(ctx)
	if err := client.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}

	m, st := newManager(t, &fakeAssessor{answer: goodAnswer})
	go m.Run(ctx, client)
	deadline := time.Now().Add(3 * time.Second)
	for srv.SubscriptionCount() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("watcher did not subscribe")
		}
		time.Sleep(10 * time.Millisecond)
	}
	srv.PushTrigger(map[string]any{"trigger": map[string]any{
		"platform": "state", "entity_id": "sensor.inverter_status",
		"from_state": map[string]any{"entity_id": "sensor.inverter_status", "state": "ok"},
		"to_state": map[string]any{"entity_id": "sensor.inverter_status", "state": "fault",
			"attributes": map[string]any{"friendly_name": "Wechselrichter"}},
	}})
	for {
		if n, err := st.OpenNoticeByDedupeKey(ctx, "inverter:fault"); err == nil {
			if n.Importance != 2 {
				t.Fatalf("notice = %+v", n)
			}
			return
		}
		if time.Now().After(deadline.Add(2 * time.Second)) {
			t.Fatal("no notice after the trigger fired")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTriggerFor(t *testing.T) {
	got, _ := json.Marshal(TriggerFor(inverter))
	want := `{"entity_id":["sensor.inverter_status"],"to":["fault","error"],"trigger":"state"}`
	if string(got) != want {
		t.Errorf("trigger = %s", got)
	}
}
