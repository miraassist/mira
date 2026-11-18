package threads

import (
	"context"
	"fmt"
	"strings"

	"github.com/miraassist/mira/internal/store"
)

// howUndone is how a thread taken back ended, for its summary.
const howUndone = "undone"

// Summarize writes an ended thread's summary into the own history of every
// participant (docs/spec/thread-history.md, The summary when a thread ends)
// and drops the thread's history, which lived as long as the thread. A
// thread nobody spoke in has nothing to write. Every failure leaves a trace;
// the other participants get theirs.
func (m *Manager) Summarize(ctx context.Context, threadID, how string) {
	th, err := m.opts.Store.GetThread(ctx, threadID)
	if err != nil {
		m.log.Warn("could not write the thread's summary", "thread", threadID, "error", err)
		return
	}
	if len(th.Participants) == 0 || m.opts.Record == nil {
		return
	}
	histories := historyKeys(th)
	record := m.record(th, how, m.lastSaid(ctx, histories))
	t := m.texts()
	for _, p := range th.Participants {
		if err := m.opts.Record(ctx, p, record, t.recordAck); err != nil {
			m.log.Warn("could not write the thread's summary", "thread", th.ID, "participant", p, "error", err)
		}
	}
	m.log.Info("thread's summary written", "thread", th.ID, "how", how, "participants", len(th.Participants))
	if how == howUndone {
		return // ended before: its history went then
	}
	for _, key := range histories {
		if err := m.opts.Store.DeleteSession(ctx, key); err != nil {
			m.log.Warn("could not drop the thread's history", "thread", th.ID, "session", key, "error", err)
		}
	}
}

// historyKeys are the thread's histories, one per channel its participants
// spoke on ("<channel>:thread:<id>").
func historyKeys(th store.Thread) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range th.Participants {
		ch, _, ok := strings.Cut(p, ":")
		if !ok || seen[ch] {
			continue
		}
		seen[ch] = true
		out = append(out, ch+":thread:"+th.ID)
	}
	return out
}

// lastSaid is Mira's last answer in the thread's history, "" for none.
func (m *Manager) lastSaid(ctx context.Context, histories []string) string {
	last := ""
	var at int64
	for _, key := range histories {
		msgs, err := m.opts.Store.ChatMessages(ctx, key)
		if err != nil {
			m.log.Warn("could not read the thread's history for its summary", "session", key, "error", err)
			continue
		}
		for _, msg := range msgs {
			if msg.Role == "assistant" && strings.TrimSpace(msg.Content) != "" && msg.ID > at {
				last, at = strings.TrimSpace(msg.Content), msg.ID
			}
		}
	}
	return last
}

// record is the summary: what the thread was, how it ended, what it holds
// (its fields, or its plan), and Mira's last sentence in it.
func (m *Manager) record(th store.Thread, how, last string) string {
	t := m.texts()
	ended := map[string]string{store.ThreadDone: t.howDone, store.ThreadCancelled: t.howCancelled,
		store.ThreadExpired: t.howExpired, howUndone: t.howUndone}[how]
	if ended == "" {
		ended = how
	}
	var b strings.Builder
	b.WriteString(t.recordHeader + "\n")
	fmt.Fprintf(&b, t.recordLine, th.Target, ended, th.Request)
	if m.isPlan(th) {
		b.WriteString("\n" + m.planStatus(th))
	} else if len(th.Fields) > 0 {
		var parts []string
		for _, k := range sortedKeys(th.Fields) {
			parts = append(parts, k+": "+th.Fields[k])
		}
		b.WriteString("\n" + strings.Join(parts, "; "))
	}
	if last != "" {
		b.WriteString("\n" + fmt.Sprintf(t.recordLast, last))
	}
	return b.String()
}
