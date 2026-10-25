package prompt

import (
	"fmt"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/i18n"
)

// How a turn's person is known, when Mira recognized them rather than being
// told (Turn.PersonBy).
const (
	ByVoice = "voice" // the speaker-id service heard their voice
	ByAlone = "alone" // the "alone at home" rule
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
	// DeviceHints are the lines Home Assistant's Assist prompt adds for the
	// device that asks (its area, whether it can start timers). They come
	// last in HA's prompt and here too, so layer 4 stays the same for every
	// device (docs/spec/kontext.md).
	DeviceHints string
	// Person is who is speaking, when known; PersonProfile is their
	// compacted personal memory (the house's is layer 5).
	Person, PersonProfile string
	// PersonBy is how Mira knows the person: "" when they signed in or wrote
	// from their messenger, ByVoice or ByAlone when she recognized them
	// (docs/spec/sprecher.md). Then the model names them when it does
	// something personal for them, so a mix-up is noticed.
	PersonBy string
	// Memories are facts retrieved for this turn. The compacted profile lives
	// in layer 5; these are the situational hits.
	Memories []string
	// MessageContext is the long form of messages announced moments ago, so a
	// follow-up question can be answered without another lookup.
	MessageContext string
	// Thread is the thread ("Vorgang") this turn belongs to or that waits at
	// its place, rendered with its own heading by package threads.
	Thread string
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
	if s := strings.TrimSpace(t.DeviceHints); s != "" {
		b.WriteString("\n" + l.deviceHints + "\n")
		b.WriteString(s)
	}
	if t.Person != "" {
		b.WriteString("\n")
		switch t.PersonBy {
		case ByVoice:
			b.WriteString(fmt.Sprintf(l.speakerRecognized, t.Person, l.byVoice))
		case ByAlone:
			b.WriteString(fmt.Sprintf(l.speakerRecognized, t.Person, l.byAlone))
		default:
			b.WriteString(fmt.Sprintf(l.speaker, t.Person))
		}
		if s := strings.TrimSpace(t.PersonProfile); s != "" {
			b.WriteString("\n" + fmt.Sprintf(l.personProfile, t.Person) + "\n")
			b.WriteString(s)
		}
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
	if s := strings.TrimSpace(t.MessageContext); s != "" {
		b.WriteString("\n\n" + l.messageContext + "\n")
		b.WriteString(s)
	}
	if s := strings.TrimSpace(t.Thread); s != "" {
		b.WriteString("\n\n" + s)
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
