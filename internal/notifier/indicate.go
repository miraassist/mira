package notifier

import (
	"context"
	"errors"
	"strings"

	"github.com/oklog/ulid/v2"

	haws "github.com/sker65/mira/internal/ha/ws"
	"github.com/sker65/mira/internal/store"
)

// In a room with mode "indicate", notices are not spoken when someone comes
// in. The room's indicator light (the LED ring of an Echo, a lamp) shows
// that something is waiting, and the user asks for it: "Hast du eine
// Benachrichtigung?". Nobody is spoken to unasked, and the wake word makes
// sure the satellite is listening when Mira answers.

// waiting returns the notices that would be spoken in area.
func (n *Notifier) waiting(ctx context.Context, area string) ([]store.Notice, error) {
	pending, err := n.pending(ctx)
	if err != nil {
		return nil, err
	}
	home := n.whoIsHome(ctx)
	var out []store.Notice
	for _, nt := range pending {
		if nt.Source == store.SourceReminder {
			continue // spoken at its time, never waits
		}
		if n.route(nt, home) == routeVoice {
			out = append(out, nt)
		}
	}
	return out, nil
}

// syncIndicators turns each indicator light on while notices wait for its
// area, and off when none do. A light is only switched when its wanted
// state changes, so the ring is not reset every minute; after a restart
// every light is set once. The caller holds n.mu.
func (n *Notifier) syncIndicators(ctx context.Context) {
	for area, light := range n.cfg.Indicators {
		if n.speakingIn[area] {
			continue // speaking restores it when the text ends
		}
		list, err := n.waiting(ctx, area)
		if err != nil {
			n.log.Error("could not check waiting notices", "area", area, "error", err)
			return
		}
		want := len(list) > 0
		if on, known := n.indicatorOn[area]; known && on == want {
			continue
		}
		if err := n.setIndicator(ctx, light, want); err != nil {
			n.log.Error("could not switch indicator", "area", area, "light", light, "error", err)
			continue
		}
		n.indicatorOn[area] = want
		n.log.Info("indicator switched", "area", area, "light", light, "on", want, "waiting", len(list))
	}
}

func (n *Notifier) setIndicator(ctx context.Context, light string, on bool) error {
	if !on {
		return n.ha.CallService(ctx, "light", "turn_off", nil, &haws.Target{EntityID: []string{light}})
	}
	return n.lightUp(ctx, light, n.cfg.Indicator.Effect)
}

// lightUp turns a light on in the indicator's colour with the given effect;
// "None" is steady.
func (n *Notifier) lightUp(ctx context.Context, light, effect string) error {
	target := &haws.Target{EntityID: []string{light}}
	style := n.cfg.Indicator
	data := map[string]any{}
	if len(style.RGBColor) == 3 {
		data["rgb_color"] = style.RGBColor
	}
	if effect != "" {
		data["effect"] = effect
	}
	if style.Brightness > 0 {
		data["brightness"] = style.Brightness
	}
	err := n.ha.CallService(ctx, "light", "turn_on", data, target)
	if err != nil && data["effect"] != nil {
		// A lamp without that effect (or without colour) refuses the call;
		// plainly on is still a sign.
		n.log.Debug("indicator style refused, switching on plainly", "light", light, "error", err)
		delete(data, "effect")
		if err = n.ha.CallService(ctx, "light", "turn_on", data, target); err != nil {
			err = n.ha.CallService(ctx, "light", "turn_on", nil, target)
		}
	}
	return err
}

// While Mira speaks on her own, the room's indicator light glows steadily,
// as the satellite's own light does when someone talks to it, and goes out
// when the text ends: the satellite leaves "responding". Until then
// syncIndicators leaves that light alone; afterwards it shows what waits.

// speaking lights the area's indicator steadily for an announcement and
// watches the satellite to restore it when the text ends. It runs before
// the announcement is sent, since start_conversation returns only after
// the listening that follows. The caller holds n.mu.
func (n *Notifier) speaking(ctx context.Context, area string) {
	light, sat := n.cfg.Indicators[area], n.sats[area]
	if light == "" || !strings.HasPrefix(sat, "assist_satellite.") {
		return
	}
	if err := n.lightUp(ctx, light, "None"); err != nil {
		n.log.Warn("could not light the indicator for an announcement", "area", area, "error", err)
		return
	}
	n.speakingIn[area] = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), n.speakingStart+idleWait)
		defer cancel()
		n.waitFor(ctx, sat, n.speakingStart, func(s string) bool { return s == "responding" })
		n.waitFor(ctx, sat, idleWait, func(s string) bool { return s != "responding" })
		n.mu.Lock()
		defer n.mu.Unlock()
		delete(n.speakingIn, area)
		delete(n.indicatorOn, area) // its state is no longer what sync set
		n.syncIndicators(ctx)
	}()
}

// SyncIndicators is syncIndicators for callers outside the notifier.
func (n *Notifier) SyncIndicators(ctx context.Context) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.syncIndicators(ctx)
}

// ReadOut is the answer to "Hast du eine Benachrichtigung?": the notices
// waiting for area (asked by voice) or for audience (asked on Telegram), as
// one text to say, marked delivered. It returns "" when nothing waits.
//
// Read out by voice, it counts as an announcement on that satellite, so a
// follow-up question ("Was genau?") gets the details as after any other.
func (n *Notifier) ReadOut(ctx context.Context, area, audience string) (string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	var list []store.Notice
	var err error
	if area != "" {
		list, err = n.waiting(ctx, area)
	} else {
		list, err = n.forAudience(ctx, audience)
	}
	if err != nil || len(list) == 0 {
		return "", err
	}
	text := Compose(n.lang, list, n.clock().In(n.loc), n.cfg.QuietHours.To)
	via := "telegram"
	if area != "" {
		via = "voice:" + area
	}
	ids := make([]string, len(list))
	for i, nt := range list {
		ids[i] = nt.ID
		if err := n.store.MarkDelivered(ctx, nt.ID, via); err != nil && !errors.Is(err, store.ErrNotFound) {
			return "", err
		}
	}
	if sat := n.sats[area]; sat != "" {
		if err := n.store.RecordAnnouncement(ctx, store.Announcement{
			ID: ulid.Make().String(), DeviceID: n.deviceID(ctx, sat), AreaID: area,
			Channel: "voice", NoticeIDs: ids,
		}); err != nil {
			return "", err
		}
	}
	n.tel.RecordNoticeDelivery(ctx, "asked", len(ids))
	n.log.Info("notices read out on request", "area", area, "audience", audience, "notices", len(ids))
	n.syncIndicators(ctx)
	return text, nil
}

// forAudience is what a person may read on Telegram: everything pending for
// the household or for them, confidential ones included (the chat is
// private).
func (n *Notifier) forAudience(ctx context.Context, audience string) ([]store.Notice, error) {
	pending, err := n.pending(ctx)
	if err != nil {
		return nil, err
	}
	var out []store.Notice
	for _, nt := range pending {
		if nt.Source == store.SourceReminder {
			continue
		}
		if nt.Audience == store.AudienceHousehold || nt.Audience == audience {
			out = append(out, nt)
		}
	}
	return out, nil
}

// Afterword is what follows an answer on a satellite: the notices waiting
// for its area, as "Übrigens: ...", marked heard. Nothing during quiet hours
// (a light switched at night wants no news) and nothing urgent, which was
// spoken already.
func (n *Notifier) Afterword(ctx context.Context, area string) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.sats[area] == "" || n.inQuietHours(n.clock()) {
		return ""
	}
	list, err := n.waiting(ctx, area)
	if err != nil {
		n.log.Error("could not check waiting notices", "area", area, "error", err)
		return ""
	}
	if len(list) == 0 {
		return ""
	}
	if max := n.cfg.MaxItemsPerAnnouncement; max > 0 && len(list) > max {
		list = list[:max] // the rest waits for the next occasion
	}
	t := textsFor(n.lang)
	var b strings.Builder
	ids := make([]string, len(list))
	for i, nt := range list {
		ids[i] = nt.ID
		if i == 0 {
			b.WriteString(t.byTheWay)
		} else {
			b.WriteString(t.also)
		}
		b.WriteString(sentence(nt.Spoken))
		if err := n.store.MarkDelivered(ctx, nt.ID, "voice:"+area); err != nil && !errors.Is(err, store.ErrNotFound) {
			n.log.Error("could not mark notice delivered", "notice", nt.ID, "error", err)
			return ""
		}
	}
	if err := n.store.RecordAnnouncement(ctx, store.Announcement{
		ID: ulid.Make().String(), DeviceID: n.deviceID(ctx, n.sats[area]), AreaID: area,
		Channel: "voice", NoticeIDs: ids,
	}); err != nil {
		n.log.Error("could not record announcement", "area", area, "error", err)
	}
	n.tel.RecordNoticeDelivery(ctx, "afterword", len(ids))
	n.log.Info("notices added to an answer", "area", area, "notices", len(ids))
	n.syncIndicators(ctx)
	return strings.TrimSpace(b.String())
}
