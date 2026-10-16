package conversation_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sker65/mira/internal/config"
	"github.com/sker65/mira/internal/store"
	"github.com/sker65/mira/internal/testsupport/fakellm"
)

func turns(t *testing.T, h *harness, key string) (n int, first string) {
	t.Helper()
	msgs, err := h.store.ChatMessages(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		if m.Role == "user" {
			if n == 0 {
				first = m.Content
			}
			n++
		}
	}
	return n, first
}

// At max_turns the history is cut back to keep_turns in one jump, and the
// cut is in the log.
func TestHistoryIsCutAtMaxTurns(t *testing.T) {
	var steps []fakellm.Step
	for i := 0; i < 8; i++ {
		steps = append(steps, fakellm.Step{Content: "Gut."})
	}
	h := newHarnessWith(t, func(c *config.Config) { c.Sessions.MaxTurns, c.Sessions.KeepTurns = 5, 2 }, steps...)
	for i, text := range []string{"eins", "zwei", "drei", "vier", "fünf"} {
		h.say(t, config.ChannelTelegram, "7", text)
		h.clock.now = h.clock.now.Add(time.Minute)
		if n, _ := turns(t, h, "telegram:7"); n != i+1 {
			t.Fatalf("after %q: %d turns", text, n)
		}
	}
	// The sixth turn finds five and cuts to two before it, then adds itself.
	h.say(t, config.ChannelTelegram, "7", "sechs")
	n, first := turns(t, h, "telegram:7")
	if n != 3 || !strings.Contains(first, "vier") {
		t.Fatalf("turns = %d, first = %q", n, first)
	}
	logs := h.logs.String()
	for _, want := range []string{`msg="history compacted"`, "reason=max_turns", "turns_before=5", "turns_after=2", "kept_from="} {
		if !strings.Contains(logs, want) {
			t.Errorf("log lacks %s:\n%s", want, logs)
		}
	}
	// In between nothing is cut: the start stays for the cache.
	h.say(t, config.ChannelTelegram, "7", "sieben")
	if n, first := turns(t, h, "telegram:7"); n != 4 || !strings.Contains(first, "vier") {
		t.Errorf("cut again: %d turns, first %q", n, first)
	}
}

// A request near the end of the context window cuts the history for the
// next turn.
func TestHistoryIsCutNearTheContextLimit(t *testing.T) {
	h := newHarnessWith(t, func(c *config.Config) {
		c.Sessions.MaxTurns, c.Sessions.KeepTurns = 50, 1
		// The fake counts the prompt's characters as tokens: a few turns fill it.
		c.LLM.ContextTokens, c.LLM.CompactAtFraction = 1, 0.9
	}, fakellm.Step{Content: "A."}, fakellm.Step{Content: "B."}, fakellm.Step{Content: "C."})
	h.say(t, config.ChannelTelegram, "7", "eins")
	h.say(t, config.ChannelTelegram, "7", "zwei")
	h.say(t, config.ChannelTelegram, "7", "drei")
	if n, first := turns(t, h, "telegram:7"); n > 2 || !strings.Contains(first, "drei") && !strings.Contains(first, "zwei") {
		t.Errorf("turns = %d, first = %q", n, first)
	}
	if logs := h.logs.String(); !strings.Contains(logs, "reason=context") || !strings.Contains(logs, "prompt_tokens=") {
		t.Errorf("log:\n%s", logs)
	}
}

// The running thread ("Vorgang") is kept whole, even if it began before the
// cut.
func TestCutKeepsTheRunningThread(t *testing.T) {
	var steps []fakellm.Step
	for i := 0; i < 8; i++ {
		steps = append(steps, fakellm.Step{Content: "Gut."})
	}
	h := newHarnessWith(t, func(c *config.Config) { c.Sessions.MaxTurns, c.Sessions.KeepTurns = 4, 1 }, steps...)
	h.say(t, config.ChannelTelegram, "7", "eins")
	h.clock.now = h.clock.now.Add(time.Minute)
	threadStart := h.clock.now
	h.say(t, config.ChannelTelegram, "7", "zwei")
	ctx := context.Background()
	if err := h.store.PutThread(ctx, store.Thread{ID: "th1", Target: "aufgabe", Place: "telegram:7",
		State: store.ThreadCollecting, CreatedAt: threadStart, UpdatedAt: threadStart, ExpiresAt: threadStart.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := h.store.SetSessionThread(ctx, "telegram:7", "th1"); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"drei", "vier", "fünf"} {
		h.clock.now = h.clock.now.Add(time.Minute)
		h.say(t, config.ChannelTelegram, "7", text)
	}
	n, first := turns(t, h, "telegram:7")
	if !strings.Contains(first, "zwei") || n != 4 {
		t.Errorf("thread not kept: %d turns, first %q", n, first)
	}
	if !strings.Contains(h.logs.String(), "kept_for_thread=th1") {
		t.Errorf("log:\n%s", h.logs.String())
	}
}
