package notifier

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sker65/mira/internal/config"
	"github.com/sker65/mira/internal/store"
)

func indicateEnv(t *testing.T) *env {
	e := newEnv(t, func(c *config.Config) {
		c.Notifier.RoomMode = map[string]string{"kueche": config.RoomIndicate}
		c.Notifier.Indicators = map[string]string{"kueche": "light.ring"}
	})
	e.now = at("08:00") // after quiet hours
	return e
}

func (e *env) lightCalls() []call {
	e.ha.mu.Lock()
	defer e.ha.mu.Unlock()
	var out []call
	for _, c := range e.ha.calls {
		if c.Domain == "light" {
			out = append(out, c)
		}
	}
	return out
}

// In an indicate room nothing is spoken; the ring shows the notice, the
// user asks for it, and the ring goes off.
func TestIndicateShowsInsteadOfSpeaking(t *testing.T) {
	e := indicateEnv(t)
	e.notice("n1", 1, nil)

	if out := e.presence("kueche"); out.Skipped != "indicate mode" || len(out.Announced) != 0 {
		t.Fatalf("presence: %+v", out)
	}
	lights := e.lightCalls()
	if len(lights) != 1 || lights[0].Service != "turn_on" || lights[0].Target.EntityID[0] != "light.ring" {
		t.Fatalf("light calls: %+v", lights)
	}
	if d := lights[0].Data; d["effect"] != "Pulse" || d["brightness"] != 160 {
		t.Errorf("style: %v", d)
	}
	// Ticks do not switch it again while nothing changes.
	e.tick()
	e.tick()
	if len(e.lightCalls()) != 1 {
		t.Errorf("light switched on every tick: %d", len(e.lightCalls()))
	}
	// Another room still announces.
	if out := e.presence("bad"); len(out.Announced) != 1 {
		t.Errorf("announce room: %+v", out)
	}

	e.notice("n2", 2, nil)
	text, err := e.n.ReadOut(context.Background(), "kueche", "")
	if err != nil || !strings.Contains(text, "Hinweis n2") {
		t.Fatalf("read out: %q %v", text, err)
	}
	if e.state("n2") != store.NoticeDelivered {
		t.Error("read notice not delivered")
	}
	lights = e.lightCalls()
	if last := lights[len(lights)-1]; last.Service != "turn_off" {
		t.Errorf("ring still on: %+v", lights)
	}
	if text, _ := e.n.ReadOut(context.Background(), "kueche", ""); text != "" {
		t.Errorf("read twice: %q", text)
	}
}

// What comes in at night is shown, not spoken; urgent notices are spoken
// anyway.
func TestIndicateAtNightAndUrgent(t *testing.T) {
	e := indicateEnv(t)
	e.now = at("02:00")
	e.notice("n1", 1, nil)
	e.tick()
	if lights := e.lightCalls(); len(lights) != 1 || lights[0].Service != "turn_on" {
		t.Errorf("not shown at night: %+v", lights)
	}
	e.notice("leak", 3, nil)
	e.tick()
	if e.state("leak") != store.NoticeDelivered {
		t.Error("urgent notice waited for the question")
	}
	// Expiry turns the ring off.
	e.now = e.now.Add(49 * time.Hour)
	e.tick()
	if lights := e.lightCalls(); lights[len(lights)-1].Service != "turn_off" {
		t.Errorf("ring on after expiry: %+v", lights)
	}
}

// Asked on Telegram, a person gets what is pending for them.
func TestReadOutForPerson(t *testing.T) {
	e := indicateEnv(t)
	e.notice("mine", 1, func(n *store.Notice) { n.Audience = "stefan"; n.Confidentiality = store.ConfidentialityConfidential })
	e.notice("other", 1, func(n *store.Notice) { n.Audience = "anna" })
	text, err := e.n.ReadOut(context.Background(), "", "stefan")
	if err != nil || !strings.Contains(text, "Hinweis mine") || strings.Contains(text, "other") {
		t.Errorf("read out: %q %v", text, err)
	}
}
