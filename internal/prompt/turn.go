package prompt

import (
	"strings"
	"time"

	"github.com/sker65/mira/internal/i18n"
)

// Turn is layer 7: everything that is new about this request. Every volatile
// value in the whole prompt belongs here and nowhere else.
type Turn struct {
	// Lang is the language of the labels.
	Lang i18n.Lang
	// Now is the wall clock the model should reason with.
	Now time.Time
	// ChannelContext describes where the request came from, for example which
	// satellite and therefore which room is meant without an explicit one.
	ChannelContext string
	// Memories are facts retrieved for this turn. The compacted profile lives
	// in layer 5; these are the situational hits.
	Memories []string
	// NoticeContext is the long form of notices announced moments ago, so a
	// follow-up question can be answered without another lookup.
	NoticeContext string
	// PendingConfirmation describes the critical action the agent asked about
	// in the previous turn and is now waiting for an answer to.
	PendingConfirmation string
	// Extra carries anything an adapter passes through, such as HA's
	// extra_system_prompt. It is deliberately confined to this layer.
	Extra string
	// Text is what the user said.
	Text string
}

// FormatTime renders a timestamp the way the model should read it back.
func FormatTime(l i18n.Lang, t time.Time) string { return i18n.FormatTime(l, t) }

// Render produces the text of layer 7. The order of the sections is fixed so
// two turns differ only where they really differ.
func (t Turn) Render() string {
	l := textsFor(t.Lang)
	var b strings.Builder
	b.WriteString(l.headerTime)
	b.WriteString("\n")
	b.WriteString(l.timeLabel)
	b.WriteString(FormatTime(t.Lang, t.Now))
	if s := strings.TrimSpace(t.ChannelContext); s != "" {
		b.WriteString("\n")
		b.WriteString(s)
	}
	if len(t.Memories) > 0 {
		b.WriteString("\n")
		b.WriteString(l.remember)
		for _, m := range t.Memories {
			if m = strings.TrimSpace(m); m != "" {
				b.WriteString("\n- ")
				b.WriteString(m)
			}
		}
	}
	if s := strings.TrimSpace(t.NoticeContext); s != "" {
		b.WriteString("\n\n" + l.noticeContext + "\n")
		b.WriteString(s)
	}
	if s := strings.TrimSpace(t.PendingConfirmation); s != "" {
		b.WriteString("\n\n" + l.pendingConfirmation)
		b.WriteString(s)
		b.WriteString("\n" + l.pendingRule)
	}
	if s := strings.TrimSpace(t.Extra); s != "" {
		b.WriteString("\n\n" + l.extraContext + "\n")
		b.WriteString(s)
	}
	b.WriteString("\n\n" + l.headerMessage + "\n")
	b.WriteString(t.Text)
	return b.String()
}
