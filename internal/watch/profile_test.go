package watch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/retry"
)

// The tests' house: a fixed time zone and a fixed now, so they pass at any
// time they run.
var (
	berlin, _ = time.LoadLocation("Europe/Berlin")
	testNow   = time.Date(2026, 11, 13, 19, 0, 0, 0, berlin)
)

func f(v float64) *float64 { return &v }

// hourly are statistics of days days before now, one row per hour, from
// row(hour start).
func hourly(days int, row func(t time.Time) (mean, min, max float64)) []haws.StatRow {
	var out []haws.StatRow
	start := testNow.Add(-time.Duration(days) * 24 * time.Hour).Truncate(time.Hour)
	for t := start; t.Before(testNow.Truncate(time.Hour)); t = t.Add(time.Hour) {
		mean, mn, mx := row(t)
		out = append(out, haws.StatRow{Start: t, Mean: f(mean), Min: f(mn), Max: f(mx)})
	}
	return out
}

// states are a plug's states every 5 minutes over the last StatesDays days,
// on (at watts) when on(t) says so, else 0 W.
func states(watts float64, on func(t time.Time) bool) []haws.Change {
	var out []haws.Change
	for t := testNow.AddDate(0, 0, -StatesDays); t.Before(testNow); t = t.Add(5 * time.Minute) {
		v := "0.0"
		if on(t) {
			v = strconv.FormatFloat(watts, 'f', -1, 64)
		}
		out = append(out, haws.Change{State: v, At: t})
	}
	return out
}

// inWindows says whether t's time of day lies in one of the windows
// ("07:40-11:40").
func inWindows(t time.Time, windows ...string) bool {
	m := t.In(berlin).Hour()*60 + t.In(berlin).Minute()
	for _, w := range windows {
		var h1, m1, h2, m2 int
		fmt.Sscanf(w, "%d:%d-%d:%d", &h1, &m1, &h2, &m2)
		from, to := h1*60+m1, h2*60+m2
		if from <= to && m >= from && m < to || from > to && (m >= from || m < to) {
			return true
		}
	}
	return false
}

// fakeRecorder plays the recorder's statistics and history.
type fakeRecorder struct {
	stats     map[string][]haws.StatRow
	states    map[string][]haws.Change
	statsErrs []error // the answers of the next statistics calls
	histErrs  map[string]error
	statCalls [][]string
	histCalls []string
}

func (r *fakeRecorder) Statistics(_ context.Context, ids []string, start time.Time, period string) (map[string][]haws.StatRow, error) {
	r.statCalls = append(r.statCalls, ids)
	if len(r.statsErrs) > 0 {
		err := r.statsErrs[0]
		r.statsErrs = r.statsErrs[1:]
		if err != nil {
			return nil, err
		}
	}
	out := map[string][]haws.StatRow{}
	for _, id := range ids {
		for _, row := range r.stats[id] {
			if !row.Start.Before(start) {
				out[id] = append(out[id], row)
			}
		}
	}
	return out, nil
}

func (r *fakeRecorder) History(_ context.Context, id string, start, end time.Time) ([]haws.Change, error) {
	r.histCalls = append(r.histCalls, id)
	if err := r.histErrs[id]; err != nil {
		return nil, err
	}
	var out []haws.Change
	for _, c := range r.states[id] {
		if !c.At.Before(start) && c.At.Before(end) {
			out = append(out, c)
		}
	}
	return out, nil
}

func newProfiler(rec Recorder, log *bytes.Buffer) *Profiler {
	return NewProfiler(ProfilerOptions{Recorder: rec, Lang: i18n.DE, Location: berlin,
		Clock: func() time.Time { return testNow }, Logger: slog.New(slog.NewTextHandler(log, nil)),
		Policy: &retry.Policy{Attempts: 3}})
}

// A fridge: on in every hour, switching within it, its mean far below its
// peak.
func cycleStats(days int) []haws.StatRow {
	return hourly(days, func(time.Time) (float64, float64, float64) { return 20, 0, 48 })
}

// CW-23 The device profile finds a cycle
func TestCW23_TheDeviceProfileFindsACycle(t *testing.T) {
	// Given a power sensor on at most hours with a mean far below its peak
	rec := &fakeRecorder{stats: map[string][]haws.StatRow{"sensor.kuehlschrank": cycleStats(StatsDays)}}
	// And ten days of states with runs of 55 to 80 minutes every two hours
	runs := []time.Duration{55, 60, 65, 70, 75, 80}
	var changes []haws.Change
	i := 0
	for at := testNow.AddDate(0, 0, -StatesDays).Add(30 * time.Minute); at.Before(testNow); at = at.Add(2 * time.Hour) {
		changes = append(changes, haws.Change{State: "47.5", At: at}, haws.Change{State: "0.0", At: at.Add(runs[i%len(runs)] * time.Minute)})
		i++
	}
	rec.states = map[string][]haws.Change{"sensor.kuehlschrank": append([]haws.Change{{State: "0.0", At: testNow.AddDate(0, 0, -StatesDays)}}, changes...)}
	var log bytes.Buffer
	// When its device profile is computed
	p, err := newProfiler(rec, &log).Profile(context.Background(), "sensor.kuehlschrank")
	if err != nil {
		t.Fatal(err)
	}
	// Then its kind is cycle with the longest run of 80 minutes and its longest pause
	if p.Kind != KindCycle || !p.Enough {
		t.Fatalf("profile: %+v", p)
	}
	text := Describe(p, StatsDays, i18n.DE, berlin)
	if !strings.Contains(text, "der längste 80 min") || !strings.Contains(text, "die längste 65 min") || !strings.Contains(text, "duty_cycle") {
		t.Errorf("described: %s", text)
	}
	if len(rec.statCalls) != 1 || len(rec.statCalls[0]) != 1 || len(rec.histCalls) != 1 {
		t.Errorf("calls: statistics %v, history %v", rec.statCalls, rec.histCalls)
	}
}

// A dishwasher: off in most hours, a peak of 1.9 kW once a week.
func runStats() []haws.StatRow {
	return hourly(StatsDays, func(t time.Time) (float64, float64, float64) {
		if t.In(berlin).Weekday() == time.Sunday && (t.In(berlin).Hour() == 19 || t.In(berlin).Hour() == 20) {
			return 700, 0, 1900
		}
		return 0, 0, 1
	})
}

// CW-24 The device profile finds a run with too few in the last days
func TestCW24_TheDeviceProfileFindsARunWithTooFewInTheLastDays(t *testing.T) {
	// Given a power sensor with an hourly peak above 500 W now and then for months
	rec := &fakeRecorder{stats: map[string][]haws.StatRow{"sensor.spuelmaschine": runStats()}}
	// And no whole run in the last ten days (decision 33: one run is enough)
	rec.states = map[string][]haws.Change{"sensor.spuelmaschine": states(0, func(time.Time) bool { return false })}
	// When its device profile is computed
	p, err := newProfiler(rec, &bytes.Buffer{}).Profile(context.Background(), "sensor.spuelmaschine")
	if err != nil {
		t.Fatal(err)
	}
	// Then its kind is device run and it says that runs are missing
	if p.Kind != KindRun || p.Enough {
		t.Fatalf("profile: %+v", p)
	}
	if text := Describe(p, StatsDays, i18n.DE, berlin); !strings.Contains(text, "es fehlen Läufe") || !strings.Contains(text, "1900 W") {
		t.Errorf("described: %s", text)
	}
	// One run of 100 minutes with a programme's pause of 15 minutes, a
	// blip 40 minutes after it: one run, enough.
	start := testNow.Add(-50 * time.Hour)
	rec.states["sensor.spuelmaschine"] = []haws.Change{
		{State: "0", At: testNow.AddDate(0, 0, -StatesDays)},
		{State: "60", At: start}, {State: "1900", At: start.Add(10 * time.Minute)}, {State: "1", At: start.Add(40 * time.Minute)},
		{State: "55", At: start.Add(55 * time.Minute)}, {State: "unavailable", At: start.Add(70 * time.Minute)},
		{State: "0", At: start.Add(100 * time.Minute)}, {State: "13", At: start.Add(140 * time.Minute)}, {State: "0", At: start.Add(145 * time.Minute)},
	}
	p, err = newProfiler(rec, &bytes.Buffer{}).Profile(context.Background(), "sensor.spuelmaschine")
	if err != nil {
		t.Fatal(err)
	}
	if !p.Enough || len(p.Runs) != 2 || p.Runs[0].Duration != 100*time.Minute || p.Runs[0].LongestPause != 15*time.Minute || p.Runs[0].Peak != 1900 {
		t.Errorf("runs: %+v", p.Runs)
	}
	if text := Describe(p, StatsDays, i18n.DE, berlin); !strings.Contains(text, "11.11. 17:00, 100 min, Spitze 1900 W, längste Pause darin 15 min") {
		t.Errorf("described: %s", text)
	}
}

// CW-25 The recorder refuses
func TestCW25_TheRecorderRefuses(t *testing.T) {
	// Given Home Assistant refuses the statistics for a sensor
	refusal := &haws.Error{Command: "recorder/statistics_during_period", Code: "invalid_format", Message: "Invalid statistic_id"}
	rec := &fakeRecorder{statsErrs: []error{refusal}}
	var log bytes.Buffer
	// When its device profile is computed
	_, err := newProfiler(rec, &log).Profile(context.Background(), "sensor.steckdose")
	// Then the tool's error names the sensor and Home Assistant's answer
	if err == nil || !strings.Contains(err.Error(), "sensor.steckdose") || !strings.Contains(err.Error(), "Invalid statistic_id") {
		t.Fatalf("error: %v", err)
	}
	// A refusal is not tried again (decision 17).
	if len(rec.statCalls) != 1 {
		t.Errorf("statistics read %d times", len(rec.statCalls))
	}
	// A lost connection is tried again, with its trace.
	rec = &fakeRecorder{statsErrs: []error{errors.New("connection lost")}, stats: map[string][]haws.StatRow{"sensor.steckdose": cycleStats(StatsDays)},
		states: map[string][]haws.Change{"sensor.steckdose": states(48, func(t time.Time) bool { return t.Minute() < 20 })}}
	if _, err := newProfiler(rec, &log).Profile(context.Background(), "sensor.steckdose"); err != nil {
		t.Fatal(err)
	}
	if len(rec.statCalls) != 2 || !strings.Contains(log.String(), "reading the statistics failed, trying again") {
		t.Errorf("calls %d, log: %s", len(rec.statCalls), log.String())
	}
	// Nothing for the sensor is an error too, naming it.
	rec = &fakeRecorder{}
	if _, err := newProfiler(rec, &log).Profile(context.Background(), "sensor.leer"); err == nil || !strings.Contains(err.Error(), "sensor.leer") || !strings.Contains(err.Error(), "none") {
		t.Errorf("error: %v", err)
	}
	// And states refused: the error names the sensor and the answer.
	rec = &fakeRecorder{stats: map[string][]haws.StatRow{"sensor.steckdose": cycleStats(StatsDays)},
		histErrs: map[string]error{"sensor.steckdose": &haws.Error{Command: "history/history_during_period", Code: "unauthorized", Message: "Unauthorized"}}}
	if _, err := newProfiler(rec, &log).Profile(context.Background(), "sensor.steckdose"); err == nil || !strings.Contains(err.Error(), "the states of sensor.steckdose") || !strings.Contains(err.Error(), "Unauthorized") {
		t.Errorf("error: %v", err)
	}
}

// The kinds from the hourly statistics, with the bounds tried on the
// maintainer's devices.
func TestKindOf(t *testing.T) {
	aquarium := func(t time.Time) (float64, float64, float64) {
		if inWindows(t, "07:00-12:00", "14:00-21:00") {
			return 50, 48, 52
		}
		return 0, 0, 0
	}
	cases := []struct {
		name string
		rows []haws.StatRow
		want Kind
	}{
		{"daily routine", hourly(30, aquarium), KindRoutine},
		{"cycle", cycleStats(30), KindCycle},
		{"device run", runStats(), KindRun},
		{"never on", hourly(30, func(time.Time) (float64, float64, float64) { return 0, 0, 2 }), KindNone},
		// A router: on all the time, never switching.
		{"constant", hourly(30, func(time.Time) (float64, float64, float64) { return 10, 9, 11 }), KindNone},
		// The grid's power, below 0 W while the solar power feeds in: no
		// device.
		{"below zero", hourly(30, func(t time.Time) (float64, float64, float64) {
			if inWindows(t, "08:00-16:00") {
				return -300, -600, 0
			}
			return 300, 200, 600
		}), KindNone},
		// A wallbox reading -0.1 W now and then: noise, off.
		{"noise below zero", hourly(30, func(t time.Time) (float64, float64, float64) {
			if t.YearDay()%7 == 0 && inWindows(t, "10:00-14:00") {
				return 5000, 0, 11000
			}
			return 0, -0.1, 0
		}), KindRun},
		// Solar power: on in daylight, but its edges differ from day to day.
		{"solar", hourly(30, func(t time.Time) (float64, float64, float64) {
			sunset := 16 + t.YearDay()%2
			if h := t.In(berlin).Hour(); h >= 8 && h < sunset {
				return 300, 0, 600
			}
			return 0, 0, 0
		}), KindNone},
		// A light on now and then, below the peak of a run.
		{"light", hourly(30, func(t time.Time) (float64, float64, float64) {
			if t.YearDay()%3 == 0 && t.In(berlin).Hour() == 20 {
				return 30, 0, 60
			}
			return 0, 0, 0
		}), KindNone},
	}
	for _, c := range cases {
		if got := KindOf(c.rows, berlin); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// A daily routine's windows from ten days of states: one day off early,
// short blips and a short gap left out, one window through midnight.
func TestRoutineWindows(t *testing.T) {
	changes := states(50, func(at time.Time) bool {
		l := at.In(berlin)
		if l.Day() == 12 && l.Hour() >= 18 { // off at 18:00 instead of 21:00
			return inWindows(at, "07:40-11:40")
		}
		if inWindows(at, "03:00-03:05") { // a blip
			return true
		}
		if inWindows(at, "15:00-15:10") { // a short gap
			return false
		}
		return inWindows(at, "07:40-11:40", "14:20-21:00", "23:00-01:00")
	})
	p := Compute("sensor.aquarium", hourly(30, func(time.Time) (float64, float64, float64) { return 0, 0, 0 }), KindRoutine,
		changes, testNow.AddDate(0, 0, -StatesDays), testNow, berlin)
	if got := Windows(p.Windows); got != "23:00-01:00, 07:40-11:40, 14:20-21:00" {
		t.Errorf("windows: %s", got)
	}
	if p.Days != 9 || p.Following != 8 || !p.Enough || p.Typical != 50 {
		t.Errorf("profile: %+v", p)
	}
	// Without recurring times: no windows, not enough.
	n := 0
	changes = states(50, func(at time.Time) bool { n++; return n%37 < 3 })
	p = Compute("sensor.aquarium", nil, KindRoutine, changes, testNow.AddDate(0, 0, -StatesDays), testNow, berlin)
	if len(p.Windows) != 0 || p.Enough {
		t.Errorf("profile: %+v", p)
	}
	if text := Describe(p, StatsDays, i18n.DE, berlin); !strings.Contains(text, "keine wiederkehrenden Zeiten") {
		t.Errorf("described: %s", text)
	}
}
