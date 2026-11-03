package conversation_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/testsupport/fakellm"
)

// warmHarness is a harness whose main model has warm slots
// (docs/spec/warm-slot.md).
func warmHarness(t *testing.T, script ...fakellm.Step) *harness {
	t.Helper()
	return newHarnessWith(t, func(c *config.Config) {
		m := c.Main()
		m.WarmSlots = true
		c.Models[config.MainModel] = m
	}, script...)
}

// waitFor waits up to two seconds for cond.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("waited in vain for %s", what)
}

// readings are the requests that read a prompt into a slot.
func readings(h *harness) []fakellm.Recorded {
	var out []fakellm.Recorded
	for _, r := range h.llm.Requests() {
		if r.Request.MaxTokens == 1 {
			out = append(out, r)
		}
	}
	return out
}

func mentions(r fakellm.Recorded, text string) bool {
	return strings.Contains(string(r.RawBody), text)
}

func lastTurn(h *harness) fakellm.Recorded {
	reqs := h.llm.Requests()
	for i := len(reqs) - 1; i >= 0; i-- {
		if reqs[i].Request.MaxTokens != 1 {
			return reqs[i]
		}
	}
	return fakellm.Recorded{}
}

// WS-01 and WS-02: after a change of the prompt a turn runs on the previous
// one in the warm slot; then the new one is read into the other slot, and
// the next turn runs on it there.
func TestWS01_ATurnAfterAChangeRunsOnThePreviousPromptInTheWarmSlot(t *testing.T) {
	h := warmHarness(t)
	h.say(t, config.ChannelVoice, "v1", "Hallo")
	if r := lastTurn(h); !r.HasSlot || r.Slot != 0 {
		t.Fatalf("first turn in slot %d (%v)", r.Slot, r.HasSlot)
	}
	h.store.SetProfile(context.Background(), store.OwnerHouse, "- Im Keller steht ein Kühlschrank")

	h.say(t, config.ChannelVoice, "v1", "Wie geht es?")
	if r := lastTurn(h); r.Slot != 0 || mentions(r, "Kühlschrank") {
		t.Fatalf("turn after the change: slot %d, new prompt %v", r.Slot, mentions(r, "Kühlschrank"))
	}

	// WS-02: the new prompt is read into slot 1 while the model is free.
	waitFor(t, "the reading", func() bool { return strings.Contains(h.logs.String(), `msg="slot warmed"`) })
	rd := readings(h)
	if len(rd) != 1 || rd[0].Slot != 1 || !mentions(rd[0], "Kühlschrank") {
		t.Fatalf("readings %d, slot %d", len(rd), rd[0].Slot)
	}
	h.say(t, config.ChannelVoice, "v1", "Und jetzt?")
	if r := lastTurn(h); r.Slot != 1 || !mentions(r, "Kühlschrank") {
		t.Fatalf("turn after the reading: slot %d, new prompt %v", r.Slot, mentions(r, "Kühlschrank"))
	}
}

// WS-02: the new prompt is read into slot 1 (covered in WS-01).
func TestWS02_TheNewPromptIsReadIntoTheOtherSlot(t *testing.T) {
	TestWS01_ATurnAfterAChangeRunsOnThePreviousPromptInTheWarmSlot(t)
}

// WS-03: a turn stops a running reading, which starts again afterwards.
func TestWS03_ATurnStopsARunningReading(t *testing.T) {
	h := warmHarness(t,
		fakellm.Step{Content: "ok"}, fakellm.Step{Content: "ok"},
		fakellm.Step{Content: "ok", Delay: 1500 * time.Millisecond}, // the first reading
	)
	h.say(t, config.ChannelVoice, "v1", "Hallo")
	h.store.SetProfile(context.Background(), store.OwnerHouse, "- Im Keller steht ein Kühlschrank")
	h.say(t, config.ChannelVoice, "v1", "Wie geht es?")
	waitFor(t, "the first reading", func() bool { return len(readings(h)) == 1 })

	h.say(t, config.ChannelVoice, "v1", "Noch da?")
	if r := lastTurn(h); r.Slot != 0 || mentions(r, "Kühlschrank") {
		t.Fatalf("turn during the reading: slot %d, new prompt %v", r.Slot, mentions(r, "Kühlschrank"))
	}
	waitFor(t, "the reading done", func() bool { return strings.Contains(h.logs.String(), `msg="slot warmed"`) })
	if n := len(readings(h)); n != 2 {
		t.Fatalf("%d readings", n)
	}
	if !strings.Contains(h.logs.String(), "reading of the prompt stopped for a turn") {
		t.Fatal("the first reading was not stopped")
	}
}

// WS-04: a newer change replaces one not yet read.
func TestWS04_ANewerChangeReplacesOneNotYetRead(t *testing.T) {
	h := warmHarness(t)
	h.say(t, config.ChannelVoice, "v1", "Hallo")
	h.store.SetProfile(context.Background(), store.OwnerHouse, "- Im Keller steht ein Kühlschrank")
	h.store.SetProfile(context.Background(), store.OwnerHouse, "- Im Keller steht eine Waschmaschine")
	h.say(t, config.ChannelVoice, "v1", "Wie geht es?")
	waitFor(t, "the reading", func() bool { return strings.Contains(h.logs.String(), `msg="slot warmed"`) })
	rd := readings(h)
	if len(rd) != 1 || !mentions(rd[0], "Waschmaschine") || mentions(rd[0], "Kühlschrank") {
		t.Fatalf("%d readings", len(rd))
	}
}

// At the start Mira reads the prompt before any turn (a warmup of the start
// becomes a reading); the first turn then finds it warm.
func TestWarmSlotsReadAtTheStartBeforeAnyTurn(t *testing.T) {
	h := warmHarness(t)
	h.engine.WarmupAll(context.Background(), "startup")
	waitFor(t, "the reading", func() bool { return strings.Contains(h.logs.String(), `msg="slot warmed"`) })
	if rd := readings(h); len(rd) != 1 || rd[0].Slot != 1 {
		t.Fatalf("readings %d", len(rd))
	}
	h.say(t, config.ChannelVoice, "v1", "Hallo")
	if r := lastTurn(h); r.Slot != 1 || r.CachedTokens == 0 {
		t.Fatalf("first turn: slot %d, cached %d", r.Slot, r.CachedTokens)
	}
}
