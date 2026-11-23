package notifier

import (
	"context"
	"sync"
)

// Shared is what the notifiers of one Mira service share across a restart
// (docs/spec/messages.md, A change of the configuration while a delivery
// runs): the service's context, on which deliveries run; the scheduled
// messages being delivered, so no notifier starts one twice; and the end of
// the last notifier's loop, where the next one begins.
type Shared struct {
	// Work is the service's life: a delivery runs on it, not on its
	// notifier's, so a restart interrupts none.
	Work context.Context

	mu         sync.Mutex
	delivering map[string]bool
	wg         sync.WaitGroup
	last       chan struct{}
}

// NewShared makes what the service's notifiers share; work is the service's
// context.
func NewShared(work context.Context) *Shared {
	done := make(chan struct{})
	close(done)
	return &Shared{Work: work, delivering: map[string]bool{}, last: done}
}

// claim marks a scheduled message as being delivered; false when it is
// already.
func (s *Shared) claim(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.delivering[id] {
		return false
	}
	s.delivering[id] = true
	return true
}

func (s *Shared) release(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.delivering, id)
}

// handover is the end of the previous loop, to begin after, and this loop's
// own end, to close when it returns.
func (s *Shared) handover() (previous <-chan struct{}, own chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	own = make(chan struct{})
	previous, s.last = s.last, own
	return previous, own
}

// Wait waits for the deliveries still running (the service's stop).
func (s *Shared) Wait() { s.wg.Wait() }
