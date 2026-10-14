package prompt

import (
	"fmt"
	"strings"
	"time"
)

// Turn is layer 7: everything that is new about this request. Every volatile
// value in the whole prompt belongs here and nowhere else.
type Turn struct {
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

var germanWeekdays = [...]string{
	time.Sunday:    "Sonntag",
	time.Monday:    "Montag",
	time.Tuesday:   "Dienstag",
	time.Wednesday: "Mittwoch",
	time.Thursday:  "Donnerstag",
	time.Friday:    "Freitag",
	time.Saturday:  "Samstag",
}

var germanMonths = [...]string{
	time.January:   "Januar",
	time.February:  "Februar",
	time.March:     "März",
	time.April:     "April",
	time.May:       "Mai",
	time.June:      "Juni",
	time.July:      "Juli",
	time.August:    "August",
	time.September: "September",
	time.October:   "Oktober",
	time.November:  "November",
	time.December:  "Dezember",
}

// FormatTime renders a timestamp the way the model should read it back.
func FormatTime(t time.Time) string {
	return fmt.Sprintf("%s, %d. %s %d, %02d:%02d Uhr",
		germanWeekdays[t.Weekday()], t.Day(), germanMonths[t.Month()], t.Year(),
		t.Hour(), t.Minute())
}

// Render produces the text of layer 7. The order of the sections is fixed so
// two turns differ only where they really differ.
func (t Turn) Render() string {
	var b strings.Builder
	b.WriteString(headerTime)
	b.WriteString("\nZeit: ")
	b.WriteString(FormatTime(t.Now))
	if s := strings.TrimSpace(t.ChannelContext); s != "" {
		b.WriteString("\n")
		b.WriteString(s)
	}
	if len(t.Memories) > 0 {
		b.WriteString("\nDu erinnerst dich:")
		for _, m := range t.Memories {
			if m = strings.TrimSpace(m); m != "" {
				b.WriteString("\n- ")
				b.WriteString(m)
			}
		}
	}
	if s := strings.TrimSpace(t.NoticeContext); s != "" {
		b.WriteString("\n\nDu hast gerade gemeldet (Details, falls nachgefragt wird):\n")
		b.WriteString(s)
	}
	if s := strings.TrimSpace(t.PendingConfirmation); s != "" {
		b.WriteString("\n\nDu wartest auf eine Bestätigung für: ")
		b.WriteString(s)
		b.WriteString("\nWenn der Nutzer jetzt zustimmt, führe genau diese Aktion aus. Wenn er ablehnt oder etwas anderes sagt, führe sie nicht aus.")
	}
	if s := strings.TrimSpace(t.Extra); s != "" {
		b.WriteString("\n\nZusätzlicher Kontext:\n")
		b.WriteString(s)
	}
	b.WriteString("\n\n## Nachricht\n")
	b.WriteString(t.Text)
	return b.String()
}
