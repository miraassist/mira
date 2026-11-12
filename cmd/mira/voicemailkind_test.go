package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/store/storetest"
	"github.com/miraassist/mira/internal/threads"
	"github.com/miraassist/mira/internal/voicemail"
)

// docs/spec/messages.md, step 4: the answering machine as a kind.

type fakeBox struct {
	messages []voicemail.Message
	err      error
}

func (b *fakeBox) ask(_ context.Context, service string, _ map[string]any) (json.RawMessage, error) {
	if b.err != nil {
		return nil, b.err
	}
	if service != "get_messages" {
		return json.RawMessage(`{"calls":[]}`), nil
	}
	return json.Marshal(map[string]any{"messages": b.messages})
}

func (b *fakeBox) record(id, name string, at time.Time) {
	b.messages = append([]voicemail.Message{{ID: id, Name: name, Date: at.Format(time.RFC3339), Minutes: 1, New: true}}, b.messages...)
}

type vmBed struct {
	a   *app
	box *fakeBox
	log *bytes.Buffer
	now time.Time
}

func voicemailKindApp(t *testing.T) *vmBed {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{Path: storetest.Path(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	loaded, problems := skills.Load([]string{"../../skills"}, map[string]map[string]any{voicemail.Skill: nil})
	if len(problems) > 0 {
		t.Fatalf("skills: %+v", problems)
	}
	b := &vmBed{box: &fakeBox{}, log: &bytes.Buffer{}, now: time.Now()}
	logger := slog.New(slog.NewTextHandler(b.log, nil))
	clock := func() time.Time { return b.now }
	b.a = &app{store: st, log: logger, lang: i18n.DE,
		threads: threads.New(threads.Options{Lang: i18n.DE, Store: st, Targets: loaded, Logger: logger, Clock: clock})}
	b.a.voicemail = voicemail.New(voicemail.Options{Lang: i18n.DE, Logger: logger, Clock: clock, Ask: b.box.ask,
		Do: func(context.Context, string, map[string]any) error { return nil }})
	return b
}

func (b *vmBed) houseThread(t *testing.T) (store.Thread, bool) {
	t.Helper()
	th, ok, err := b.a.threads.HouseThread(context.Background(), voicemail.Skill)
	if err != nil {
		t.Fatal(err)
	}
	return th, ok
}

func (b *vmBed) voicemailMessages(t *testing.T) []store.Message {
	t.Helper()
	all, err := b.a.store.ListMessages(context.Background(), store.MessageFilter{States: []string{store.MessagePending},
		Receiver: store.ReceiverHouse})
	if err != nil {
		t.Fatal(err)
	}
	var out []store.Message
	for _, m := range all {
		if m.Kind == store.KindVoicemail {
			out = append(out, m)
		}
	}
	return out
}

func TestMS16_ARecordingStartsTheHousesThreadAndMessage(t *testing.T) {
	b := voicemailKindApp(t)
	// A recording of last week: the expiry counts from now, never from its
	// date (decision 11).
	b.box.record("0:13", "Oma", b.now.Add(-7*24*time.Hour))
	b.a.voicemailArrived(context.Background(), 1)
	th, ok := b.houseThread(t)
	if !ok || th.Place != store.PlaceHouse || !strings.Contains(planText(th), "Oma") || !strings.Contains(planText(th), "(id 0:13)") {
		t.Fatalf("thread = %+v", th)
	}
	if !near(th.ExpiresAt, time.Now().Add(5*24*time.Hour)) {
		t.Fatalf("expires %s", th.ExpiresAt)
	}
	ms := b.voicemailMessages(t)
	if len(ms) != 1 || len(ms[0].Entries) != 1 || ms[0].Entries[0].ThreadID != th.ID || !strings.HasPrefix(ms[0].Entries[0].Text, "Oma, ") {
		t.Fatalf("messages = %+v", ms)
	}
}

func TestMS17_AFurtherRecordingExtendsThem(t *testing.T) {
	b := voicemailKindApp(t)
	ctx := context.Background()
	b.box.record("0:12", "Ben", b.now.Add(-2*time.Hour))
	b.a.voicemailArrived(ctx, 1)
	first, _ := b.houseThread(t)
	later := b.now.Add(-time.Minute)
	b.box.record("0:13", "Oma", later)
	b.a.voicemailArrived(ctx, 2)
	b.a.voicemailArrived(ctx, 2) // read again: nothing twice
	th, _ := b.houseThread(t)
	if th.ID != first.ID || strings.Count(planText(th), "- [ ]") != 2 {
		t.Fatalf("thread %s (first %s), plan:\n%s", th.ID, first.ID, planText(th))
	}
	if !near(th.ExpiresAt, time.Now().Add(5*24*time.Hour)) {
		t.Fatalf("expires %s", th.ExpiresAt)
	}
	ms := b.voicemailMessages(t)
	if len(ms) != 1 || len(ms[0].Entries) != 2 {
		t.Fatalf("messages = %+v", ms)
	}
}

func TestMS18_HeardAtTheBoxTheThreadEndsAndTheMessageGoes(t *testing.T) {
	b := voicemailKindApp(t)
	ctx := context.Background()
	b.box.record("0:13", "Oma", b.now.Add(-time.Minute))
	b.a.voicemailArrived(ctx, 1)
	th, _ := b.houseThread(t)
	b.a.voicemailHeard(ctx)
	if ms := b.voicemailMessages(t); len(ms) != 0 {
		t.Fatalf("still waiting: %+v", ms)
	}
	got, _ := b.a.store.GetThread(ctx, th.ID)
	if strings.Contains(planText(got), "- [ ]") || !store.ThreadOpen(got.State) {
		t.Fatalf("thread %s, plan:\n%s", got.State, planText(got))
	}
	// Open the minutes a done plan stays ("nochmal"), then it ends.
	if !near(got.ExpiresAt, b.now.Add(5*time.Minute)) {
		t.Fatalf("expires %s", got.ExpiresAt)
	}
}

func TestMS20_TheBoxCannotBeRead(t *testing.T) {
	b := voicemailKindApp(t)
	b.box.err = errors.New("mira_fritz/get_messages: timeout")
	b.a.voicemailArrived(context.Background(), 1)
	if _, ok := b.houseThread(t); ok {
		t.Fatal("a thread was started")
	}
	if ms := b.voicemailMessages(t); len(ms) != 0 {
		t.Fatalf("messages = %+v", ms)
	}
	if !strings.Contains(b.log.String(), "could not read the answering machine's list") || !strings.Contains(b.log.String(), "timeout") {
		t.Fatalf("log:\n%s", b.log)
	}
}

// planText is a thread's plan as stored.
func planText(th store.Thread) string {
	var c struct{ Plan string }
	_ = json.Unmarshal(th.Content, &c)
	return c.Plan
}

// near: the same time, give or take a second (the database keeps
// milliseconds, a recording's date seconds).
func near(a, b time.Time) bool { d := a.Sub(b); return d > -time.Second && d < time.Second }

func TestMS34_NewRecordingsWhenMiraStarts(t *testing.T) {
	b := voicemailKindApp(t)
	ctx := context.Background()
	// Unheard for over a week (as the test box's own): one message with two
	// entries all the same, and a thread that waits 5 days from now.
	b.box.record("0:12", "Ben", b.now.Add(-10*24*time.Hour))
	b.box.record("0:13", "Oma", b.now.Add(-8*24*time.Hour))
	b.a.voicemailCounted(ctx, -1, 2)
	b.a.voicemailCounted(ctx, -1, 2) // the watch starts again
	th, ok := b.houseThread(t)
	if !ok || strings.Count(planText(th), "- [ ]") != 2 {
		t.Fatalf("thread %v:\n%s", ok, planText(th))
	}
	if ms := b.voicemailMessages(t); len(ms) != 1 || len(ms[0].Entries) != 2 {
		t.Fatalf("messages = %+v", ms)
	}
	// Nothing new at the start: nothing.
	b2 := voicemailKindApp(t)
	b2.a.voicemailCounted(ctx, -1, 0)
	if _, ok := b2.houseThread(t); ok {
		t.Fatal("a thread without recordings")
	}
}
