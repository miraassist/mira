package history

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/ha/structure"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

var berlin, _ = time.LoadLocation("Europe/Berlin")

// fake is Home Assistant's recorder for the tests.
type fake struct {
	states  []haws.State
	changes map[string][]haws.Change
	stats   map[string]map[string][]haws.StatRow // period -> id -> rows
	asked   []string
	from    time.Time
}

func (f *fake) History(_ context.Context, id string, start, _ time.Time) ([]haws.Change, error) {
	f.asked = append(f.asked, "history "+id)
	f.from = start
	var out []haws.Change
	var before *haws.Change
	for _, c := range f.changes[id] {
		if c.At.After(start) {
			out = append(out, c)
		} else {
			cc := c
			before = &cc
		}
	}
	// As Home Assistant: the state at the start first, if recorded.
	if before != nil {
		out = append([]haws.Change{{State: before.State, At: start}}, out...)
	}
	return out, nil
}

func (f *fake) GetStates(_ context.Context, flt haws.StateFilter) ([]haws.State, error) {
	f.asked = append(f.asked, "states")
	var out []haws.State
	for _, s := range f.states {
		for _, id := range flt.EntityIDs {
			if s.EntityID == id {
				out = append(out, s)
			}
		}
	}
	return out, nil
}

func (f *fake) Statistics(_ context.Context, ids []string, start time.Time, period string) (map[string][]haws.StatRow, error) {
	f.asked = append(f.asked, "statistics "+period)
	out := map[string][]haws.StatRow{}
	for _, id := range ids {
		for _, r := range f.stats[period][id] {
			if !r.Start.Before(start.Truncate(time.Hour)) {
				out[id] = append(out[id], r)
			}
		}
	}
	return out, nil
}

func at(day, h, m int) time.Time                { return time.Date(2026, 11, day, h, m, 0, 0, berlin) }
func ptr(v float64) *float64                    { return &v }
func ch(state string, at time.Time) haws.Change { return haws.Change{State: state, At: at} }

// house is the exposed entities as the structure knows them: id, name and
// area.
func house(exposed []string) structure.Snapshot {
	snap := structure.Snapshot{EntityNames: map[string][]string{}, EntityArea: map[string]string{}, EntityLabel: map[string]string{},
		AreaNames: map[string]string{"bad": "Bad", "flur": "Flur", "keller": "Keller", "garten": "Garten"},
		AreaKeys:  map[string]string{"bad": "bad", "flur": "flur", "keller": "keller", "garten": "garten"}}
	names := map[string][2]string{
		"binary_sensor.fenster_bad":     {"Fenster Bad", "bad"},
		"binary_sensor.bewegung_flur":   {"Bewegung Flur", "flur"},
		"sensor.waschmaschine_leistung": {"Waschmaschine Leistung", "keller"},
		"sensor.wohnzimmer_temperatur":  {"Wohnzimmer Temperatur", "garten"},
		"sensor.zaehler":                {"Zähler", "keller"},
	}
	for _, id := range exposed {
		n := names[id]
		snap.EntityNames[id] = []string{id, strings.ToLower(n[0])}
		snap.EntityLabel[id], snap.EntityArea[id] = n[0], n[1]
	}
	return snap
}

func run(t *testing.T, f *fake, now time.Time, exposed []string, argsJSON string) string {
	t.Helper()
	snap := house(exposed)
	tool := New(Options{Lang: i18n.DE, Source: f, Structure: func() structure.Snapshot { return snap },
		Clock: func() time.Time { return now }, Location: berlin})
	out, err := tool.Run(context.Background(), tools.Invocation{Args: json.RawMessage(argsJSON)})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func window(state string, last time.Time) haws.State {
	return haws.State{EntityID: "binary_sensor.fenster_bad", State: state, LastChanged: last.UTC().Format(time.RFC3339Nano),
		Attributes: map[string]any{"friendly_name": "Fenster Bad", "device_class": "window"}}
}

var bath = []string{"binary_sensor.fenster_bad"}

// VL-01: a window's day becomes periods with durations and totals.
func TestVL01_AWindowsDayBecomesPeriods(t *testing.T) {
	f := &fake{states: []haws.State{window("off", at(3, 18, 20))}, changes: map[string][]haws.Change{
		"binary_sensor.fenster_bad": {ch("off", at(2, 22, 0)), ch("on", at(3, 7, 12)), ch("off", at(3, 7, 40)),
			ch("on", at(3, 18, 5)), ch("off", at(3, 18, 20))}}}
	out := run(t, f, at(3, 19, 30), bath, `{"name":"Fenster Bad","period":"today"}`)
	for _, want := range []string{"Fenster Bad (binary_sensor.fenster_bad, window), 00:00 bis jetzt:",
		"off 00:00–07:12 (7 h 12 min) · on 07:12–07:40 (28 min) · off 07:40–18:05 (10 h 25 min) · on 18:05–18:20 (15 min) · off 18:20–jetzt (1 h 10 min)",
		"on 43 min (2-mal)", "off 18 h 47 min (3-mal)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

// VL-02: the state at the start of the period counts from the start.
func TestVL02_TheStateAtTheStartCountsFromTheStart(t *testing.T) {
	f := &fake{states: []haws.State{window("off", at(3, 1, 30))}, changes: map[string][]haws.Change{
		"binary_sensor.fenster_bad": {ch("on", at(2, 22, 0)), ch("off", at(3, 1, 30))}}}
	out := run(t, f, at(3, 9, 0), bath, `{"name":"Fenster Bad","period":"today"}`)
	if !strings.Contains(out, "on 00:00–01:30 (1 h 30 min)") {
		t.Fatalf("%s", out)
	}
}

// VL-03: a long history is shortened, the totals are not, and the answer
// stays short.
func TestVL03_ALongHistoryIsShortened(t *testing.T) {
	var cs []haws.Change
	for i := 0; i < 80; i++ {
		s := "off"
		if i%2 == 1 {
			s = "on"
		}
		cs = append(cs, haws.Change{State: s, At: at(3, 0, 0).Add(time.Duration(i) * 10 * time.Minute)})
	}
	f := &fake{states: []haws.State{{EntityID: "binary_sensor.bewegung_flur", State: "on", LastChanged: cs[79].At.UTC().Format(time.RFC3339Nano),
		Attributes: map[string]any{"friendly_name": "Bewegung Flur", "device_class": "motion"}}},
		changes: map[string][]haws.Change{"binary_sensor.bewegung_flur": cs}}
	out := run(t, f, at(3, 19, 30), []string{"binary_sensor.bewegung_flur"}, `{"name":"binary_sensor.bewegung_flur"}`)
	if !strings.Contains(out, "… 71 weitere …") || !strings.Contains(out, "(40-mal)") || len([]rune(out)) > 1000 {
		t.Fatalf("%d characters:\n%s", len([]rune(out)), out)
	}
}

// VL-04: the last period ends with the live state, which the recorder has
// not written yet.
func TestVL04_TheLastPeriodEndsWithTheLiveState(t *testing.T) {
	f := &fake{states: []haws.State{window("on", at(3, 19, 29))}, changes: map[string][]haws.Change{
		"binary_sensor.fenster_bad": {ch("off", at(2, 22, 0))}}}
	out := run(t, f, at(3, 19, 30), bath, `{"name":"Fenster Bad"}`)
	if !strings.Contains(out, "on 19:29–jetzt (1 min)") {
		t.Fatalf("%s", out)
	}
}

// VL-05: an entity not exposed to Assist is answered like an unknown one,
// without asking Home Assistant.
func TestVL05_AnEntityNotExposedIsUnknown(t *testing.T) {
	f := &fake{states: []haws.State{{EntityID: "light.secret", State: "on"}}}
	out := run(t, f, at(3, 19, 30), bath, `{"name":"light.secret"}`)
	if out != textsFor(i18n.DE).unknown || len(f.asked) != 0 {
		t.Fatalf("%q, asked %v", out, f.asked)
	}
	if run(t, &fake{}, at(3, 19, 30), bath, `{"name":"Fenster Bad"}`) != out {
		t.Fatal("an exposed entity that does not exist answers otherwise")
	}
}

// VL-06: a period beyond 31 days or in the future is refused with the
// reason, without asking Home Assistant.
func TestVL06_APeriodTooLongOrInTheFutureIsRefused(t *testing.T) {
	f := &fake{}
	if out := run(t, f, at(3, 19, 30), bath, `{"name":"Fenster Bad","from":"2026-09-20T00:00"}`); out != textsFor(i18n.DE).tooLong {
		t.Fatalf("%q", out)
	}
	if out := run(t, f, at(3, 19, 30), bath, `{"name":"Fenster Bad","from":"2026-11-04T08:00"}`); out != textsFor(i18n.DE).future {
		t.Fatalf("%q", out)
	}
	if len(f.asked) != 0 {
		t.Fatalf("asked %v", f.asked)
	}
}

// VL-07: a history that begins later than asked says so, and the time
// before does not count as a state.
func TestVL07_AHistoryThatBeginsLaterSaysSo(t *testing.T) {
	f := &fake{states: []haws.State{window("off", at(1, 9, 0))}, changes: map[string][]haws.Change{
		"binary_sensor.fenster_bad": {ch("on", at(1, 8, 0)), ch("off", at(1, 9, 0))}}}
	out := run(t, f, at(3, 19, 30), bath, `{"name":"Fenster Bad","period":"last_31_days"}`)
	if !strings.Contains(out, "Verlauf erst ab 01.11. 08:00.") || !strings.Contains(out, "on 01.11. 08:00–09:00 (1 h)") {
		t.Fatalf("%s", out)
	}
}

// VL-08: yesterday runs from midnight to midnight local time, 25 hours on
// the day the clocks went back.
func TestVL08_TheFixedPeriodsFollowTheTimeZone(t *testing.T) {
	f := &fake{states: []haws.State{window("off", time.Date(2026, 10, 20, 9, 0, 0, 0, berlin))}, changes: map[string][]haws.Change{
		"binary_sensor.fenster_bad": {ch("off", time.Date(2026, 10, 20, 9, 0, 0, 0, berlin))}}}
	out := run(t, f, time.Date(2026, 10, 26, 1, 0, 0, 0, berlin), bath, `{"name":"Fenster Bad","period":"yesterday"}`)
	if !f.from.Equal(time.Date(2026, 10, 25, 0, 0, 0, 0, berlin)) || !strings.Contains(out, "off 1 Tg. 1 h (1-mal)") ||
		!strings.Contains(out, "25.10. 00:00 bis 26.10. 00:00:") {
		t.Fatalf("from %v\n%s", f.from, out)
	}
}

func power(class string) haws.State {
	return haws.State{EntityID: "sensor.waschmaschine_leistung", State: "0",
		Attributes: map[string]any{"friendly_name": "Waschmaschine Leistung", "state_class": class, "unit_of_measurement": "W"}}
}

var washer = []string{"sensor.waschmaschine_leistung"}

// VL-09: a power meter with a threshold becomes running periods, from the
// 5-minute statistics.
func TestVL09_APowerMeterWithAThresholdBecomesRunningPeriods(t *testing.T) {
	var rows []haws.StatRow
	for m := time.Duration(0); m < 24*time.Hour; m += 5 * time.Minute {
		w := 0.4
		if t := at(2, 0, 0).Add(m); !t.Before(at(2, 9, 5)) && t.Before(at(2, 10, 45)) {
			w = 500
		}
		rows = append(rows, haws.StatRow{Start: at(2, 0, 0).Add(m), Mean: ptr(w), Min: ptr(w), Max: ptr(w)})
	}
	f := &fake{states: []haws.State{power("measurement")}, stats: map[string]map[string][]haws.StatRow{
		"5minute": {"sensor.waschmaschine_leistung": rows}}}
	out := run(t, f, at(3, 19, 30), washer, `{"name":"Waschmaschine Leistung","period":"yesterday","above":5}`)
	if !strings.Contains(out, "über 5 W: 09:05–10:45 (1 h 40 min)") || !strings.Contains(out, "in 5-Minuten-Werten") {
		t.Fatalf("%s", out)
	}
}

// VL-10: a measurement without a threshold gives minimum, maximum and mean
// per day.
func TestVL10_AMeasurementWithoutAThresholdGivesDays(t *testing.T) {
	var rows []haws.StatRow
	for d := 0; d < 3; d++ {
		for h := 0; h < 24; h++ {
			v := 18 + float64(h)/4
			rows = append(rows, haws.StatRow{Start: at(1+d, h, 0), Mean: ptr(v), Min: ptr(v - 0.5), Max: ptr(v + 0.5)})
		}
	}
	f := &fake{states: []haws.State{{EntityID: "sensor.wohnzimmer_temperatur", State: "21.4",
		Attributes: map[string]any{"friendly_name": "Wohnzimmer Temperatur", "state_class": "measurement", "unit_of_measurement": "°C"}}},
		stats: map[string]map[string][]haws.StatRow{"hour": {"sensor.wohnzimmer_temperatur": rows}}}
	out := run(t, f, at(3, 23, 59), []string{"sensor.wohnzimmer_temperatur"}, `{"name":"Wohnzimmer Temperatur","from":"2026-11-01T00:00"}`)
	if !strings.Contains(out, "01.11.: Minimum 17,5, Maximum 24,2, Mittel 20,9 °C") || strings.Count(out, "Minimum") != 3 {
		t.Fatalf("%s", out)
	}
}

// VL-11: older measurements come hourly, and the answer says so.
func TestVL11_OlderMeasurementsComeHourly(t *testing.T) {
	var hourly, fine []haws.StatRow
	for d := 0; d < 21; d++ {
		day := time.Date(2026, 10, 14+d, 0, 0, 0, 0, berlin)
		for h := 0; h < 24; h++ {
			start := day.Add(time.Duration(h) * time.Hour)
			w := 0.4
			if h == 9 || h == 10 {
				w = 500
			}
			if start.Before(at(1, 0, 0)) {
				hourly = append(hourly, haws.StatRow{Start: start, Mean: ptr(w / 2), Max: ptr(w)})
				continue
			}
			for m := 0; m < 60; m += 5 {
				fine = append(fine, haws.StatRow{Start: start.Add(time.Duration(m) * time.Minute), Mean: ptr(w)})
			}
		}
	}
	f := &fake{states: []haws.State{power("measurement")}, stats: map[string]map[string][]haws.StatRow{
		"5minute": {"sensor.waschmaschine_leistung": fine}, "hour": {"sensor.waschmaschine_leistung": hourly}}}
	out := run(t, f, at(3, 19, 30), washer, `{"name":"Waschmaschine Leistung","from":"2026-10-14T00:00","above":5}`)
	for _, want := range []string{"bis 01.11. 00:00 in Stundenwerten, danach in 5-Minuten-Werten", "über 5 W, auf Stunden genau: 14.10. 09:00–11:00 (höchstens 2 h)",
		"03.11. 09:00–11:00 (2 h)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

// VL-12: a number without statistics is not read from the raw history.
func TestVL12_ANumberWithoutStatisticsIsNotReadRaw(t *testing.T) {
	f := &fake{states: []haws.State{{EntityID: "sensor.zaehler", State: "1234", Attributes: map[string]any{}}}}
	out := run(t, f, at(3, 19, 30), []string{"sensor.zaehler"}, `{"name":"Zähler"}`)
	if out != textsFor(i18n.DE).noStatistics || strings.Contains(strings.Join(f.asked, " "), "history") {
		t.Fatalf("%q, asked %v", out, f.asked)
	}
}

// The entity is found by its name, and two that fit are named with their
// areas instead of a guess.
func TestTheEntityIsFoundByItsNameAndAmbiguityIsNamed(t *testing.T) {
	f := &fake{states: []haws.State{window("off", at(3, 18, 20))}, changes: map[string][]haws.Change{
		"binary_sensor.fenster_bad": {ch("off", at(2, 22, 0))}}}
	snap := house(bath)
	snap.EntityNames["binary_sensor.fenster_keller"] = []string{"binary_sensor.fenster_keller", "fenster"}
	snap.EntityNames["binary_sensor.fenster_bad"] = append(snap.EntityNames["binary_sensor.fenster_bad"], "fenster")
	snap.EntityLabel["binary_sensor.fenster_keller"], snap.EntityArea["binary_sensor.fenster_keller"] = "Fenster", "keller"
	tool := New(Options{Lang: i18n.DE, Source: f, Structure: func() structure.Snapshot { return snap },
		Clock: func() time.Time { return at(3, 19, 30) }, Location: berlin})
	out, _ := tool.Run(context.Background(), tools.Invocation{Args: json.RawMessage(`{"name":"Fenster"}`)})
	if out != "Mehrere passen: Fenster Bad (Bad), Fenster (Keller). Nenne den Bereich." {
		t.Fatalf("%q", out)
	}
	out, _ = tool.Run(context.Background(), tools.Invocation{Args: json.RawMessage(`{"name":"Fenster","area":"Bad"}`)})
	if !strings.Contains(out, "Fenster Bad (binary_sensor.fenster_bad, window)") {
		t.Fatalf("%q", out)
	}
}

// A time of day alone is today.
func TestATimeOfDayAloneIsToday(t *testing.T) {
	f := &fake{states: []haws.State{window("off", at(3, 18, 20))}, changes: map[string][]haws.Change{
		"binary_sensor.fenster_bad": {ch("off", at(2, 22, 0))}}}
	run(t, f, at(3, 19, 30), bath, `{"name":"Fenster Bad","from":"08:00","to":"17:28"}`)
	if !f.from.Equal(at(3, 8, 0)) {
		t.Fatalf("from %v", f.from)
	}
}
