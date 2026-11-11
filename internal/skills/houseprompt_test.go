package skills

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/store"
)

// A job of the scheduler waits for Home Assistant's prompt instead of
// failing at Mira's start (docs/spec/context.md, 1.2).

func TestHP07_AScheduledJobWaitsForHomeAssistantsPrompt(t *testing.T) {
	sk, _ := loadPlan(t, "", nil)
	st := openStore(t)
	tasker := &answerTasker{answer: `{"importance":1,"spoken":"Heute ist Markt."}`}
	prompt := make(chan struct{})
	r := &Runner{Set: berlinSet(t, sk), Tasker: tasker, Store: st, ReadyWait: 2 * time.Second,
		Ready: func(ctx context.Context) error {
			select {
			case <-prompt:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}}
	done := make(chan struct{})
	go func() { defer close(done); r.run(context.Background(), r.jobs(false)[0], r.log()) }()

	// While the prompt is missing the job waits: nothing ran.
	select {
	case <-done:
		t.Fatal("the job ran without Home Assistant's prompt")
	case <-time.After(50 * time.Millisecond):
	}
	if _, ok, err := st.LastRun(context.Background(), "schedule:plan:morgen"); ok || err != nil {
		t.Errorf("a last run while waiting (%v)", err)
	}

	// When the prompt comes, it runs.
	close(prompt)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the job did not run after the prompt came")
	}
	if run := lastRun(t, st, "schedule:plan:morgen"); run.Status != store.RunDone || run.Text != "Heute ist Markt." {
		t.Errorf("run: %+v", run)
	}
}

func TestHP08_AJobThatWaitedInVainRunsAndFails(t *testing.T) {
	sk, _ := loadPlan(t, "", nil)
	st := openStore(t)
	tasker := &answerTasker{err: errors.New("no house structure: home assistant's prompt is missing")}
	var waited time.Duration
	r := &Runner{Set: berlinSet(t, sk), Tasker: tasker, Store: st, ReadyWait: 30 * time.Millisecond,
		Ready: func(ctx context.Context) error {
			started := time.Now()
			<-ctx.Done()
			waited = time.Since(started)
			return ctx.Err()
		}}
	r.run(context.Background(), r.jobs(false)[0], r.log())
	if waited < 20*time.Millisecond {
		t.Errorf("the job waited only %s", waited)
	}
	run := lastRun(t, st, "schedule:plan:morgen")
	if run.Status != store.RunFailed || !strings.Contains(run.Error, "prompt is missing") {
		t.Errorf("run: %+v", run)
	}
}
