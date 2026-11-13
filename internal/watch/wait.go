package watch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/miraassist/mira/internal/excerpt"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

// Waiting for a device's next run (docs/spec/device-watch.md, decisions 14,
// 15, 19): a candidate whose device profile is not enough yet gets a thread
// of the house, the offer's, which waits for its next run. The service
// follows the power of every waiting offer with one trigger subscription,
// filtered at the source; after a run the device profile is computed again,
// and with enough the offer comes as an entry of a message, its thread
// waiting for the yes.

// OfferSkill is the skill whose target is the offer's thread.
const OfferSkill = "ueberwachung-anbieten"

// The offer thread's fields: the device's power sensor, the blueprint and,
// after the run, the numbers of its device profile.
const (
	FieldDevice    = "device"
	FieldBlueprint = "blueprint"
	FieldNumbers   = "numbers"
)

// OfferKeep is how long an offer waits for the device's run (decision 19).
const OfferKeep = 30 * 24 * time.Hour

// Tool names of the offer.
const (
	ToolWait    = "watch_wait"
	ToolAccept  = "watch_accept"
	ToolDecline = "watch_decline"
)

// Offer is one offer's thread.
type Offer struct {
	Thread, Entity, Name, Blueprint, Numbers string
	Started, Expires                         time.Time
}

// Offers keeps the offers' threads (Mira's threads, wired in the service).
type Offers interface {
	// Open are the open offer threads.
	Open(ctx context.Context) ([]Offer, error)
	// Start starts an offer's thread at the house with its plan, expiring
	// at deadline unless a turn takes it up.
	Start(ctx context.Context, o Offer, plan string, deadline time.Time) (string, error)
	// SetNumbers writes the numbers after the run and ticks the plan's
	// step of waiting.
	SetNumbers(ctx context.Context, thread, numbers, waitStep string) error
	// Post puts the offer's entry into the house's message, leading to its
	// thread.
	Post(ctx context.Context, thread, spoken, details string, expires time.Time) error
	// End ends an offer's thread: done, or cancelled when declined.
	End(ctx context.Context, thread string, declined bool)
	// Current is the offer the turn belongs to, else the one of the device
	// (taken up for the turn); false without one.
	Current(ctx context.Context, inv tools.Invocation, device string) (Offer, bool, error)
	// Draft drafts the watch of an accepted offer as the skill aufgabe
	// does, in the turn: its proposal, or that it comes.
	Draft(ctx context.Context, inv tools.Invocation, o Offer) (string, error)
}

// WaiterHA is what waiting needs of Home Assistant.
type WaiterHA interface {
	SubscribeRaw(ctx context.Context, cmd map[string]any, handler func(json.RawMessage)) (func(), error)
	GetStates(ctx context.Context, f haws.StateFilter) ([]haws.State, error)
}

type WaiterOptions struct {
	HA       WaiterHA
	Profiler *Profiler
	Offers   Offers
	// Decline remembers a declined offer (table watch_declined).
	Decline func(ctx context.Context, entity, blueprint string) error
	// Watched are the entities Mira's watches watch already.
	Watched func(ctx context.Context) (map[string]bool, error)
	Lang    i18n.Lang
	Logger  *slog.Logger
	Clock   func() time.Time
	// RunEnd replaces how long the power stays below OnAbove before a run
	// counts as ended (tests on the test HA); zero for the kind's.
	RunEnd time.Duration
}

// Waiter follows the devices whose offer waits for their next run.
type Waiter struct {
	o   WaiterOptions
	log *slog.Logger
	// ends carries the entities whose run ended, from the subscriptions'
	// handlers (which never block) to Run.
	ends chan string

	mu   sync.Mutex
	subs map[string]func() // by entity
}

func NewWaiter(o WaiterOptions) *Waiter {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Clock == nil {
		o.Clock = time.Now
	}
	return &Waiter{o: o, log: o.Logger.With("component", "watch"), ends: make(chan string, 16), subs: map[string]func(){}}
}

type waitTexts struct {
	plan, waitStep                              string
	waiting, already, enough, none, entry, more string
	accepted, declined, notYet, noOffer         string
	descWait, descAccept, descDecline, entity   string
	device                                      string
}

var waitCatalog = map[i18n.Lang]waitTexts{
	i18n.DE: {
		plan:     "- [ ] Auf den nächsten Lauf von %s warten\n- [ ] Den Wächter anbieten (Ja: watch_accept, Nein: watch_decline)",
		waitStep: "nächsten Lauf",
		waiting:  "Gemerkt: Mira wartet auf den nächsten Lauf von %s (höchstens 30 Tage) und bietet den Wächter danach als Nachricht an. Sag dem Nutzer: „%s nehm ich in die Überwachung, wenn es das nächste Mal gelaufen ist.“",
		already:  "Mira wartet schon seit %s auf den nächsten Lauf von %s; danach kommt das Angebot. Sag das dem Nutzer.",
		enough:   "Das Geräteprofil von %s reicht schon für einen Wächter: richte ihn gleich als Aufgabe ein, statt zu warten.",
		none:     "%s ist kein Gerät, das ein Blueprint nach seinen Läufen überwacht (%s); da gibt es nichts abzuwarten.",
		entry:    "%s ist gelaufen. Wie versprochen: Soll ich es jetzt überwachen?",
		more:     "Angebot eines Wächters für %s, nach seinem Lauf: %[3]s\nJa: watch_accept mit device %[2]s. Nein: watch_decline mit device %[2]s.",
		accepted: "Angenommen.",
		declined: "Abgelehnt: Mira bietet %s für diesen Wächter nicht wieder an. Sagt der Nutzer später „Pass doch auf … auf“, richtet sie ihn trotzdem ein.",
		notYet:   "%s ist noch nicht gelaufen; das Angebot kommt danach.",
		noOffer:  "Kein Angebot eines Wächters, das hier wartet",

		descWait:    "Wenn das Geräteprofil eines Geräts noch nicht reicht (es fehlen Läufe): merkt sich das Gerät, wartet auf seinen nächsten Lauf und bietet den Wächter danach als Nachricht an (höchstens 30 Tage). Für „Pass auf die Spülmaschine auf“, wenn device_profile sagt, dass Läufe fehlen.",
		descAccept:  "Das Angebot eines Wächters nach dem Lauf eines Geräts annehmen (der Nutzer sagt ja): Mira entwirft den Wächter und schlägt ihn vor.",
		descDecline: "Das Angebot eines Wächters ablehnen (der Nutzer sagt nein): Mira merkt sich das Gerät als abgelehnt und bietet es nicht wieder an.",
		entity:      "Die id des Leistungssensors (z. B. sensor.spuelmaschine_leistung) oder sein Name",
		device:      "Die id des Leistungssensors des Angebots; leer für das Angebot, um das es gerade geht",
	},
	i18n.EN: {
		plan:     "- [ ] Wait for the next run of %s\n- [ ] Offer the watch (yes: watch_accept, no: watch_decline)",
		waitStep: "next run",
		waiting:  "Noted: Mira waits for the next run of %s (at most 30 days) and offers the watch as a message after it. Tell the user: \"I'll take %s into the watching after its next run.\"",
		already:  "Mira has been waiting since %s for the next run of %s; the offer comes after it. Tell the user so.",
		enough:   "The device profile of %s is enough for a watch already: make it as a task now instead of waiting.",
		none:     "%s is no device a blueprint watches by its runs (%s); there is nothing to wait for.",
		entry:    "%s has run. As promised: shall I watch it now?",
		more:     "Offer of a watch for %s, after its run: %[3]s\nYes: watch_accept with device %[2]s. No: watch_decline with device %[2]s.",
		accepted: "Accepted.",
		declined: "Declined: Mira does not offer %s for this watch again. If the user later says \"do watch …\", she makes it all the same.",
		notYet:   "%s has not run yet; the offer comes after it.",
		noOffer:  "No offer of a watch waiting here",

		descWait:    "When a device's profile is not enough yet (runs are missing): notes the device, waits for its next run and offers the watch as a message after it (at most 30 days). For \"watch the dishwasher\" when device_profile says runs are missing.",
		descAccept:  "Accept the offer of a watch after a device's run (the user says yes): Mira drafts the watch and proposes it.",
		descDecline: "Decline the offer of a watch (the user says no): Mira keeps the device as declined and does not offer it again.",
		entity:      "The power sensor's id (e.g. sensor.dishwasher_power) or its name",
		device:      "The power sensor's id of the offer; empty for the offer at hand",
	},
}

func (w *Waiter) texts() waitTexts { return i18n.Pick(waitCatalog, w.o.Lang) }

// runEnd is how long the power stays below OnAbove before a run of a kind
// counts as ended: a device run's programme pauses (runJoin), a minute for
// the others.
func (w *Waiter) runEnd(kind Kind) time.Duration {
	if w.o.RunEnd > 0 {
		return w.o.RunEnd
	}
	if kind == KindRun {
		return runJoin
	}
	return time.Minute
}

// Wait is the promise (tool watch_wait): a candidate whose device profile
// is not enough gets its offer's thread, and its power is followed. A
// profile that is enough or no candidate is said instead; an offer that
// waits already is said.
func (w *Waiter) Wait(ctx context.Context, s Sensor, pr Profile) (string, error) {
	t := w.texts()
	switch {
	case pr.Kind == KindNone:
		return fmt.Sprintf(t.none, s.Name, Describe(pr, StatsDays, w.o.Lang, w.o.Profiler.opts.Location)), nil
	case pr.Enough:
		return fmt.Sprintf(t.enough, s.Name), nil
	}
	open, err := w.o.Offers.Open(ctx)
	if err != nil {
		return "", fmt.Errorf("the waiting offers: %w", err)
	}
	for _, o := range open {
		if o.Entity == s.ID {
			return fmt.Sprintf(t.already, o.Started.In(w.o.Profiler.opts.Location).Format("02.01."), s.Name), nil
		}
	}
	o := Offer{Entity: s.ID, Name: s.Name, Blueprint: pr.Kind.Blueprint()}
	o.Thread, err = w.o.Offers.Start(ctx, o, fmt.Sprintf(t.plan, s.Name), w.o.Clock().Add(OfferKeep))
	if err != nil {
		return "", fmt.Errorf("the offer's thread for %s: %w", s.ID, err)
	}
	w.log.Info("a watch offer waits for the device's next run", "entity", s.ID, "blueprint", o.Blueprint, "thread", o.Thread)
	if err := w.subscribe(ctx, o); err != nil {
		w.log.Error("could not follow the power of a waiting watch offer; it waits, and is followed again at the next start",
			"entity", s.ID, "thread", o.Thread, "error", err)
	}
	return fmt.Sprintf(t.waiting, s.Name, s.Name), nil
}

// subscribe follows the end of the device's runs: a numeric state trigger
// on its power, below OnAbove for the kind's runEnd (decision 17: the
// WebSocket client renews it after a reconnect).
func (w *Waiter) subscribe(ctx context.Context, o Offer) error {
	w.mu.Lock()
	_, ok := w.subs[o.Entity]
	w.mu.Unlock()
	if ok {
		return nil
	}
	end := w.runEnd(KindOfBlueprint(o.Blueprint))
	cmd := map[string]any{"type": "subscribe_trigger", "trigger": map[string]any{
		"trigger": "numeric_state", "entity_id": o.Entity, "below": OnAbove,
		"for": map[string]any{"seconds": int(end / time.Second)},
	}}
	entity := o.Entity
	cancel, err := w.o.HA.SubscribeRaw(ctx, cmd, func(json.RawMessage) {
		// On the connection's read loop: only pass it on.
		select {
		case w.ends <- entity:
		default:
			w.log.Warn("a run's end came while many others wait to be read; it is dropped, the next run counts", "entity", entity)
		}
	})
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.subs[o.Entity] = cancel
	w.mu.Unlock()
	w.log.Info("following the power of a waiting watch offer", "entity", o.Entity, "run_end", end)
	return nil
}

// unsubscribe stops following an entity.
func (w *Waiter) unsubscribe(entity string) {
	w.mu.Lock()
	cancel, ok := w.subs[entity]
	delete(w.subs, entity)
	w.mu.Unlock()
	if ok {
		cancel()
	}
}

// Resume follows again every offer that waits for a run, at the service's
// start: an entity Home Assistant no longer has ends its offer; one that
// cannot be followed stays waiting (tried again at the next start).
func (w *Waiter) Resume(ctx context.Context) {
	open, err := w.o.Offers.Open(ctx)
	if err != nil {
		w.log.Error("could not read the waiting watch offers; none is followed until the next start", "error", err)
		return
	}
	for _, o := range open {
		if o.Entity == "" {
			w.log.Warn("a watch offer without a device; it ends", "thread", o.Thread)
			w.o.Offers.End(ctx, o.Thread, false)
			continue
		}
		if o.Numbers != "" {
			continue // offered, waiting for the yes
		}
		states, err := w.o.HA.GetStates(ctx, haws.StateFilter{EntityIDs: []string{o.Entity}})
		if err != nil {
			w.log.Error("could not read a waiting watch offer's device; it is followed again at the next start", "entity", o.Entity,
				"thread", o.Thread, "error", err)
			continue
		}
		if len(states) == 0 {
			w.log.Warn("a waiting watch offer's device is gone from home assistant; its offer ends", "entity", o.Entity, "thread", o.Thread)
			w.o.Offers.End(ctx, o.Thread, false)
			continue
		}
		if err := w.subscribe(ctx, o); err != nil {
			w.log.Error("could not follow the power of a waiting watch offer; it waits, and is followed again at the next start",
				"entity", o.Entity, "thread", o.Thread, "error", err)
		}
	}
}

// Run handles the ends of runs until ctx ends.
func (w *Waiter) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case entity := <-w.ends:
			w.ran(ctx, entity)
		}
	}
}

// ran is a run of entity that ended: its device profile again; with enough,
// the offer's numbers, its entry, and no more following.
func (w *Waiter) ran(ctx context.Context, entity string) {
	open, err := w.o.Offers.Open(ctx)
	if err != nil {
		w.log.Error("a run ended, but the waiting watch offers cannot be read; the next run counts", "entity", entity, "error", err)
		return
	}
	var o Offer
	for _, x := range open {
		if x.Entity == entity && x.Numbers == "" {
			o = x
		}
	}
	if o.Thread == "" {
		w.log.Info("a run ended for no waiting watch offer; not followed any more", "entity", entity)
		w.unsubscribe(entity)
		return
	}
	pr, err := w.o.Profiler.ProfileAs(ctx, entity, KindOfBlueprint(o.Blueprint))
	if err != nil {
		w.log.Warn("a run ended, but the device profile cannot be computed; the offer waits for the next run", "entity", entity,
			"thread", o.Thread, "error", err)
		return
	}
	if !pr.Enough {
		w.log.Info("a run ended, still not enough for the watch; the offer waits for the next run", "entity", entity,
			"thread", o.Thread, "runs", len(pr.Runs))
		return
	}
	if w.o.Watched != nil {
		watched, err := w.o.Watched(ctx)
		if err != nil {
			w.log.Warn("could not read Mira's watches; the offer comes all the same", "entity", entity, "error", err)
		} else if watched[entity] {
			w.log.Info("a waiting watch offer's device is watched meanwhile; its offer ends", "entity", entity, "thread", o.Thread)
			w.unsubscribe(entity)
			w.o.Offers.End(ctx, o.Thread, false)
			return
		}
	}
	t := w.texts()
	numbers := Describe(pr, StatsDays, w.o.Lang, w.o.Profiler.opts.Location)
	if err := w.o.Offers.SetNumbers(ctx, o.Thread, numbers, t.waitStep); err != nil {
		w.log.Error("a run ended with enough, but the offer's thread cannot be written; the next run tries again", "entity", entity,
			"thread", o.Thread, "error", err)
		return
	}
	w.unsubscribe(entity)
	details := fmt.Sprintf(t.more, o.Name, entity, numbers)
	if err := w.o.Offers.Post(ctx, o.Thread, fmt.Sprintf(t.entry, o.Name), details, o.Expires); err != nil {
		w.log.Error("could not leave the entry of a watch offer; its thread waits without it", "entity", entity, "thread", o.Thread,
			"error", err)
		return
	}
	w.log.Info("watch offered after the device's run", "entity", entity, "thread", o.Thread, "blueprint", o.Blueprint,
		"runs", len(pr.Runs))
}

// Ended stops following an offer's device when its thread ended; one that
// expired before the device ran says so with the days waited.
func (w *Waiter) Ended(o Offer) {
	w.mu.Lock()
	_, following := w.subs[o.Entity]
	w.mu.Unlock()
	w.unsubscribe(o.Entity)
	days := int(w.o.Clock().Sub(o.Started).Hours() / 24)
	switch {
	case o.Numbers == "" && !w.o.Clock().Before(o.Expires):
		w.log.Info("a watch offer expired before the device ran; not followed any more", "entity", o.Entity, "thread", o.Thread,
			"days_waited", days)
	case following:
		w.log.Info("a watch offer ended; its device is not followed any more", "entity", o.Entity, "thread", o.Thread, "days_waited", days)
	}
}

// Tools are the offer's answers, with the skill of the offer: yes and no.
func (w *Waiter) Tools() []tools.InternalTool {
	t := w.texts()
	schema := fmt.Sprintf(`{"type":"object","properties":{"device":{"type":"string","description":%q}}}`, t.device)
	current := func(ctx context.Context, inv tools.Invocation) (Offer, error) {
		var a struct {
			Device string `json:"device"`
		}
		if len(inv.Args) > 0 {
			if err := json.Unmarshal(inv.Args, &a); err != nil {
				return Offer{}, fmt.Errorf("%q not understood: %w", excerpt.Bytes(inv.Args), err)
			}
		}
		o, ok, err := w.o.Offers.Current(ctx, inv, strings.TrimSpace(a.Device))
		if err != nil {
			return Offer{}, err
		}
		if !ok {
			return Offer{}, errors.New(t.noOffer)
		}
		return o, nil
	}
	return []tools.InternalTool{
		{
			Name: ToolAccept, Description: t.descAccept, Skill: OfferSkill, Schema: schema,
			Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
				o, err := current(ctx, inv)
				if err != nil {
					return "", err
				}
				if o.Numbers == "" {
					return fmt.Sprintf(t.notYet, o.Name), nil
				}
				w.log.Info("watch offer accepted; drafting the watch", "entity", o.Entity, "thread", o.Thread, "blueprint", o.Blueprint)
				w.o.Offers.End(ctx, o.Thread, false)
				return w.o.Offers.Draft(ctx, inv, o)
			},
		},
		{
			Name: ToolDecline, Description: t.descDecline, Skill: OfferSkill, Schema: schema,
			Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
				o, err := current(ctx, inv)
				if err != nil {
					return "", err
				}
				if err := w.o.Decline(ctx, o.Entity, o.Blueprint); err != nil {
					return "", fmt.Errorf("the decline of %s (%s): %w", o.Entity, o.Blueprint, err)
				}
				w.log.Info("watch offer declined", "entity", o.Entity, "thread", o.Thread, "blueprint", o.Blueprint)
				w.unsubscribe(o.Entity)
				w.o.Offers.End(ctx, o.Thread, true)
				return fmt.Sprintf(t.declined, o.Name), nil
			},
		},
	}
}

// WaitTool is watch_wait, with the skill that makes tasks: find looks up the
// power sensor Mira may see (as for device_profile).
func (w *Waiter) WaitTool(find func(ctx context.Context, id string) (Sensor, error)) tools.InternalTool {
	t := w.texts()
	return tools.InternalTool{
		Name: ToolWait, Description: t.descWait, Skill: Skill,
		Schema: fmt.Sprintf(`{"type":"object","properties":{"entity":{"type":"string","description":%q}},"required":["entity"]}`, t.entity),
		Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
			var a struct {
				Entity string `json:"entity"`
			}
			if err := json.Unmarshal(inv.Args, &a); err != nil {
				return "", fmt.Errorf("%q not understood: %w", excerpt.Bytes(inv.Args), err)
			}
			s, err := find(ctx, strings.TrimSpace(a.Entity))
			if err != nil {
				return "", err
			}
			if !s.Power() {
				return "", fmt.Errorf(i18n.Pick(toolCatalog, w.o.Lang).notPower, s.Name, s.ID, s.DeviceClass, s.Unit)
			}
			pr, err := w.o.Profiler.Profile(ctx, s.ID)
			if err != nil {
				return "", err
			}
			return w.Wait(ctx, s, pr)
		},
	}
}
