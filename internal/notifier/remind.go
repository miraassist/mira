package notifier

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sker65/mira/internal/store"
)

// Remind delivers a due reminder now. The user chose the time, so quiet
// hours and cooldowns do not apply: it is spoken in the room it was asked
// for in (unless quiet mode is on), otherwise it goes privately to its
// audience. The reminder is kept as a delivered notice, so it shows in the
// web UI and notices_list like any other.
func (n *Notifier) Remind(ctx context.Context, r store.Reminder) error {
	n.waitIdle(ctx, r.AreaID)
	n.mu.Lock()
	defer n.mu.Unlock()

	nt, _, err := n.store.PutNotice(ctx, store.Notice{
		ID: "reminder-" + r.ID, Source: store.SourceReminder, DedupeKey: "reminder:" + r.ID,
		Importance: 2, Audience: r.Audience,
		Spoken:    fmt.Sprintf(textsFor(n.lang).reminder, strings.TrimRight(strings.TrimSpace(r.Text), ".")),
		ExpiresAt: n.clock().Add(12 * time.Hour),
	})
	if err != nil {
		return err
	}
	return n.deliverAsked(ctx, nt, r.AreaID)
}

// Tell delivers a message someone asked for and is waiting on, such as the
// proposal for a standing order that was planned in the background. Like a
// reminder it is spoken in the room it was asked in, otherwise it goes
// privately to its audience.
func (n *Notifier) Tell(ctx context.Context, nt store.Notice, areaID string) error {
	n.waitIdle(ctx, areaID)
	n.mu.Lock()
	defer n.mu.Unlock()
	if nt.ExpiresAt.IsZero() {
		nt.ExpiresAt = n.clock().Add(12 * time.Hour)
	}
	nt, _, err := n.store.PutNotice(ctx, nt)
	if err != nil {
		return err
	}
	return n.deliverAsked(ctx, nt, areaID)
}

// deliverAsked speaks nt in the area unless quiet mode is on, falling back
// to a private message. n.mu is held.
func (n *Notifier) deliverAsked(ctx context.Context, nt store.Notice, areaID string) error {
	quiet, err := n.store.GetBool(ctx, store.KeyQuietMode, false)
	if err != nil {
		return err
	}
	defer n.syncIndicators(ctx)
	if sat := n.sats[areaID]; sat != "" && !quiet {
		err := n.announce(ctx, areaID, []store.Notice{nt})
		if err == nil {
			return nil
		}
		n.log.Warn("message could not be spoken, sending it privately", "notice", nt.ID, "error", err)
	}
	return n.sendPrivate(ctx, nt)
}

// DueReminders delivers every reminder that is due. A failed delivery is
// retried on the next call, and given up after store.MaxReminderAttempts.
func (n *Notifier) DueReminders(ctx context.Context) {
	due, err := n.store.PendingReminders(ctx, n.clock())
	if err != nil {
		if ctx.Err() == nil {
			n.log.Error("could not read reminders", "error", err)
		}
		return
	}
	for _, r := range due {
		if err := n.Remind(ctx, r); err != nil {
			gaveUp, ferr := n.store.ReminderAttemptFailed(ctx, r.ID)
			n.log.Error("reminder delivery failed", "reminder", r.ID, "gave_up", gaveUp, "error", errors.Join(err, ferr))
			continue
		}
		if err := n.store.SetReminderState(ctx, r.ID, store.ReminderDone); err != nil {
			n.log.Error("could not close reminder", "reminder", r.ID, "error", err)
			continue
		}
		n.log.Info("reminder delivered", "reminder", r.ID, "area", r.AreaID, "audience", r.Audience)
	}
}

// idleWait bounds how long a message waits for a busy satellite.
const idleWait = time.Minute

// waitIdle waits until the area's satellite is idle: a message that arrives
// while it still speaks the answer that asked for it would cut that answer
// off, and the announcement would time out on the busy satellite. After
// idleWait it gives up waiting and delivers anyway.
func (n *Notifier) waitIdle(ctx context.Context, areaID string) {
	sat := n.sats[areaID]
	if !strings.HasPrefix(sat, "assist_satellite.") {
		return
	}
	deadline := time.Now().Add(idleWait)
	for {
		states, err := n.ha.States(ctx)
		if err != nil {
			return
		}
		busy := false
		for _, s := range states {
			if s.EntityID == sat {
				busy = s.State != "idle" && s.State != "unavailable" && s.State != "unknown"
			}
		}
		if !busy {
			return
		}
		if time.Now().After(deadline) {
			n.log.Warn("satellite still busy, delivering anyway", "satellite", sat)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(n.idlePoll):
		}
	}
}
