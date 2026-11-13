package watch

import (
	"fmt"
	"time"

	"github.com/miraassist/mira/internal/i18n"
)

// KindOfBlueprint is the kind of device a blueprint of a power sensor
// watches with numbers from its device profile; KindNone for the others (a
// daily routine's times are the user's, decision 43).
func KindOfBlueprint(name string) Kind {
	switch name {
	case "duty_cycle":
		return KindCycle
	case "device_run":
		return KindRun
	}
	return KindNone
}

type numberTexts struct {
	few, maxOn, maxOff, maxMinutes, offFor, heat string
}

var numberCatalog = map[i18n.Lang]numberTexts{
	i18n.DE: {
		few:        "für den Blueprint %s reicht der Verlauf von %s noch nicht (in den Zuständen der letzten %[3]d Tage %[4]d ganze Läufe, Zustände seit %[5]s): Mira wartet auf den nächsten Lauf, statt Zahlen zu raten",
		maxOn:      "max_on (%v min) muss länger sein als der längste normale Lauf von %s (%d min), sonst meldet sich Mira bei jedem langen Lauf",
		maxOff:     "max_off (%v min) muss länger sein als die längste normale Pause von %s (%d min), sonst meldet sich Mira bei jeder langen Pause",
		maxMinutes: "max_minutes (%v min) muss länger sein als der längste Lauf von %s (%d min)",
		offFor:     "off_for (%v min) muss länger sein als die längste Pause in einem Lauf von %s (%d min), sonst zerfällt ein Lauf in mehrere",
		heat:       "heat_above (%v W) muss unter der Spitze jedes Laufs von %s liegen (die niedrigste: %.0f W), sonst meldet sich Mira nach einem normalen Lauf",
	},
	i18n.EN: {
		few:        "for the blueprint %s the history of %s is not enough yet (%[3]d days of states, %[4]d whole runs, states since %[5]s): Mira waits for the next run instead of guessing numbers",
		maxOn:      "max_on (%v min) must be longer than the longest normal run of %s (%d min), else Mira tells of every long run",
		maxOff:     "max_off (%v min) must be longer than the longest normal pause of %s (%d min), else Mira tells of every long pause",
		maxMinutes: "max_minutes (%v min) must be longer than the longest run of %s (%d min)",
		offFor:     "off_for (%v min) must be longer than the longest pause within a run of %s (%d min), else a run falls apart into several",
		heat:       "heat_above (%v W) must be below the peak of every run of %s (the lowest: %.0f W), else Mira tells after a normal run",
	},
}

// CheckNumbers is the code's check of a cycle's and a device run's numbers
// against what the device profile saw (docs/spec/device-watch.md, "The tool
// device_profile"): a profile without enough is refused, and a number that
// a normal run or pause of the last days would already reach. inputs are
// the checked inputs (Check), the defaults filled from the blueprint.
func CheckNumbers(b Blueprint, inputs map[string]any, pr Profile, l i18n.Lang, loc *time.Location) error {
	t := i18n.Pick(numberCatalog, l)
	if !pr.Enough {
		since := "-"
		if !pr.StatesFrom.IsZero() {
			since = pr.StatesFrom.In(loc).Format("2006-01-02 15:04")
		}
		return fmt.Errorf(t.few, b.Name, pr.Entity, StatesDays, len(pr.Runs), since)
	}
	var longest, longestPause, longestWithin time.Duration
	lowestPeak := -1.0
	for i, r := range pr.Runs {
		longest = max(longest, r.Duration)
		longestWithin = max(longestWithin, r.LongestPause)
		if i > 0 {
			longestPause = max(longestPause, r.PauseBefore)
		}
		if lowestPeak < 0 || r.Peak < lowestPeak {
			lowestPeak = r.Peak
		}
	}
	over := func(name string, d time.Duration) (float64, bool) {
		v := valueOr(b, inputs, name)
		return v, v > float64(minutes(d))
	}
	switch b.Name {
	case "duty_cycle":
		if v, ok := over("max_on", longest); !ok {
			return fmt.Errorf(t.maxOn, num(v), pr.Entity, minutes(longest))
		}
		if v, ok := over("max_off", longestPause); !ok {
			return fmt.Errorf(t.maxOff, num(v), pr.Entity, minutes(longestPause))
		}
	case "device_run":
		if v, ok := over("max_minutes", longest); !ok {
			return fmt.Errorf(t.maxMinutes, num(v), pr.Entity, minutes(longest))
		}
		if v, ok := over("off_for", longestWithin); !ok {
			return fmt.Errorf(t.offFor, num(v), pr.Entity, minutes(longestWithin))
		}
		if heat := valueOr(b, inputs, "heat_above"); heat > 0 && heat >= lowestPeak {
			return fmt.Errorf(t.heat, num(heat), pr.Entity, lowestPeak)
		}
	}
	return nil
}
