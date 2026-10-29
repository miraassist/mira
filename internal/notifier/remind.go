package notifier

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/store"
)

// Remind delivers a due scheduled message (a reminder) now. The user chose
// the time, so quiet hours and cooldowns do not apply: it is spoken in the
// room it was asked for in (unless quiet mode is on), otherwise it goes
// privately to its receiver. It stays a delivered message, so it shows in
// the web UI and messages_list like any other.
func (n *Notifier) Remind(ctx context.Context, m store.Message) error {
	n.waitIdle(ctx, m.AreaID)
	n.mu.Lock()
	defer n.mu.Unlock()

	ctx = n.forMessage(ctx, m)
	if m.Source == store.SourceReminder {
		m.Spoken = fmt.Sprintf(textsFor(n.langOf(ctx)).reminder, strings.TrimRight(strings.TrimSpace(m.Spoken), "."))
	}
	if err := n.store.ReleaseScheduled(ctx, m.ID, m.Spoken); err != nil {
		return err
	}
	m.State = store.MessagePending
	return n.deliverAsked(ctx, m, m.AreaID, true)
}

// Tell delivers a message someone asked for and is waiting on, such as the
// proposal for a standing order that was planned in the background. Like a
// reminder it is spoken in the room it was asked in, otherwise it goes
// privately to its receiver.
func (n *Notifier) Tell(ctx context.Context, nt store.Message, areaID string) error {
	n.waitIdle(ctx, areaID)
	n.mu.Lock()
	defer n.mu.Unlock()
	if nt.ExpiresAt.IsZero() {
		nt.ExpiresAt = n.clock().Add(12 * time.Hour)
	}
	nt, _, err := n.store.PutMessage(ctx, nt)
	if err != nil {
		return err
	}
	// The answer it follows has just been spoken in that room: the chime
	// would only be cut off by the announcement right after it.
	return n.deliverAsked(ctx, nt, areaID, false)
}

// deliverAsked speaks nt in the area unless quiet mode is on, falling back
// to a private message. n.mu is held.
func (n *Notifier) deliverAsked(ctx context.Context, nt store.Message, areaID string, preannounce bool) error {
	quiet, err := n.store.GetBool(ctx, store.KeyQuietMode, false)
	if err != nil {
		return err
	}
	defer n.syncIndicators(ctx)
	if sat := n.sats[areaID]; sat != "" && !quiet {
		err := n.announce(ctx, areaID, []store.Message{nt}, preannounce)
		if err == nil {
			return nil
		}
		n.log.Warn("message could not be spoken, sending it privately", "message", nt.ID, "error", err)
	}
	return n.sendPrivate(ctx, nt)
}

// DueMessages delivers every scheduled message that is due. A failed
// delivery is retried on the next call, and given up after
// store.MaxDeliveryAttempts.
func (n *Notifier) DueMessages(ctx context.Context) {
	for _, m := range n.due(ctx) {
		n.remindOne(ctx, m)
	}
}

// dueInBackground delivers each due message on its own goroutine: a
// reminder waits for its satellite to be quiet (up to minutes), and the
// notifier's loop must go on meanwhile (motion, urgent messages). One that
// is still being delivered is not started again.
func (n *Notifier) dueInBackground(ctx context.Context) {
	for _, m := range n.due(ctx) {
		n.mu.Lock()
		busy := n.delivering[m.ID]
		n.delivering[m.ID] = true
		n.mu.Unlock()
		if busy {
			continue
		}
		go func(m store.Message) {
			defer func() {
				n.mu.Lock()
				delete(n.delivering, m.ID)
				n.mu.Unlock()
			}()
			n.remindOne(ctx, m)
		}(m)
	}
}

func (n *Notifier) due(ctx context.Context) []store.Message {
	due, err := n.store.ScheduledMessages(ctx, n.clock())
	if err != nil && ctx.Err() == nil {
		n.log.Error("could not read scheduled messages", "error", err)
	}
	return due
}

func (n *Notifier) remindOne(ctx context.Context, m store.Message) {
	if err := n.Remind(ctx, m); err != nil {
		gaveUp, ferr := n.store.DeliveryFailed(ctx, m.ID)
		n.log.Error("scheduled message delivery failed", "message", m.ID, "gave_up", gaveUp, "error", errors.Join(err, ferr))
		return
	}
	n.log.Info("scheduled message delivered", "message", m.ID, "area", m.AreaID, "receiver", m.Receiver)
}

// idleWait bounds how long a message waits for a busy satellite.
const idleWait = time.Minute

// waitIdle waits until the area's satellite has finished speaking: the
// satellite idle and the speakers of its device no longer playing (the
// satellite turns idle while the audio still plays), then a short pause.
// A message that arrives while it still speaks the answer that asked for
// it would cut that answer off or follow it without a breath, and the
// announcement would time out on the busy satellite. After idleWait it
// gives up waiting and delivers anyway.
func (n *Notifier) waitIdle(ctx context.Context, areaID string) {
	sat := n.sats[areaID]
	if !strings.HasPrefix(sat, "assist_satellite.") {
		return
	}
	idle := func(state string) bool { return state == "idle" || state == "unavailable" || state == "unknown" }
	if !n.waitFor(ctx, sat, idleWait, idle) {
		n.log.Warn("satellite still busy, delivering anyway", "satellite", sat)
		return
	}
	for _, speaker := range n.speakers(ctx, sat) {
		n.waitWhile(ctx, speaker, idleWait, func(state string) bool { return state == "playing" || state == "buffering" })
	}
	select {
	case <-ctx.Done():
	case <-time.After(n.speakPause):
	}
}

// speakers are the media players of the satellite's device: where its
// voice actually plays.
func (n *Notifier) speakers(ctx context.Context, sat string) []string {
	device := n.deviceID(ctx, sat)
	if device == "" || n.structure == nil {
		return nil
	}
	var out []string
	for _, id := range n.structure().DeviceEntities[device] {
		if strings.HasPrefix(id, "media_player.") {
			out = append(out, id)
		}
	}
	return out
}

// waitWhile waits while busy says so of an entity, for at most limit. An
// entity without a state is not busy.
func (n *Notifier) waitWhile(ctx context.Context, entity string, limit time.Duration, busy func(state string) bool) {
	n.ha.WaitState(ctx, entity, limit, func(s string) bool { return !busy(s) })
}

// waitFor waits until ok accepts the satellite's state, for at most limit;
// it reports whether ok did.
func (n *Notifier) waitFor(ctx context.Context, sat string, limit time.Duration, ok func(state string) bool) bool {
	return n.ha.WaitState(ctx, sat, limit, ok)
}
