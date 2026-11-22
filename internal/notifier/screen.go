package notifier

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/store"
)

// A satellite without an indicator light may show waiting messages on its
// screen instead: an overlay its device offers as a Home Assistant action
// with a tag and a duration (found by the app, e.g. esphome.show_notify of
// an Echo Show with TECHO5). It is a hint at the moment something new
// arrives, not a lasting light: an overlay with the same tag replaces the
// last one. When everything was heard it is taken down by the device's
// action of the same name with "_clear" (esphome.show_notify_clear, by the
// tag). Firmware without it refuses the call: Mira logs that, and the hint
// runs out after its minute.

// screenFor is how long the overlay stays.
const screenFor = 60 * time.Second

// syncScreens shows or updates each screen's overlay when the number of
// messages waiting in its room changes, and takes it down when none wait.
// Every screen of a room shows the room's (docs/spec/satellites-rooms.md).
// The caller holds n.mu.
func (n *Notifier) syncScreens(ctx context.Context) {
	busy := n.busyScreens(ctx)
	w, err := n.readWaiting(ctx)
	if err != nil {
		n.log.Error("could not check waiting messages", "error", err)
		return
	}
	for sat, action := range n.screens {
		area := n.areaOf(sat)
		// What one perceives as messages are the entries: a gathered
		// message with three counts three.
		count := 0
		for _, m := range w.in(area) {
			if m.Kind == store.KindVoicemail {
				continue // the answering machine's hint counts it (syncPhoneScreens)
			}
			count += max(1, len(m.Entries))
		}
		if count == n.screenCount[sat] {
			continue
		}
		if count == 0 {
			// Taking it down covers nothing: no need to wait for the turn.
			n.screenCount[sat] = 0
			n.clearOverlay(ctx, action, "mira", area)
			continue
		}
		if busy[sat] {
			n.showWhenIdle(sat)
			continue
		}
		t := textsFor(n.langOf(n.inArea(ctx, area)))
		text := t.screenOne
		if count > 1 {
			text = fmt.Sprintf(t.screenMany, count)
		}
		if err := n.overlay(ctx, action, "mira", text, "mdi:message-badge", screenFor); err != nil {
			n.log.Warn("could not show waiting messages on the screen", "area", area, "action", action, "error", err)
			continue
		}
		n.screenCount[sat] = count
		n.log.Info("screen hint", "area", area, "satellite", sat, "waiting", count)
	}
}

// syncPhoneScreens shows on each screen what is new on the answering
// machine, as a hint of its own beside Mira's messages, when it changes;
// it is taken down when nothing is new (docs/spec/voicemail.md). The caller
// holds n.mu.
func (n *Notifier) syncPhoneScreens(ctx context.Context, news PhoneNews) {
	busy := n.busyScreens(ctx)
	for sat, action := range n.screens {
		area := n.areaOf(sat)
		if n.phoneShown[sat] == news || n.phoneHeld[area] {
			continue
		}
		if news.Messages == 0 && news.Missed == 0 {
			n.phoneShown[sat] = news
			n.clearOverlay(ctx, action, "phone", area)
			continue
		}
		if busy[sat] {
			n.showWhenIdle(sat)
			continue
		}
		t := textsFor(n.langOf(n.inArea(ctx, area)))
		var parts []string
		switch {
		case news.Messages == 1:
			parts = append(parts, t.phoneMessage)
		case news.Messages > 1:
			parts = append(parts, fmt.Sprintf(t.phoneMessages, news.Messages))
		}
		switch {
		case news.Missed == 1:
			parts = append(parts, t.phoneMissed)
		case news.Missed > 1:
			parts = append(parts, fmt.Sprintf(t.phoneMisseds, news.Missed))
		}
		icon := "mdi:voicemail"
		if news.Messages == 0 {
			icon = "mdi:phone-missed"
		}
		if err := n.overlay(ctx, action, "phone", strings.Join(parts, " · "), icon, screenFor); err != nil {
			n.log.Warn("could not show the answering machine on the screen", "area", area, "error", err)
			continue
		}
		n.phoneShown[sat] = news
		n.log.Info("screen hint for the answering machine", "area", area, "satellite", sat, "messages", news.Messages, "missed", news.Missed)
	}
}

// inConversation says whether a satellite's state is a conversation:
// listening, thinking or speaking.
func inConversation(state string) bool {
	return state == "listening" || state == "processing" || state == "responding"
}

// busyScreens are the satellites with a screen that are in a conversation
// now: a hint there would cover the turn screen, and one would not see that
// Mira listens. Read once per sync, filtered to the satellites.
func (n *Notifier) busyScreens(ctx context.Context) map[string]bool {
	var ids []string
	for sat := range n.screens {
		if strings.HasPrefix(sat, "assist_satellite.") {
			ids = append(ids, sat)
		}
	}
	busy := map[string]bool{}
	if len(ids) == 0 {
		return busy
	}
	states, err := n.ha.GetStates(ctx, haws.StateFilter{EntityIDs: ids})
	if err != nil {
		return busy
	}
	for _, st := range states {
		if inConversation(st.State) {
			busy[st.EntityID] = true
		}
	}
	return busy
}

// areaOf is the area a satellite is in, "" for none.
func (n *Notifier) areaOf(sat string) string {
	for area, sats := range n.sats {
		if slices.Contains(sats, sat) {
			return area
		}
	}
	return ""
}

// showWhenIdle shows a screen's hints once its satellite has left the
// conversation: it watches the satellite's state (no polling), one waiter
// per satellite. The caller holds n.mu.
func (n *Notifier) showWhenIdle(sat string) {
	if n.awaitingIdle[sat] {
		return
	}
	n.awaitingIdle[sat] = true
	go func() {
		// The wait is bounded by its limit on the clock.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		n.ha.WaitState(ctx, sat, idleWait*10, func(s string) bool { return !inConversation(s) })
		n.mu.Lock()
		defer n.mu.Unlock()
		delete(n.awaitingIdle, sat)
		n.syncIndicators(ctx)
	}()
}

// HoldPhoneScreen keeps the answering machine's hint off an area's screen
// while it plays messages: each one heard lowers the count, and the hint
// would cover the card of what plays. Released, the screen gets the count
// as it is now (docs/spec/voicemail.md, decision 10).
func (n *Notifier) HoldPhoneScreen(ctx context.Context, area string, hold bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if hold {
		n.phoneHeld[area] = true
		return
	}
	delete(n.phoneHeld, area)
	n.syncIndicators(ctx)
}

func (n *Notifier) overlay(ctx context.Context, action, tag, text, icon string, d time.Duration) error {
	domain, service, ok := splitAction(action)
	if !ok {
		return fmt.Errorf("not an action: %q", action)
	}
	return n.ha.CallService(ctx, domain, service, map[string]any{
		"title": "Mira", "message": text, "icon": icon, "chime": false, "speak": false,
		"tag": tag, "seconds": int(d.Seconds()),
	}, nil)
}

// clearOverlay takes down the overlay with the tag by the device's clear
// action. A failure (older firmware without it, the device away) is logged;
// the hint then runs out after its minute, and it is not tried again.
func (n *Notifier) clearOverlay(ctx context.Context, action, tag, area string) {
	clear := action + "_clear"
	domain, service, ok := splitAction(clear)
	if !ok {
		n.log.Warn("could not take the screen hint down", "area", area, "action", clear, "error", "not an action")
		return
	}
	if err := n.ha.CallService(ctx, domain, service, map[string]any{"tag": tag}, nil); err != nil {
		n.log.Warn("could not take the screen hint down; it runs out after its minute", "area", area, "action", clear, "tag", tag, "error", err)
		return
	}
	n.log.Info("screen hint taken down", "area", area, "action", clear, "tag", tag)
}

func splitAction(action string) (string, string, bool) {
	for i := 0; i < len(action); i++ {
		if action[i] == '.' {
			return action[:i], action[i+1:], i > 0 && i < len(action)-1
		}
	}
	return "", "", false
}
