package timer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

func TestTextsComplete(t *testing.T) {
	if missing := i18n.Missing(catalog); len(missing) > 0 {
		t.Errorf("texts missing: %v", missing)
	}
}

// fakeHA keeps the timers like the integration does.
type fakeHA struct {
	timers []Timer
	saved  []map[string]any
	fail   error
	next   int
}

func (f *fakeHA) Call(ctx context.Context, cmd map[string]any) (json.RawMessage, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	switch cmd["type"] {
	case "mira/once/list":
		raw, _ := json.Marshal(map[string]any{"timers": f.timers})
		return raw, nil
	case "mira/once/save":
		f.saved = append(f.saved, cmd)
		tm := Timer{Title: cmd["title"].(string), Due: cmd["due"].(string), Owner: cmd["owner"].(string), Area: cmd["area"].(string), Kind: cmd["kind"].(string)}
		raw, _ := json.Marshal(cmd["actions"])
		json.Unmarshal(raw, &tm.Actions)
		if id, ok := cmd["automation_id"].(string); ok {
			tm.ID = id
			for i := range f.timers {
				if f.timers[i].ID == id {
					f.timers[i] = tm
				}
			}
		} else {
			f.next++
			tm.ID = fmt.Sprintf("t%d", f.next)
			f.timers = append(f.timers, tm)
		}
		return json.RawMessage(`{}`), nil
	case "mira/once/delete":
		id := cmd["automation_id"].(string)
		for i := range f.timers {
			if f.timers[i].ID == id {
				f.timers = append(f.timers[:i], f.timers[i+1:]...)
			}
		}
		return json.RawMessage(`{}`), nil
	}
	return nil, errors.New("unknown " + fmt.Sprint(cmd["type"]))
}

var now = time.Date(2026, 10, 18, 22, 5, 0, 0, time.UTC)

func setup() (map[string]tools.InternalTool, *fakeHA) {
	ha := &fakeHA{}
	list := Tools(Options{
		Lang: i18n.DE, HA: ha, Clock: func() time.Time { return now }, Location: time.UTC,
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
	if out := run(t, by[ToolSleep], anna, `{"minutes":5}`); !strings.Contains(out, "konnte nicht angelegt werden: too many timers") {
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
	out := run(t, by[ToolList], anna, `{}`)
	if strings.Count(out, "\n") != 0 || !strings.Contains(out, "in 30 Minuten") || !strings.Contains(out, "Raum Wohnzimmer") {
		t.Errorf("list: %q", out)
	}
	if out := run(t, by[ToolList], tools.Invocation{Person: "zoe", Area: "bad"}, `{}`); out != "Keine offenen Timer." {
		t.Errorf("empty: %q", out)
	}
}

func TestExtendAddsToTheDueTimeAndKeepsTheActions(t *testing.T) {
	by, ha := setup()
	run(t, by[ToolSleep], anna, `{"minutes":30,"lights_off":true}`)
	out := run(t, by[ToolExtend], anna, `{"add_minutes":10}`)
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
	run(t, by[ToolExtend], anna, `{"add_minutes":10}`)
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
	out := run(t, by[ToolCancel], anna, `{}`)
	if !strings.Contains(out, "mehrere Timer") || !strings.Contains(out, "a (") || len(ha.timers) != 2 {
		t.Errorf("ask: %q %+v", out, ha.timers)
	}
	if out := run(t, by[ToolCancel], anna, `{"id":"b"}`); out != "Der Timer ist abgebrochen." || len(ha.timers) != 1 || ha.timers[0].ID != "a" {
		t.Errorf("cancel by id: %q %+v", out, ha.timers)
	}
	if out := run(t, by[ToolCancel], anna, `{"id":"zzz"}`); !strings.Contains(out, "keinen offenen Timer") {
		t.Errorf("unknown id: %q", out)
	}
	if out := run(t, by[ToolCancel], anna, `{}`); out != "Der Timer ist abgebrochen." || len(ha.timers) != 0 {
		t.Errorf("cancel the only one: %q %+v", out, ha.timers)
	}
}
