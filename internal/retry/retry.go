// Package retry is Mira's one way to try something again
// (docs/spec/retry.md): the error is classified first, only a retryable one
// is tried again, every retry is bounded and waits as its policy says, and a
// wait the other side names is kept.
package retry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// Growth is how the wait grows from one attempt to the next.
type Growth int

const (
	// Exponential doubles the wait: for something that may be down a while.
	Exponential Growth = iota
	// Linear adds the first wait each time.
	Linear
	// Fixed waits the same each time: for something local that restarts.
	Fixed
)

// Policy bounds a retry and sets its waits.
type Policy struct {
	// Attempts is the number of attempts in all, the first included; 0 means
	// no bound (only for a connection Mira lives on, with Max set).
	Attempts int
	// First is the wait after the first failed attempt.
	First time.Duration
	// Max is the longest wait; 0 means none. A wait the other side names that
	// is longer fails at once.
	Max    time.Duration
	Growth Growth
}

// Wait is the wait after failed failed attempts (1 for the first).
func (p Policy) Wait(failed int) time.Duration {
	if failed < 1 {
		failed = 1
	}
	w := p.First
	switch p.Growth {
	case Linear:
		w = p.First * time.Duration(failed)
	case Exponential:
		for i := 1; i < failed; i++ {
			w *= 2
			if p.Max > 0 && w >= p.Max {
				break
			}
		}
	}
	if p.Max > 0 && w > p.Max {
		w = p.Max
	}
	return w
}

// Exhausted says whether failed attempts use up the policy.
func (p Policy) Exhausted(failed int) bool {
	return p.Attempts > 0 && failed >= p.Attempts
}

// mark classifies the error it wraps; the outermost mark in a chain wins.
type mark struct {
	err       error
	retryable bool
}

func (m mark) Error() string   { return m.err.Error() }
func (m mark) Unwrap() error   { return m.err }
func (m mark) Retryable() bool { return m.retryable }

// Fatal marks err as one another attempt cannot help: it heals only by a
// change (a setting, a token, the request).
func Fatal(err error) error {
	if err == nil {
		return nil
	}
	return mark{err, false}
}

// Transient marks err as one another attempt may help, whatever it wraps.
func Transient(err error) error {
	if err == nil {
		return nil
	}
	return mark{err, true}
}

type after struct {
	err  error
	wait time.Duration
}

func (a after) Error() string             { return a.err.Error() }
func (a after) Unwrap() error             { return a.err }
func (a after) RetryAfter() time.Duration { return a.wait }
func (a after) Retryable() bool           { return true }

// After marks err as retryable no earlier than wait: a wait the other side
// named (Retry-After, Google's retryDelay).
func After(err error, wait time.Duration) error {
	if err == nil {
		return nil
	}
	return after{err, wait}
}

// Retryable says whether another attempt could help: the outermost error in
// the chain with a Retryable method decides (a mark of this package, or an
// error that classifies itself, such as Home Assistant's refusal); an error
// nobody classified is retryable (decision 2: the bound protects).
func Retryable(err error) bool {
	if err == nil {
		return false
	}
	var c interface{ Retryable() bool }
	if errors.As(err, &c) {
		return c.Retryable()
	}
	return true
}

// Join joins errs of parts tried one after the other (the persons of a
// message, the channels of a person): retryable when one of them is, fatal
// when none is.
func Join(errs ...error) error {
	joined := errors.Join(errs...)
	if joined == nil {
		return nil
	}
	for _, e := range errs {
		if e != nil && Retryable(e) {
			return Transient(joined)
		}
	}
	return Fatal(joined)
}

// Named is the wait the other side named in err, if any.
func Named(err error) (time.Duration, bool) {
	var n interface{ RetryAfter() time.Duration }
	if errors.As(err, &n) && n.RetryAfter() > 0 {
		return n.RetryAfter(), true
	}
	return 0, false
}

// WaitTooLong is the error of a call whose named wait is longer than the
// caller can wait.
type WaitTooLong struct {
	Err  error
	Wait time.Duration
}

func (w *WaitTooLong) Error() string {
	return fmt.Sprintf("asked to wait %s: %v", w.Wait.Round(time.Millisecond), w.Err)
}
func (w *WaitTooLong) Unwrap() error { return w.Err }

// GaveUp is the error of a retry that used up its attempts.
type GaveUp struct {
	Err      error
	Attempts int
}

func (g *GaveUp) Error() string { return fmt.Sprintf("%v (after %d attempts)", g.Err, g.Attempts) }
func (g *GaveUp) Unwrap() error { return g.Err }

// next is the wait before the next attempt after failed attempts that
// ended in err; a non-nil error when there is none: err itself when it is
// fatal or the wait does not fit the deadline, a GaveUp when the attempts are
// used up, a WaitTooLong when the named wait is longer than allowed.
func (p Policy) next(err error, failed int, left time.Duration, bounded bool) (time.Duration, error) {
	if !Retryable(err) {
		return 0, err
	}
	if p.Exhausted(failed) {
		return 0, &GaveUp{err, failed}
	}
	wait := p.Wait(failed)
	if named, ok := Named(err); ok {
		if p.Max > 0 && named > p.Max || bounded && named > left {
			return 0, &WaitTooLong{err, named}
		}
		if named > wait {
			wait = named
		}
	}
	if bounded && wait > left {
		return 0, err
	}
	return wait, nil
}

// OnRetry is told of each further attempt: the attempt that failed (1 for the
// first), the wait before the next, and the error.
type OnRetry func(failed int, wait time.Duration, err error)

// Warn is an OnRetry writing the warning of decision 7: what failed, the
// attempt, the bound, the wait and the error, with attrs.
func Warn(log *slog.Logger, what string, p Policy, attrs ...any) OnRetry {
	return func(failed int, wait time.Duration, err error) {
		args := append([]any{"attempt", failed, "of", p.Attempts, "wait", wait.Round(time.Millisecond).String(), "error", err}, attrs...)
		log.Warn(what+" failed, trying again", args...)
	}
}

// Do calls fn until it succeeds, its error is fatal, the attempts are used
// up or ctx ends; onRetry, when set, is told before each wait. It returns
// fn's last error (a GaveUp or WaitTooLong around it where that is why).
func Do(ctx context.Context, p Policy, fn func(context.Context) error, onRetry OnRetry) error {
	return DoBy(ctx, func(error) Policy { return p }, fn, onRetry)
}

// DoBy is Do with the policy chosen by the error: the waits fit the error
// (a 429 waits longer than a lost connection). The attempts are counted
// across errors; the policy of the last error bounds them.
func DoBy(ctx context.Context, choose func(error) Policy, fn func(context.Context) error, onRetry OnRetry) error {
	for failed := 1; ; failed++ {
		err := fn(ctx)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return err
		}
		deadline, bounded := ctx.Deadline()
		wait, stop := choose(err).next(err, failed, time.Until(deadline), bounded)
		if stop != nil {
			return stop
		}
		if onRetry != nil {
			onRetry(failed, wait, err)
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return err
		case <-t.C:
		}
	}
}

// Next is for attempts kept across ticks and restarts (a message's
// delivery): after failed attempts ending in err, when to try again; an
// error when not at all (err when fatal, a GaveUp when used up).
func (p Policy) Next(now time.Time, failed int, err error) (time.Time, error) {
	wait, stop := p.next(err, failed, 0, false)
	if stop != nil {
		return time.Time{}, stop
	}
	return now.Add(wait), nil
}
