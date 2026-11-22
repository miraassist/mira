package notifier

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/agent"
	"github.com/miraassist/mira/internal/excerpt"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/store"
)

// Composer runs one isolated model request with a JSON answer (the
// engine's Assess). Every announcement is worded by it
// (docs/spec/messages.md, decision 29).
type Composer func(ctx context.Context, instruction string, schema json.RawMessage) (string, error)

// Instruction is the kind's instruction for its announcement: the section
// "Ansage" of the skill that takes the kind up (docs/spec/messages.md,
// decision 30); an error when the skill is not there or has none.
type Instruction func(ctx context.Context, kind string) (string, error)

// composeTimeout bounds the model's wording.
const composeTimeout = 20 * time.Second

var composeSchema = json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"},"ids":{"type":"array","items":{"type":"string"}}},"required":["text","ids"]}`)

// ComposeError is a failed wording: Say is the sentence of a failed model
// (docs/spec/messages.md, decision 31).
type ComposeError struct {
	Say string
	Err error
}

func (e *ComposeError) Error() string { return e.Err.Error() }
func (e *ComposeError) Unwrap() error { return e.Err }

// compose words messages for speaking: the model gets each message with
// its kind, its entries and the threads they lead to, and the instructions
// of the kinds (docs/spec/messages.md, decisions 29 to 31). No fixed form
// stands in for it: a failure is a ComposeError with the sentence of a
// failed model.
func (n *Notifier) compose(ctx context.Context, messages []store.Message) (string, error) {
	return n.composeAs(ctx, messages, false)
}

// composeAs is compose, for an afterword added to an answer when afterword
// is set (docs/spec/messages.md, decision 32).
func (n *Notifier) composeAs(ctx context.Context, messages []store.Message, afterword bool) (string, error) {
	t := textsFor(n.langOf(ctx))
	if n.composer == nil {
		return "", &ComposeError{Say: t.failedModel, Err: errors.New("no model to word the announcement")}
	}
	now := n.clock.Now().In(n.loc)
	instruction := n.composeInstruction(ctx, messages, now)
	if afterword {
		instruction += t.composeAfterword
	}
	cctx, cancel := context.WithTimeout(ctx, composeTimeout)
	defer cancel()
	raw, err := n.composer(cctx, instruction, composeSchema)
	if err != nil {
		// An answer that does not follow the schema is the model's, not a
		// lost connection: only the other failures are the key or the
		// address.
		say := t.failedModel
		if !errors.Is(err, agent.ErrOutput) {
			switch llm.Cause(err) {
			case llm.CauseKey:
				say = t.keyRefused
			case llm.CauseUnreachable:
				say = t.unreachable
			}
		}
		return "", &ComposeError{Say: say, Err: fmt.Errorf("word the announcement: %w", err)}
	}
	var out struct {
		Text string   `json:"text"`
		IDs  []string `json:"ids"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return "", &ComposeError{Say: t.failedModel, Err: fmt.Errorf("the announcement's answer is not JSON %q: %w", excerpt.Of(raw), err)}
	}
	if strings.TrimSpace(out.Text) == "" {
		return "", &ComposeError{Say: t.failedModel, Err: fmt.Errorf("the announcement's answer has no text: %q", excerpt.Of(raw))}
	}
	// Every message must be in it; one left out would count as heard.
	for _, m := range messages {
		if !slices.Contains(out.IDs, m.ID) {
			return "", &ComposeError{Say: t.failedModel, Err: fmt.Errorf("the announcement's answer leaves out message %s: %q", m.ID, excerpt.Of(raw))}
		}
	}
	return strings.TrimSpace(out.Text), nil
}

// composeInstruction is the model's task: the messages, each with its kind,
// its entries and their threads (id, skill, plan), and the instruction of
// each kind with a skill, once.
func (n *Notifier) composeInstruction(ctx context.Context, messages []store.Message, now time.Time) string {
	t := textsFor(n.langOf(ctx))
	var list, kinds strings.Builder
	seen := map[string]bool{}
	for _, m := range messages {
		fmt.Fprintf(&list, t.composeMessage, m.ID, m.Kind, n.when(ctx, m.CreatedAt, now), t.importance, m.Importance)
		entries := m.Entries
		if len(entries) == 0 { // a message of the form before kinds
			entries = []store.Entry{{Text: m.Spoken, ThreadID: m.ThreadID, CreatedAt: m.CreatedAt}}
		}
		for _, e := range entries {
			fmt.Fprintf(&list, t.composeEntry, strings.TrimSpace(e.Text))
			if e.ThreadID != "" {
				fmt.Fprintf(&list, t.composeThread, e.ThreadID, store.KindSkill(m.Kind), n.planOf(ctx, e.ThreadID))
			}
			list.WriteString("\n")
			if d := strings.TrimSpace(e.Details); d != "" {
				fmt.Fprintf(&list, t.composeDetails, strings.Join(strings.Fields(d), " "))
			}
		}
		if skill := store.KindSkill(m.Kind); skill != "" && !seen[m.Kind] && n.instruction != nil {
			seen[m.Kind] = true
			if text, err := n.instruction(ctx, m.Kind); err == nil {
				fmt.Fprintf(&kinds, t.composeKind, m.Kind, skill, text)
			}
		}
	}
	instruction := fmt.Sprintf(t.composeTask, now.Format("15:04"), list.String())
	if kinds.Len() > 0 {
		instruction += t.composeKinds + kinds.String()
	}
	if n.goodMorning(now) {
		instruction += t.composeMorning
	}
	return instruction
}

// planOf is a thread's plan on one line, "" when it has none or cannot be
// read (the entry is still said; the log names it).
func (n *Notifier) planOf(ctx context.Context, thread string) string {
	th, err := n.store.GetThread(ctx, thread)
	if err != nil {
		n.log.Warn("could not read a thread for the announcement; its plan is left out", "thread", thread, "error", err)
		return ""
	}
	var c struct {
		Plan string `json:"plan"`
	}
	if len(th.Content) == 0 || json.Unmarshal(th.Content, &c) != nil {
		return ""
	}
	return strings.Join(strings.Fields(c.Plan), " ")
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
	clock := spokenClock(n.langOf(ctx), at)
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

func dayDiff(at, now time.Time) int {
	a := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, now.Location())
	b := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return int(b.Sub(a).Hours() / 24)
}

// spokenClock says a time the way it is spoken: "3:13", or "3:13 AM" in English.
func spokenClock(l i18n.Lang, at time.Time) string {
	if l == i18n.EN {
		return at.Format("3:04 PM")
	}
	return strings.TrimPrefix(at.Format("15:04"), "0")
}
