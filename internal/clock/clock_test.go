package clock

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

var ten = time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)

// fired is whether a timer's channel holds its time, without waiting.
func fired(c <-chan time.Time) (time.Time, bool) {
	select {
	case at := <-c:
		return at, true
	default:
		return time.Time{}, false
	}
}

func TestCK01_TheFakeClockStandsStillUntilItIsMoved(t *testing.T) {
	f := NewFake(ten)
	tm := f.NewTimer(5 * time.Minute)
	if _, ok := fired(tm.C()); ok || !f.Now().Equal(ten) {
		t.Fatalf("fired before the move, now %s", f.Now())
	}
	f.Advance(5 * time.Minute)
	if at, ok := fired(tm.C()); !ok || !at.Equal(ten.Add(5*time.Minute)) {
		t.Errorf("fired %v at %s", ok, at)
	}
	if !f.Now().Equal(ten.Add(5*time.Minute)) || f.Since(ten) != 5*time.Minute || f.Until(ten) != -5*time.Minute {
		t.Errorf("now %s", f.Now())
	}
}

func TestCK02_AdvanceFiresTheDueTimersInTheOrderOfTheirTimes(t *testing.T) {
	f := NewFake(ten)
	var order []int
	for _, m := range []int{3, 1, 2} {
		f.AfterFunc(time.Duration(m)*time.Minute, func() { order = append(order, m) })
	}
	f.Advance(3 * time.Minute)
	if !slices.Equal(order, []int{1, 2, 3}) {
		t.Errorf("order %v", order)
	}
}

func TestCK03_AnAfterFuncHasRunWhenAdvanceReturns(t *testing.T) {
	f := NewFake(ten)
	var at time.Time
	f.AfterFunc(time.Second, func() { at = f.Now() })
	f.Advance(time.Second)
	if !at.Equal(ten.Add(time.Second)) {
		t.Errorf("ran at %s", at)
	}
}

func TestCK04_AStoppedTimerDoesNotFire(t *testing.T) {
	f := NewFake(ten)
	tm := f.NewTimer(time.Minute)
	if !tm.Stop() {
		t.Error("Stop: not pending")
	}
	f.Advance(2 * time.Minute)
	if _, ok := fired(tm.C()); ok || tm.Stop() || f.Waiters() != 0 {
		t.Errorf("fired %v, waiters %d", ok, f.Waiters())
	}
}

func TestCK05_AResetTimerFiresAtItsNewTime(t *testing.T) {
	f := NewFake(ten)
	tm := f.NewTimer(time.Minute)
	if !tm.Reset(3 * time.Minute) {
		t.Error("Reset: not pending")
	}
	f.Advance(2 * time.Minute)
	if _, ok := fired(tm.C()); ok {
		t.Fatal("fired at its old time")
	}
	f.Advance(time.Minute)
	if at, ok := fired(tm.C()); !ok || !at.Equal(ten.Add(3*time.Minute)) {
		t.Errorf("fired %v at %s", ok, at)
	}
}

func TestCK06_ATickerFiresOnceForEachPeriodItPasses(t *testing.T) {
	f := NewFake(ten)
	tk := f.NewTicker(time.Minute)
	var ticks []time.Time
	for range 3 {
		f.Advance(time.Minute)
		if at, ok := fired(tk.C()); ok {
			ticks = append(ticks, at)
		}
	}
	want := []time.Time{ten.Add(time.Minute), ten.Add(2 * time.Minute), ten.Add(3 * time.Minute)}
	if !slices.EqualFunc(ticks, want, time.Time.Equal) {
		t.Errorf("ticks %v", ticks)
	}
	tk.Stop()
	f.Advance(time.Minute)
	if _, ok := fired(tk.C()); ok || f.Waiters() != 0 {
		t.Errorf("ticked after Stop, waiters %d", f.Waiters())
	}
}

func TestCK07_TheTestLearnsWhenTheCodeWaitsOnTheClock(t *testing.T) {
	f := NewFake(ten)
	went := make(chan time.Time)
	go func() { went <- <-f.After(time.Minute) }()
	<-f.AwaitWaiters(1) // the goroutine's timer is pending: moving now cannot come too early
	f.Advance(time.Minute)
	if at := <-went; !at.Equal(ten.Add(time.Minute)) {
		t.Errorf("went on at %s", at)
	}
	select {
	case <-f.AwaitWaiters(0):
	default:
		t.Error("AwaitWaiters(0) not closed at once")
	}
}

func TestCK08_TheRealClockIsTheTimePackage(t *testing.T) {
	c := Or(nil)
	before := time.Now()
	if now := c.Now(); now.Before(before) {
		t.Errorf("now %s before %s", now, before)
	}
	<-c.After(0)
	if Or(NewFake(ten)).Now() != ten {
		t.Error("Or replaced a given clock")
	}
}

func TestCK09_AdvanceToNextJumpsToTheEarliestTimer(t *testing.T) {
	f := NewFake(ten)
	late := f.NewTimer(time.Hour)
	early := f.NewTimer(10 * time.Second)
	if d := f.AdvanceToNext(); d != 10*time.Second {
		t.Errorf("moved %s", d)
	}
	if _, ok := fired(early.C()); !ok {
		t.Error("the early timer did not fire")
	}
	if _, ok := fired(late.C()); ok {
		t.Error("the late timer fired")
	}
	late.Stop()
	if d := f.AdvanceToNext(); d != 0 || !f.Now().Equal(ten.Add(10*time.Second)) {
		t.Errorf("moved %s with nothing pending", d)
	}
}

func TestCK10_DriveLetsEveryWaitOfTheCodeRunOut(t *testing.T) {
	f := NewFake(ten)
	var woke []time.Time
	f.Drive(func() {
		woke = append(woke, <-f.After(time.Minute))
		woke = append(woke, <-f.After(time.Hour))
	})
	want := []time.Time{ten.Add(time.Minute), ten.Add(time.Minute + time.Hour)}
	if !slices.EqualFunc(woke, want, time.Time.Equal) {
		t.Errorf("woke %v", woke)
	}
}

func TestCK11_AWaitOfZeroFiresAtOnce(t *testing.T) {
	f := NewFake(ten)
	if at, ok := fired(f.After(0)); !ok || !at.Equal(ten) {
		t.Errorf("After(0) fired %v at %s", ok, at)
	}
	ran := make(chan struct{})
	f.AfterFunc(-time.Second, func() { close(ran) })
	<-ran
	if f.Waiters() != 0 {
		t.Errorf("waiters %d", f.Waiters())
	}
}

func TestCK12_TheClockNeverGoesBack(t *testing.T) {
	f := NewFake(ten)
	defer func() {
		if recover() == nil {
			t.Error("Set before now did not panic")
		}
		if !f.Now().Equal(ten) {
			t.Errorf("now %s", f.Now())
		}
	}()
	f.Set(ten.Add(-time.Minute))
}

func TestCK13_AContextEndsWhenTheClockPassesItsTimeout(t *testing.T) {
	f := NewFake(ten)
	ctx, cancel := WithTimeout(context.Background(), f, time.Minute)
	defer cancel()
	child, stop := context.WithCancel(ctx)
	defer stop()
	if d, ok := ctx.Deadline(); !ok || !d.Equal(ten.Add(time.Minute)) {
		t.Errorf("deadline %s %v", d, ok)
	}
	f.Advance(59 * time.Second)
	if ctx.Err() != nil {
		t.Fatalf("ended early: %v", ctx.Err())
	}
	f.Advance(time.Second)
	<-child.Done()
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) || !errors.Is(child.Err(), context.DeadlineExceeded) {
		t.Errorf("err %v, child %v", ctx.Err(), child.Err())
	}
	// Cancelled before its time, it is cancelled, not expired, and its
	// timer is gone.
	ctx2, cancel2 := WithTimeout(context.Background(), f, time.Minute)
	cancel2()
	if !errors.Is(ctx2.Err(), context.Canceled) || f.Waiters() != 0 {
		t.Errorf("err %v, waiters %d", ctx2.Err(), f.Waiters())
	}
}
