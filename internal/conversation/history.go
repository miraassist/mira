package conversation

import (
	"context"
	"errors"
	"strings"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/ha/structure"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/tools"
)

// The thread's history (docs/spec/thread-history.md): a turn that belongs to
// a thread runs in the thread's history, whoever speaks; a turn that comes
// into a thread late moves there when it is done.

// turnPre is what is known of a turn before its history is opened: what was
// just announced at its satellite, the thread it belongs to by the code's
// rules (Assignment 3a, 3b), and where its history ended before it.
type turnPre struct {
	messageText string
	followedUp  []string
	announced   string
	// thread is the thread whose history the turn runs in, "" for the own
	// history; own is the speaker's own history's key.
	thread string
	own    string
	// seq is the last message of the turn's history before the turn.
	seq int
}

// threadHistoryKey is a thread's history's session key.
func threadHistoryKey(ch config.Channel, threadID string) string {
	return SessionKey(ch, "thread:"+threadID)
}

// threadHistoryFor says whether a channel's turns of a thread run in the
// thread's history. A Telegram chat is one history that every turn of the
// chat runs in, also in a thread (docs/spec/telegram-history.md, decision 1).
func threadHistoryFor(ch config.Channel) bool {
	return ch != config.ChannelTelegram
}

func isThreadHistory(key string) bool {
	_, rest, ok := strings.Cut(key, ":")
	return ok && strings.HasPrefix(rest, "thread:")
}

// threadOf is the open thread a turn belongs to before the model runs: the
// direct answer to an announcement of the thread (3a), else the same
// conversation of Home Assistant or chat (3b). Who speaks does not count
// (decision 2); a private area stays a hard border. "" for none.
func (e *Engine) threadOf(ctx context.Context, req Request, followedUp []string, announced, area string, snap structure.Snapshot) string {
	if e.joinThread == nil {
		return ""
	}
	ids := []string{announced}
	for _, id := range followedUp {
		if n, err := e.store.GetMessage(ctx, id); err == nil {
			ids = append(ids, n.ThreadID)
		}
	}
	linked, err := e.store.ConversationThread(ctx, string(req.Channel), req.ConversationID)
	switch {
	case err == nil:
		ids = append(ids, linked)
	case !errors.Is(err, store.ErrNotFound):
		e.log.Warn("could not read the turn's thread; the turn runs in its own history",
			"conversation", req.ConversationID, "error", err)
	}
	for _, id := range ids {
		if id == "" {
			continue
		}
		th, err := e.store.GetThread(ctx, id)
		if err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				e.log.Warn("could not read the turn's thread; the turn runs in its own history",
					"conversation", req.ConversationID, "thread", id, "error", err)
			}
			continue
		}
		if store.ThreadOpen(th.State) && withinBorder(th, area, snap) {
			return th.ID
		}
	}
	return ""
}

// withinBorder: a turn in a private area joins only that area's threads, and
// no turn from outside joins one of a private area.
func withinBorder(th store.Thread, area string, snap structure.Snapshot) bool {
	thArea, ofArea := strings.CutPrefix(th.Place, "area:")
	if area != "" && snap.Private[area] {
		return ofArea && thArea == area
	}
	return !(ofArea && snap.Private[thArea])
}

// lastSeq is where a history ends before a turn; 0 when it cannot be read
// (then a late turn moves whole history with it, which the log says).
func (e *Engine) lastSeq(ctx context.Context, key string) int {
	seq, err := e.store.LastChatSeq(ctx, key)
	if err != nil {
		e.log.Warn("could not read where the history ends", "session", key, "error", err)
	}
	return seq
}

// joinHistory makes the thread's history belong to its thread, records the
// speaker as a participant and links the conversation, so the next turn of
// it is the thread's too. It returns the session as it is now.
func (e *Engine) joinHistory(ctx context.Context, sess store.Session, threadID, own string, ch config.Channel) store.Session {
	e.join(ctx, sess.Key, threadID, ch, sess.ConversationID, own)
	if now, err := e.store.GetSession(ctx, sess.Key); err == nil {
		return now
	}
	sess.ThreadID = threadID
	return sess
}

// join links a thread's history, a participant and a conversation to a
// thread; every failure leaves a trace and the rest goes on.
func (e *Engine) join(ctx context.Context, key, threadID string, ch config.Channel, conversationID, own string) {
	if err := e.joinThread(ctx, key, threadID); err != nil {
		e.log.Warn("could not link the thread's history to its thread", "thread", threadID, "session", key, "error", err)
	}
	if own != "" {
		if err := e.store.AddThreadParticipant(ctx, threadID, own); err != nil {
			e.log.Warn("could not record a participant of the thread", "thread", threadID, "participant", own, "error", err)
		}
	}
	if conversationID != "" {
		if err := e.store.LinkConversation(ctx, string(ch), conversationID, threadID); err != nil {
			e.log.Warn("could not link the conversation to its thread", "thread", threadID,
				"conversation", conversationID, "error", err)
		}
	}
}

// afterTurn moves a turn that started or took up a thread into the thread's
// history (decision 6): the turn's session was linked to another open
// thread than the one it ran in. Its own session's link is then put back.
// A Telegram chat's turn is not moved: the chat is linked and a participant.
func (e *Engine) afterTurn(ctx context.Context, key, own string, pre turnPre, req Request) {
	if e.joinThread == nil {
		return
	}
	sess, err := e.store.GetSession(ctx, key)
	if err != nil || sess.ThreadID == "" || sess.ThreadID == pre.thread {
		return
	}
	threadID := sess.ThreadID
	if !threadHistoryFor(req.Channel) {
		// A chat's turn stays in the chat; the chat keeps its link to the
		// thread and is a participant (docs/spec/telegram-history.md).
		if th, err := e.store.GetThread(ctx, threadID); err == nil && store.ThreadOpen(th.State) {
			e.join(ctx, key, threadID, req.Channel, req.ConversationID, own)
		}
		return
	}
	defer func() {
		if err := e.store.SetSessionThread(ctx, key, pre.thread); err != nil {
			e.log.Warn("could not put back the history's link to its thread", "session", key, "error", err)
		}
	}()
	th, err := e.store.GetThread(ctx, threadID)
	if err != nil || !store.ThreadOpen(th.State) {
		return // ended in the same turn: the turn stays where it is
	}
	to := threadHistoryKey(req.Channel, threadID)
	if _, err := e.store.UpsertSession(ctx, store.Session{Key: to, Channel: sess.Channel,
		ConversationID: sess.ConversationID, DeviceID: sess.DeviceID, AreaID: sess.AreaID, Language: sess.Language}, 0); err != nil {
		e.log.Error("could not move the turn into the thread's history", "thread", threadID, "session", key, "error", err)
		e.join(ctx, key, threadID, req.Channel, req.ConversationID, own)
		return
	}
	moved, err := e.store.MoveChatMessages(ctx, key, to, pre.seq)
	if err != nil {
		e.log.Error("could not move the turn into the thread's history", "thread", threadID, "session", key, "error", err)
	}
	// The skills the turn opened go with it.
	if open, err := e.store.SessionSkills(ctx, key); err == nil {
		for _, sk := range open {
			if err := e.store.AddSessionSkill(ctx, to, sk); err != nil {
				e.log.Warn("could not open a skill in the thread's history", "thread", threadID, "skill", sk, "error", err)
			}
		}
	}
	e.join(ctx, to, threadID, req.Channel, req.ConversationID, own)
	e.log.Info("turn moved into the thread's history", "thread", threadID, "from", key, "messages", moved)
}

// writeHeld writes the summaries the turn held back.
func (e *Engine) writeHeld(ctx context.Context, held []tools.HeldSummary) {
	if e.summarize == nil {
		return
	}
	for _, h := range held {
		e.summarize(ctx, h.Thread, h.How)
	}
}

// Record writes a thread's summary into an own history as one turn: the
// record and the answer that closes it (docs/spec/thread-history.md). The
// history is created or kept alive by its channel's time; no lock is taken,
// so a turn running in it may end after the record.
func (e *Engine) Record(ctx context.Context, key, record, ack string) error {
	ch, _, _ := strings.Cut(key, ":")
	if err := e.store.TouchSession(ctx, key, ch, e.cfg.SessionTTL(config.Channel(ch)).Duration()); err != nil {
		return err
	}
	return e.store.AppendChatMessages(ctx, key,
		store.ChatMessage{Role: "user", Content: record},
		store.ChatMessage{Role: "assistant", Content: ack})
}
