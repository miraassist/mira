package notifier

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/ha/structure"
	haws "github.com/miraassist/mira/internal/ha/ws"
)

// restarted is the notifier a restart makes: the same environment and the
// same shared state, a new instance.
func restarted(e *env, shared *Shared) *Notifier {
	entities, _ := e.ha.Entities(context.Background())
	snap := structure.Build(structure.Registry{Entities: entities})
	n := New(Options{Shared: shared, Config: e.cfg, Store: e.st, HA: e.ha, Clock: e.clk,
		Location: time.UTC, Logger: slog.New(slog.NewTextHandler(e.log, nil)),
		Structure: func() structure.Snapshot { return snap }, Composer: echoComposer})
	n.speakPause = 0
	return n
}

// MS-45 A change of the configuration does not interrupt a reminder being
// delivered (docs/spec/messages.md, A change of the configuration while a
// delivery runs): it runs on the service's life, and the new notifier does
// not deliver it a second time.
func TestMS45_AChangeOfTheConfigurationDoesNotInterruptAReminderBeingDelivered(t *testing.T) {
	e := newEnv(t, nil)
	shared := NewShared(context.Background())
	e.n.shared = shared
	e.clk.Set(at("08:00"))
	sat := e.n.primary("kueche")
	e.ha.set(sat, "responding", nil) // busy: the reminder waits for it
	e.reminder("r1", at("07:59"), "kueche", "ben")
	// Given a reminder is being delivered and waits for its satellite
	e.n.dueInBackground(shared.Work)
	<-e.ha.awaitWaiting(sat)
	// When the configuration changes and the notifier is restarted
	n2 := restarted(e, shared)
	n2.dueInBackground(shared.Work)
	n2.DueMessages(shared.Work)
	// Then the reminder is delivered as it would have been
	e.ha.set(sat, "idle", nil)
	<-e.ha.awaitCall(func(c call) bool { return c.Domain == "assist_satellite" })
	shared.Wait()
	// And the new notifier did not deliver it a second time
	if called := e.ha.servicesCalled(); len(called) != 1 {
		t.Errorf("services %v: delivered %d times", called, len(called))
	}
}

// MS-46 The new notifier begins when the old one's step is over: its loop
// (its subscriptions first) begins only after the old loop ended.
func TestMS46_TheNewNotifierBeginsWhenTheOldOnesStepIsOver(t *testing.T) {
	e := newEnv(t, nil)
	shared := NewShared(context.Background())
	e.n.shared = shared
	old := &fakeTriggers{handlers: map[string]haws.EventHandler{}, subscribed: map[string]chan struct{}{}}
	oldCtx, stopOld := context.WithCancel(context.Background())
	oldDone := make(chan error, 1)
	go func() { oldDone <- e.n.Run(oldCtx, old) }()
	old.mu.Lock()
	oldReady := old.ready("binary_sensor.bad_motion")
	old.mu.Unlock()
	<-oldReady // the old loop runs

	next := &fakeTriggers{handlers: map[string]haws.EventHandler{}, subscribed: map[string]chan struct{}{}}
	n2 := restarted(e, shared)
	nextCtx, stopNext := context.WithCancel(context.Background())
	t.Cleanup(stopNext)
	go func() { _ = n2.Run(nextCtx, next) }()
	next.mu.Lock()
	nextReady := next.ready("binary_sensor.bad_motion")
	next.mu.Unlock()
	select {
	case <-nextReady:
		t.Fatal("the new notifier began while the old loop ran")
	default:
	}
	// When the configuration changes: the old loop ends after its step
	stopOld()
	<-oldDone
	// Then the new notifier begins
	<-nextReady
}
