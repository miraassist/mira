package watch

import (
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/i18n"
)

// A cycle's and a device run's numbers are checked against what the device
// profile saw: a number a normal run or pause would reach is refused, as is
// a profile without enough.
func TestCheckNumbers(t *testing.T) {
	from := testNow.AddDate(0, 0, -StatesDays)
	// A fridge: on 20 minutes of every hour, off 40.
	fridge := Compute("sensor.k", nil, KindCycle, states(45, func(t time.Time) bool { return t.Minute() < 20 }), from, testNow, berlin)
	// A dishwasher: one run of two hours three days ago, at 2000 W, with a
	// pause of 10 minutes in it.
	day := testNow.AddDate(0, 0, -3)
	start := time.Date(day.Year(), day.Month(), day.Day(), 19, 0, 0, 0, berlin)
	dish := Compute("sensor.s", nil, KindRun, states(2000, func(t time.Time) bool {
		return !t.Before(start) && t.Before(start.Add(2*time.Hour)) && (t.Before(start.Add(time.Hour)) || !t.Before(start.Add(70*time.Minute)))
	}), from, testNow, berlin)
	if !fridge.Enough || !dish.Enough {
		t.Fatalf("profiles: %+v %+v", fridge, dish)
	}
	cycle, _ := ByName("duty_cycle")
	run, _ := ByName("device_run")
	for _, c := range []struct {
		b      Blueprint
		inputs map[string]any
		pr     Profile
		want   string
	}{
		{cycle, map[string]any{"max_on": 60.0, "max_off": 120.0}, fridge, ""},
		{cycle, map[string]any{"max_on": 20.0, "max_off": 120.0}, fridge, "max_on (20 min) muss länger sein als der längste normale Lauf von sensor.k (20 min)"},
		{cycle, map[string]any{"max_on": 60.0, "max_off": 30.0}, fridge, "max_off (30 min) muss länger sein als die längste normale Pause von sensor.k (40 min)"},
		{run, map[string]any{"max_minutes": 180.0, "heat_above": 1500.0}, dish, ""},
		{run, map[string]any{"max_minutes": 100.0}, dish, "max_minutes (100 min) muss länger sein als der längste Lauf von sensor.s (120 min)"},
		{run, map[string]any{"max_minutes": 180.0, "off_for": 5.0}, dish, "off_for (5 min) muss länger sein als die längste Pause in einem Lauf von sensor.s (10 min)"},
		{run, map[string]any{"max_minutes": 180.0, "heat_above": 2500.0}, dish, "heat_above (2500 W) muss unter der Spitze jedes Laufs von sensor.s liegen (die niedrigste: 2000 W)"},
		{run, map[string]any{"max_minutes": 180.0}, Profile{Entity: "sensor.s", Kind: KindRun}, "reicht der Verlauf von sensor.s noch nicht (in den Zuständen der letzten 10 Tage 0 ganze Läufe"},
	} {
		err := CheckNumbers(c.b, c.inputs, c.pr, i18n.DE, berlin)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s %v: %v", c.b.Name, c.inputs, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s %v: %v, want %q", c.b.Name, c.inputs, err, c.want)
		}
	}
	if KindOfBlueprint("daily_routine") != KindNone || KindOfBlueprint("duty_cycle") != KindCycle || KindOfBlueprint("device_run") != KindRun {
		t.Error("KindOfBlueprint")
	}
}
