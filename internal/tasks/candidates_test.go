package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/ha/structure"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/retry"
	"github.com/miraassist/mira/internal/tools"
	"github.com/miraassist/mira/internal/watch"
)

var berlin, _ = time.LoadLocation("Europe/Berlin")

// onAt says whether a time of day lies in one of the windows ("07:40-11:40").
func onAt(t time.Time, windows ...string) bool {
	l := t.In(berlin)
	m := l.Hour()*60 + l.Minute()
	for _, w := range windows {
		from, _ := strconv.Atoi(w[0:2])
		fromM, _ := strconv.Atoi(w[3:5])
		to, _ := strconv.Atoi(w[6:8])
		toM, _ := strconv.Atoi(w[9:11])
		if a, b := from*60+fromM, to*60+toM; m >= a && m < b {
			return true
		}
	}
	return false
}

// plugPast is a plug's hourly statistics of the last 30 days and its states
// every 5 minutes of the last 10, before now, from on.
func plugPast(now time.Time, watts float64, on func(time.Time) bool) ([]haws.StatRow, []haws.Change) {
	var rows []haws.StatRow
	for h := now.AddDate(0, 0, -watch.RequestDays).Truncate(time.Hour); h.Before(now.Truncate(time.Hour)); h = h.Add(time.Hour) {
		n := 0
		for m := 0; m < 60; m++ {
			if on(h.Add(time.Duration(m) * time.Minute)) {
				n++
			}
		}
		mean, mn, mx := watts*float64(n)/60, 0.0, 0.0
		if n == 60 {
			mn = watts
		}
		if n > 0 {
			mx = watts
		}
		rows = append(rows, haws.StatRow{Start: h, Mean: &mean, Min: &mn, Max: &mx})
	}
	var states []haws.Change
	for t := now.AddDate(0, 0, -watch.StatesDays).Truncate(5 * time.Minute); t.Before(now); t = t.Add(5 * time.Minute) {
		v := "0.0"
		if on(t) {
			v = strconv.FormatFloat(watts, 'f', -1, 64)
		}
		states = append(states, haws.Change{State: v, At: t})
	}
	return rows, states
}

func power(id, name, unit string) haws.State {
	return haws.State{EntityID: id, State: "0", Attributes: map[string]any{"friendly_name": name, "device_class": "power",
		"state_class": "measurement", "unit_of_measurement": unit}}
}

// candidateSetup is watchSetup with the house's power sensors: two plugs with
// a daily routine (an aquarium, a pump), a fridge in a cycle (its blueprint
// is not there yet), a router always on, a solar power in kW and a plug Mira
// may not see; and the device profile reading them.
func candidateSetup(t *testing.T, log io.Writer) *env {
	t.Helper()
	e, _ := watchSetup(t)
	e.src.states = append(e.src.states,
		power("sensor.aquarium_leistung", "Aquarium", "W"),
		power("sensor.pumpe_leistung", "Teichpumpe", "W"),
		power("sensor.kuehlschrank_leistung", "Kühlschrank", "W"),
		power("sensor.router_leistung", "Router", "W"),
		power("sensor.solar_leistung", "Solar", "kW"),
		power("sensor.verborgen_leistung", "Verborgen", "W"))
	e.src.stats = map[string][]haws.StatRow{}
	e.src.history = map[string][]haws.Change{}
	past := func(id string, watts float64, on func(time.Time) bool) {
		e.src.stats[id], e.src.history[id] = plugPast(e.now, watts, on)
	}
	past("sensor.aquarium_leistung", 50, func(t time.Time) bool { return onAt(t, "07:40-11:40", "14:20-21:00") })
	past("sensor.pumpe_leistung", 80, func(t time.Time) bool { return onAt(t, "06:00-08:00") })
	past("sensor.kuehlschrank_leistung", 45, func(t time.Time) bool { return t.Minute() < 20 })
	past("sensor.router_leistung", 10, func(time.Time) bool { return true })
	past("sensor.verborgen_leistung", 50, func(t time.Time) bool { return onAt(t, "07:40-11:40") })
	snap := e.m.opts.Snapshot()
	exposed := map[string]bool{}
	for k, v := range snap.ExposedEntities {
		exposed[k] = v
	}
	for _, id := range []string{"sensor.aquarium_leistung", "sensor.pumpe_leistung", "sensor.kuehlschrank_leistung", "sensor.router_leistung", "sensor.solar_leistung"} {
		exposed[id] = true
	}
	snap.ExposedEntities = exposed
	opts := e.m.opts
	opts.Snapshot = func() structure.Snapshot { return snap }
	opts.Logger = slog.New(slog.NewTextHandler(log, nil))
	opts.Profiler = watch.NewProfiler(watch.ProfilerOptions{Recorder: e.src, Lang: i18n.DE, Location: berlin,
		Clock: func() time.Time { return e.now }, Logger: opts.Logger, Policy: &retry.Policy{Attempts: 1}})
	e.m = New(opts)
	return e
}

func candidatesTool(t *testing.T, m *Manager) (string, error) {
	t.Helper()
	for _, tool := range m.Tools() {
		if tool.Name == ToolWatchCandidates {
			if !tool.ReadOnly || tool.Skill != watch.Skill {
				t.Errorf("the tool: read-only %v, skill %q", tool.ReadOnly, tool.Skill)
			}
			return tool.Run(context.Background(), tools.Invocation{Args: json.RawMessage(`{}`)})
		}
	}
	t.Fatal("no tool watch_candidates")
	return "", nil
}

// watchWater makes the water meter watched by one of Mira's automations.
func watchWater(e *env) {
	e.ha.automations["mira_wasser"] = map[string]any{"alias": "Wasser läuft", "use_blueprint": map[string]any{
		"path": "mira/water_runs_on.yaml", "input": map[string]any{"meter": "sensor.wasserzaehler", "duration": 30.0, "message": "Um …"}}}
}

// CW-32 Mira lists the candidates of the house
func TestCW32_MiraListsTheCandidatesOfTheHouse(t *testing.T) {
	// Given the house has a water meter and a fake plug with a daily routine
	// (decision 38: the cycle's blueprint comes with step 7)
	e := candidateSetup(t, io.Discard)
	// When the user says "Gibt's Geräte, die du überwachen könntest?"
	out, err := candidatesTool(t, e.m)
	if err != nil {
		t.Fatal(err)
	}
	// Then Mira names the water meter and the plug with what she would watch on each
	for _, want := range []string{
		"Wasserzähler Keller (sensor.wasserzaehler): water_", "water_runs_on: Wasser läuft ohne Pause", "water_never_rests: Wasser ruht nie",
		"Aquarium (sensor.aquarium_leistung): Tagesablauf: an über 5 W (meist 50 W) jeden Tag zu diesen Zeiten: 07:40-11:40, 14:20-21:00",
		"Teichpumpe (sensor.pumpe_leistung): Tagesablauf", "(Blueprint daily_routine)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// Not the fridge (no blueprint for a cycle yet), the router, the sensor
	// in kW, the one Mira may not see.
	for _, not := range []string{"kuehlschrank", "router", "solar", "verborgen", "Nicht lesbar"} {
		if strings.Contains(out, not) {
			t.Errorf("%q named in:\n%s", not, out)
		}
	}
	// One read of the statistics for all power sensors in W Mira may see;
	// states only of the candidates.
	if len(e.src.statCalls) != 1 || len(e.src.statCalls[0]) != 4 || slices.Contains(e.src.statCalls[0], "sensor.solar_leistung") {
		t.Errorf("statistics read: %v", e.src.statCalls)
	}
}

// CW-33 What is watched or declined is not listed
func TestCW33_WhatIsWatchedOrDeclinedIsNotListed(t *testing.T) {
	// Given the water meter is watched already and the plug was declined
	e := candidateSetup(t, io.Discard)
	watchWater(e)
	if err := e.st.DeclineWatch(context.Background(), "sensor.aquarium_leistung", "daily_routine"); err != nil {
		t.Fatal(err)
	}
	// When the user asks for candidates
	out, err := candidatesTool(t, e.m)
	if err != nil {
		t.Fatal(err)
	}
	// Then neither is named
	if strings.Contains(out, "wasserzaehler") || strings.Contains(out, "aquarium") || !strings.Contains(out, "sensor.pumpe_leistung") {
		t.Errorf("listed:\n%s", out)
	}
	if !strings.Contains(out, "schon überwacht 1, abgelehnt 1") {
		t.Errorf("the skipped ones are not counted:\n%s", out)
	}
}

// CW-34 No candidates
func TestCW34_NoCandidates(t *testing.T) {
	// Given every candidate of the house is watched or declined
	e := candidateSetup(t, io.Discard)
	watchWater(e)
	for _, id := range []string{"sensor.aquarium_leistung", "sensor.pumpe_leistung"} {
		if err := e.st.DeclineWatch(context.Background(), id, "daily_routine"); err != nil {
			t.Fatal(err)
		}
	}
	// When the user asks for candidates
	out, err := candidatesTool(t, e.m)
	if err != nil {
		t.Fatal(err)
	}
	// Then Mira says she finds nothing new to watch
	if !strings.HasPrefix(out, "Nichts Neues zu überwachen") || strings.Contains(out, "Kandidaten für") {
		t.Errorf("answer:\n%s", out)
	}
}

// CW-35 A sensor that cannot be read
func TestCW35_ASensorThatCannotBeRead(t *testing.T) {
	// Given the recorder refuses the statistics of one power sensor
	var log bytes.Buffer
	e := candidateSetup(t, &log)
	delete(e.src.stats, "sensor.pumpe_leistung") // answered with nothing for it
	e.src.histErrs = map[string]error{"sensor.aquarium_leistung": &haws.Error{Command: "history/history_during_period", Code: "unknown_error", Message: "Database busy"}}
	// When the user asks for candidates
	out, err := candidatesTool(t, e.m)
	if err != nil {
		t.Fatal(err)
	}
	// Then the others are listed
	if !strings.Contains(out, "sensor.wasserzaehler") {
		t.Errorf("the water meter is not listed:\n%s", out)
	}
	// And the answer names the sensor that could not be read
	for _, want := range []string{"Nicht lesbar", "Teichpumpe (sensor.pumpe_leistung): the statistics of sensor.pumpe_leistung: home assistant answered with none",
		"Aquarium (sensor.aquarium_leistung): the states of sensor.aquarium_leistung", "Database busy"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	for _, want := range []string{"home assistant has no statistics of a power sensor", "entity=sensor.pumpe_leistung",
		"could not read the states of a candidate", "entity=sensor.aquarium_leistung"} {
		if !strings.Contains(log.String(), want) {
			t.Errorf("log misses %q:\n%s", want, log.String())
		}
	}
	// The statistics of all refused: the water meter is listed, the power
	// sensors named with Home Assistant's answer.
	log.Reset()
	e = candidateSetup(t, &log)
	e.src.statsErr = &haws.Error{Command: "recorder/statistics_during_period", Code: "invalid_format", Message: "Invalid statistic_id"}
	out, err = candidatesTool(t, e.m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "sensor.wasserzaehler") || !strings.Contains(out, "die 4 Leistungssensoren") || !strings.Contains(out, "Invalid statistic_id") {
		t.Errorf("answer:\n%s", out)
	}
	if !strings.Contains(log.String(), "could not read the statistics of the power sensors") {
		t.Errorf("log:\n%s", log.String())
	}
}
