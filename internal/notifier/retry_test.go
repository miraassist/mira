package notifier

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/store"
)

// Retries of a private delivery: docs/spec/retry.md, step 2.

func (e *env) logs() *bytes.Buffer {
	var buf bytes.Buffer
	e.n.log = slog.New(slog.NewTextHandler(&buf, nil))
	return &buf
}

func downChannel() error {
	return &haws.Error{Command: "call_service", Code: "home_assistant_error", Message: "Telegram is down"}
}

func TestRY10_APersonWithoutAChannelIsGivenUpAtOnce(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.Persons["anna"] = config.Person{Entity: "person.anna"} })
	logs := e.logs()
	e.message("posts", 1, private("anna"))
	e.now = at("08:00")
	e.tick()
	if st := e.state("posts"); st != store.MessageFailed {
		t.Fatalf("state = %q, want failed", st)
	}
	e.now = at("08:01")
	e.tick()
	out := logs.String()
	if strings.Count(out, "level=ERROR") != 1 || !strings.Contains(out, "fatal=true") ||
		!strings.Contains(out, "no private channel configured") {
		t.Errorf("log = %s", out)
	}
}

func TestRY11_AChannelThatIsDownIsTriedAgainLaterNotEveryMinute(t *testing.T) {
	e := newEnv(t, nil)
	e.tg.fail = map[string]error{"notify.ben": downChannel()}
	e.message("posts", 1, private("ben"))
	start := at("08:00")
	var tried []int
	for m := 0; m < 10; m++ {
		e.now = start.Add(time.Duration(m) * time.Minute)
		before := e.tg.tries["notify.ben"]
		e.tick()
		if e.tg.tries["notify.ben"] > before {
			tried = append(tried, m)
		}
	}
	if got := tried; len(got) != 4 || got[0] != 0 || got[1] != 1 || got[2] != 3 || got[3] != 7 {
		t.Fatalf("tried at minutes %v, want [0 1 3 7]", got)
	}
	if st := e.state("posts"); st != store.MessagePending {
		t.Errorf("state = %q, want pending", st)
	}
	e.tg.fail = nil
	e.now = start.Add(15 * time.Minute)
	if sent := e.tick(); len(sent) != 1 || len(e.tg.sent["notify.ben"]) != 1 {
		t.Errorf("not delivered once the channel is up: %v %v", sent, e.tg.sent)
	}
}

func TestRY12_After10AttemptsTheMessageIsGivenUp(t *testing.T) {
	e := newEnv(t, nil)
	logs := e.logs()
	e.tg.fail = map[string]error{"notify.ben": downChannel()}
	e.message("posts", 1, private("ben"))
	e.now = at("08:00")
	for i := 0; i < 20 && e.state("posts") == store.MessagePending; i++ {
		e.tick()
		m, _ := e.st.GetMessage(context.Background(), "posts")
		e.now = m.RetryAt // 1, 2, 4 … 256 min: 16:31 at the last, before quiet hours
	}
	if st := e.state("posts"); st != store.MessageFailed || e.tg.tries["notify.ben"] != 10 {
		t.Fatalf("state = %q after %d attempts", st, e.tg.tries["notify.ben"])
	}
	out := logs.String()
	if strings.Count(out, "private delivery failed, trying again") != 9 ||
		!strings.Contains(out, `msg="private delivery given up"`) || !strings.Contains(out, "attempts=10") {
		t.Errorf("log = %s", out)
	}
}

func TestRY13_HomeAssistantsNotFoundIsFatal(t *testing.T) {
	e := newEnv(t, nil)
	e.tg.fail = map[string]error{"notify.ben": &haws.Error{Command: "call_service", Code: "not_found", Message: "Action notify.ben not found."}}
	e.message("posts", 1, private("ben"))
	e.now = at("08:00")
	e.tick()
	if st := e.state("posts"); st != store.MessageFailed || e.tg.tries["notify.ben"] != 1 {
		t.Fatalf("state = %q after %d attempts", st, e.tg.tries["notify.ben"])
	}
}

func TestRY14_TheNextAttemptSurvivesARestart(t *testing.T) {
	e := newEnv(t, nil)
	e.tg.fail = map[string]error{"notify.ben": downChannel()}
	e.message("posts", 1, private("ben"))
	e.now = at("08:00")
	e.tick()
	e.tick()
	e.tg.fail = nil
	m, err := e.st.GetMessage(context.Background(), "posts")
	if err != nil || m.Attempts != 1 || !m.RetryAt.Equal(at("08:01")) {
		t.Fatalf("kept: attempts %d, retry at %s, %v", m.Attempts, m.RetryAt, err)
	}
	// A new notifier on the same database: the time is kept, not reset.
	e.n = New(Options{Config: e.cfg, Store: e.st, HA: e.ha, Clock: func() time.Time { return e.now },
		Location: time.UTC, Logger: slog.Default(), Structure: e.n.structure})
	e.n.speakPause = 0
	e.now = at("08:00").Add(30 * time.Second)
	if sent := e.tick(); len(sent) != 0 {
		t.Fatalf("tried before its time: %v", sent)
	}
	e.now = at("08:01")
	if sent := e.tick(); len(sent) != 1 {
		t.Errorf("not tried at its time: %v", sent)
	}
}

// Retries of a reminder: docs/spec/retry.md, step 3.

func TestRY20_AReminderThatCannotBeDeliveredForGoodFailsAtOnce(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.Persons["anna"] = config.Person{Entity: "person.anna"} })
	logs := e.logs()
	e.reminder("r1", e.now, "", "anna")
	e.n.DueMessages(context.Background())
	if st := e.state("r1"); st != store.MessageFailed {
		t.Fatalf("state = %q, want failed", st)
	}
	if out := logs.String(); !strings.Contains(out, "fatal=true") || strings.Contains(out, "trying again") {
		t.Errorf("log = %s", out)
	}
}

func TestRY21_AReminderIsTriedAgainAfter15_30_45s(t *testing.T) {
	e := newEnv(t, nil)
	e.tg.fail = map[string]error{"notify.ben": downChannel()}
	e.reminder("r1", e.now, "", "ben")
	var waits []time.Duration
	for i := 0; i < 10 && e.state("r1") != store.MessageFailed; i++ {
		e.n.DueMessages(context.Background())
		m, _ := e.st.GetMessage(context.Background(), "r1")
		if m.State == store.MessageScheduled {
			waits = append(waits, m.DeliverAt.Sub(e.now))
			e.now = m.DeliverAt
		}
	}
	want := []time.Duration{15 * time.Second, 30 * time.Second, 45 * time.Second, 60 * time.Second}
	if len(waits) != len(want) || e.tg.tries["notify.ben"] != 5 || e.state("r1") != store.MessageFailed {
		t.Fatalf("waits %v, %d attempts, state %s", waits, e.tg.tries["notify.ben"], e.state("r1"))
	}
	for i := range want {
		if waits[i] != want[i] {
			t.Errorf("waits = %v, want %v", waits, want)
		}
	}
	// Its words are not prefixed again on each attempt.
	m, _ := e.st.GetMessage(context.Background(), "r1")
	if m.Spoken != "Erinnerung: Müll rausbringen." {
		t.Errorf("spoken = %q", m.Spoken)
	}
}
