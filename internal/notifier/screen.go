package notifier

import (
	"context"
	"fmt"
	"strings"
	"time"

	haws "github.com/miraassist/mira/internal/ha/ws"
)

// A satellite without an indicator light may show waiting messages on its
// screen instead: an overlay its device offers as a Home Assistant action
// with a tag and a duration (found by the app, e.g. esphome.show_notify of
// an Echo Show with TECHO5). It is a hint at the moment something new
// arrives, not a lasting light: an overlay with the same tag replaces the
// last one, and when everything was heard it is taken away.

// screenFor is how long the overlay stays.
const screenFor = 60 * time.Second

// syncScreens shows or updates each area's overlay when the number of
// waiting messages changes, and takes it away when none wait. The caller
// holds n.mu.
func (n *Notifier) syncScreens(ctx context.Context) {
	busy := n.busyAreas(ctx)
	w, err := n.readWaiting(ctx)
	if err != nil {
		n.log.Error("could not check waiting messages", "error", err)
		return
	}
	for area, action := range n.screens {
		if n.cfg.Indicators[area] != "" {
			continue // the light shows it
		}
		list := w.in(area)
		count := len(list)
		if count == n.screenCount[area] {
			continue
		}
		if busy[area] {
			n.showWhenIdle(area)
			continue
		}
		t := textsFor(n.langOf(n.inArea(ctx, area)))
		now := n.clock()
		switch {
		case count > 0:
			text := t.screenOne
			if count > 1 {
				text = fmt.Sprintf(t.screenMany, count)
			}
			err = n.overlay(ctx, action, "mira", text, "mdi:message-badge", screenFor)
			n.screenShown[area] = now
		case now.Sub(n.screenShown[area]) < screenFor:
			// Still on the screen: replace it with a short "all heard".
			err = n.overlay(ctx, action, "mira", t.screenHeard, "mdi:check", time.Second)
		}
		if err != nil {
			n.log.Warn("could not show waiting messages on the screen", "area", area, "action", action, "error", err)
			continue
		}
		n.screenCount[area] = count
		n.log.Info("screen hint", "area", area, "waiting", count)
	}
}

// syncPhoneScreens shows on each screen what is new on the answering
// machine, as a hint of its own beside Mira's messages, when it changes;
// it goes when nothing is new (docs/spec/anrufbeantworter.md). The caller
// holds n.mu.
func (n *Notifier) syncPhoneScreens(ctx context.Context, news PhoneNews) {
	busy := n.busyAreas(ctx)
	for area, action := range n.screens {
		if n.cfg.Indicators[area] != "" || n.phoneShown[area] == news || n.phoneHeld[area] {
			continue
		}
		if busy[area] {
			n.showWhenIdle(area)
			continue
		}
		t := textsFor(n.langOf(n.inArea(ctx, area)))
		var err error
		if news.Messages > 0 || news.Missed > 0 {
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
			err = n.overlay(ctx, action, "phone", strings.Join(parts, " · "), icon, screenFor)
		} else {
			err = n.overlay(ctx, action, "phone", t.screenHeard, "mdi:check", time.Second)
		}
		if err != nil {
			n.log.Warn("could not show the answering machine on the screen", "area", area, "error", err)
			continue
		}
		n.phoneShown[area] = news
		n.log.Info("screen hint for the answering machine", "area", area, "messages", news.Messages, "missed", news.Missed)
	}
}

// inConversation says whether a satellite's state is a conversation:
// listening, thinking or speaking.
func inConversation(state string) bool {
	return state == "listening" || state == "processing" || state == "responding"
}

// busyAreas are the areas with a screen whose satellite is in a
// conversation now: a hint there would cover the turn screen, and one
// would not see that Mira listens. Read once per sync, filtered to the
// satellites.
func (n *Notifier) busyAreas(ctx context.Context) map[string]bool {
	areaOf := map[string]string{}
	var ids []string
	for area := range n.screens {
		if sat := n.sats[area]; strings.HasPrefix(sat, "assist_satellite.") {
			areaOf[sat] = area
			ids = append(ids, sat)
		}
	}
	busy := map[string]bool{}
	states, err := n.ha.GetStates(ctx, haws.StateFilter{EntityIDs: ids})
	if err != nil {
		return busy
	}
	for _, st := range states {
		if inConversation(st.State) {
			busy[areaOf[st.EntityID]] = true
		}
	}
	return busy
}

// showWhenIdle shows an area's hints once its satellite has left the
// conversation: it watches the satellite's state (no polling), one waiter
// per area. The caller holds n.mu.
func (n *Notifier) showWhenIdle(area string) {
	if n.awaitingIdle[area] {
		return
	}
	n.awaitingIdle[area] = true
	sat := n.sats[area]
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), idleWait*10)
		defer cancel()
		n.ha.WaitState(ctx, sat, idleWait*10, func(s string) bool { return !inConversation(s) })
		n.mu.Lock()
		defer n.mu.Unlock()
		delete(n.awaitingIdle, area)
		n.syncIndicators(ctx)
	}()
}

// HoldPhoneScreen keeps the answering machine's hint off an area's screen
// while it plays messages: each one heard lowers the count, and the hint
// would cover the card of what plays. Released, the screen gets the count
// as it is now (docs/spec/anrufbeantworter.md, decision 10).
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

func splitAction(action string) (string, string, bool) {
	for i := 0; i < len(action); i++ {
		if action[i] == '.' {
			return action[:i], action[i+1:], i > 0 && i < len(action)-1
		}
	}
	return "", "", false
}
