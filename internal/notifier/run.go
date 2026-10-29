package notifier

import (
	"context"
	"time"

	haws "github.com/miraassist/mira/internal/ha/ws"
)

// Subscriber is the part of the WebSocket client Run needs.
type Subscriber interface {
	SubscribeTrigger(ctx context.Context, trigger map[string]any, handler haws.EventHandler) (func(), error)
}

// Run watches the presence sensors and runs Tick every minute until ctx ends.
func (n *Notifier) Run(ctx context.Context, sub Subscriber) error {
	motion := make(chan string, 16)
	for area, sensors := range n.cfg.MorningTrigger.Presence {
		if !n.areas[area] {
			continue
		}
		for _, sensor := range sensors {
			trigger := map[string]any{"trigger": "state", "entity_id": sensor, "to": "on"}
			cancel, err := sub.SubscribeTrigger(ctx, trigger, func(haws.Event) {
				select {
				case motion <- area:
				default: // a burst of motion events; one is enough
				}
			})
			if err != nil {
				n.log.Error("could not watch presence sensor", "area", area, "sensor", sensor, "error", err)
				continue
			}
			defer cancel()
			n.log.Info("watching presence", "area", area, "sensor", sensor)
		}
	}

	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	// Reminders are checked more often: "in five minutes" should not come
	// a minute late.
	remind := time.NewTicker(15 * time.Second)
	defer remind.Stop()
	n.DueMessages(ctx)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case area := <-motion:
			out, err := n.OnPresence(ctx, area)
			switch {
			case err != nil:
				n.log.Error("announcement failed", "area", area, "error", err)
			case len(out.Announced) > 0 || len(out.Private) > 0:
				n.log.Info("presence handled", "area", area, "announced", len(out.Announced), "private", len(out.Private))
			default:
				n.log.Debug("presence ignored", "area", area, "reason", out.Skipped)
			}
		case <-remind.C:
			n.dueInBackground(ctx)
		case <-tick.C:
			if _, err := n.Tick(ctx); err != nil && ctx.Err() == nil {
				n.log.Error("notifier tick failed", "error", err)
			}
		}
	}
}
