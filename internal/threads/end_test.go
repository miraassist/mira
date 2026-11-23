package threads

import (
	"context"
	"github.com/miraassist/mira/internal/clock"
	"sync"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/store/storetest"
)

// docs/spec/thread-history.md, step 1: every end goes through the one place.

// resultFinisher answers every work with its result.
type resultFinisher struct {
	async bool
	res   Result
}

func (f resultFinisher) Async() bool { return f.async }
func (f resultFinisher) Work(context.Context, store.Thread) (Result, error) {
	return f.res, nil
}
func (f resultFinisher) Commit(context.Context, store.Thread) (string, error) { return "", nil }

// endEnv is a manager with one target "sofort" (one field, finished by fin)
// and a record of the ends its hook was told.
type endEnv struct {
	m     *Manager
	st    *store.Store
	mu    sync.Mutex
	ended []string
	told  chan store.Message
	// endedCh tells each end the hook heard of.
	endedCh chan string
}

func newEndEnv(t *testing.T, fin Finisher) *endEnv {
	t.Helper()
	clk := clock.NewFake(time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC))
	st, err := store.Open(context.Background(), store.Options{Path: storetest.Path(t), Clock: clk.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	e := &endEnv{st: st, told: make(chan store.Message, 4), endedCh: make(chan string, 4)}
	sofort := skills.Skill{Name: "sofort", Description: "sofort", Body: "Frag nach.",
		Target: &skills.Target{Finish: skills.Finish{Name: "fin"}, Fields: []skills.TargetField{{Name: "was", Description: "was"}}}}
	e.m = New(Options{Lang: i18n.DE, Store: st, Targets: []skills.Skill{sofort},
		Finishers:  map[string]Finisher{"fin": fin},
		InlineWait: 10 * time.Millisecond,
		// The turn's wait never runs out: the finisher answers in time.
		Clock: clk,
		Deliver: func(_ context.Context, n store.Message, _ string) error {
			e.told <- n
			return nil
		},
		Ended: func(_ context.Context, th store.Thread) {
			e.mu.Lock()
			defer e.mu.Unlock()
			e.ended = append(e.ended, th.ID)
			e.endedCh <- th.ID
		}})
	return e
}

func (e *endEnv) endedCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.ended)
}

// TV-01 A finisher's result ends its thread through the one place
func TestTV01_AFinishersResultEndsItsThreadThroughTheOnePlace(t *testing.T) {
	for _, c := range []struct {
		name  string
		fin   resultFinisher
		state string
	}{
		{"done in the foreground", resultFinisher{res: Result{Done: "Erledigt."}}, store.ThreadDone},
		{"cancelled in the foreground", resultFinisher{res: Result{Cancel: "Geht nicht."}}, store.ThreadCancelled},
		{"done in the background", resultFinisher{async: true, res: Result{Done: "Erledigt."}}, store.ThreadDone},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newEndEnv(t, c.fin)
			ctx := context.Background()
			inv := voiceIn(t, e.st, "voice:a", "wohnzimmer")
			if _, err := e.m.Start(ctx, inv, "sofort", "Mach das"); err != nil {
				t.Fatal(err)
			}
			if _, err := e.m.Fill(ctx, inv, map[string]string{"was": "das"}); err != nil {
				t.Fatal(err)
			}
			<-e.endedCh // the one place told the end
			if n := e.endedCount(); n != 1 {
				t.Fatalf("the hook was told %d times, want once", n)
			}
			th, err := e.st.GetThread(ctx, e.ended[0])
			if err != nil || th.State != c.state {
				t.Errorf("thread = %+v, %v; want %s", th.State, err, c.state)
			}
		})
	}
}

// TV-02 Taking back a done thread goes through the same code
func TestTV02_TakingBackADoneThreadGoesThroughTheSameCode(t *testing.T) {
	m, st, _ := actionSetup(t)
	var mu sync.Mutex
	var ended []string
	m.opts.Ended = func(_ context.Context, th store.Thread) {
		mu.Lock()
		defer mu.Unlock()
		ended = append(ended, th.ID)
	}
	ctx := context.Background()
	inv := voiceIn(t, st, "voice:a", "schlafzimmer")
	if _, err := m.Start(ctx, inv, "wecker", "Weck mich morgen um 6"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Fill(ctx, inv, map[string]string{"time": "06:00"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Undo(ctx, "area:schlafzimmer"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ended) != 2 || ended[0] != ended[1] {
		t.Fatalf("ends told: %v, want the done and the taken back one of the same thread", ended)
	}
	if th, err := st.GetThread(ctx, ended[0]); err != nil || th.State != store.ThreadCancelled {
		t.Errorf("thread after undo: %s %v", th.State, err)
	}
}
