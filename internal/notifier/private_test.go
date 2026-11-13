package notifier

import (
	"context"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/store"
)

// Private messages: docs/spec/schedule-receiver.md, step 1.

func private(receiver string) func(*store.Message) {
	return func(n *store.Message) {
		n.Confidentiality = store.ConfidentialityPrivate
		n.Receiver = receiver
	}
}

func TestPS01_APrivateMessageGoesOutOverTheChannelsAtTheTick(t *testing.T) {
	e := newEnv(t, nil)
	e.message("posts", 1, private("ben"))
	e.now = at("08:00")
	if sent := e.tick(); len(sent) != 1 || len(e.tg.sent["notify.ben"]) != 1 {
		t.Fatalf("sent = %v, telegram = %v", sent, e.tg.sent)
	}
	if got := e.tg.sent["notify.ben"][0]; !strings.Contains(got, "Hinweis posts") || !strings.Contains(got, "Details posts") {
		t.Errorf("sent text = %q, want spoken text and details", got)
	}
	if st := e.state("posts"); st != store.MessageDelivered {
		t.Errorf("state = %q, want delivered", st)
	}
	if len(e.ha.calls) != 0 {
		t.Errorf("a private message was spoken: %+v", e.ha.calls)
	}
}

func TestPS02_APrivateMessageIsNeverSpoken(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.Notifier.ConfidentialVoice = config.ConfidentialVoiceWhenAlone })
	e.message("posts", 2, private("ben"))
	e.now = at("08:00")
	ctx := context.Background()
	if w, err := e.n.waiting(ctx, "bad"); err != nil || len(w) != 0 {
		t.Fatalf("a private message counts as waiting: %v %v", w, err)
	}
	out := e.presence("bad")
	if len(out.Announced) != 0 || len(e.ha.calls) != 0 {
		t.Fatalf("spoken: %+v %+v", out, e.ha.calls)
	}
	if len(out.Private) != 1 || len(e.tg.sent["notify.ben"]) != 1 {
		t.Fatalf("not sent privately: %+v %v", out, e.tg.sent)
	}
}

func TestPS03_APrivateMessageWaitsForTheEndOfQuietHours(t *testing.T) {
	e := newEnv(t, nil) // 03:00, inside quiet hours
	e.message("posts", 1, private("ben"))
	if sent := e.tick(); len(sent) != 0 || len(e.tg.sent) != 0 {
		t.Fatalf("sent in quiet hours: %v %v", sent, e.tg.sent)
	}
}

func TestPS04_AnUrgentPrivateMessageIsNotAnnounced(t *testing.T) {
	e := newEnv(t, nil)
	e.message("posts", 3, private("ben"))
	if sent := e.tick(); len(sent) != 1 || len(e.tg.sent["notify.ben"]) != 1 {
		t.Fatalf("sent = %v, telegram = %v", sent, e.tg.sent)
	}
	if len(e.ha.calls) != 0 {
		t.Errorf("an urgent private message was announced: %+v", e.ha.calls)
	}
}

// A private message of the house goes to every person with a channel and
// skips the persons without one (docs/spec/group-watch.md, decision 20).
func TestAPrivateMessageOfTheHouseGoesToEveryPersonWithAChannel(t *testing.T) {
	e := newEnv(t, func(c *config.Config) {
		c.Persons["anna"] = config.Person{Entity: "person.anna", Channels: []string{"notify.anna"}}
		c.Persons["martin"] = config.Person{Entity: "person.martin"}
	})
	e.message("fenster", 2, private(store.ReceiverHouse))
	e.now = at("08:00")
	if sent := e.tick(); len(sent) != 1 || len(e.tg.sent["notify.ben"]) != 1 || len(e.tg.sent["notify.anna"]) != 1 {
		t.Fatalf("sent = %v, channels = %v", sent, e.tg.sent)
	}
	if strings.Contains(e.logged(), "partly failed") {
		t.Errorf("a person without a channel counted as failed: %s", e.logged())
	}
}
