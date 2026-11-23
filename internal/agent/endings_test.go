package agent

import (
	"context"
	"errors"
	"testing"
)

// EN-08 A run removes only its own cancel (docs/spec/endings.md): two runs
// under one id; the first ends, and the second can still be cancelled.
func TestEN08_ARunRemovesOnlyItsOwnCancel(t *testing.T) {
	e := setup(t)
	spec := Spec{Name: "tasks.automation", Profile: Isolated, Rights: AllRights}
	firstIn, firstGo := make(chan struct{}), make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- e.rt.Job(context.Background(), spec, CallContext{CallID: "th1", Rights: AllRights}, func(ctx context.Context) error {
			close(firstIn)
			<-firstGo
			return nil
		})
	}()
	<-firstIn
	secondIn := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- e.rt.Job(context.Background(), spec, CallContext{CallID: "th1", Rights: AllRights}, func(ctx context.Context) error {
			close(secondIn)
			<-ctx.Done()
			return ctx.Err()
		})
	}()
	<-secondIn
	// When the first ends
	close(firstGo)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	// Then the second can still be cancelled
	if !e.rt.Cancel("th1") {
		t.Fatal("no run to cancel")
	}
	if err := <-secondDone; !errors.Is(err, ErrCancelled) {
		t.Errorf("the second run: %v", err)
	}
}
