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
	persons := map[string]config.Person{"ben": {Name: "Ben"}, "anna": {Name: "Anna"}}
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
	if _, err := run(t, set, Invocation{Channel: config.ChannelTelegram, SessionKey: "telegram:42", Person: "ben"}, `{"text":"Müll","at":"2026-10-16 19:00"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, set, voice, `{"text":"Arzt","at":"2026-10-17T08:30","for":"anna"}`); err != nil {
		t.Fatal(err)
	}
	list, _ := st.ScheduledMessages(context.Background(), time.Time{})
	if len(list) != 3 {
		t.Fatalf("reminders: %+v", list)
	}
	timer, trash, doctor := list[0], list[1], list[2]
	if !timer.DeliverAt.Equal(now.Add(10*time.Minute)) || timer.AreaID != "kueche" || timer.Receiver != "house" {
		t.Errorf("timer: %+v", timer)
	}
	if trash.Receiver != "ben" || trash.AreaID != "" {
		t.Errorf("telegram reminder should go to the asking person: %+v", trash)
	}
	if doctor.Receiver != "anna" || doctor.DeliverAt.Hour() != 8 {
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

// A message names its sender, goes to the house or a person, and follows the
// receiver's setting whose messages they take (docs/spec/nachrichten.md).
func TestMessageSend(t *testing.T) {
	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	now := time.Date(2026, 10, 15, 18, 0, 0, 0, time.UTC)
	persons := map[string]config.Person{
		"anna": {Name: "Anna"},
		"ben":  {Name: "Ben", AcceptFrom: config.AcceptPersons},
		"oma":  {Name: "Oma", AcceptFrom: config.AcceptNone},
	}
	var send InternalTool
	for _, it := range Reminders(ReminderOptions{Lang: i18n.DE, Store: st, Clock: func() time.Time { return now },
		Location: time.UTC, Persons: func() map[string]config.Person { return persons },
		AreaName: func(id string) string { return map[string]string{"kueche": "Küche"}[id] }}) {
		if it.Name == ToolMessageSend {
			send = it
		}
	}
	anna := Invocation{Channel: config.ChannelTelegram, Person: "anna"}
	someone := Invocation{Channel: config.ChannelVoice, Area: "kueche"}

	if _, err := run(t, send, anna, `{"to":"Ben","text":"Es wird später."}`); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, send, someone, `{"to":"house","text":"Der Schornsteinfeger kommt um 10"}`); err != nil {
		t.Fatal(err)
	}
	// Ben takes messages from persons only; Oma none at all.
	if _, err := run(t, send, someone, `{"to":"ben","text":"x"}`); err == nil || !strings.Contains(err.Error(), "Ben nimmt nur") {
		t.Errorf("anonymous to ben: %v", err)
	}
	if _, err := run(t, send, anna, `{"to":"oma","text":"x"}`); err == nil || !strings.Contains(err.Error(), "Oma nimmt gerade keine") {
		t.Errorf("to oma: %v", err)
	}
	if _, err := run(t, send, anna, `{"to":"Zoe","text":"x"}`); err == nil {
		t.Error("unknown receiver accepted")
	}
	// With a time it waits until then.
	if _, err := run(t, send, anna, `{"to":"ben","text":"Müll rausbringen","at":"2026-10-16 07:00"}`); err != nil {
		t.Fatal(err)
	}

	msgs, _ := st.ListMessages(context.Background(), store.MessageFilter{States: []string{store.MessagePending}})
	got := map[string]store.Message{}
	for _, m := range msgs {
		got[m.Receiver] = m
	}
	if m := got["ben"]; m.Spoken != "Anna lässt dir ausrichten: Es wird später." || m.Sender != "person:anna" {
		t.Errorf("to ben: %+v", m)
	}
	if m := got["house"]; m.Spoken != "Jemand im Bereich Küche lässt ausrichten: Der Schornsteinfeger kommt um 10." || m.Sender != "area:kueche" {
		t.Errorf("to the house: %+v", m)
	}
	scheduled, _ := st.ScheduledMessages(context.Background(), time.Time{})
	if len(scheduled) != 1 || scheduled[0].Receiver != "ben" || scheduled[0].DeliverAt.Hour() != 7 {
		t.Errorf("scheduled: %+v", scheduled)
	}
}
