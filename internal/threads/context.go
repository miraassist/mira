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
// messages announced on this satellite moments ago), or an announcement that
// named its thread (announced), joins that thread: the link is the code's,
// not the model's. Joining changes nothing about who speaks. Otherwise a
// thread waiting at the turn's place (what the conversation said the turn
// belongs to) is shown, and the model decides whether the user continues
// it, cancels it or talks about something else.
//
// foreign reports a thread that carries third-party text (a plan made from
// a web page): the turn then asks before it keeps or passes anything on.
func (m *Manager) Context(ctx context.Context, sess store.Session, place string, followedUp []string, announced string) (text string, foreign bool) {
	if !m.HasTargets() {
		return "", false
	}
	t := m.texts()
	threadIDs := []string{announced}
	for _, id := range followedUp {
		if n, err := m.opts.Store.GetMessage(ctx, id); err == nil {
			threadIDs = append(threadIDs, n.ThreadID)
		}
	}
	for _, id := range threadIDs {
		if id == "" {
			continue
		}
		if th, err := m.opts.Store.GetThread(ctx, id); err == nil && store.ThreadOpen(th.State) {
			// Another person's thread is not theirs to answer (Assignment 3a).
			if owner, ok := strings.CutPrefix(th.Place, "person:"); ok {
				if speaker, ok := strings.CutPrefix(place, "person:"); ok && speaker != owner {
					continue
				}
			}
			if sess.ThreadID != th.ID {
				m.link(ctx, sess.Key, th)
				sess.ThreadID = th.ID
			}
		}
	}
	if sess.ThreadID != "" {
		if th, err := m.opts.Store.GetThread(ctx, sess.ThreadID); err == nil && store.ThreadOpen(th.State) {
			return t.headerCurrent + "\n" + m.describe(th, m.lang(ctx)), m.isPlan(th) && planOf(th).Foreign
		}
	}
	open, err := m.openAt(ctx, visible(place, sess.AreaID))
	if err != nil || len(open) == 0 {
		return "", false
	}
	// At most three, newest first; each shown with its skill's tools there
	// to go on with it.
	var parts []string
	for _, th := range open[:min(len(open), maxWaiting)] {
		_ = m.opts.Store.AddSessionSkill(ctx, sess.Key, th.Target)
		parts = append(parts, t.headerWaiting+"\n"+m.describe(th, m.lang(ctx)))
		foreign = foreign || (m.isPlan(th) && planOf(th).Foreign)
	}
	return strings.Join(parts, "\n\n"), foreign
}

// maxWaiting is how many waiting threads a turn is shown.
const maxWaiting = 3

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
