package retry

import (
	"bytes"
	"context"
	"errors"
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
	err := Do(context.Background(), Policy{Attempts: 5, First: time.Millisecond}, func(context.Context) error {
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
	_ = Do(context.Background(), Policy{Attempts: 5, First: time.Millisecond}, func(context.Context) error {
		calls++
		return classified{false}
	}, nil)
	if calls != 1 {
		t.Errorf("an error whose Retryable says no was called %d times", calls)
	}
}

func TestRY02_ARetryableErrorIsTriedUpToTheBound(t *testing.T) {
	calls := 0
	err := Do(context.Background(), Policy{Attempts: 3, First: time.Millisecond}, func(context.Context) error {
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
	err := Do(context.Background(), Policy{Attempts: 3, First: time.Millisecond}, func(context.Context) error {
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
	p := Policy{Attempts: 3, First: time.Millisecond}
	var times []time.Time
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := Do(ctx, p, func(context.Context) error {
		times = append(times, time.Now())
		if len(times) == 1 {
			return After(errDown, 200*time.Millisecond)
		}
		return nil
	}, nil)
	if err != nil || len(times) != 2 {
		t.Fatalf("attempts = %d, err = %v", len(times), err)
	}
	if d := times[1].Sub(times[0]); d < 200*time.Millisecond {
		t.Errorf("the second attempt came after %s, before the named 200ms", d)
	}

	short, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()
	calls := 0
	began := time.Now()
	err = Do(short, p, func(context.Context) error { calls++; return After(errDown, time.Second) }, nil)
	var w *WaitTooLong
	if !errors.As(err, &w) || w.Wait != time.Second || calls != 1 || time.Since(began) > 50*time.Millisecond {
		t.Fatalf("err = %v, calls = %d, took %s", err, calls, time.Since(began))
	}
	if !strings.Contains(err.Error(), "asked to wait 1s") || !errors.Is(err, errDown) {
		t.Errorf("error = %q", err)
	}
	// Longer than the policy's longest wait fails at once too.
	err = Do(context.Background(), Policy{Attempts: 3, First: time.Millisecond, Max: 10 * time.Millisecond},
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
	err := Do(context.Background(), p, func(context.Context) error {
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
