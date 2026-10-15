package notifier

import (
	"context"
	"fmt"
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
	if e.ha.calls[0].Data["preannounce"] != true {
		t.Errorf("no chime before a reminder: %v", e.ha.calls[0].Data)
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

// start_conversation returns only after the satellite stopped listening; a
// timeout while it listens is not a failure and sends nothing privately.
func TestStartConversationTimeoutCountsAsSpoken(t *testing.T) {
	e := newEnv(t, nil)
	sat := e.n.sats["kueche"]
	e.ha.set(sat, "idle", map[string]any{"supported_features": float64(3)})
	e.ha.callErr = fmt.Errorf("call_service timed out: %w", context.DeadlineExceeded)
	err := e.n.Tell(context.Background(), store.Notice{ID: "p1", Source: store.SourceTask, DedupeKey: "p1",
		Importance: 2, Audience: "stefan", Spoken: "Mein Vorschlag."}, "kueche")
	if err != nil {
		t.Fatal(err)
	}
	if called := e.ha.servicesCalled(); len(called) != 1 || called[0] != "assist_satellite.start_conversation" {
		t.Fatalf("services: %v", called)
	}
	if len(e.tg.sent) != 0 {
		t.Errorf("sent privately as well: %v", e.tg.sent)
	}
	if e.ha.calls[0].Data["preannounce"] != false {
		t.Errorf("chime before a proposal: %v", e.ha.calls[0].Data)
	}
}

// The satellite turns idle while its speaker still plays the answer: the
// announcement waits for the speaker, then a breath.
func TestTellLetsTheSatelliteFinishSpeaking(t *testing.T) {
	e := newEnv(t, nil)
	e.n.idlePoll = 10 * time.Millisecond
	e.n.speakPause = 50 * time.Millisecond
	e.ha.set("media_player.kueche", "playing", nil)
	done := make(chan time.Time, 1)
	go func() {
		e.n.Tell(context.Background(), store.Notice{ID: "p1", Source: store.SourceTask, DedupeKey: "p1",
			Importance: 2, Audience: store.AudienceHousehold, Spoken: "Mein Vorschlag."}, "kueche")
		done <- time.Now()
	}()
	time.Sleep(100 * time.Millisecond)
	if len(e.ha.servicesCalled()) != 0 {
		t.Fatal("announced while the speaker still played")
	}
	stopped := time.Now()
	e.ha.set("media_player.kueche", "idle", nil)
	select {
	case at := <-done:
		if at.Sub(stopped) < 50*time.Millisecond {
			t.Error("no breath after the speaker stopped")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("not announced after the speaker stopped")
	}
	if len(e.ha.servicesCalled()) != 1 {
		t.Errorf("services: %v", e.ha.servicesCalled())
	}
}
