package timer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

func TestTextsComplete(t *testing.T) {
	if missing := i18n.Missing(catalog); len(missing) > 0 {
		t.Errorf("texts missing: %v", missing)
	}
}

// fakeHA plays the integration (build, list, label) and Home Assistant's
// automation config API, keeping the timers.
type fakeHA struct {
	own     *fakeOwn
	timers  []Timer
	saved   []map[string]any // the build commands
	pending map[string]Timer // built, not yet saved
	labels  map[string]string
	deleted []string
	fail    error
	next    int
}

func (f *fakeHA) Call(ctx context.Context, cmd map[string]any) (json.RawMessage, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	switch cmd["type"] {
	case "mira/once/list":
		raw, _ := json.Marshal(map[string]any{"timers": f.timers})
		return raw, nil
	case "mira/automation/list":
		return json.RawMessage(`{"automations":[]}`), nil
	case "mira/once/build":
		f.saved = append(f.saved, cmd)
		tm := Timer{Title: cmd["title"].(string), Due: cmd["due"].(string), Owner: cmd["owner"].(string), Area: cmd["area"].(string), Kind: cmd["kind"].(string)}
		raw, _ := json.Marshal(cmd["actions"])
		json.Unmarshal(raw, &tm.Actions)
		if id, ok := cmd["automation_id"].(string); ok {
			tm.ID = id
		} else {
			f.next++
			tm.ID = fmt.Sprintf("t%d", f.next)
		}
		tm.EntityID = "automation." + tm.ID
		if f.pending == nil {
			f.pending = map[string]Timer{}
		}
		f.pending[tm.ID] = tm
		return json.Marshal(map[string]any{"id": tm.ID, "config": map[string]any{"alias": tm.Title}})
	case "mira/automation/label":
		if f.labels == nil {
			f.labels = map[string]string{}
		}
		f.labels[cmd["automation_id"].(string)] = cmd["label"].(string)
		return json.RawMessage(`{}`), nil
	}
	return nil, errors.New("unknown " + fmt.Sprint(cmd["type"]))
}

func (f *fakeHA) CallService(context.Context, string, string, map[string]any, *haws.Target) error {
	return nil
}

func (f *fakeHA) SaveAutomationConfig(_ context.Context, id string, _ map[string]any) error {
	tm := f.pending[id]
	for i := range f.timers {
		if f.timers[i].ID == id {
			f.timers[i] = tm
			return nil
		}
	}
	f.timers = append(f.timers, tm)
	return nil
}

func (f *fakeHA) DeleteAutomationConfig(_ context.Context, id string) error {
	f.deleted = append(f.deleted, id)
	for i := range f.timers {
		if f.timers[i].ID == id {
			f.timers = append(f.timers[:i], f.timers[i+1:]...)
			return nil
		}
	}
	return errors.New("not found")
}

// fakeOwn is Mira's note of the automations she created.
type fakeOwn struct {
	ids        map[string]string
	takenOver  bool
	failRecord error
}

func newOwn(ids ...string) *fakeOwn {
	o := &fakeOwn{ids: map[string]string{}}
	for _, id := range ids {
		o.ids[id] = "once"
	}
	return o
}

func (o *fakeOwn) RecordOwnAutomation(_ context.Context, id, kind string) error {
	if o.failRecord != nil {
		return o.failRecord
	}
	if _, ok := o.ids[id]; !ok {
		o.ids[id] = kind
	}
	return nil
}

func (o *fakeOwn) IsOwnAutomation(_ context.Context, id string) (bool, error) {
	_, ok := o.ids[id]
	return ok, nil
}

func (o *fakeOwn) ForgetOwnAutomation(_ context.Context, id string) error {
	delete(o.ids, id)
	return nil
}

func (o *fakeOwn) OwnAutomationsTakenOver(context.Context) (bool, error) { return o.takenOver, nil }

func (o *fakeOwn) TakeOverOwnAutomations(_ context.Context, ids map[string]string) error {
	for id, kind := range ids {
		o.ids[id] = kind
	}
	o.takenOver = true
	return nil
}

var now = time.Date(2026, 10, 18, 22, 5, 0, 0, time.UTC)

func setup() (map[string]tools.InternalTool, *fakeHA) {
	ha := &fakeHA{own: newOwn()}
	list := Tools(Options{
		Lang: i18n.DE, HA: ha, Own: ha.own, Clock: func() time.Time { return now }, Location: time.UTC,
		Players: func(area string) []string {
			return map[string][]string{"wohnzimmer": {"media_player.show_speaker"}}[area]
		},
		Lights: func(area string) []string {
			return map[string][]string{"wohnzimmer": {"light.farblampe"}, "bad": {"light.bad"}}[area]
		},
		Area: func(name string) string {
			return map[string]string{"wohnzimmer": "wohnzimmer", "bad": "bad", "küche": "kueche"}[name]
		},
		AreaName: func(id string) string {
			return map[string]string{"wohnzimmer": "Wohnzimmer", "bad": "Bad", "kueche": "Küche"}[id]
		},
		Forbidden: func() map[string]any { return map[string]any{"domains": []string{"lock"}, "entities": []string{}} },
	})
	by := map[string]tools.InternalTool{}
	for _, tl := range list {
		by[tl.Name] = tl
	}
	return by, ha
}

func run(t *testing.T, tl tools.InternalTool, inv tools.Invocation, args string) string {
	t.Helper()
	inv.Args = json.RawMessage(args)
	out, err := tl.Run(context.Background(), inv)
	if err != nil {
		t.Fatalf("%s: %v", tl.Name, err)
	}
	return out
}

var anna = tools.Invocation{Person: "anna", Area: "wohnzimmer"}

func TestSleepTimerStopsThePlayerInTheRoom(t *testing.T) {
	by, ha := setup()
	out := run(t, by[ToolSleep], anna, `{"minutes":30}`)
	if !strings.Contains(out, "22:35") || !strings.Contains(out, "in 30 Minuten") || strings.Contains(out, "Licht") {
		t.Errorf("answer: %q", out)
	}
	if len(ha.saved) != 1 {
		t.Fatalf("saved: %v", ha.saved)
	}
	s := ha.saved[0]
	if s["due"] != now.Add(30*time.Minute).Format(time.RFC3339) || s["owner"] != "anna" || s["area"] != "wohnzimmer" || s["kind"] != "sleep" {
		t.Errorf("save: %v", s)
	}
	actions := s["actions"].([]map[string]any)
	if len(actions) != 1 || actions[0]["action"] != "media_player.media_stop" {
		t.Errorf("actions: %v", actions)
	}
	if s["forbidden"].(map[string]any)["domains"].([]string)[0] != "lock" {
		t.Errorf("forbidden not passed on: %v", s["forbidden"])
	}
}

func TestLightsOnlyOnRequest(t *testing.T) {
	by, ha := setup()
	out := run(t, by[ToolSleep], anna, `{"minutes":20,"lights_off":true}`)
	if !strings.Contains(out, "Licht geht aus") {
		t.Errorf("answer: %q", out)
	}
	actions := ha.saved[0]["actions"].([]map[string]any)
	if len(actions) != 2 || actions[1]["action"] != "light.turn_off" {
		t.Errorf("actions: %v", actions)
	}
	// A room with a light and no speaker still gets its timer, for the light.
	run(t, by[ToolSleep], tools.Invocation{Person: "anna", Area: "bad"}, `{"minutes":10,"lights_off":true}`)
	if a := ha.saved[1]["actions"].([]map[string]any); len(a) != 1 || a[0]["action"] != "light.turn_off" {
		t.Errorf("bathroom actions: %v", a)
	}
}

func TestSleepTimerSaysWhatIsMissing(t *testing.T) {
	by, ha := setup()
	if out := run(t, by[ToolSleep], tools.Invocation{Person: "anna"}, `{"minutes":5}`); !strings.Contains(out, "welchem Raum") {
		t.Errorf("no room: %q", out)
	}
	if out := run(t, by[ToolSleep], anna, `{"minutes":5,"room":"Garten"}`); !strings.Contains(out, "welchem Raum") {
		t.Errorf("unknown room: %q", out)
	}
	if out := run(t, by[ToolSleep], anna, `{"minutes":5,"room":"Küche"}`); !strings.Contains(out, "weder ein Lautsprecher noch ein Licht") {
		t.Errorf("nothing to stop: %q", out)
	}
	if out := run(t, by[ToolSleep], anna, `{"minutes":0}`); !strings.Contains(out, "mehr als 0") {
		t.Errorf("zero minutes: %q", out)
	}
	if len(ha.saved) != 0 {
		t.Errorf("saved anyway: %v", ha.saved)
	}
	ha.fail = errors.New("too many timers")
	if out := run(t, by[ToolSleep], anna, `{"minutes":5}`); !strings.Contains(out, "Anlegen fehlgeschlagen: too many timers") {
		t.Errorf("failure: %q", out)
	}
}

func TestASecondSleepTimerInTheRoomReplacesTheFirst(t *testing.T) {
	by, ha := setup()
	run(t, by[ToolSleep], anna, `{"minutes":30}`)
	run(t, by[ToolSleep], anna, `{"minutes":15}`)
	if len(ha.timers) != 1 || ha.timers[0].Due != now.Add(15*time.Minute).Format(time.RFC3339) {
		t.Errorf("timers: %+v", ha.timers)
	}
	// Another person gets their own.
	run(t, by[ToolSleep], tools.Invocation{Person: "ben", Area: "wohnzimmer"}, `{"minutes":45}`)
	if len(ha.timers) != 2 {
		t.Errorf("timers: %+v", ha.timers)
	}
}

func TestListShowsOnlyTheUsersOwnOpenTimers(t *testing.T) {
	by, ha := setup()
	run(t, by[ToolSleep], anna, `{"minutes":30}`)
	run(t, by[ToolSleep], tools.Invocation{Person: "ben", Area: "wohnzimmer"}, `{"minutes":45}`)
	ha.timers = append(ha.timers, Timer{ID: "old", Owner: "anna", Area: "bad", Ran: true, Due: now.Format(time.RFC3339)})
	out := run(t, by[ToolSleep], anna, `{"action":"list"}`)
	if strings.Count(out, "\n") != 0 || !strings.Contains(out, "in 30 Minuten") || !strings.Contains(out, "Raum Wohnzimmer") {
		t.Errorf("list: %q", out)
	}
	if out := run(t, by[ToolSleep], tools.Invocation{Person: "zoe", Area: "bad"}, `{"action":"list"}`); out != "Keine offenen Schlaf-Timer." {
		t.Errorf("empty: %q", out)
	}
}

func TestExtendAddsToTheDueTimeAndKeepsTheActions(t *testing.T) {
	by, ha := setup()
	run(t, by[ToolSleep], anna, `{"minutes":30,"lights_off":true}`)
	out := run(t, by[ToolSleep], anna, `{"action":"extend","minutes":10}`)
	if !strings.Contains(out, "22:45") || !strings.Contains(out, "in 40 Minuten") {
		t.Errorf("answer: %q", out)
	}
	last := ha.saved[len(ha.saved)-1]
	if last["automation_id"] != "t1" || len(ha.timers[0].Actions) != 2 {
		t.Errorf("extend: %v / %+v", last, ha.timers)
	}
	if len(ha.timers) != 1 || ha.timers[0].Due != now.Add(40*time.Minute).Format(time.RFC3339) {
		t.Errorf("timers: %+v", ha.timers)
	}
	// Due but not run (Home Assistant was off): the extension counts from now.
	ha.timers[0].Due = now.Add(-time.Hour).Format(time.RFC3339)
	run(t, by[ToolSleep], anna, `{"action":"extend","minutes":10}`)
	if ha.timers[0].Due != now.Add(10*time.Minute).Format(time.RFC3339) {
		t.Errorf("overdue extend: %+v", ha.timers[0])
	}
}

func TestSeveralTimersAreAskedAbout(t *testing.T) {
	by, ha := setup()
	ha.timers = []Timer{
		{ID: "a", Title: "Schlaf-Timer Wohnzimmer", Owner: "anna", Area: "wohnzimmer", Due: now.Add(10 * time.Minute).Format(time.RFC3339), Kind: "sleep"},
		{ID: "b", Title: "Schlaf-Timer Bad", Owner: "anna", Area: "bad", Due: now.Add(20 * time.Minute).Format(time.RFC3339), Kind: "sleep"},
	}
	ha.own.ids["a"], ha.own.ids["b"] = "once", "once"
	out := run(t, by[ToolSleep], anna, `{"action":"cancel"}`)
	if !strings.Contains(out, "mehrere Schlaf-Timer") || !strings.Contains(out, "a (") || len(ha.timers) != 2 {
		t.Errorf("ask: %q %+v", out, ha.timers)
	}
	if out := run(t, by[ToolSleep], anna, `{"action":"cancel","id":"b"}`); out != "Der Schlaf-Timer ist abgebrochen." || len(ha.timers) != 1 || ha.timers[0].ID != "a" {
		t.Errorf("cancel by id: %q %+v", out, ha.timers)
	}
	if out := run(t, by[ToolSleep], anna, `{"action":"cancel","id":"zzz"}`); !strings.Contains(out, "keinen offenen Schlaf-Timer") {
		t.Errorf("unknown id: %q", out)
	}
	if out := run(t, by[ToolSleep], anna, `{"action":"cancel"}`); out != "Der Schlaf-Timer ist abgebrochen." || len(ha.timers) != 0 {
		t.Errorf("cancel the only one: %q %+v", out, ha.timers)
	}
}

// HD-25 A finished timer goes when it has run
func TestHD25_AFinishedTimerGoesWhenItHasRun(t *testing.T) {
	ha := &fakeHA{timers: []Timer{
		{ID: "waiting", EntityID: "automation.waiting"},                      // not run yet: stays, watched
		{ID: "fading", EntityID: "automation.fading", Ran: true, Running: 1}, // still fading out: stays
		{ID: "done", EntityID: "automation.done", Ran: true},                 // ran while Mira was off: goes
	}}
	watched := make(chan []string, 4)
	var changed func()
	timers := New(Options{Lang: i18n.DE, HA: ha, Own: newOwn(), Clock: func() time.Time { return now },
		Watch: func(_ context.Context, ids []string, fn func()) (func(), error) {
			changed = fn
			watched <- ids
			return func() {}, nil
		}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go timers.Run(ctx)

	if ids := <-watched; !slices.Equal(ids, []string{"automation.fading", "automation.waiting"}) {
		t.Fatalf("watched: %v", ids)
	}
	if !slices.Equal(ha.deleted, []string{"done"}) {
		t.Fatalf("deleted at the start: %v", ha.deleted)
	}
	// The fading one ends: its state changes, and it goes.
	for i := range ha.timers {
		if ha.timers[i].ID == "fading" {
			ha.timers[i].Running = 0
		}
	}
	changed()
	if ids := <-watched; !slices.Equal(ids, []string{"automation.waiting"}) {
		t.Fatalf("watched after: %v", ids)
	}
	if !slices.Equal(ha.deleted, []string{"done", "fading"}) {
		t.Errorf("deleted: %v", ha.deleted)
	}
}

// tidyOnce runs one look at the timers, as at the start.
func tidyOnce(o Options) ([]string, bool, *controller) {
	c := New(o).c
	ids, ok := c.tidy(context.Background())
	return ids, ok, c
}

// HD-42 A finished timer without a row stays
func TestHD42_AFinishedTimerWithoutARowStays(t *testing.T) {
	ha := &fakeHA{timers: []Timer{{ID: "fremd", EntityID: "automation.fremd", Ran: true}}}
	own := newOwn()
	own.takenOver = true
	var logs strings.Builder
	o := Options{Lang: i18n.DE, HA: ha, Own: own, Logger: slog.New(slog.NewTextHandler(&logs, nil))}
	c := New(o).c
	for range 3 {
		if _, ok := c.tidy(context.Background()); !ok {
			t.Fatal("not tidied")
		}
	}
	if len(ha.deleted) != 0 || len(ha.timers) != 1 {
		t.Errorf("deleted a timer Mira did not create: %v", ha.deleted)
	}
	if n := strings.Count(logs.String(), "was not created by mira, left alone"); n != 1 || !strings.Contains(logs.String(), "automation=fremd") {
		t.Errorf("logged %d times: %s", n, logs.String())
	}
}

// HD-44 Tasks and timers from before are taken over once
func TestHD44_TasksAndTimersFromBeforeAreTakenOverOnce(t *testing.T) {
	ha := &fakeHA{timers: []Timer{{ID: "alt", EntityID: "automation.alt", Ran: true}}}
	own := newOwn()
	var logs strings.Builder
	o := Options{Lang: i18n.DE, HA: ha, Own: own, Logger: slog.New(slog.NewTextHandler(&logs, nil))}

	// Home Assistant cannot be read: nothing is taken over, nothing tidied.
	ha.fail = errors.New("not connected")
	if _, ok, _ := tidyOnce(o); ok || own.takenOver || len(own.ids) != 0 {
		t.Fatalf("taken over without the lists: %v %v", own.takenOver, own.ids)
	}
	if !strings.Contains(logs.String(), "could not take over") {
		t.Errorf("log: %s", logs.String())
	}

	ha.fail = nil
	if _, ok, _ := tidyOnce(o); !ok {
		t.Fatal("not tidied")
	}
	if !own.takenOver || !slices.Equal(ha.deleted, []string{"alt"}) {
		t.Errorf("the timer from before: taken over %v, deleted %v", own.takenOver, ha.deleted)
	}
	if !strings.Contains(logs.String(), "took over the automations labelled as mira's from before") || !strings.Contains(logs.String(), "count=1") {
		t.Errorf("log: %s", logs.String())
	}

	// Once: a timer labelled later is not taken over.
	ha.timers = []Timer{{ID: "spaeter", EntityID: "automation.spaeter", Ran: true}}
	tidyOnce(o)
	if slices.Contains(ha.deleted, "spaeter") {
		t.Errorf("taken over again: %v", ha.deleted)
	}
}

// HD-40 A new task or timer is recorded as Mira's
func TestHD40_ANewTimerIsRecordedAsMiras(t *testing.T) {
	by, ha := setup()
	run(t, by[ToolSleep], anna, `{"minutes":30}`)
	if len(ha.timers) != 1 || ha.own.ids[ha.timers[0].ID] != "once" {
		t.Errorf("timer %v, noted %v", ha.timers, ha.own.ids)
	}
}
