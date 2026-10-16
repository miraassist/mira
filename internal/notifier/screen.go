package notifier

import (
	"context"
	"fmt"
	"time"
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
	for area, action := range n.screens {
		if n.cfg.Indicators[area] != "" {
			continue // the light shows it
		}
		list, err := n.waiting(ctx, area)
		if err != nil {
			n.log.Error("could not check waiting messages", "area", area, "error", err)
			return
		}
		count := len(list)
		if count == n.screenCount[area] {
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
			err = n.overlay(ctx, action, text, "mdi:message-badge", screenFor)
			n.screenShown[area] = now
		case now.Sub(n.screenShown[area]) < screenFor:
			// Still on the screen: replace it with a short "all heard".
			err = n.overlay(ctx, action, t.screenHeard, "mdi:check", time.Second)
		}
		if err != nil {
			n.log.Warn("could not show waiting messages on the screen", "area", area, "action", action, "error", err)
			continue
		}
		n.screenCount[area] = count
		n.log.Info("screen hint", "area", area, "waiting", count)
	}
}

func (n *Notifier) overlay(ctx context.Context, action, text, icon string, d time.Duration) error {
	domain, service, ok := splitAction(action)
	if !ok {
		return fmt.Errorf("not an action: %q", action)
	}
	return n.ha.CallService(ctx, domain, service, map[string]any{
		"title": "Mira", "message": text, "icon": icon, "chime": false, "speak": false,
		"tag": "mira", "seconds": int(d.Seconds()),
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
