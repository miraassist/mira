package notifier

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sker65/mira/internal/store"
)

func (e *env) reminder(id string, due time.Time, area, audience string) {
	e.t.Helper()
	if err := e.st.AddReminder(context.Background(), store.Reminder{ID: id, DueAt: due, Text: "Müll rausbringen", AreaID: area, Audience: audience}); err != nil {
		e.t.Fatal(err)
	}
}

// A reminder is spoken at its time in the room it was asked for in, even in
// quiet hours (it is 03:00 in the env): the user chose the time.
func TestReminderIsSpokenInItsRoomAtItsTime(t *testing.T) {
	e := newEnv(t, nil)
	e.reminder("r1", e.now.Add(time.Minute), "kueche", store.AudienceHousehold)

	e.n.DueReminders(context.Background())
	if len(e.ha.servicesCalled()) != 0 {
		t.Fatal("delivered before its time")
	}
	e.now = e.now.Add(2 * time.Minute)
	e.n.DueReminders(context.Background())
	called := e.ha.servicesCalled()
	if len(called) != 1 || !strings.HasPrefix(called[0], "assist_satellite.") {
		t.Fatalf("services: %v", called)
	}
	if text := e.ha.calls[0].Data["message"]; text != "Erinnerung: Müll rausbringen." {
		if text2 := e.ha.calls[0].Data["start_message"]; text2 != "Erinnerung: Müll rausbringen." {
			t.Errorf("spoken: %v / %v", text, text2)
		}
	}
	if left, _ := e.st.PendingReminders(context.Background(), time.Time{}); len(left) != 0 {
		t.Error("reminder still pending")
	}
	e.n.DueReminders(context.Background())
	if len(e.ha.servicesCalled()) != 1 {
		t.Error("delivered twice")
	}
}

// Without a room, or with quiet mode on, the reminder goes privately.
func TestReminderWithoutRoomGoesPrivately(t *testing.T) {
	e := newEnv(t, nil)
	e.reminder("r1", e.now, "", "stefan")
	e.st.SetBool(context.Background(), store.KeyQuietMode, true)
	e.reminder("r2", e.now, "bad", store.AudienceHousehold)

	e.n.DueReminders(context.Background())
	if len(e.ha.servicesCalled()) != 0 {
		t.Errorf("spoken despite no room / quiet mode: %v", e.ha.servicesCalled())
	}
	if got := e.tg.sent[111]; len(got) != 2 || got[0] != "Erinnerung: Müll rausbringen." {
		t.Errorf("telegram: %v", got)
	}
}

// A proposal waits until the satellite has finished speaking the answer
// that asked for it.
func TestTellWaitsForIdleSatellite(t *testing.T) {
	e := newEnv(t, nil)
	e.n.idlePoll = 10 * time.Millisecond
	sat := e.n.sats["kueche"]
	e.ha.set(sat, "responding", nil)
	go func() {
		time.Sleep(50 * time.Millisecond)
		if len(e.ha.servicesCalled()) != 0 {
			t.Error("spoke over the busy satellite")
		}
		e.ha.set(sat, "idle", nil)
	}()
	err := e.n.Tell(context.Background(), store.Notice{ID: "p1", Source: store.SourceTask, DedupeKey: "p1",
		Importance: 2, Audience: store.AudienceHousehold, Spoken: "Mein Vorschlag."}, "kueche")
	if err != nil {
		t.Fatal(err)
	}
	if called := e.ha.servicesCalled(); len(called) != 1 {
		t.Fatalf("services: %v", called)
	}
}
