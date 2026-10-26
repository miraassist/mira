package threads

import (
	"context"
	"fmt"
	"github.com/miraassist/mira/internal/i18n"
	"strings"

	"github.com/miraassist/mira/internal/store"
)

// maxLog bounds the thread log shown to the model.
const maxLog = 12

// Context links a turn to its thread and renders the thread for prompt
// layer 7. A turn that answers one of a thread's messages (followedUp, the
// messages announced on this satellite moments ago) joins that thread: the
// link is the code's, not the model's. Otherwise a thread waiting at the
// turn's place (what the conversation said the turn belongs to) is shown,
// and the model decides whether the user continues
// it, cancels it or talks about something else.
//
// foreign reports a thread that carries third-party text (a plan made from
// a web page): the turn then asks before it keeps or passes anything on.
func (m *Manager) Context(ctx context.Context, sess store.Session, place string, followedUp []string) (text string, foreign bool) {
	if !m.HasTargets() {
		return "", false
	}
	t := m.texts()
	for _, id := range followedUp {
		n, err := m.opts.Store.GetMessage(ctx, id)
		if err != nil || n.ThreadID == "" {
			continue
		}
		if th, err := m.opts.Store.GetThread(ctx, n.ThreadID); err == nil && store.ThreadOpen(th.State) {
			if sess.ThreadID != th.ID {
				_ = m.opts.Store.SetSessionThread(ctx, sess.Key, th.ID)
				sess.ThreadID = th.ID
			}
		}
	}
	if sess.ThreadID != "" {
		if th, err := m.opts.Store.GetThread(ctx, sess.ThreadID); err == nil && store.ThreadOpen(th.State) {
			return t.headerCurrent + "\n" + m.describe(th, m.lang(ctx)), m.isPlan(th) && planOf(th).Foreign
		}
	}
	open, err := m.opts.Store.OpenThreads(ctx, place)
	if err != nil || len(open) == 0 {
		return "", false
	}
	return t.headerWaiting + "\n" + m.describe(open[0], m.lang(ctx)), m.isPlan(open[0]) && planOf(open[0]).Foreign
}

// describe renders a thread for the model: target, request, fields, log
// and what it waits for; the skill's texts in l.
func (m *Manager) describe(th store.Thread, l i18n.Lang) string {
	t := m.texts()
	sk := m.targets[th.Target]
	var b strings.Builder
	fmt.Fprintf(&b, t.lineTarget, th.Target, sk.DescriptionIn(l))
	fmt.Fprintf(&b, t.lineRequest, th.Request)
	b.WriteString(m.status(th))
	log := th.Log
	if len(log) > maxLog {
		log = log[len(log)-maxLog:]
	}
	if len(log) > 0 {
		b.WriteString("\n" + t.lineLog)
		for _, e := range log {
			who := t.whoUser
			if e.Who == "mira" {
				who = t.whoMira
			}
			fmt.Fprintf(&b, "\n- %s: %s", who, e.Text)
		}
	}
	if m.isPlan(th) {
		// A plan outlives the conversation that started it: its
		// instructions go with it into every later one.
		b.WriteString("\n" + t.planHint + "\n" + t.lineInstructions + "\n" + sk.BodyIn(l))
	} else {
		b.WriteString("\n" + t.stateHint[th.State])
	}
	return b.String()
}

// status lists the fields: filled, missing; for a plan, where it stands.
func (m *Manager) status(th store.Thread) string {
	t := m.texts()
	sk := m.targets[th.Target]
	if sk.Target.Plan {
		if th.Calls[PlanField] != "" {
			return t.linePlan + "\n" + fmt.Sprintf(t.planByAgent, sk.Target.Agent)
		}
		return t.linePlan + "\n" + m.planStatus(th)
	}
	var b strings.Builder
	b.WriteString(t.lineFields)
	for _, f := range sk.Target.Fields {
		v := strings.TrimSpace(th.Fields[f.Name])
		switch {
		case v == "" && th.Calls[f.Name] != "":
			// An agent fills it: nothing to ask the user.
			v = fmt.Sprintf(t.fieldByAgent, f.Agent)
		case v == "":
			v = t.fieldMissing
		}
		desc := f.Description
		if len(f.Enum) > 0 {
			desc += " (" + strings.Join(f.Enum, " | ") + ")"
		}
		fmt.Fprintf(&b, "\n- %s – %s: %s", f.Name, desc, v)
	}
	var ask []string
	for _, name := range missing(sk.Target, th.Fields) {
		if th.Calls[name] == "" {
			ask = append(ask, name)
		}
	}
	if len(ask) > 0 {
		fmt.Fprintf(&b, "\n"+t.stillMissing, strings.Join(ask, ", "))
	}
	return b.String()
}

// summary is a thread in one paragraph, the details of its messages.
func (m *Manager) summary(th store.Thread) string {
	var parts []string
	for _, k := range sortedKeys(th.Fields) {
		parts = append(parts, k+": "+th.Fields[k])
	}
	return strings.TrimSpace(th.Request + " (" + strings.Join(parts, "; ") + ")")
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
