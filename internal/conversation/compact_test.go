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
		m := c.Main()
		m.ContextTokens, m.CompactAtFraction = 1, 0.9
		c.Models[config.MainModel] = m
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
	if err := h.store.PutThread(ctx, &store.Thread{ID: "th1", Target: "aufgabe", Place: "telegram:7",
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

// The log says how large each request is and what went into it that changes:
// the turns of history grow turn by turn, and the long start is described
// only the first time, not while it stays the same.
func TestLogDescribesTheContext(t *testing.T) {
	h := newHarness(t, fakellm.Step{Content: "Eins."}, fakellm.Step{Content: "Zwei."}, fakellm.Step{Content: "Drei."})
	for _, text := range []string{"Erste Frage", "Zweite Frage", "Dritte Frage"} {
		h.say(t, config.ChannelTelegram, "c1", text)
	}
	var contexts, rounds []string
	for _, line := range strings.Split(h.logs.String(), "\n") {
		switch {
		case strings.Contains(line, `msg="context built"`):
			contexts = append(contexts, line)
		case strings.Contains(line, `msg="model round"`):
			rounds = append(rounds, line)
		}
	}
	if len(contexts) != 3 || len(rounds) != 3 {
		t.Fatalf("%d context lines, %d round lines:\n%s", len(contexts), len(rounds), h.logs.String())
	}
	for i, want := range []string{"history_turns=0", "history_turns=1", "history_turns=2"} {
		if !strings.Contains(contexts[i], want) {
			t.Errorf("turn %d: %s lacks %s", i+1, contexts[i], want)
		}
	}
	if !strings.Contains(contexts[0], "prefix_changed=true") || !strings.Contains(contexts[0], "tools_chars=") {
		t.Errorf("the first turn must describe the start: %s", contexts[0])
	}
	for _, later := range contexts[1:] {
		if strings.Contains(later, "prefix_changed") {
			t.Errorf("an unchanged start must not be described again: %s", later)
		}
	}
	for _, r := range rounds {
		for _, field := range []string{"ms=", "messages=", "message_chars=", "tools=", "tool_chars=", "prompt_tokens=", "cached_tokens="} {
			if !strings.Contains(r, field) {
				t.Errorf("round line lacks %s: %s", field, r)
			}
		}
	}
}

// The chat's history (docs/spec/telegram-history.md, step 5).

// runningThread puts an open thread created at "at" and links the chat's
// history to it.
func runningThread(t *testing.T, h *harness, at time.Time) {
	t.Helper()
	ctx := context.Background()
	if err := h.store.PutThread(ctx, &store.Thread{ID: "th1", Target: "aufgabe", Place: "telegram:7",
		State: store.ThreadCollecting, CreatedAt: at, UpdatedAt: at, ExpiresAt: at.Add(48 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := h.store.SetSessionThread(ctx, "telegram:7", "th1"); err != nil {
		t.Fatal(err)
	}
}

func gut(n int) []fakellm.Step {
	var steps []fakellm.Step
	for i := 0; i < n; i++ {
		steps = append(steps, fakellm.Step{Content: "Gut."})
	}
	return steps
}

// TC-13 A thread is kept from the turn it came into the chat
func TestTC13_AThreadIsKeptFromTheTurnItCameIntoTheChat(t *testing.T) {
	h := newHarnessWith(t, func(c *config.Config) { c.Sessions.MaxTurns, c.Sessions.KeepTurns = 6, 3 }, gut(8)...)
	created := h.clock.now.Add(-24 * time.Hour) // before the chat's oldest turn
	for i, text := range []string{"t1", "t2", "t3", "t4", "t5", "t6"} {
		h.say(t, config.ChannelTelegram, "7", text)
		if i == 4 { // the thread comes into the chat with t5
			runningThread(t, h, created)
			if err := h.store.LinkConversation(context.Background(), "telegram", "7", "th1"); err != nil {
				t.Fatal(err)
			}
		}
		h.clock.now = h.clock.now.Add(time.Minute)
	}
	h.say(t, config.ChannelTelegram, "7", "t7")
	if n, first := turns(t, h, "telegram:7"); n != 4 || !strings.Contains(first, "t4") {
		t.Errorf("turns = %d, first = %q", n, first)
	}
}

// TC-14 Linking the chat to the same thread again keeps the time
func TestTC14_LinkingTheChatToTheSameThreadAgainKeepsTheTime(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.store.LinkConversation(ctx, "telegram", "7", "th1"); err != nil {
		t.Fatal(err)
	}
	_, first, _ := h.store.ConversationLink(ctx, "telegram", "7")
	h.clock.now = h.clock.now.Add(time.Hour)
	if err := h.store.LinkConversation(ctx, "telegram", "7", "th1"); err != nil {
		t.Fatal(err)
	}
	if _, at, err := h.store.ConversationLink(ctx, "telegram", "7"); err != nil || !at.Equal(first) {
		t.Errorf("linked at %v (%v), want %v", at, err, first)
	}
	if err := h.store.LinkConversation(ctx, "telegram", "7", "th2"); err != nil {
		t.Fatal(err)
	}
	if id, at, err := h.store.ConversationLink(ctx, "telegram", "7"); err != nil || id != "th2" || !at.After(first) {
		t.Errorf("another thread: %s at %v (%v)", id, at, err)
	}
}

// TC-15 At the context limit the running thread is cut too
func TestTC15_AtTheContextLimitTheRunningThreadIsCutToo(t *testing.T) {
	h := newHarnessWith(t, func(c *config.Config) {
		c.Sessions.MaxTurns, c.Sessions.KeepTurns = 50, 1
		m := c.Main()
		m.ContextTokens, m.CompactAtFraction = 1, 0.9
		c.Models[config.MainModel] = m
	}, gut(3)...)
	created := h.clock.now.Add(-time.Hour)
	for i, text := range []string{"eins", "zwei", "drei"} {
		h.clock.now = h.clock.now.Add(time.Minute)
		h.say(t, config.ChannelTelegram, "7", text)
		if i == 0 { // the thread is running since before "eins"
			runningThread(t, h, created)
		}
	}
	if n, first := turns(t, h, "telegram:7"); n > 2 || strings.Contains(first, "eins") {
		t.Errorf("the thread was kept at the context limit: %d turns, first %q", n, first)
	}
	if logs := h.logs.String(); !strings.Contains(logs, "reason=context") || !strings.Contains(logs, "thread_cut=th1") ||
		strings.Contains(logs, "kept_for_thread") {
		t.Errorf("log:\n%s", logs)
	}
}

// TC-16 A cut the running thread prevents is logged
func TestTC16_ACutTheRunningThreadPreventsIsLogged(t *testing.T) {
	h := newHarnessWith(t, func(c *config.Config) { c.Sessions.MaxTurns, c.Sessions.KeepTurns = 4, 1 }, gut(6)...)
	h.say(t, config.ChannelTelegram, "7", "eins")
	runningThread(t, h, h.clock.now)
	if err := h.store.LinkConversation(context.Background(), "telegram", "7", "th1"); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"zwei", "drei", "vier", "fünf"} {
		h.clock.now = h.clock.now.Add(time.Minute)
		h.say(t, config.ChannelTelegram, "7", text)
	}
	if n, first := turns(t, h, "telegram:7"); n != 5 || !strings.Contains(first, "eins") {
		t.Errorf("turns = %d, first = %q", n, first)
	}
	logs := h.logs.String()
	for _, want := range []string{`msg="history cannot be cut: the running thread began at its first turn"`, "session=telegram:7", "thread=th1"} {
		if !strings.Contains(logs, want) {
			t.Errorf("log lacks %s:\n%s", want, logs)
		}
	}
}
