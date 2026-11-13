package watch

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

// waitHA plays Home Assistant's trigger subscriptions and states.
type waitHA struct {
	mu        sync.Mutex
	cmds      []map[string]any
	handlers  map[string]func(json.RawMessage)
	cancelled []string
	subErr    error
	gone      map[string]bool
}

func (h *waitHA) SubscribeRaw(_ context.Context, cmd map[string]any, handler func(json.RawMessage)) (func(), error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cmds = append(h.cmds, cmd)
	if h.subErr != nil {
		return nil, h.subErr
	}
	entity := cmd["trigger"].(map[string]any)["entity_id"].(string)
	if h.handlers == nil {
		h.handlers = map[string]func(json.RawMessage){}
	}
	h.handlers[entity] = handler
	return func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.cancelled = append(h.cancelled, entity)
		delete(h.handlers, entity)
	}, nil
}

func (h *waitHA) GetStates(_ context.Context, f haws.StateFilter) ([]haws.State, error) {
	var out []haws.State
	for _, id := range f.EntityIDs {
		if !h.gone[id] {
			out = append(out, haws.State{EntityID: id, State: "0"})
		}
	}
	return out, nil
}

// fire is Home Assistant's trigger event for an entity's run that ended.
func (h *waitHA) fire(entity string) bool {
	h.mu.Lock()
	handler := h.handlers[entity]
	h.mu.Unlock()
	if handler == nil {
		return false
	}
	handler(json.RawMessage(`{"variables":{"trigger":{"platform":"numeric_state"}}}`))
	return true
}

// memOffers keeps the offers' threads in memory.
type memOffers struct {
	mu      sync.Mutex
	offers  map[string]*Offer
	ended   map[string]bool // thread -> declined
	posts   []string
	plans   []string
	drafted []Offer
	n       int
}

func (m *memOffers) Open(context.Context) ([]Offer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Offer
	for id, o := range m.offers {
		if _, done := m.ended[id]; !done {
			out = append(out, *o)
		}
	}
	return out, nil
}

func (m *memOffers) Start(_ context.Context, o Offer, plan string, deadline time.Time) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.n++
	o.Thread = "t" + string(rune('0'+m.n))
	o.Started, o.Expires = testNow, deadline
	if m.offers == nil {
		m.offers, m.ended = map[string]*Offer{}, map[string]bool{}
	}
	m.offers[o.Thread] = &o
	m.plans = append(m.plans, plan)
	return o.Thread, nil
}

func (m *memOffers) SetNumbers(_ context.Context, thread, numbers, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.offers[thread].Numbers = numbers
	return nil
}

func (m *memOffers) Post(_ context.Context, thread, spoken, details string, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.posts = append(m.posts, thread+": "+spoken+" | "+details)
	return nil
}

func (m *memOffers) End(_ context.Context, thread string, declined bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ended[thread] = declined
}

func (m *memOffers) Current(context.Context, tools.Invocation, string) (Offer, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, o := range m.offers {
		if _, done := m.ended[id]; !done {
			return *o, true, nil
		}
	}
	return Offer{}, false, nil
}

func (m *memOffers) Draft(_ context.Context, _ tools.Invocation, o Offer) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.drafted = append(m.drafted, o)
	return "Mein Vorschlag: …", nil
}

func newWaiter(ha *waitHA, rec *fakeRecorder, offers *memOffers, log *bytes.Buffer) *Waiter {
	return NewWaiter(WaiterOptions{HA: ha, Profiler: newProfiler(rec, log), Offers: offers, Lang: i18n.DE,
		Logger: slog.New(slog.NewTextHandler(log, nil)), Clock: func() time.Time { return testNow },
		Decline: func(context.Context, string, string) error { return nil }})
}

// handled runs the waiter until the run's end that came is handled: it
// handles one end and stops.
func handled(t *testing.T, w *Waiter) {
	t.Helper()
	select {
	case entity := <-w.ends:
		w.ran(context.Background(), entity)
	case <-time.After(5 * time.Second):
		t.Fatal("no run's end came")
	}
}

// CW-30 A candidate waits for its next run
func TestCW30_ACandidateWaitsForItsNextRun(t *testing.T) {
	// Given the dishwasher is a candidate with too few runs in the last days
	rec := &fakeRecorder{stats: map[string][]haws.StatRow{"sensor.spuelmaschine": runStats()},
		states: map[string][]haws.Change{"sensor.spuelmaschine": states(0, func(time.Time) bool { return false })}}
	ha, offers := &waitHA{}, &memOffers{}
	var log bytes.Buffer
	w := newWaiter(ha, rec, offers, &log)
	// When the user says "Pass auf die Spülmaschine auf"
	tool := w.WaitTool(func(context.Context, string) (Sensor, error) {
		return Sensor{ID: "sensor.spuelmaschine", Name: "Spülmaschine", DeviceClass: "power", Unit: "W"}, nil
	})
	if tool.Skill != Skill {
		t.Errorf("watch_wait comes with %q", tool.Skill)
	}
	out, err := tool.Run(context.Background(), tools.Invocation{Args: json.RawMessage(`{"entity":"Spülmaschine"}`)})
	if err != nil {
		t.Fatal(err)
	}
	// Then Mira says she takes it into the watching after its next run
	if !strings.Contains(out, "Spülmaschine nehm ich in die Überwachung, wenn es das nächste Mal gelaufen ist") {
		t.Errorf("answer: %s", out)
	}
	// Its offer's thread waits, 30 days, and its power is followed at the
	// source: below 5 W for 30 minutes ends a run.
	o, ok, _ := offers.Current(context.Background(), tools.Invocation{}, "")
	if !ok || o.Entity != "sensor.spuelmaschine" || o.Blueprint != "device_run" || o.Numbers != "" || o.Expires != testNow.Add(30*24*time.Hour) {
		t.Fatalf("offer: %+v", o)
	}
	if len(ha.cmds) != 1 {
		t.Fatalf("subscriptions: %v", ha.cmds)
	}
	trigger, _ := json.Marshal(ha.cmds[0])
	if want := `{"trigger":{"below":5,"entity_id":"sensor.spuelmaschine","for":{"seconds":1800},"trigger":"numeric_state"},"type":"subscribe_trigger"}`; string(trigger) != want {
		t.Errorf("subscription: %s", trigger)
	}
	// Asked again, the offer waits already: no second thread.
	if out, _ := tool.Run(context.Background(), tools.Invocation{Args: json.RawMessage(`{"entity":"Spülmaschine"}`)}); !strings.Contains(out, "wartet schon") || len(offers.offers) != 1 {
		t.Errorf("again: %s, offers %d", out, len(offers.offers))
	}
	// And when its next run ended and the device profile has enough, the offer comes as an entry with its thread
	start := testNow.Add(-3 * time.Hour)
	rec.states["sensor.spuelmaschine"] = append([]haws.Change{{State: "0.0", At: testNow.AddDate(0, 0, -StatesDays)}},
		haws.Change{State: "80", At: start}, haws.Change{State: "1900", At: start.Add(20 * time.Minute)},
		haws.Change{State: "0", At: start.Add(120 * time.Minute)})
	if !ha.fire("sensor.spuelmaschine") {
		t.Fatal("not subscribed")
	}
	handled(t, w)
	if len(offers.posts) != 1 {
		t.Fatalf("posts: %v\n%s", offers.posts, log.String())
	}
	o = *offers.offers[o.Thread]
	if !strings.Contains(o.Numbers, "120 min, Spitze 1900 W") {
		t.Errorf("numbers: %s", o.Numbers)
	}
	if !strings.Contains(offers.posts[0], o.Thread+": Spülmaschine ist gelaufen. Wie versprochen: Soll ich es jetzt überwachen?") ||
		!strings.Contains(offers.posts[0], "watch_accept") {
		t.Errorf("entry: %s", offers.posts[0])
	}
	// The thread waits for the yes; the power is not followed any more.
	if len(ha.cancelled) != 1 || !strings.Contains(log.String(), "watch offered after the device's run") {
		t.Errorf("cancelled %v, log: %s", ha.cancelled, log.String())
	}
	// The yes drafts the watch and ends the offer.
	for _, tl := range w.Tools() {
		if tl.Skill != OfferSkill {
			t.Errorf("%s comes with %q", tl.Name, tl.Skill)
		}
		if tl.Name != ToolAccept {
			continue
		}
		if out, err := tl.Run(context.Background(), tools.Invocation{}); err != nil || out != "Mein Vorschlag: …" {
			t.Errorf("accept: %q %v", out, err)
		}
	}
	if len(offers.drafted) != 1 || offers.drafted[0].Numbers == "" || offers.ended[o.Thread] {
		t.Errorf("drafted %+v, ended %v", offers.drafted, offers.ended)
	}
}

// A run that leaves the profile short keeps the offer waiting; at the start
// the offers that wait are followed again, a gone device's ends; one that
// cannot be followed stays; an offer that expires says the days waited.
func TestWaitingOffersErrors(t *testing.T) {
	rec := &fakeRecorder{stats: map[string][]haws.StatRow{"sensor.spuelmaschine": runStats()},
		states: map[string][]haws.Change{"sensor.spuelmaschine": states(0, func(time.Time) bool { return false })}}
	ha, offers := &waitHA{}, &memOffers{}
	var log bytes.Buffer
	w := newWaiter(ha, rec, offers, &log)
	pr, err := w.o.Profiler.Profile(context.Background(), "sensor.spuelmaschine")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Wait(context.Background(), Sensor{ID: "sensor.spuelmaschine", Name: "Spülmaschine"}, pr); err != nil {
		t.Fatal(err)
	}
	// A blip that is no whole run: still waiting, with a log line.
	ha.fire("sensor.spuelmaschine")
	handled(t, w)
	if !strings.Contains(log.String(), "still not enough for the watch") {
		t.Errorf("log: %s", log.String())
	}
	if len(offers.posts) != 0 || len(ha.cancelled) != 0 {
		t.Errorf("posts %v, cancelled %v", offers.posts, ha.cancelled)
	}
	// The service starts again: the waiting offer is followed again; a
	// second whose device is gone ends.
	offers.offers["t9"] = &Offer{Thread: "t9", Entity: "sensor.weg", Blueprint: "device_run", Started: testNow}
	ha2 := &waitHA{gone: map[string]bool{"sensor.weg": true}}
	w2 := newWaiter(ha2, rec, offers, &log)
	w2.Resume(context.Background())
	if len(ha2.cmds) != 1 || ha2.cmds[0]["trigger"].(map[string]any)["entity_id"] != "sensor.spuelmaschine" {
		t.Errorf("followed again: %v", ha2.cmds)
	}
	if _, ended := offers.ended["t9"]; !ended || !strings.Contains(log.String(), "device is gone from home assistant; its offer ends") {
		t.Errorf("gone: %v\n%s", offers.ended, log.String())
	}
	// A subscription refused: a log line with the entity; it stays waiting.
	ha3 := &waitHA{subErr: &haws.Error{Command: "subscribe_trigger", Code: "unauthorized", Message: "Unauthorized"}}
	log.Reset()
	newWaiter(ha3, rec, offers, &log).Resume(context.Background())
	if !strings.Contains(log.String(), "could not follow the power of a waiting watch offer") || !strings.Contains(log.String(), "entity=sensor.spuelmaschine") ||
		!strings.Contains(log.String(), "Unauthorized") {
		t.Errorf("log: %s", log.String())
	}
	if _, ended := offers.ended["t1"]; ended {
		t.Error("the offer ended")
	}
	// Expired before the device ran: the subscription ends, with the days.
	log.Reset()
	o := *offers.offers["t1"]
	o.Started, o.Expires = testNow.Add(-30*24*time.Hour), testNow.Add(-time.Minute)
	w2.Ended(o)
	if len(ha2.cancelled) != 1 || !strings.Contains(log.String(), "expired before the device ran") || !strings.Contains(log.String(), "days_waited=30") {
		t.Errorf("cancelled %v, log: %s", ha2.cancelled, log.String())
	}
	// A profile that is enough or no candidate waits for nothing.
	if out, _ := w.Wait(context.Background(), Sensor{Name: "Kühlschrank"}, Profile{Kind: KindCycle, Enough: true}); !strings.Contains(out, "reicht schon") {
		t.Errorf("enough: %s", out)
	}
	if out, _ := w.Wait(context.Background(), Sensor{Name: "Lampe"}, Profile{}); !strings.Contains(out, "kein Gerät") {
		t.Errorf("none: %s", out)
	}
}
