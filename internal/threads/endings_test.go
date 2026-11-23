package threads

import (
	"context"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/clock"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/testsupport/logcap"
)

// EN-01 A turn that ended takes no result of its agents
// (docs/spec/endings.md): the result is offered and refused, so the agent's
// side sends it as a message; a turn that still answers takes it; a second
// wait on the same thread stays when the first stops waiting.
func TestEN01_ATurnThatEndedTakesNoResultOfItsAgents(t *testing.T) {
	// The turn's wait never runs out on this clock: only its end counts.
	m := New(Options{Lang: i18n.DE, Clock: clock.NewFake(time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC))})

	alive := m.await("t1")
	said := make(chan string, 1)
	go func() { said <- m.waitFor(context.Background(), "t1", alive) }()
	if !m.handOver("t1", "Fertig.") || <-said != "Fertig." {
		t.Fatal("a turn that still answers did not take the result")
	}

	ctx, endTurn := context.WithCancel(context.Background())
	ended := m.await("t2")
	gone := make(chan string, 1)
	go func() { gone <- m.waitFor(ctx, "t2", ended) }()
	endTurn()
	if m.handOver("t2", "Fertig.") {
		t.Error("a turn that ended took the result")
	}
	if s := <-gone; s != "" {
		t.Errorf("the ended turn says %q", s)
	}

	first := m.await("t3")
	second := m.await("t3")
	m.unawait("t3", first)
	m.waitMu.Lock()
	kept := m.waiting["t3"] == second
	m.waitMu.Unlock()
	if !kept {
		t.Error("the first wait removed the second's")
	}
}

// EN-04 A thread's work cancelled with its thread is no failure.
func TestEN04_AThreadsWorkCancelledWithItsThreadIsNoFailure(t *testing.T) {
	lines := logcap.Default(t)
	sofort := skills.Skill{Name: "sofort", Description: "sofort", Body: "Frag nach.",
		Target: &skills.Target{Finish: skills.Finish{Name: "fin"}}}
	m := New(Options{Lang: i18n.DE, Targets: []skills.Skill{sofort},
		Run: func(context.Context, string, string, func(context.Context) error) error { return ErrWorkCancelled }})
	res := m.runWork(context.Background(), nil, store.Thread{ID: "th1", Target: "sofort"})
	if !res.cancelled || res.Cancel != "" {
		t.Errorf("result %+v", res)
	}
	if lines.Line("thread work failed") != "" || lines.Line("thread work cancelled with its thread", "thread=th1") == "" {
		t.Errorf("log:\n%s", lines.String())
	}
}
