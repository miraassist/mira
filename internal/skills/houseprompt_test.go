package skills

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/clock"
	"github.com/miraassist/mira/internal/store"
)

// A job of the scheduler waits for Home Assistant's prompt instead of
// failing at Mira's start (docs/spec/context.md, 1.2).

func TestHP07_AScheduledJobWaitsForHomeAssistantsPrompt(t *testing.T) {
	sk, _ := loadPlan(t, "", nil)
	st := openStore(t)
	tasker := &answerTasker{answer: `{"importance":1,"spoken":"Heute ist Markt."}`}
	prompt, asked := make(chan struct{}), make(chan struct{})
	r := &Runner{Set: berlinSet(t, sk), Tasker: tasker, Store: st, ReadyWait: 2 * time.Second,
		Ready: func(ctx context.Context) error {
			close(asked)
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
	<-asked
	select {
	case <-done:
		t.Fatal("the job ran without Home Assistant's prompt")
	default:
	}
	if _, ok, err := st.LastRun(context.Background(), store.RunKey{Skill: "plan", Schedule: "morgen"}); ok || err != nil {
		t.Errorf("a last run while waiting (%v)", err)
	}

	// When the prompt comes, it runs.
	close(prompt)
	<-done // it runs once the prompt came
	if run := lastRun(t, st, store.RunKey{Skill: "plan", Schedule: "morgen"}); run.Status != store.RunDone || run.Text != "Heute ist Markt." {
		t.Errorf("run: %+v", run)
	}
}

func TestHP08_AJobThatWaitedInVainRunsAndFails(t *testing.T) {
	sk, _ := loadPlan(t, "", nil)
	st := openStore(t)
	tasker := &answerTasker{err: errors.New("no house structure: home assistant's prompt is missing")}
	var waited time.Duration
	clk := clock.NewFake(time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC))
	r := &Runner{Set: berlinSet(t, sk), Tasker: tasker, Store: st, ReadyWait: 30 * time.Millisecond, Clock: clk,
		Ready: func(ctx context.Context) error {
			started := clk.Now()
			<-ctx.Done()
			waited = clk.Since(started)
			return ctx.Err()
		}}
	clk.Drive(func() { r.run(context.Background(), r.jobs(false)[0], r.log()) }) // the wait runs out
	if waited != 30*time.Millisecond {
		t.Errorf("the job waited %s", waited)
	}
	run := lastRun(t, st, store.RunKey{Skill: "plan", Schedule: "morgen"})
	if run.Status != store.RunFailed || !strings.Contains(run.Error, "prompt is missing") {
		t.Errorf("run: %+v", run)
	}
}
