package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sker65/mira/internal/config"
	"github.com/sker65/mira/internal/i18n"
	"github.com/sker65/mira/internal/store"
)

func reminderTools(t *testing.T) (map[string]InternalTool, *store.Store, time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 15, 18, 0, 0, 0, time.UTC)
	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "db"), Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	persons := map[string]config.Person{"ben": {TelegramUserID: 42}, "anna": {}}
	byName := map[string]InternalTool{}
	for _, it := range Reminders(ReminderOptions{Lang: i18n.DE, Store: st, Clock: func() time.Time { return now },
		Location: time.UTC, Persons: func() map[string]config.Person { return persons }}) {
		byName[it.Name] = it
	}
	return byName, st, now
}

func run(t *testing.T, it InternalTool, inv Invocation, args string) (string, error) {
	t.Helper()
	inv.Args = json.RawMessage(args)
	return it.Run(context.Background(), inv)
}

func TestReminderSet(t *testing.T) {
	tl, st, now := reminderTools(t)
	set := tl[ToolReminderSet]
	voice := Invocation{Channel: config.ChannelVoice, SessionKey: "voice:v1"}
	st.UpsertSession(context.Background(), store.Session{Key: "voice:v1", Channel: "voice", ConversationID: "v1", AreaID: "kueche"}, time.Hour)

	out, err := run(t, set, voice, `{"text":"Nudeln abgießen","in_minutes":10}`)
	if err != nil || !strings.Contains(out, "Donnerstag, 15. Oktober 2026, 18:10 Uhr") {
		t.Fatalf("timer: %q %v", out, err)
	}
	if _, err := run(t, set, Invocation{Channel: config.ChannelTelegram, SessionKey: "telegram:42"}, `{"text":"Müll","at":"2026-10-16 19:00"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, set, voice, `{"text":"Arzt","at":"2026-10-17T08:30","for":"anna"}`); err != nil {
		t.Fatal(err)
	}
	list, _ := st.PendingReminders(context.Background(), time.Time{})
	if len(list) != 3 {
		t.Fatalf("reminders: %+v", list)
	}
	timer, trash, doctor := list[0], list[1], list[2]
	if !timer.DueAt.Equal(now.Add(10*time.Minute)) || timer.AreaID != "kueche" || timer.Receiver != "house" {
		t.Errorf("timer: %+v", timer)
	}
	if trash.Receiver != "ben" || trash.AreaID != "" {
		t.Errorf("telegram reminder should go to the asking person: %+v", trash)
	}
	if doctor.Receiver != "anna" || doctor.DueAt.Hour() != 8 {
		t.Errorf("named person: %+v", doctor)
	}

	for args, want := range map[string]string{
		`{"text":"x","at":"2026-10-15 17:00"}`:                "Vergangenheit",
		`{"text":"x","at":"morgen"}`:                          "Zeit nicht verstanden",
		`{"text":"x"}`:                                        "Zeit nicht verstanden",
		`{"text":"x","at":"2026-10-16 08:00","in_minutes":5}`: "either",
	} {
		if _, err := run(t, set, voice, args); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", args, err)
		}
	}

	out, _ = run(t, tl[ToolReminderList], voice, `{}`)
	if !strings.Contains(out, "Nudeln abgießen") || !strings.Contains(out, "(anna)") {
		t.Errorf("list: %s", out)
	}
	if out, err := run(t, tl[ToolReminderCancel], voice, `{"id":"`+timer.ID+`"}`); err != nil || out != "Erinnerung gelöscht." {
		t.Errorf("cancel: %q %v", out, err)
	}
	if _, err := run(t, tl[ToolReminderCancel], voice, `{"id":"`+timer.ID+`"}`); err == nil {
		t.Error("cancelled twice")
	}
}

func TestReminderTextsComplete(t *testing.T) {
	for _, l := range i18n.All {
		for _, it := range Reminders(ReminderOptions{Lang: l}) {
			if it.Description == "" || !json.Valid([]byte(it.Schema)) {
				t.Errorf("%s %s: bad description or schema", l, it.Name)
			}
		}
	}
}
