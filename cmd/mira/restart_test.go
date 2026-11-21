package main

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
)

// BE-27: saves of the settings and changes of the persons at once restart
// the notifier one after the other; afterwards exactly one runs.
func TestBE27_RestartsAtOnceLeaveOneNotifier(t *testing.T) {
	a, u, _ := sectionsApp(t)
	ctx := u.runCtx
	a.restartMu.Lock()
	a.startProactive(ctx)
	a.proactiveOn = true
	a.restartMu.Unlock()

	whole, err := config.Default().SectionJSON("notifier")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := u.SetSections(context.Background(), map[string]json.RawMessage{"notifier": whole}); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			a.restartProactive(ctx)
		}()
	}
	// Every restart comes back (before, one could wait for ever)
	if !within(10*time.Second, wg.Wait) {
		t.Fatal("a restart did not come back")
	}
	// And stopping the one notifier stops everything it ran: no second one
	// is left running without its cancel
	a.proactiveMu.Lock()
	cancel := a.proactiveCancel
	a.proactiveMu.Unlock()
	cancel()
	if !within(5*time.Second, a.proactiveWG.Wait) {
		t.Fatal("a notifier still runs after the last one was stopped")
	}
}

// BE-27: before the first start (and in an eval) a restart starts nothing.
func TestBE27_ARestartBeforeTheFirstStartStartsNothing(t *testing.T) {
	a, u, _ := sectionsApp(t)
	a.restartProactive(u.runCtx)
	a.proactiveMu.Lock()
	started := a.proactiveCancel != nil
	a.proactiveMu.Unlock()
	if started {
		t.Fatal("a restart before the first start started the notifier")
	}
}

func within(d time.Duration, fn func()) bool {
	done := make(chan struct{})
	go func() { fn(); close(done) }()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}
