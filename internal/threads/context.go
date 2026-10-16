package threads

import (
	"context"
	"fmt"
	"strings"

	"github.com/sker65/mira/internal/config"
	"github.com/sker65/mira/internal/store"
)

// maxLog bounds the thread log shown to the model.
const maxLog = 12

// Context links a turn to its thread and renders the thread for prompt
// layer 7. A turn that answers one of a thread's messages (followedUp, the
// messages announced on this satellite moments ago) joins that thread: the
// link is the code's, not the model's. Otherwise a thread waiting at the
// turn's place is shown, and the model decides whether the user continues
// it, cancels it or talks about something else.
func (m *Manager) Context(ctx context.Context, sess store.Session, followedUp []string) string {
	if !m.HasTargets() {
		return ""
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
			return t.headerCurrent + "\n" + m.describe(th)
		}
	}
	place := sess.Key
	if sess.Channel == string(config.ChannelVoice) && sess.AreaID != "" {
		place = "area:" + sess.AreaID
	}
	open, err := m.opts.Store.OpenThreads(ctx, place)
	if err != nil || len(open) == 0 {
		return ""
	}
	return t.headerWaiting + "\n" + m.describe(open[0])
}

// describe renders a thread for the model: target, request, fields, log
// and what it waits for.
func (m *Manager) describe(th store.Thread) string {
	t := m.texts()
	sk := m.targets[th.Target]
	var b strings.Builder
	fmt.Fprintf(&b, t.lineTarget, th.Target, sk.Description)
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
	b.WriteString("\n" + t.stateHint[th.State])
	return b.String()
}

// status lists the fields: filled, missing.
func (m *Manager) status(th store.Thread) string {
	t := m.texts()
	sk := m.targets[th.Target]
	var b strings.Builder
	b.WriteString(t.lineFields)
	for _, f := range sk.Target.Fields {
		v := strings.TrimSpace(th.Fields[f.Name])
		if v == "" {
			v = t.fieldMissing
		}
		desc := f.Description
		if len(f.Enum) > 0 {
			desc += " (" + strings.Join(f.Enum, " | ") + ")"
		}
		fmt.Fprintf(&b, "\n- %s – %s: %s", f.Name, desc, v)
	}
	if miss := missing(sk.Target, th.Fields); len(miss) > 0 {
		fmt.Fprintf(&b, "\n"+t.stillMissing, strings.Join(miss, ", "))
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
