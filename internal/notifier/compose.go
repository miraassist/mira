package notifier

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/sker65/mira/internal/i18n"
	"github.com/sker65/mira/internal/store"
)

// Composer runs one isolated model request with a JSON answer (the
// engine's Assess). With it, several messages become one announcement the
// model words as a whole; without it, or when it fails, the fixed form is
// used.
type Composer func(ctx context.Context, instruction string, schema json.RawMessage) (string, error)

// composeTimeout bounds the model's wording; the fixed form follows.
const composeTimeout = 20 * time.Second

// olderThan is from when a message gets its time said: "the washing machine
// is done" heard hours later needs "at one".
const olderThan = time.Hour

var composeSchema = json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"},"ids":{"type":"array","items":{"type":"string"}}},"required":["text","ids"]}`)

// compose words messages for speaking. One message is said as it is (with
// its time if it is older); several go to the model as one announcement.
func (n *Notifier) compose(ctx context.Context, messages []store.Message) string {
	now := n.clock().In(n.loc)
	morning := n.goodMorning(now)
	if len(messages) > 1 && n.composer != nil {
		if text, err := n.composeWithModel(ctx, messages, now, morning); err == nil {
			return text
		} else {
			n.log.Warn("model could not word the announcement, using the fixed form", "error", err)
		}
	}
	return n.fixed(ctx, messages, now, morning)
}

func (n *Notifier) composeWithModel(ctx context.Context, messages []store.Message, now time.Time, morning bool) (string, error) {
	t := textsFor(n.langOf(ctx))
	var list strings.Builder
	for _, m := range messages {
		fmt.Fprintf(&list, "- [%s] %s · %s %d · %s\n", m.ID, n.when(ctx, m.CreatedAt, now), t.importance, m.Importance, strings.TrimSpace(m.Spoken))
	}
	instruction := fmt.Sprintf(t.composeTask, now.Format("15:04"), list.String())
	if morning {
		instruction += t.composeMorning
	}
	ctx, cancel := context.WithTimeout(ctx, composeTimeout)
	defer cancel()
	raw, err := n.composer(ctx, instruction, composeSchema)
	if err != nil {
		return "", err
	}
	var out struct {
		Text string   `json:"text"`
		IDs  []string `json:"ids"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return "", fmt.Errorf("answer is not JSON: %w", err)
	}
	if strings.TrimSpace(out.Text) == "" {
		return "", fmt.Errorf("empty announcement")
	}
	// Every message must be in it; one left out would count as heard.
	for _, m := range messages {
		if !slices.Contains(out.IDs, m.ID) {
			return "", fmt.Errorf("message %s left out", m.ID)
		}
	}
	return strings.TrimSpace(out.Text), nil
}

// fixed is the announcement without a model: each message as a sentence,
// an older one with its time in front.
func (n *Notifier) fixed(ctx context.Context, messages []store.Message, now time.Time, morning bool) string {
	t := textsFor(n.langOf(ctx))
	var b strings.Builder
	if morning {
		b.WriteString(t.goodMorning)
	}
	if len(messages) > 1 {
		fmt.Fprintf(&b, t.intro, countWord(t, len(messages)))
	}
	for i, m := range messages {
		switch {
		case i == 0:
		case i == len(messages)-1:
			b.WriteString(t.and)
		default:
			b.WriteString(t.also)
		}
		b.WriteString(n.timed(ctx, m, now))
	}
	return b.String()
}

// timed is a message as a sentence, with its time in front when it is
// older than olderThan and its text came from outside (an automation, a
// person). Mira's own messages carry their time where it matters: a task's
// verdict is worded with it, a collection day is about tomorrow.
func (n *Notifier) timed(ctx context.Context, m store.Message, now time.Time) string {
	text := sentence(m.Spoken)
	if m.CreatedAt.IsZero() || now.Sub(m.CreatedAt) < olderThan ||
		(m.Source != store.SourceExternal && m.Source != store.SourcePerson) {
		return text
	}
	return n.whenPrefix(ctx, m.CreatedAt, now) + text
}

// goodMorning: the first announcement after the quiet hours, before 11.
func (n *Notifier) goodMorning(now time.Time) bool {
	h := now.Hour()
	return h*60+now.Minute() >= n.cfg.QuietHours.To.MinutesOfDay() && h < 11
}

// when describes a past time for the model: "13:05 today", "03:13
// tonight", "21:10 yesterday" or a date.
func (n *Notifier) when(ctx context.Context, at, now time.Time) string {
	t := textsFor(n.langOf(ctx))
	at = at.In(n.loc)
	clock := clock(n.langOf(ctx), at)
	switch day := dayDiff(at, now); {
	case day == 0 && at.Hour() < 6 && now.Hour() >= 6:
		return fmt.Sprintf(t.whenNight, clock)
	case day == 0:
		return fmt.Sprintf(t.whenToday, clock)
	case day == 1:
		return fmt.Sprintf(t.whenYesterday, clock)
	default:
		return fmt.Sprintf(t.whenDate, at.Format("2.1."), clock)
	}
}

// whenPrefix is the same as the start of a spoken sentence.
func (n *Notifier) whenPrefix(ctx context.Context, at, now time.Time) string {
	t := textsFor(n.langOf(ctx))
	at = at.In(n.loc)
	clock := clock(n.langOf(ctx), at)
	switch day := dayDiff(at, now); {
	case day == 0 && at.Hour() < 6 && now.Hour() >= 6:
		return fmt.Sprintf(t.prefixNight, clock)
	case day == 0:
		return fmt.Sprintf(t.prefixToday, clock)
	case day == 1:
		return fmt.Sprintf(t.prefixYesterday, clock)
	default:
		return fmt.Sprintf(t.prefixDate, at.Format("2.1."), clock)
	}
}

func dayDiff(at, now time.Time) int {
	a := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, now.Location())
	b := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return int(b.Sub(a).Hours() / 24)
}

// clock says a time the way it is spoken: "3:13", or "3:13 AM" in English.
func clock(l i18n.Lang, at time.Time) string {
	if l == i18n.EN {
		return at.Format("3:04 PM")
	}
	return strings.TrimPrefix(at.Format("15:04"), "0")
}
