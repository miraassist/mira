// Package clock is the time code waits on (docs/spec/tests.md): the real
// clock in production, a fake one in tests, which stands still until the test
// moves it. Every timer, ticker, After and AfterFunc of Mira's code goes
// through a Clock, so that no test waits for real time.
package clock

import "time"

// Clock is what code that waits needs of time.
type Clock interface {
	Now() time.Time
	Since(t time.Time) time.Duration
	Until(t time.Time) time.Duration
	After(d time.Duration) <-chan time.Time
	NewTimer(d time.Duration) Timer
	AfterFunc(d time.Duration, f func()) Timer
	NewTicker(d time.Duration) Ticker
}

// Timer is a time.Timer: C fires once; Stop reports whether it was pending;
// Reset arms it anew and reports whether it was pending. The channel of a
// timer made by AfterFunc is nil.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
	Reset(d time.Duration) bool
}

// Ticker is a time.Ticker.
type Ticker interface {
	C() <-chan time.Time
	Stop()
	Reset(d time.Duration)
}

// Real is the time package.
func Real() Clock { return real{} }

// Or is c, or the real clock when c is nil: for options that default to it.
func Or(c Clock) Clock {
	if c == nil {
		return real{}
	}
	return c
}

type real struct{}

func (real) Now() time.Time                         { return time.Now() }
func (real) Since(t time.Time) time.Duration        { return time.Since(t) }
func (real) Until(t time.Time) time.Duration        { return time.Until(t) }
func (real) After(d time.Duration) <-chan time.Time { return time.After(d) }
func (real) NewTimer(d time.Duration) Timer         { return realTimer{time.NewTimer(d)} }
func (real) AfterFunc(d time.Duration, f func()) Timer {
	return realTimer{time.AfterFunc(d, f)}
}
func (real) NewTicker(d time.Duration) Ticker { return realTicker{time.NewTicker(d)} }

type realTimer struct{ t *time.Timer }

func (r realTimer) C() <-chan time.Time        { return r.t.C }
func (r realTimer) Stop() bool                 { return r.t.Stop() }
func (r realTimer) Reset(d time.Duration) bool { return r.t.Reset(d) }

type realTicker struct{ t *time.Ticker }

func (r realTicker) C() <-chan time.Time   { return r.t.C }
func (r realTicker) Stop()                 { r.t.Stop() }
func (r realTicker) Reset(d time.Duration) { r.t.Reset(d) }
