package tools_test

import (
	"testing"

	"github.com/miraassist/mira/internal/tools"
)

// When the satellite keeps listening: docs/spec/keep-listening.md, step 1.
// The engine's side (the question mark, the fourth source) is checked in
// internal/conversation; here the turn's state.

func TestKL01_AToolThatWaitsForTheUserMarksTheTurn(t *testing.T) {
	ts := &tools.TurnState{}
	if ts.WaitsForUser() {
		t.Fatal("a fresh turn waits for nothing")
	}
	ts.WaitForUser()
	if !ts.WaitsForUser() {
		t.Error("the mark did not hold")
	}
}

func TestKL03_AFinishInTheSameTurnClearsTheMark(t *testing.T) {
	ts := &tools.TurnState{}
	ts.WaitForUser()
	ts.DialogEnded()
	if ts.WaitsForUser() {
		t.Error("the dialog ended, and the turn still waits")
	}
	// A nil turn (no conversation: the web UI, an agent) takes both without
	// a panic and waits for nothing.
	var none *tools.TurnState
	none.WaitForUser()
	none.DialogEnded()
	if none.WaitsForUser() {
		t.Error("a turn that is none waits for the user")
	}
}
