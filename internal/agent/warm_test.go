package agent

import (
	"context"
	"testing"
)

// WS-07: a helper runs in the spare slot of a model with warm slots, and a
// reading there stops for it; it waits to start again (docs/spec/warm-slot.md).
func TestWS07_AHelperRunsInTheSpareSlotAndLeavesTheWarmOneAlone(t *testing.T) {
	e := setup(t)
	m := e.cfg.Main()
	m.WarmSlots = true
	e.cfg.Models["main"] = m
	w := e.rt.Warm("main")
	w.Changed()
	reading, slot, ok := w.BeginReading(context.Background())
	if !ok || slot != 1 {
		t.Fatalf("reading %v in slot %d", ok, slot)
	}
	if _, err := e.rt.Call(context.Background(), background("helper", ReadHouse), "x", CallContext{Rights: AllRights}); err != nil {
		t.Fatal(err)
	}
	if got := e.main.reqs[0].IDSlot; got == nil || *got != 1 {
		t.Fatalf("helper's slot %v", got)
	}
	if reading.Err() == nil {
		t.Fatal("the reading was not stopped")
	}
	w.EndReading(reading, false)
	if _, _, ok := w.BeginReading(context.Background()); !ok {
		t.Fatal("the stopped reading does not wait to start again")
	}
}

// A model without warm slots has no keeper.
func TestWS05_AModelWithoutWarmSlotsHasNoKeeper(t *testing.T) {
	e := setup(t)
	if e.rt.Warm("main") != nil {
		t.Fatal("keeper without warm_slots")
	}
}
