package retry

import (
	"bytes"
	"context"
	"errors"
	"github.com/miraassist/mira/internal/clock"
	"log/slog"
	"strings"
	"testing"
	"time"
)

var errDown = errors.New("down")

type classified struct{ ok bool }

func (c classified) Error() string   { return "classified" }
func (c classified) Retryable() bool { return c.ok }

func TestRY01_AFatalErrorIsNotTriedAgain(t *testing.T) {
	calls := 0
	err := driven(context.Background(), Policy{Attempts: 5, First: time.Millisecond}, func(context.Context) error {
		calls++
		return Fatal(errDown)
	}, nil)
	if calls != 1 || !errors.Is(err, errDown) {
		t.Fatalf("calls = %d, err = %v", calls, err)
	}
	if Retryable(err) {
		t.Error("a fatal error says it is retryable")
	}
	calls = 0
	_ = driven(context.Background(), Policy{Attempts: 5, First: time.Millisecond}, func(context.Context) error {
		calls++
		return classified{false}
	}, nil)
	if calls != 1 {
		t.Errorf("an error whose Retryable says no was called %d times", calls)
	}
}

func TestRY02_ARetryableErrorIsTriedUpToTheBound(t *testing.T) {
	calls := 0
	err := driven(context.Background(), Policy{Attempts: 3, First: time.Millisecond}, func(context.Context) error {
		calls++
		return classified{true}
	}, nil)
	var g *GaveUp
	if calls != 3 || !errors.As(err, &g) || g.Attempts != 3 {
		t.Fatalf("calls = %d, err = %v", calls, err)
	}
	if !strings.Contains(err.Error(), "after 3 attempts") {
		t.Errorf("error = %q", err)
	}
}

func TestRY03_AnErrorNobodyClassifiedCountsAsRetryable(t *testing.T) {
	calls := 0
	err := driven(context.Background(), Policy{Attempts: 3, First: time.Millisecond}, func(context.Context) error {
		calls++
		if calls == 1 {
			return errDown
		}
		return nil
	}, nil)
	if err != nil || calls != 2 {
		t.Fatalf("calls = %d, err = %v", calls, err)
	}
}

func TestRY04_WaitsGrowAsThePolicySays(t *testing.T) {
	s := time.Second
	for _, c := range []struct {
		p    Policy
		want []time.Duration
	}{
		{Policy{First: s, Max: 10 * s, Growth: Exponential}, []time.Duration{s, 2 * s, 4 * s, 8 * s, 10 * s, 10 * s}},
		{Policy{First: s, Max: 10 * s, Growth: Linear}, []time.Duration{s, 2 * s, 3 * s, 4 * s, 5 * s}},
		{Policy{First: s, Max: 10 * s, Growth: Fixed}, []time.Duration{s, s, s}},
		{Policy{First: time.Minute, Growth: Exponential}, []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute}},
	} {
		for i, w := range c.want {
			if got := c.p.Wait(i + 1); got != w {
				t.Errorf("%+v wait %d = %s, want %s", c.p, i+1, got, w)
			}
		}
	}
	// A wait far down an exponential policy does not overflow past its Max.
	if got := (Policy{First: time.Second, Max: time.Hour}).Wait(200); got != time.Hour {
		t.Errorf("wait 200 = %s", got)
	}
}

func TestRY05_ANamedWaitIsKeptOrTheCallFailsAtOnce(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC))
	p := Policy{Attempts: 3, First: time.Millisecond, Clock: clk}
	var times []time.Time
	done := make(chan error, 1)
	go func() {
		done <- Do(context.Background(), p, func(context.Context) error {
			times = append(times, clk.Now())
			if len(times) == 1 {
				return After(errDown, 200*time.Millisecond)
			}
			return nil
		}, nil)
	}()
	<-clk.AwaitWaiters(1)               // Do waits on the clock
	clk.Advance(199 * time.Millisecond) // the policy's 1 ms has passed, the named 200 ms not
	if clk.Waiters() != 1 {
		t.Fatal("the second attempt came before the named wait")
	}
	clk.Advance(time.Millisecond)
	if err := <-done; err != nil || len(times) != 2 || times[1].Sub(times[0]) != 200*time.Millisecond {
		t.Fatalf("attempts %v, err %v", times, err)
	}

	// A named wait longer than the call's deadline fails at once, without
	// waiting.
	short, cancel := clock.WithTimeout(context.Background(), clk, time.Hour) // the deadline on the same clock
	defer cancel()
	calls := 0
	err := Do(short, p, func(context.Context) error { calls++; return After(errDown, 2*time.Hour) }, nil)
	var w *WaitTooLong
	if !errors.As(err, &w) || w.Wait != 2*time.Hour || calls != 1 {
		t.Fatalf("err = %v, calls = %d", err, calls)
	}
	if !strings.Contains(err.Error(), "asked to wait 2h0m0s") || !errors.Is(err, errDown) {
		t.Errorf("error = %q", err)
	}
	// Longer than the policy's longest wait fails at once too.
	err = driven(context.Background(), Policy{Attempts: 3, First: time.Millisecond, Max: 10 * time.Millisecond},
		func(context.Context) error { return After(errDown, time.Second) }, nil)
	if !errors.As(err, &w) {
		t.Errorf("err = %v", err)
	}
}

func TestRY06_EveryFurtherAttemptLeavesAWarning(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	p := Policy{Attempts: 3, First: time.Millisecond}
	calls := 0
	err := driven(context.Background(), p, func(context.Context) error {
		calls++
		if calls == 1 {
			return errDown
		}
		return nil
	}, Warn(log, "private delivery", p, "message", "m1"))
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Count(out, "level=WARN") != 1 {
		t.Fatalf("log = %s", out)
	}
	for _, want := range []string{`msg="private delivery failed, trying again"`, "attempt=1", "of=3", "wait=1ms", "error=down", "message=m1"} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %s: %s", want, out)
		}
	}
}

func TestJoinIsRetryableWhenOnePartIs(t *testing.T) {
	if !Retryable(Join(Fatal(errDown), errDown)) {
		t.Error("one retryable part: not retryable")
	}
	if Retryable(Join(Fatal(errDown), classified{false})) {
		t.Error("all parts fatal: retryable")
	}
	if Retryable(Transient(Fatal(errDown))) != true || Retryable(Fatal(Transient(errDown))) != false {
		t.Error("the outermost mark does not win")
	}
}

func TestNextKeepsAttemptsAcrossTicks(t *testing.T) {
	p := Policy{Attempts: 10, First: time.Minute, Growth: Exponential}
	now := time.Date(2026, 11, 11, 8, 0, 0, 0, time.UTC)
	at, err := p.Next(now, 3, errDown)
	if err != nil || !at.Equal(now.Add(4*time.Minute)) {
		t.Fatalf("next = %s, %v", at, err)
	}
	if _, err := p.Next(now, 10, errDown); err == nil {
		t.Error("the 10th failure is not given up")
	}
	if _, err := p.Next(now, 1, Fatal(errDown)); !errors.Is(err, errDown) {
		t.Errorf("fatal: %v", err)
	}
	at, err = p.Next(now, 1, After(errDown, 5*time.Minute))
	if err != nil || !at.Equal(now.Add(5*time.Minute)) {
		t.Errorf("named: %s, %v", at, err)
	}
}

// driven is Do on a fake clock, moved on to each wait as Do reaches it: the
// waits pass without real time (docs/spec/tests.md).
func driven(ctx context.Context, p Policy, fn func(context.Context) error, onRetry OnRetry) error {
	clk := clock.NewFake(time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC))
	p.Clock = clk
	done := make(chan error, 1)
	go func() { done <- Do(ctx, p, fn, onRetry) }()
	for {
		select {
		case err := <-done:
			return err
		case <-clk.AwaitWaiters(1):
			clk.AdvanceToNext()
		}
	}
}
