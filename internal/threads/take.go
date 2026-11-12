package threads

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/tools"
)

// Take makes the chosen thread the turn's own (docs/spec/messages.md,
// decision 9): every thread tool then acts on it until it ends. A thread is
// the asker's when it is of the house, of the speaking person, or at the
// turn's places (its room, its chat); another one is refused with what is
// open for the asker.
func (m *Manager) Take(ctx context.Context, inv tools.Invocation, id string) (string, error) {
	t := m.texts()
	places := append(m.turnPlaces(ctx, inv), store.PlaceHouse)
	th, err := m.opts.Store.GetThread(ctx, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
	case err != nil:
		return "", err
	default:
		if store.ThreadOpen(th.State) && contains(places, th.Place) {
			th = m.toRoom(ctx, inv, th)
			m.link(ctx, inv.SessionKey, th)
			m.log.Info("thread taken up", "thread", th.ID, "target", th.Target, "session", inv.SessionKey)
			return t.headerCurrent + "\n" + m.describe(th, m.lang(ctx)), nil
		}
	}
	m.log.Info("thread_take refused: not open, or not the asker's", "thread", id, "places", places)
	open, err := m.openAt(ctx, places)
	if err != nil {
		return "", err
	}
	if len(open) == 0 {
		return "", errors.New(t.noneOpen)
	}
	var b strings.Builder
	b.WriteString(t.notYours)
	for _, o := range open {
		fmt.Fprintf(&b, t.openLine, o.ID, o.Target, o.Request)
	}
	return "", errors.New(b.String())
}

// receiversOf are whose waiting entries a place sees: a person's and the
// house's, or the house's only.
func receiversOf(place string) []string {
	if p, ok := strings.CutPrefix(place, "person:"); ok {
		return []string{p, store.ReceiverHouse}
	}
	return []string{store.ReceiverHouse}
}

// stillOpen names the waiting entries of th's receiver and the house beyond
// th's own, when th has just ended (decision 10); "" when none.
func (m *Manager) stillOpen(ctx context.Context, th store.Thread) string {
	leads, err := m.opts.Store.Leads(ctx, receiversOf(th.Place))
	if err != nil {
		m.log.Warn("could not read what still waits; the answer does not name it", "thread", th.ID, "error", err)
		return ""
	}
	var lines []string
	for _, l := range leads {
		if l.ThreadID != th.ID {
			lines = append(lines, l.Text)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return fmt.Sprintf(m.texts().stillOpen, strings.Join(lines, "; "))
}

// waitingLine is the one line of what waits for the turn's place: per kind how
// many entries (decision 10); "" when nothing waits.
func (m *Manager) waitingLine(ctx context.Context, place string) string {
	leads, err := m.opts.Store.Leads(ctx, receiversOf(place))
	if err != nil {
		m.log.Warn("could not read what waits; the turn does not see it", "place", place, "error", err)
		return ""
	}
	if len(leads) == 0 {
		return ""
	}
	count := map[string]int{}
	for _, l := range leads {
		count[l.Kind]++
	}
	kinds := make([]string, 0, len(count))
	for k := range count {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	parts := make([]string, len(kinds))
	for i, k := range kinds {
		parts[i] = fmt.Sprintf("%d × %s", count[k], k)
	}
	return fmt.Sprintf(m.texts().waitingLine, strings.Join(parts, ", "))
}
