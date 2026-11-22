package alarm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// fakeHA plays the integration (validate, label, list by label), Home
// Assistant's automation config API and its event bus.
type fakeHA struct {
	saved      map[string]map[string]any // id → config
	labels     map[string]string
	ran        map[string]bool
	running    map[string]int
	deleted    []string
	fired      []map[string]any
	services   []string
	validated  int
	refuse     string // validate answers not valid with this error
	failDelete bool   // Home Assistant answers "Resource not found"
	onFire     func()
}

func newHA() *fakeHA {
	return &fakeHA{saved: map[string]map[string]any{}, labels: map[string]string{}, ran: map[string]bool{},
		running: map[string]int{}}
}

func (f *fakeHA) Call(_ context.Context, cmd map[string]any) (json.RawMessage, error) {
	switch cmd["type"] {
	case "mira/automation/validate":
		f.validated++
		if f.refuse != "" {
			return json.Marshal(map[string]any{"valid": false, "error": f.refuse})
		}
		return json.RawMessage(`{"valid":true}`), nil
	case "mira/automation/label":
		f.labels[cmd["automation_id"].(string)] = cmd["label"].(string)
		return json.RawMessage(`{}`), nil
	case "mira/automation/list":
		if cmd["label"] != "alarm" {
			return nil, fmt.Errorf("listed label %v", cmd["label"])
		}
		var out []map[string]any
		for id, config := range f.saved {
			if f.labels[id] != "alarm" {
				continue
			}
			last := ""
			if f.ran[id] {
				last = "2026-10-19T06:00:00+00:00"
			}
			out = append(out, map[string]any{"id": id, "entity_id": "automation." + id, "current": f.running[id],
				"last_triggered": last, "config": config})
		}
		slices.SortFunc(out, func(a, b map[string]any) int { return strings.Compare(a["id"].(string), b["id"].(string)) })
		return json.Marshal(map[string]any{"automations": out})
	case "fire_event":
		f.fired = append(f.fired, cmd)
		if f.onFire != nil {
			f.onFire()
		}
		return json.RawMessage(`{}`), nil
	}
	return nil, errors.New("unknown " + fmt.Sprint(cmd["type"]))
}

func (f *fakeHA) CallService(_ context.Context, domain, service string, _ map[string]any, _ *haws.Target) error {
	f.services = append(f.services, domain+"."+service)
	return nil
}

func (f *fakeHA) SaveAutomationConfig(_ context.Context, id string, config map[string]any) error {
	// As Home Assistant stores it: JSON.
	raw, _ := json.Marshal(config)
	var back map[string]any
	json.Unmarshal(raw, &back)
	f.saved[id] = back
	return nil
}

func (f *fakeHA) DeleteAutomationConfig(_ context.Context, id string) error {
	if f.failDelete {
		return errors.New("400 Bad Request {\"message\":\"Resource not found\"}")
	}
	f.deleted = append(f.deleted, id)
	delete(f.saved, id)
	return nil
}

type fakeOwn struct{ ids map[string]string }

func (o *fakeOwn) RecordOwnAutomation(_ context.Context, id, kind string) error {
	o.ids[id] = kind
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
func (o *fakeOwn) OwnAutomationsTakenOver(context.Context) (bool, error)           { return true, nil }
func (o *fakeOwn) TakeOverOwnAutomations(context.Context, map[string]string) error { return nil }

var now = time.Date(2026, 10, 18, 22, 5, 0, 0, time.UTC) // a Sunday

const (
	show  = "assist_satellite.show"
	echo  = "assist_satellite.echo"  // a speaker that cannot announce
	plain = "assist_satellite.plain" // a satellite without a speaker
	all   = 4 | 1048576              // volume_set and media_announce
)

type env struct {
	tool    tools.InternalTool
	alarms  *Alarms
	ha      *fakeHA
	own     *fakeOwn
	watched []string
	changed func()
}

func setup() *env {
	e := &env{ha: newHA(), own: &fakeOwn{ids: map[string]string{}}}
	n := 0
	e.alarms = New(Options{
		Lang: i18n.DE, HA: e.ha, Own: e.own, Clock: func() time.Time { return now }, Location: time.UTC,
		Speaker: func(sat string) string {
			return map[string]string{show: "media_player.show_speaker", echo: "media_player.echo_speaker"}[sat]
		},
		Features: func(_ context.Context, entity string) (int, error) {
			return map[string]int{"media_player.show_speaker": all, "media_player.echo_speaker": 4}[entity], nil
		},
		SoundURL: func(_ context.Context, name string) string {
			return "http://ha:8123/api/mira/panel/ui/sounds/" + name + ".mp3"
		},
		AreaName: func(id string) string {
			return map[string]string{"schlafzimmer": "Schlafzimmer", "kinderzimmer": "Kinderzimmer"}[id]
		},
		Forbidden: func() map[string]any { return map[string]any{"domains": []string{"lock"}, "entities": []string{}} },
		Watch: func(_ context.Context, entities []string, changed func()) (func(), error) {
			e.watched, e.changed = entities, changed
			return func() {}, nil
		},
		Settle: 50 * time.Millisecond,
		NewID: func() string {
			n++
			return fmt.Sprintf("mira_alarm%04d", n)
		},
	})
	e.tool = e.alarms.Tools()[0]
	return e
}

func (e *env) call(t *testing.T, inv tools.Invocation, args string) string {
	t.Helper()
	inv.Args = json.RawMessage(args)
	out, err := e.tool.Run(context.Background(), inv)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

var anna = tools.Invocation{Person: "anna", Area: "schlafzimmer"}

// set is the thread's finish: alarm.set with the fields and the place.
func (e *env) set(person, area, satellite, at, days string) error {
	return e.alarms.SetAction(context.Background(), map[string]any{"person": person, "area": area,
		"satellite": satellite, "time": at, "days": days})
}

func (e *env) setAnna(t *testing.T, at, days string) {
	t.Helper()
	if err := e.set("anna", "schlafzimmer", show, at, days); err != nil {
		t.Fatal(err)
	}
}

func (e *env) only(t *testing.T) (string, map[string]any) {
	t.Helper()
	if len(e.ha.saved) != 1 {
		t.Fatalf("saved %d automations", len(e.ha.saved))
	}
	for id, c := range e.ha.saved {
		return id, c
	}
	return "", nil
}

// actions are the automation's top-level actions, as JSON text each.
func actions(c map[string]any) []string {
	var out []string
	for _, a := range c["actions"].([]any) {
		raw, _ := json.Marshal(a)
		out = append(out, string(raw))
	}
	return out
}

func TestWK02_ATECHO5GetsMirasAlarmNotTheDevices(t *testing.T) {
	e := setup()
	e.setAnna(t, "06:00", "")
	id, _ := e.only(t)
	if e.ha.labels[id] != "alarm" || e.own.ids[id] != "alarm" {
		t.Errorf("label %q, own %q", e.ha.labels[id], e.own.ids[id])
	}
	for _, s := range e.ha.services {
		if strings.HasPrefix(s, "esphome.") {
			t.Errorf("called %s", s)
		}
	}
}

func TestWK03_ASatelliteGetsMirasAutomation(t *testing.T) {
	e := setup()
	e.setAnna(t, "6:00", "weekdays")
	_, c := e.only(t)
	v := c["variables"].(map[string]any)
	if v["mira_time"] != "06:00" || v["mira_days"] != "mon,tue,wed,thu,fri" || v["mira_owner"] != "anna" ||
		v["mira_area"] != "schlafzimmer" || v["mira_kind"] != "alarm" {
		t.Errorf("variables %v", v)
	}
	trig, _ := json.Marshal(c["triggers"])
	cond, _ := json.Marshal(c["conditions"])
	if string(trig) != `[{"at":"06:00:00","id":"ring","trigger":"time"}]` ||
		string(cond) != `[{"condition":"time","weekday":["mon","tue","wed","thu","fri"]}]` {
		t.Errorf("triggers %s, conditions %s", trig, cond)
	}
	acts := strings.Join(actions(c), "\n")
	for _, want := range []string{`"entity_id":"media_player.show_speaker"`, "wake_rise.mp3", "wake_loop.mp3", `"announce":true`} {
		if !strings.Contains(acts, want) {
			t.Errorf("actions lack %s:\n%s", want, acts)
		}
	}
	if !strings.Contains(acts, "Mo, Di, Mi, Do, Fr um 06:00 im Raum Schlafzimmer konnte nicht klingeln") {
		t.Errorf("message of a ring that fails:\n%s", acts)
	}
}

func TestWK04_ASatelliteThatCanTakeNoAnnouncementGetsNoAlarm(t *testing.T) {
	for _, sat := range []string{echo, plain} {
		e := setup()
		err := e.set("anna", "schlafzimmer", sat, "06:00", "")
		var r *Refusal
		if len(e.ha.saved) != 0 || !errors.As(err, &r) || !strings.Contains(r.Text, "kann keinen Wecker") {
			t.Errorf("%s: saved %d: %v", sat, len(e.ha.saved), err)
		}
	}
}

// The ringing run waits for the satellite listening and ends on it; what
// Home Assistant does with it was tried on the test HA (state table).
func TestWK05_TheWakeWordEndsTheRinging(t *testing.T) {
	e := setup()
	e.setAnna(t, "06:00", "")
	_, c := e.only(t)
	acts := strings.Join(actions(c), "\n")
	if !strings.Contains(acts, `{"entity_id":"assist_satellite.show","id":"reaction","to":"listening","trigger":"state"}`) ||
		!strings.Contains(acts, `in ['reaction', 'cancel']`) {
		t.Errorf("no reaction ends the rounds:\n%s", acts)
	}
}

func TestWK06_WithoutAReactionTheRingingStopsAfterTenMinutes(t *testing.T) {
	e := setup()
	e.setAnna(t, "06:00", "")
	_, c := e.only(t)
	acts := strings.Join(actions(c), "\n")
	if !strings.Contains(acts, "timedelta(minutes=10)") || !strings.Contains(acts, "now() \\u003e= as_datetime(until)") {
		t.Errorf("no ten minutes:\n%s", acts)
	}
}

// Only the time starts the automation: a turn at the satellite (its
// listening) is a signal the ringing run waits for, never a trigger.
func TestWK07_AnotherTurnAtTheSatelliteStartsNothing(t *testing.T) {
	e := setup()
	e.setAnna(t, "06:00", "")
	_, c := e.only(t)
	if trig := c["triggers"].([]any); len(trig) != 1 || trig[0].(map[string]any)["trigger"] != "time" {
		t.Errorf("triggers %v", trig)
	}
	if c["mode"] != "single" {
		t.Errorf("mode %v", c["mode"])
	}
}

func TestWK08_ARefusalOfTheIntegrationReachesTheUserAndTheLog(t *testing.T) {
	e := setup()
	e.ha.refuse = "a task may not call lock.unlock"
	err := e.set("anna", "schlafzimmer", show, "06:00", "")
	if len(e.ha.saved) != 0 || err == nil || !strings.Contains(err.Error(), "kein Wecker") || !strings.Contains(err.Error(), "lock.unlock") {
		t.Errorf("saved %d: %v", len(e.ha.saved), err)
	}
}

func TestWK09_AOnceAlarmIsDeletedAfterItRang(t *testing.T) {
	e := setup()
	e.setAnna(t, "06:00", "")
	e.setAnna(t, "07:00", "daily")
	once, daily := "mira_alarm0001", "mira_alarm0002"
	e.ha.ran[once], e.ha.ran[daily] = true, true
	ids, ok := e.alarms.c.tidy(context.Background())
	if !ok || !slices.Equal(e.ha.deleted, []string{once}) || !slices.Equal(ids, []string{"automation." + daily}) {
		t.Errorf("deleted %v, kept %v", e.ha.deleted, ids)
	}
	// Still ringing: not yet.
	e.setAnna(t, "08:00", "")
	e.ha.ran["mira_alarm0003"], e.ha.running["mira_alarm0003"] = true, 1
	e.alarms.c.tidy(context.Background())
	if slices.Contains(e.ha.deleted, "mira_alarm0003") {
		t.Error("deleted while it rings")
	}
}

func TestWK10_ThePersonsAlarmsAreListed(t *testing.T) {
	e := setup()
	e.setAnna(t, "06:00", "")
	if err := e.set("ben", "kinderzimmer", show, "07:00", ""); err != nil {
		t.Fatal(err)
	}
	out := e.call(t, anna, `{"action":"list"}`)
	if !strings.Contains(out, "06:00") || !strings.Contains(out, "Schlafzimmer") || strings.Contains(out, "07:00") {
		t.Errorf("list %q", out)
	}
}

func TestWK13_AVolumeTurnedDownTheNightBeforeDoesNotMakeTheAlarmQuiet(t *testing.T) {
	e := setup()
	e.setAnna(t, "06:00", "")
	_, c := e.only(t)
	acts := actions(c)
	set := slices.IndexFunc(acts, func(a string) bool { return strings.Contains(a, `"volume_level":0.9`) })
	play := slices.IndexFunc(acts, func(a string) bool { return strings.Contains(a, "media_player.play_media") })
	back := slices.IndexFunc(acts, func(a string) bool { return strings.Contains(a, `{{ before }}`) })
	if set < 0 || play < 0 || set > play || back < play {
		t.Errorf("volume set at %d, play at %d, back at %d", set, play, back)
	}
}

func TestWK14_WithoutASatelliteNoAlarmIsSet(t *testing.T) {
	e := setup()
	// From Telegram the thread has no room, so the place has no satellite.
	err := e.set("anna", "", "", "06:00", "")
	if len(e.ha.saved) != 0 || err == nil || !strings.Contains(err.Error(), "nur an einem Satelliten") {
		t.Errorf("saved %d: %v", len(e.ha.saved), err)
	}
}

// Deleting a ringing alarm cancels its run first (decision 15), which sets
// the volume back; then the automation goes.
func TestDeletingARingingAlarmCancelsItFirst(t *testing.T) {
	e := setup()
	e.setAnna(t, "06:00", "")
	id, _ := e.only(t)
	e.ha.ran[id], e.ha.running[id] = true, 1
	e.ha.onFire = func() {
		e.ha.running[id] = 0
		e.changed() // as the subscription: it only passes the change on
	}
	out := e.call(t, anna, `{"action":"delete"}`)
	if len(e.ha.fired) != 1 || e.ha.fired[0]["event_type"] != CancelEvent ||
		e.ha.fired[0]["event_data"].(map[string]any)["automation"] != id {
		t.Errorf("fired %v", e.ha.fired)
	}
	if !slices.Equal(e.ha.deleted, []string{id}) || !strings.Contains(out, "gelöscht") {
		t.Errorf("deleted %v: %q", e.ha.deleted, out)
	}
	// Deleted meanwhile by the tidy loop: still deleted for the user.
	e.setAnna(t, "09:00", "")
	gone := ""
	for i, c := range e.ha.saved {
		if c["variables"].(map[string]any)["mira_time"] == "09:00" {
			gone = i
		}
	}
	e.ha.ran[gone], e.ha.running[gone] = true, 1
	e.ha.onFire = func() { delete(e.ha.saved, gone); e.changed() }
	e.ha.failDelete = true
	if out := e.call(t, anna, `{"action":"delete","id":"`+gone+`"}`); !strings.Contains(out, "gelöscht") {
		t.Errorf("already gone: %q", out)
	}
	e.ha.failDelete = false
	// One not ringing goes without an event.
	e.setAnna(t, "07:00", "")
	e.call(t, anna, `{"action":"delete"}`)
	if len(e.ha.fired) != 2 || !slices.Equal(e.ha.deleted, []string{"mira_alarm0001", "mira_alarm0003"}) {
		t.Errorf("fired %d, deleted %v", len(e.ha.fired), e.ha.deleted)
	}
}

func TestParseDays(t *testing.T) {
	for in, want := range map[string]string{
		"": "", "once": "", "weekdays": "mon,tue,wed,thu,fri", "weekends": "sat,sun",
		"daily": "mon,tue,wed,thu,fri,sat,sun", "mon,wed": "mon,wed", "Wed Mon": "mon,wed", "monday, friday": "mon,fri",
	} {
		got, ok := parseDays(in)
		if !ok || strings.Join(got, ",") != want {
			t.Errorf("%q: %v %v, want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"montag", "x", "mo,xx"} {
		if _, ok := parseDays(bad); ok {
			t.Errorf("%q accepted", bad)
		}
	}
}

// "Nein, halb sieben": thread_undo calls alarm.delete_created with the
// fields of the alarm just set; it deletes that one, not another.
func TestTheUndoDeletesTheAlarmJustSet(t *testing.T) {
	e := setup()
	e.setAnna(t, "06:00", "weekdays")
	e.setAnna(t, "06:00", "")
	err := e.alarms.DeleteCreated(context.Background(), map[string]any{"person": "anna", "area": "schlafzimmer",
		"satellite": show, "time": "6:00", "days": "weekdays"})
	if err != nil || !slices.Equal(e.ha.deleted, []string{"mira_alarm0001"}) {
		t.Errorf("deleted %v: %v", e.ha.deleted, err)
	}
	if err := e.alarms.DeleteCreated(context.Background(), map[string]any{"person": "anna", "area": "schlafzimmer",
		"time": "07:00", "days": ""}); err == nil {
		t.Error("deleted an alarm that is not there")
	}
}

// Setting the same alarm again makes no second one (decision 18).
func TestTheSameAlarmTwiceIsOne(t *testing.T) {
	e := setup()
	e.setAnna(t, "06:30", "")
	e.setAnna(t, "06:30", "")
	e.setAnna(t, "06:30", "weekdays")
	if len(e.ha.saved) != 2 {
		t.Errorf("saved %d alarms", len(e.ha.saved))
	}
}

func TestAOnceAlarmSaysTodayOrTomorrow(t *testing.T) {
	e := setup()
	if got := e.alarms.c.when("23:00", nil); got != "heute (So, 18.10.) um 23:00" {
		t.Errorf("%q", got)
	}
	if got := e.alarms.c.when("06:00", nil); got != "morgen (Mo, 19.10.) um 06:00" {
		t.Errorf("%q", got)
	}
}
