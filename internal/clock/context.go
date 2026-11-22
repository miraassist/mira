package clock

import (
	"context"
	"sync"
	"time"
)

// WithTimeout is context.WithTimeout on the clock: the context ends when
// the clock passes d, and then its Err is context.DeadlineExceeded, as the
// time package's. On the real clock it is context.WithTimeout itself.
func WithTimeout(parent context.Context, c Clock, d time.Duration) (context.Context, context.CancelFunc) {
	c = Or(c)
	if _, ok := c.(real); ok {
		return context.WithTimeout(parent, d)
	}
	inner, cancel := context.WithCancel(parent)
	ctx := &timeoutCtx{Context: inner, deadline: c.Now().Add(d), done: make(chan struct{})}
	// Its own Done, closed after Err is set: a child context then takes its
	// Err from it (DeadlineExceeded), not from the inner context.
	context.AfterFunc(inner, func() { close(ctx.done) })
	timer := c.AfterFunc(d, func() {
		ctx.mu.Lock()
		if inner.Err() == nil {
			ctx.expired = true
		}
		ctx.mu.Unlock()
		cancel()
	})
	return ctx, func() { timer.Stop(); cancel() }
}

// timeoutCtx is a context that ends at a deadline of a clock that is not
// the real one.
type timeoutCtx struct {
	context.Context
	deadline time.Time
	done     chan struct{}
	mu       sync.Mutex
	expired  bool
}

func (c *timeoutCtx) Deadline() (time.Time, bool) { return c.deadline, true }
func (c *timeoutCtx) Done() <-chan struct{}       { return c.done }

func (c *timeoutCtx) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.expired {
		return context.DeadlineExceeded
	}
	return c.Context.Err()
}
