package notifier

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/retry"
	"github.com/miraassist/mira/internal/store"
)

// Remind delivers a due scheduled message (a reminder) now. The user chose
// the time, so quiet hours and cooldowns do not apply: it is spoken in the
// room it was asked for in (unless quiet mode is on), otherwise it goes
// privately to its receiver. It stays a delivered message, so it shows in
// the web UI and messages_list like any other.
func (n *Notifier) Remind(ctx context.Context, m store.Message) error {
	n.waitIdle(ctx, n.satOn(ctx, m.AreaID, m.OriginDevice))
	n.mu.Lock()
	defer n.mu.Unlock()

	ctx = n.forMessage(ctx, m)
	// A reminder tried again already carries its words (released before).
	if m.Source == store.SourceReminder && m.Attempts == 0 {
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
	n.waitIdle(ctx, n.satOn(ctx, areaID, nt.OriginDevice))
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

// deliverAsked speaks nt in the area unless quiet mode is on, else, or when
// speaking fails, sends it privately (decided: docs/spec/fallbacks.md, row
// 11): at the satellite it was asked at, while that is in the room, else at
// the room's primary. n.mu is held.
func (n *Notifier) deliverAsked(ctx context.Context, nt store.Message, areaID string, preannounce bool) error {
	quiet, err := n.store.GetBool(ctx, store.KeyQuietMode, false)
	if err != nil {
		return err
	}
	defer n.syncIndicators(ctx)
	if sat := n.satOn(ctx, areaID, nt.OriginDevice); sat != "" && !quiet {
		err := n.announce(ctx, areaID, sat, []store.Message{nt}, preannounce)
		if err == nil {
			return nil
		}
		n.log.Warn("message could not be spoken, sending it privately", "message", nt.ID, "error", err)
	}
	return n.sendPrivate(ctx, nt)
}

// reminderPolicy bounds the attempts of a reminder (docs/spec/retry.md,
// row 2): it is for now, so 15, 30, 45, 60 s apart.
var reminderPolicy = retry.Policy{Attempts: 5, First: 15 * time.Second, Growth: retry.Linear}

// DueMessages delivers every scheduled message that is due. A failed
// delivery with a retryable error is scheduled again as reminderPolicy
// says; a fatal one is given up at once.
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
		failed := m.Attempts + 1
		next, stop := reminderPolicy.Next(n.clock(), failed, err)
		if _, ferr := n.store.DeliveryFailed(ctx, m.ID, next); ferr != nil {
			n.log.Error("could not keep the failed delivery of a scheduled message", "message", m.ID, "error", ferr)
		}
		switch {
		case stop == nil:
			n.log.Warn("scheduled message delivery failed, trying again", "message", m.ID, "attempt", failed,
				"of", reminderPolicy.Attempts, "wait", next.Sub(n.clock()).String(), "error", err)
		case retry.Retryable(err):
			n.log.Error("scheduled message delivery given up", "message", m.ID, "attempts", failed, "error", err)
		default:
			n.log.Error("scheduled message delivery failed", "message", m.ID, "fatal", true, "error", err)
		}
		return
	}
	n.log.Info("scheduled message delivered", "message", m.ID, "area", m.AreaID, "receiver", m.Receiver)
}

// idleWait is how often a message that waits for a busy satellite says so
// in the log; it waits on until the satellite is idle.
const idleWait = time.Minute

// waitIdle waits until the satellite that will speak has finished: the
// satellite idle and the speakers of its device no longer playing (the
// satellite turns idle while the audio still plays), then a short pause.
// A message that arrives while it still speaks the answer that asked for
// it would cut that answer off or follow it without a breath, and the
// announcement would time out on the busy satellite. It waits until the
// satellite reports idle, however long: nothing is cut off
// (docs/spec/fallbacks.md, row 12).
func (n *Notifier) waitIdle(ctx context.Context, sat string) {
	if !strings.HasPrefix(sat, "assist_satellite.") {
		return
	}
	idle := func(state string) bool { return state == "idle" || state == "unavailable" || state == "unknown" }
	for !n.waitFor(ctx, sat, idleWait, idle) {
		if ctx.Err() != nil {
			return
		}
		n.log.Info("satellite still busy; the message waits until it is idle", "satellite", sat)
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
