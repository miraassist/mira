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
	quiet, err := n.store.GetBool(ctx, store.KeyQuietMode, false)
	if err != nil {
		return err
	}
	defer n.syncIndicators(ctx)
	if sat := n.sats[r.AreaID]; sat != "" && !quiet {
		err := n.announce(ctx, r.AreaID, []store.Notice{nt})
		if err == nil {
			return nil
		}
		n.log.Warn("reminder could not be spoken, sending it privately", "reminder", r.ID, "error", err)
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
