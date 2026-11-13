package main

import (
	"context"
	"errors"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/threads"
	"github.com/miraassist/mira/internal/tools"
	"github.com/miraassist/mira/internal/watch"
)

// watchOffers keeps the offers of a watch that wait for a device's next run
// as threads of the house of the target ueberwachung-anbieten, and their
// entries as entries of the house's simple message
// (docs/spec/device-watch.md, step 8).
type watchOffers struct{ a *app }

// offerOf is an offer's thread as the waiter knows it.
func offerOf(th store.Thread) watch.Offer {
	return watch.Offer{Thread: th.ID, Entity: th.Fields[watch.FieldDevice], Name: th.Request,
		Blueprint: th.Fields[watch.FieldBlueprint], Numbers: th.Fields[watch.FieldNumbers],
		Started: th.CreatedAt, Expires: th.ExpiresAt}
}

func (w watchOffers) Open(ctx context.Context) ([]watch.Offer, error) {
	open, err := w.a.store.OpenThreads(ctx, "")
	if err != nil {
		return nil, err
	}
	var out []watch.Offer
	for _, th := range open {
		if th.Target == watch.OfferSkill {
			out = append(out, offerOf(th))
		}
	}
	return out, nil
}

func (w watchOffers) Start(ctx context.Context, o watch.Offer, plan string, deadline time.Time) (string, error) {
	if w.a.threads == nil {
		return "", errors.New("no threads")
	}
	th, err := w.a.threads.StartAt(ctx, threads.Begin{Target: watch.OfferSkill, Request: o.Name, Plan: plan, Deadline: deadline,
		Fields: map[string]string{watch.FieldDevice: o.Entity, watch.FieldBlueprint: o.Blueprint}})
	return th.ID, err
}

func (w watchOffers) SetNumbers(ctx context.Context, thread, numbers, waitStep string) error {
	if err := w.a.threads.SetFields(ctx, thread, map[string]string{watch.FieldNumbers: numbers}); err != nil {
		return err
	}
	if err := w.a.threads.TickPlan(ctx, thread, waitStep); err != nil {
		w.a.log.Warn("could not tick the offer's step of waiting; the plan shows it open", "thread", thread, "error", err)
	}
	return nil
}

// Post leaves the offer's entry in the house's simple message, leading to
// its thread (as a found device's, docs/spec/messages.md).
func (w watchOffers) Post(ctx context.Context, thread, spoken, details string, expires time.Time) error {
	_, _, err := w.a.store.PutMessage(ctx, store.Message{
		ID: ulid.Make().String(), Kind: store.KindSimple, Source: store.SourceTask,
		DedupeKey: "watch-offer:" + thread, Importance: 1, Receiver: store.ReceiverHouse,
		Spoken: spoken, Details: details, ThreadID: thread, ExpiresAt: expires,
	})
	return err
}

func (w watchOffers) End(ctx context.Context, thread string, declined bool) {
	if declined {
		if err := w.a.threads.CancelThread(ctx, thread); err != nil && !errors.Is(err, store.ErrNotFound) {
			w.a.log.Warn("could not end a declined watch offer; it ends at its expiry", "thread", thread, "error", err)
		}
		return
	}
	w.a.threads.Finish(ctx, thread)
}

// Current is the offer the turn belongs to, else the open offer of the
// device, taken up for the turn.
func (w watchOffers) Current(ctx context.Context, inv tools.Invocation, device string) (watch.Offer, bool, error) {
	if th, ok := w.a.threads.TurnThreadOf(ctx, inv, watch.OfferSkill); ok && th.Fields[watch.FieldDevice] != "" &&
		(device == "" || th.Fields[watch.FieldDevice] == device) {
		return offerOf(th), true, nil
	}
	if device == "" {
		return watch.Offer{}, false, nil
	}
	id, ok, err := w.a.threads.TakeItem(ctx, inv, watch.OfferSkill, watch.FieldDevice, device)
	if err != nil || !ok {
		return watch.Offer{}, false, err
	}
	th, err := w.a.store.GetThread(ctx, id)
	if err != nil {
		return watch.Offer{}, false, err
	}
	return offerOf(th), true, nil
}

// Draft drafts the accepted watch as the skill aufgabe does: a thread of
// aufgabe in the turn, its fields from the offer, whose drafting proposes
// the watch.
func (w watchOffers) Draft(ctx context.Context, inv tools.Invocation, o watch.Offer) (string, error) {
	t := i18n.Pick(offerDraftTexts, w.a.lang)
	if _, err := w.a.threads.Start(ctx, inv, watch.Skill, t.request+o.Name); err != nil {
		return "", err
	}
	return w.a.threads.Fill(ctx, inv, map[string]string{
		"watch":    o.Entity,
		"when":     t.when,
		"measure":  t.measure + o.Blueprint + ": " + o.Numbers,
		"reaction": "message",
	})
}

// offerDraftTexts are the fields of the watch an accepted offer drafts.
var offerDraftTexts = map[i18n.Lang]struct{ request, when, measure string }{
	i18n.DE: {request: "Überwachung, wie nach dem Lauf angeboten: ", when: "wenn es von seinem Geräteprofil abweicht",
		measure: "Zahlen aus dem Geräteprofil, für den Blueprint "},
	i18n.EN: {request: "Watch, as offered after its run: ", when: "when it leaves its device profile",
		measure: "numbers from the device profile, for the blueprint "},
}
