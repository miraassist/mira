package notifier

import (
	"context"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/config"
	haws "github.com/miraassist/mira/internal/ha/ws"
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

// PS-09 A private message to a Telegram chat keeps its Markdown and parses
// (docs/spec/channels.md, Telegram's Markdown)
func TestPS09_APrivateMessageToATelegramChatKeepsItsMarkdownAndParses(t *testing.T) {
	// Given a person whose channel is a notify entity of telegram_bot, and one with the companion app
	e := newEnv(t, func(c *config.Config) {
		c.Persons["ben"] = config.Person{Entity: "person.ben", Channels: []string{"notify.telegram_ben"}}
		c.Persons["anna"] = config.Person{Entity: "person.anna", Channels: []string{"notify.anna_phone"}}
	})
	e.ha.set("notify.telegram_ben", "unknown", nil)
	e.ha.set("notify.anna_phone", "unknown", nil)
	e.ha.set("person.anna", "home", nil)
	e.ha.platforms = map[string]string{"notify.telegram_ben": "telegram_bot", "notify.anna_phone": "mobile_app"}
	// When a private message with an address with "_", a name like max_muster and "_kursiv_" goes out
	text := func(n *store.Message) {
		n.Spoken = "YouTube: _kursiv_ von max_muster."
		n.Details = "• Kanal: Video (11 min). https://www.youtube.com/watch?v=wSFJwZ9_fGg"
	}
	e.message("ben-list", 1, func(n *store.Message) { private("ben")(n); text(n) })
	e.message("anna-list", 1, func(n *store.Message) { private("anna")(n); text(n) })
	e.now = at("08:00")
	e.tick()
	// Then the Telegram chat gets the "_" of the address and the name escaped and "_kursiv_" as it is
	tg := strings.Join(e.tg.sent["notify.telegram_ben"], "\n")
	if !strings.Contains(tg, `watch?v=wSFJwZ9\_fGg`) || !strings.Contains(tg, `max\_muster`) || !strings.Contains(tg, "_kursiv_") ||
		strings.Contains(tg, `\_kursiv`) {
		t.Errorf("telegram got %q", tg)
	}
	// And the companion app gets the text unchanged
	app := strings.Join(e.tg.sent["notify.anna_phone"], "\n")
	if !strings.Contains(app, "wSFJwZ9_fGg") || strings.Contains(app, `\`) {
		t.Errorf("app got %q", app)
	}
}

// PS-10 A message Telegram cannot parse is not tried again
func TestPS10_AMessageTelegramCannotParseIsNotTriedAgain(t *testing.T) {
	// Given a channel that refuses the message with "Can't parse entities"
	e := newEnv(t, nil)
	logs := e.logs()
	e.tg.fail = map[string]error{"notify.ben": &haws.Error{Command: "call_service", Code: "unknown_error",
		Message: "Can't parse entities: can't find end of the entity starting at byte offset 221"}}
	e.message("posts", 1, private("ben"))
	// When the private delivery runs
	e.now = at("08:00")
	e.tick()
	// Then the message is failed at once, after one attempt, with a log line
	if st := e.state("posts"); st != store.MessageFailed || e.tg.tries["notify.ben"] != 1 {
		t.Fatalf("state = %q after %d attempts", st, e.tg.tries["notify.ben"])
	}
	if out := logs.String(); !strings.Contains(out, `msg="private delivery failed"`) || !strings.Contains(out, "fatal=true") {
		t.Errorf("log = %s", out)
	}
}

func TestTelegramMarkdown(t *testing.T) {
	for in, want := range map[string]string{
		"*fett* und _kursiv_ und `code`":        "*fett* und _kursiv_ und `code`",
		"ein einzelnes * Sternchen":             `ein einzelnes \* Sternchen`,
		"[Text](https://a.example/x_y) und a_b": `[Text](https://a.example/x_y) und a\_b`,
		"Liste [1 von 3":                        `Liste \[1 von 3`,
		"schon \\_ maskiert":                    "schon \\_ maskiert",
		"snake_case und _kursiv_ und 3*4":       `snake\_case und _kursiv_ und 3\*4`,
	} {
		if got := TelegramMarkdown(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}
