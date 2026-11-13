package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/store/storetest"
	"github.com/miraassist/mira/internal/threads"
	"github.com/miraassist/mira/internal/tools"
	"github.com/miraassist/mira/internal/watch"
)

// docs/spec/device-watch.md, step 8: the offer's thread and its entry.

// noTriggers plays Home Assistant for the waiter: subscriptions that never
// fire.
type noTriggers struct{}

func (noTriggers) SubscribeRaw(context.Context, map[string]any, func(json.RawMessage)) (func(), error) {
	return func() {}, nil
}

func (noTriggers) GetStates(context.Context, haws.StateFilter) ([]haws.State, error) { return nil, nil }

func offerApp(t *testing.T) (*app, *bytes.Buffer) {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{Path: storetest.Path(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	loaded, problems := skills.Load([]string{"../../skills"}, map[string]map[string]any{watch.OfferSkill: nil})
	if len(problems) > 0 {
		t.Fatalf("skills: %+v", problems)
	}
	var log bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&log, nil))
	a := &app{store: st, log: logger, lang: i18n.DE,
		threads: threads.New(threads.Options{Lang: i18n.DE, Store: st, Targets: loaded, Logger: logger,
			CodeStarted: []string{watch.OfferSkill}})}
	a.watchWaiter = watch.NewWaiter(watch.WaiterOptions{HA: noTriggers{}, Offers: watchOffers{a}, Decline: st.DeclineWatch,
		Lang: i18n.DE, Logger: logger})
	return a, &log
}

// offered is an offer as it stands after the device's run: its thread at
// the house, its numbers, its entry.
func offered(t *testing.T, a *app) store.Thread {
	t.Helper()
	ctx := context.Background()
	o := watchOffers{a}
	id, err := o.Start(ctx, watch.Offer{Entity: "sensor.spuelmaschine", Name: "Spülmaschine", Blueprint: "device_run"},
		"- [ ] Auf den nächsten Lauf von Spülmaschine warten\n- [ ] Den Wächter anbieten", time.Now().Add(watch.OfferKeep))
	if err != nil {
		t.Fatal(err)
	}
	if err := o.SetNumbers(ctx, id, "Gerätelauf: … 120 min, Spitze 1883 W", "nächsten Lauf"); err != nil {
		t.Fatal(err)
	}
	if err := o.Post(ctx, id, "Spülmaschine ist gelaufen. Wie versprochen: Soll ich es jetzt überwachen?", "details", time.Now().Add(watch.OfferKeep)); err != nil {
		t.Fatal(err)
	}
	th, err := a.store.GetThread(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return th
}

func pendingOf(t *testing.T, a *app, thread string) int {
	t.Helper()
	all, err := a.store.ListMessages(context.Background(), store.MessageFilter{States: []string{store.MessagePending}, Receiver: store.ReceiverHouse})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, m := range all {
		for _, e := range m.Entries {
			if e.ThreadID == thread && m.Kind == store.KindSimple {
				n++
			}
		}
	}
	return n
}

// CW-36 A declined offer
func TestCW36_ADeclinedOffer(t *testing.T) {
	a, log := offerApp(t)
	ctx := context.Background()
	// Given the offer for the dishwasher came after its run
	th := offered(t, a)
	if th.Place != store.PlaceHouse || th.Fields[watch.FieldDevice] != "sensor.spuelmaschine" || th.Fields[watch.FieldNumbers] == "" ||
		!near(th.ExpiresAt, time.Now().Add(watch.OfferKeep)) {
		t.Fatalf("thread = %+v", th)
	}
	if pendingOf(t, a, th.ID) != 1 {
		t.Fatal("no entry leading to the offer's thread")
	}
	// When the user says "Nein, lass mal" (the answer to the entry: the
	// turn takes the offer up)
	inv := tools.Invocation{SessionKey: "voice:k1", Channel: config.ChannelVoice, Area: "kueche", DeviceID: "sat-kueche"}
	if _, err := a.store.UpsertSession(ctx, store.Session{Key: inv.SessionKey, Channel: string(inv.Channel), AreaID: "kueche"}, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := a.threads.Take(ctx, inv, th.ID); err != nil {
		t.Fatal(err)
	}
	var decline tools.InternalTool
	for _, tl := range a.watchWaiter.Tools() {
		if tl.Name == watch.ToolDecline {
			decline = tl
		}
	}
	out, err := decline.Run(ctx, tools.Invocation{SessionKey: inv.SessionKey, Channel: inv.Channel, Area: inv.Area, DeviceID: inv.DeviceID, Args: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	// Then the thread ends and the dishwasher is kept as declined
	got, _ := a.store.GetThread(ctx, th.ID)
	if got.State != store.ThreadCancelled || pendingOf(t, a, th.ID) != 0 {
		t.Errorf("thread %s, entries %d", got.State, pendingOf(t, a, th.ID))
	}
	declines, err := a.store.WatchDeclines(ctx)
	if err != nil || len(declines) != 1 || declines[0].EntityID != "sensor.spuelmaschine" || declines[0].Blueprint != "device_run" {
		t.Errorf("declines = %+v %v", declines, err)
	}
	// And it is not offered again (the candidates leave declined ones out,
	// CW-33); the answer says so.
	if !strings.Contains(out, "bietet Spülmaschine für diesen Wächter nicht wieder an") || !strings.Contains(log.String(), "watch offer declined") {
		t.Errorf("answer %q, log:\n%s", out, log.String())
	}
	// And "Pass doch auf die Spülmaschine auf" makes the watch all the same
	// and forgets the decline: internal/tasks, TestCW36_MakingTheWatchByVoiceForgetsTheDecline.
}

// Only watch_wait starts an offer: thread_start is refused, naming the
// skill whose tools answer (decision 57).
func TestOfferIsNotStartedByThreadStart(t *testing.T) {
	a, _ := offerApp(t)
	_, err := a.threads.Start(context.Background(), tools.Invocation{SessionKey: "voice:k1", Channel: config.ChannelVoice}, watch.OfferSkill, "Spülmaschine")
	if err == nil || !strings.Contains(err.Error(), "legt Mira selbst an") {
		t.Fatalf("err = %v", err)
	}
	if open, _ := (watchOffers{a}).Open(context.Background()); len(open) != 0 {
		t.Errorf("offers: %+v", open)
	}
}

// The yes ends the offer and drafts the watch in the turn as a thread of
// the skill aufgabe; an offer still waiting for its run says so.
func TestOfferAcceptedDraftsTheWatch(t *testing.T) {
	a, _ := offerApp(t)
	ctx := context.Background()
	th := offered(t, a)
	inv := tools.Invocation{SessionKey: "voice:k1", Channel: config.ChannelVoice, Area: "kueche", Args: json.RawMessage(`{"device":"sensor.spuelmaschine"}`)}
	o, ok, err := watchOffers{a}.Current(ctx, inv, "sensor.spuelmaschine")
	if err != nil || !ok || o.Thread != th.ID || o.Numbers == "" {
		t.Fatalf("current: %+v %v %v", o, ok, err)
	}
	// Ended as done; its entry leaves.
	watchOffers{a}.End(ctx, th.ID, false)
	got, _ := a.store.GetThread(ctx, th.ID)
	if got.State != store.ThreadDone || pendingOf(t, a, th.ID) != 0 {
		t.Errorf("thread %s, entries %d", got.State, pendingOf(t, a, th.ID))
	}
	// Waiting still: nothing to accept.
	id, err := watchOffers{a}.Start(ctx, watch.Offer{Entity: "sensor.waschmaschine", Name: "Waschmaschine", Blueprint: "device_run"},
		"- [ ] warten", time.Now().Add(watch.OfferKeep))
	if err != nil {
		t.Fatal(err)
	}
	for _, tl := range a.watchWaiter.Tools() {
		if tl.Name != watch.ToolAccept {
			continue
		}
		out, err := tl.Run(ctx, tools.Invocation{SessionKey: "voice:k2", Channel: config.ChannelVoice, Args: json.RawMessage(`{"device":"sensor.waschmaschine"}`)})
		if err != nil || !strings.Contains(out, "Waschmaschine ist noch nicht gelaufen") {
			t.Errorf("accept while waiting: %q %v", out, err)
		}
	}
	if got, _ := a.store.GetThread(ctx, id); !store.ThreadOpen(got.State) {
		t.Errorf("the waiting offer ended: %s", got.State)
	}
}
