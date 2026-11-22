package clock

import (
	"slices"
	"sync"
	"time"
)

// Fake is a test's clock (docs/spec/tests.md): it stands still until the
// test moves it with Advance or Set. AwaitWaiters tells the test when the
// code waits on it, so the test moves it only then.
type Fake struct {
	mu      sync.Mutex
	now     time.Time
	pending []*fakeTimer
	seq     int
	awaits  []fakeAwait
}

type fakeAwait struct {
	n    int
	done chan struct{}
}

// NewFake is a fake clock at now.
func NewFake(now time.Time) *Fake { return &Fake{now: now} }

func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *Fake) Since(t time.Time) time.Duration { return f.Now().Sub(t) }
func (f *Fake) Until(t time.Time) time.Duration { return t.Sub(f.Now()) }

func (f *Fake) After(d time.Duration) <-chan time.Time { return f.NewTimer(d).C() }

func (f *Fake) NewTimer(d time.Duration) Timer {
	t := &fakeTimer{f: f, c: make(chan time.Time, 1)}
	f.arm(t, d)
	return t
}

func (f *Fake) AfterFunc(d time.Duration, fn func()) Timer {
	t := &fakeTimer{f: f, fn: fn}
	f.arm(t, d)
	return t
}

func (f *Fake) NewTicker(d time.Duration) Ticker {
	if d <= 0 {
		panic("clock: non-positive ticker period")
	}
	t := &fakeTimer{f: f, c: make(chan time.Time, 1), period: d}
	f.arm(t, d)
	return tickerOf{t}
}

// Advance moves the clock by d and fires every timer and ticker that is due,
// in the order of their times (a ticker once for each period it passes). An
// AfterFunc runs here: when Advance returns, it has run.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	target := f.now.Add(d)
	f.mu.Unlock()
	f.Set(target)
}

// Set moves the clock to t and fires what is due; a t before now is ignored.
func (f *Fake) Set(t time.Time) {
	for {
		f.mu.Lock()
		if t.Before(f.now) {
			f.mu.Unlock()
			return
		}
		next := f.earliest()
		if next == nil || next.when.After(t) {
			f.now = t
			f.mu.Unlock()
			return
		}
		f.now = next.when
		f.remove(next)
		if next.period > 0 {
			next.when = next.when.Add(next.period)
			f.add(next)
		}
		fn, c, at := next.fn, next.c, f.now
		f.mu.Unlock()
		if fn != nil {
			fn()
		} else {
			select {
			case c <- at:
			default: // as time's: a channel of one, a tick not taken is dropped
			}
		}
	}
}

// AdvanceToNext moves the clock to the earliest pending timer or ticker and
// fires it (and what is due with it); the move it made, 0 when nothing is
// pending. For a test that drives code through its waits one by one.
func (f *Fake) AdvanceToNext() time.Duration {
	f.mu.Lock()
	next := f.earliest()
	if next == nil {
		f.mu.Unlock()
		return 0
	}
	from, to := f.now, next.when
	f.mu.Unlock()
	f.Set(to)
	return to.Sub(from)
}

// Waiters is the number of pending timers and tickers.
func (f *Fake) Waiters() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.pending)
}

// AwaitWaiters is closed once at least n timers and tickers are pending, at
// once when they already are: the signal that the code waits on the clock.
func (f *Fake) AwaitWaiters(n int) <-chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	done := make(chan struct{})
	if len(f.pending) >= n {
		close(done)
		return done
	}
	f.awaits = append(f.awaits, fakeAwait{n: n, done: done})
	return done
}

func (f *Fake) arm(t *fakeTimer, d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t.when = f.now.Add(d)
	f.add(t)
}

// add and remove keep pending; the caller holds mu.
func (f *Fake) add(t *fakeTimer) {
	f.seq++
	t.seq = f.seq
	f.pending = append(f.pending, t)
	kept := f.awaits[:0]
	for _, a := range f.awaits {
		if len(f.pending) >= a.n {
			close(a.done)
			continue
		}
		kept = append(kept, a)
	}
	f.awaits = kept
}

func (f *Fake) remove(t *fakeTimer) bool {
	i := slices.Index(f.pending, t)
	if i < 0 {
		return false
	}
	f.pending = slices.Delete(f.pending, i, i+1)
	return true
}

// earliest is the pending timer due first (the one armed first on a tie).
func (f *Fake) earliest() *fakeTimer {
	var first *fakeTimer
	for _, t := range f.pending {
		if first == nil || t.when.Before(first.when) || t.when.Equal(first.when) && t.seq < first.seq {
			first = t
		}
	}
	return first
}

type fakeTimer struct {
	f      *Fake
	c      chan time.Time
	fn     func()
	when   time.Time
	period time.Duration
	seq    int
}

func (t *fakeTimer) C() <-chan time.Time { return t.c }

func (t *fakeTimer) Stop() bool {
	t.f.mu.Lock()
	defer t.f.mu.Unlock()
	return t.f.remove(t)
}

func (t *fakeTimer) Reset(d time.Duration) bool {
	t.f.mu.Lock()
	defer t.f.mu.Unlock()
	was := t.f.remove(t)
	if t.period > 0 {
		t.period = d
	}
	t.when = t.f.now.Add(d)
	t.f.add(t)
	return was
}

// tickerOf is a fake ticker: Stop and Reset without a result.
type tickerOf struct{ t *fakeTimer }

func (k tickerOf) C() <-chan time.Time   { return k.t.c }
func (k tickerOf) Stop()                 { k.t.Stop() }
func (k tickerOf) Reset(d time.Duration) { k.t.Reset(d) }
