package conversation

import (
	"context"
	"errors"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/store"
)

// Compaction (docs/spec/context.md): the model sees the running thread and
// the last turns. When the history reaches MaxTurns, or a request used
// CompactAtFraction of the model's context window, it is cut back to
// KeepTurns: whole turns only, in one jump, so its start stays the same in
// between and the cache holds. The running thread is kept whole, in a
// Telegram chat from the turn it came into the chat, and not at the context
// limit (docs/spec/telegram-history.md, decision 7). Every cut is logged,
// since it breaks the cache on purpose and loses what was said.

// compactIfLong cuts the history before a turn when it has reached
// MaxTurns.
func (e *Engine) compactIfLong(ctx context.Context, key string, sess store.Session) {
	msgs, err := e.store.ChatMessages(ctx, key)
	if err != nil {
		e.log.Warn("could not read the history to compact it", "session", key, "error", err)
		return
	}
	if turnStarts(msgs) < e.cfg.Sessions.MaxTurns {
		return
	}
	e.compact(ctx, key, sess, msgs, "max_turns", "max_turns", e.cfg.Sessions.MaxTurns)
}

// compactForContext cuts the history after a turn whose request used
// CompactAtFraction of the model's context window, so the next one fits.
func (e *Engine) compactForContext(ctx context.Context, key string, promptTokens int, model config.LLM) {
	if model.ContextTokens <= 0 || model.CompactAtFraction <= 0 {
		return
	}
	limit := int(float64(model.ContextTokens) * model.CompactAtFraction)
	if promptTokens < limit {
		return
	}
	sess, err := e.store.GetSession(ctx, key)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		e.log.Warn("could not read the session to compact it", "session", key, "error", err)
		return
	}
	msgs, err := e.store.ChatMessages(ctx, key)
	if err != nil {
		e.log.Warn("could not read the history to compact it", "session", key, "error", err)
		return
	}
	e.compact(ctx, key, sess, msgs, "context", "prompt_tokens", promptTokens, "limit", limit,
		"context_tokens", model.ContextTokens)
}

// compact drops everything before the last KeepTurns turns, but never the
// running thread.
func (e *Engine) compact(ctx context.Context, key string, sess store.Session, msgs []store.ChatMessage, reason string, detail ...any) {
	starts := turnIndexes(msgs)
	keep := e.cfg.Sessions.KeepTurns
	if len(starts) <= keep {
		e.log.Warn("history cannot be cut further: it has no more turns than are kept",
			append([]any{"session", key, "reason", reason, "turns", len(starts), "keep_turns", keep}, detail...)...)
		return
	}
	cut := starts[len(starts)-keep]
	protected, cutThread := "", ""
	if from, thread := e.threadStart(ctx, sess, msgs, starts); from >= 0 && from < cut {
		if reason == "context" {
			// At the context limit the thread is cut too: its state is in
			// every turn's context, the history must fit the window.
			cutThread = thread
		} else {
			cut, protected = from, thread
		}
	}
	if cut <= 0 {
		e.log.Warn("history cannot be cut: the running thread began at its first turn",
			append([]any{"session", key, "thread", protected, "reason", reason, "turns", len(starts)}, detail...)...)
		return
	}
	if err := e.store.TruncateChatMessagesBefore(ctx, key, msgs[cut].Seq); err != nil {
		e.log.Error("history compaction failed", "session", key, "error", err)
		return
	}
	before := len(starts)
	after := 0
	for _, i := range starts {
		if i >= cut {
			after++
		}
	}
	attrs := []any{
		"session", key, "reason", reason,
		"turns_before", before, "turns_after", after, "messages_removed", cut,
		"removed_from", msgs[0].CreatedAt.In(e.loc).Format(time.DateTime),
		"kept_from", msgs[cut].CreatedAt.In(e.loc).Format(time.DateTime),
	}
	if protected != "" {
		attrs = append(attrs, "kept_for_thread", protected)
	}
	if cutThread != "" {
		attrs = append(attrs, "thread_cut", cutThread)
	}
	e.log.Info("history compacted", append(attrs, detail...)...)
}

// threadStart is the index of the first turn of the session's running
// thread, which stays whole; -1 without one. In a Telegram chat's history
// it is the turn in which the thread came into the chat: the last turn that
// began at or before the chat's link to it.
func (e *Engine) threadStart(ctx context.Context, sess store.Session, msgs []store.ChatMessage, starts []int) (int, string) {
	if sess.ThreadID == "" {
		return -1, ""
	}
	th, err := e.store.GetThread(ctx, sess.ThreadID)
	if err != nil {
		return -1, ""
	}
	switch th.State {
	case store.ThreadDone, store.ThreadCancelled, store.ThreadExpired:
		return -1, ""
	}
	if sess.Channel == string(config.ChannelTelegram) && !isThreadHistory(sess.Key) {
		thread, at, err := e.store.ConversationLink(ctx, sess.Channel, sess.ConversationID)
		switch {
		case err == nil && thread == th.ID:
			from := starts[0]
			for _, i := range starts {
				if msgs[i].CreatedAt.After(at) {
					break
				}
				from = i
			}
			return from, th.ID
		case err != nil && !errors.Is(err, store.ErrNotFound):
			e.log.Warn("could not read the chat's link to its thread; the thread is kept from its creation",
				"session", sess.Key, "thread", th.ID, "error", err)
		}
	}
	for _, i := range starts {
		if !msgs[i].CreatedAt.Before(th.CreatedAt) {
			return i, th.ID
		}
	}
	return -1, ""
}

// turnIndexes are where turns start: each user message. Cutting there never
// separates a tool call from its result.
func turnIndexes(msgs []store.ChatMessage) []int {
	var out []int
	for i, m := range msgs {
		if m.Role == "user" {
			out = append(out, i)
		}
	}
	return out
}

func turnStarts(msgs []store.ChatMessage) int { return len(turnIndexes(msgs)) }
