package watch

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	haws "github.com/miraassist/mira/internal/ha/ws"
)

// Kind is what a power sensor's device does as its device profile sees it
// (docs/spec/device-watch.md, decision 12).
type Kind string

const (
	// KindNone: no candidate, or nothing a blueprint watches.
	KindNone    Kind = ""
	KindRoutine Kind = "daily_routine"
	KindCycle   Kind = "cycle"
	KindRun     Kind = "device_run"
)

// Blueprint is the blueprint that watches a kind.
func (k Kind) Blueprint() string {
	switch k {
	case KindRoutine:
		return "daily_routine"
	case KindCycle:
		return "duty_cycle"
	case KindRun:
		return "device_run"
	}
	return ""
}

// The bounds of the device profile, tried read-only on the maintainer's
// devices on 2026-11-13 (docs/spec/device-watch.md, "The device profile").
const (
	// OnAbove is the power above which a device counts as on: the
	// blueprints' default of on_above.
	OnAbove = 5.0
	// routineClear: an hour of the day belongs to a daily routine when the
	// device was on in it on at least this share of the days, or off on at
	// least this share.
	routineClear = 0.9
	// cycleOnHours and cycleSwitching: a cycle is on in at least this
	// share of the hours, and switches on and off within at least that
	// share of them (its hourly min at or below OnAbove).
	cycleOnHours   = 0.9
	cycleSwitching = 0.5
	// runOnHours and runPeak: a device run is on in at most this share of
	// the hours and drew at least this much in one of them.
	runOnHours = 0.2
	runPeak    = 500.0
	// runJoin: a pause shorter than this belongs to the device run (a
	// programme's own pauses).
	runJoin = 30 * time.Minute
	// windowShortest: a daily routine's windows shorter than this are
	// dropped, gaps between them shorter than this are joined: the
	// blueprint's default tolerance.
	windowShortest = 15 * time.Minute
	// routineOffBy: a day follows the windows when it differs from them by
	// at most this much.
	routineOffBy = 60 * time.Minute
	// Enough: a daily routine needs this many days that follow its
	// windows, a cycle states of this many days, a device run one run.
	routineDays = 3
	cycleDays   = 2
	// The periods read: the hourly statistics for one profile and for the
	// request, the states.
	StatsDays   = 90
	RequestDays = 30
	StatesDays  = 10
)

// KindOf finds a power sensor's kind from its hourly statistics (mean, min,
// max), the hours of the day taken in loc. A sensor that went below
// -OnAbove is no device (the grid, a battery, an inverter feeding in; noise
// around 0 W is off); one never above OnAbove is no candidate.
func KindOf(rows []haws.StatRow, loc *time.Location) Kind {
	var n, on, switching int
	peak := 0.0
	type slot struct{ on, all int }
	var hours [24]slot
	for _, r := range rows {
		if r.Max == nil {
			continue
		}
		if r.Min != nil && *r.Min < -OnAbove {
			return KindNone
		}
		n++
		h := r.Start.In(loc).Hour()
		hours[h].all++
		if *r.Max > OnAbove {
			on++
			hours[h].on++
			if r.Min == nil || *r.Min <= OnAbove {
				switching++
			}
		}
		peak = math.Max(peak, *r.Max)
	}
	if on == 0 {
		return KindNone
	}
	routine, onSlots, offSlots := true, 0, 0
	for _, s := range hours {
		if s.all < routineDays {
			routine = false
			break
		}
		share := float64(s.on) / float64(s.all)
		switch {
		case share >= routineClear:
			onSlots++
		case share <= 1-routineClear:
			offSlots++
		default:
			routine = false
		}
	}
	share := float64(on) / float64(n)
	switch {
	case routine && onSlots > 0 && offSlots > 0:
		return KindRoutine
	case share >= cycleOnHours && float64(switching)/float64(n) >= cycleSwitching:
		return KindCycle
	case share <= runOnHours && peak >= runPeak:
		return KindRun
	}
	return KindNone
}

// Peak is the highest hourly max of the statistics.
func Peak(rows []haws.StatRow) float64 {
	peak := 0.0
	for _, r := range rows {
		if r.Max != nil {
			peak = math.Max(peak, *r.Max)
		}
	}
	return peak
}

// Run is a time the device was on: for a cycle one time above OnAbove, for
// a device run those times joined over its programme's pauses.
type Run struct {
	Start    time.Time
	Duration time.Duration
	// PauseBefore is the time since the run before; 0 for the first.
	PauseBefore time.Duration
	Peak        float64
	// LongestPause is the longest pause within a device run.
	LongestPause time.Duration
}

// Window is a time of the day the device is on, in minutes of the day; To
// before From passes midnight.
type Window struct{ From, To int }

func (w Window) String() string {
	return fmt.Sprintf("%02d:%02d-%02d:%02d", w.From/60, w.From%60, w.To/60%24, w.To%60)
}

// Windows says them as the blueprint's input reads them:
// "07:40-11:40, 14:20-21:00".
func Windows(ws []Window) string {
	parts := make([]string, len(ws))
	for i, w := range ws {
		parts[i] = w.String()
	}
	return strings.Join(parts, ", ")
}

// Profile is a device's profile: its kind and its numbers
// (docs/spec/device-watch.md, "The device profile").
type Profile struct {
	Entity string
	Kind   Kind
	// Peak is the highest hourly max of the statistics; StatsFrom the
	// first hour they hold.
	Peak      float64
	StatsFrom time.Time
	// StatesFrom is the first state of the last StatesDays days.
	StatesFrom time.Time
	// Typical is the median power while on, in the states.
	Typical float64
	// A daily routine: its windows, the full days of the states and how
	// many of them follow the windows.
	Windows         []Window
	Days, Following int
	// A cycle's runs and a device run's runs, whole within the states (a
	// run cut by their start or still on at their end is left out).
	Runs []Run
	// Enough says whether the numbers suffice to make a watch
	// (decision 14).
	Enough bool
}

// value reads a state as watts; ok is false for a state that is not a
// number (unavailable, unknown): it changes nothing.
func value(s string) (float64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f, err == nil && !math.IsNaN(f)
}

// segment is one time above OnAbove; cut when it began before the states
// did, open when it still runs at their end.
type segment struct {
	from, to  time.Time
	peak      float64
	cut, open bool
}

// segments are the times the states were above OnAbove between from and
// end. A state that is not a number keeps the one before it (decision 26).
func segments(changes []haws.Change, from, end time.Time) []segment {
	var out []segment
	var cur *segment
	for _, c := range changes {
		v, ok := value(c.State)
		if !ok {
			continue
		}
		at := c.At
		if at.Before(from) {
			at = from
		}
		switch {
		case v > OnAbove && cur == nil:
			cur = &segment{from: at, peak: v, cut: !at.After(from)}
		case v > OnAbove:
			cur.peak = math.Max(cur.peak, v)
		case cur != nil:
			cur.to = at
			out = append(out, *cur)
			cur = nil
		}
	}
	if cur != nil {
		cur.to, cur.open = end, true
		out = append(out, *cur)
	}
	return out
}

// runsOf turns segments into runs, joining the ones whose pause is shorter
// than join (0 joins none). Runs cut by the states' start or still on at
// their end are left out, as is the pause before the first whole one.
func runsOf(segs []segment, join time.Duration) []Run {
	var joined []segment
	var pauses []time.Duration // the longest pause within each joined one
	for _, s := range segs {
		if n := len(joined); n > 0 && join > 0 && s.from.Sub(joined[n-1].to) < join {
			last := &joined[n-1]
			pauses[n-1] = max(pauses[n-1], s.from.Sub(last.to))
			last.to, last.peak, last.open = s.to, math.Max(last.peak, s.peak), s.open
			continue
		}
		joined = append(joined, s)
		pauses = append(pauses, 0)
	}
	var out []Run
	for i, s := range joined {
		if s.cut || s.open {
			continue
		}
		r := Run{Start: s.from, Duration: s.to.Sub(s.from), Peak: s.peak, LongestPause: pauses[i]}
		if i > 0 {
			r.PauseBefore = s.from.Sub(joined[i-1].to)
		}
		out = append(out, r)
	}
	return out
}

// typical is the median of the values above OnAbove.
func typical(changes []haws.Change) float64 {
	var on []float64
	for _, c := range changes {
		if v, ok := value(c.State); ok && v > OnAbove {
			on = append(on, v)
		}
	}
	if len(on) == 0 {
		return 0
	}
	sort.Float64s(on)
	return on[len(on)/2]
}

// Compute is a device profile from its hourly statistics and its states
// between from and end, the days taken in loc. kind, when given, is the
// kind already found from the statistics (the request reads them for all
// sensors at once); else it is found here.
func Compute(entity string, stats []haws.StatRow, kind Kind, changes []haws.Change, from, end time.Time, loc *time.Location) Profile {
	p := Profile{Entity: entity, Kind: kind, Peak: Peak(stats)}
	if p.Kind == KindNone && len(stats) > 0 {
		p.Kind = KindOf(stats, loc)
	}
	for _, r := range stats {
		if r.Max != nil && (p.StatsFrom.IsZero() || r.Start.Before(p.StatsFrom)) {
			p.StatsFrom = r.Start
		}
	}
	sort.SliceStable(changes, func(i, j int) bool { return changes[i].At.Before(changes[j].At) })
	if len(changes) > 0 {
		p.StatesFrom = changes[0].At
		if p.StatesFrom.Before(from) {
			p.StatesFrom = from
		}
	}
	p.Typical = typical(changes)
	segs := segments(changes, p.StatesFrom, end)
	switch p.Kind {
	case KindRoutine:
		p.Windows, p.Days, p.Following = routine(changes, p.StatesFrom, end, loc)
		p.Enough = len(p.Windows) > 0 && p.Following >= routineDays
	case KindCycle:
		p.Runs = runsOf(segs, 0)
		p.Enough = len(p.Runs) > 0 && end.Sub(p.StatesFrom) >= cycleDays*24*time.Hour
	case KindRun:
		p.Runs = runsOf(segs, runJoin)
		p.Enough = len(p.Runs) > 0
	}
	return p
}

// routine finds a daily routine's windows in the states: per minute of the
// day, on when the device was on at that minute on more than half of the
// full days; gaps shorter than windowShortest are joined and windows shorter
// than it dropped. following counts the days that differ from the windows
// by at most routineOffBy.
func routine(changes []haws.Change, from, end time.Time, loc *time.Location) (windows []Window, days, following int) {
	const day = 24 * 60
	first := from.In(loc)
	start := time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, loc)
	if start.Before(from) {
		start = start.AddDate(0, 0, 1)
	}
	var masks [][day]bool
	i := 0
	on := false
	for d := start; !d.AddDate(0, 0, 1).After(end); d = d.AddDate(0, 0, 1) {
		var mask [day]bool
		next := d.AddDate(0, 0, 1)
		for t := d; t.Before(next); t = t.Add(time.Minute) {
			for i < len(changes) && !changes[i].At.After(t) {
				if v, ok := value(changes[i].State); ok {
					on = v > OnAbove
				}
				i++
			}
			lt := t.In(loc)
			mask[lt.Hour()*60+lt.Minute()] = on
		}
		masks = append(masks, mask)
	}
	days = len(masks)
	if days == 0 {
		return nil, 0, 0
	}
	var majority [day]bool
	for m := 0; m < day; m++ {
		n := 0
		for _, mask := range masks {
			if mask[m] {
				n++
			}
		}
		majority[m] = n*2 > days
	}
	short := int(windowShortest / time.Minute)
	// Join short gaps (runs of off between on), then drop short windows
	// (runs of on between off).
	for _, keep := range []bool{true, false} {
		for m := 0; m < day; {
			if majority[m] == keep {
				m++
				continue
			}
			n := m
			for n < day && majority[n] != keep {
				n++
			}
			// A run of !keep from m to n, between runs of keep (or the
			// day's edges, which are left as they are).
			if n-m < short && m > 0 && n < day {
				for k := m; k < n; k++ {
					majority[k] = keep
				}
			}
			m = n
		}
	}
	for m := 0; m < day; {
		if !majority[m] {
			m++
			continue
		}
		n := m
		for n < day && majority[n] {
			n++
		}
		windows = append(windows, Window{From: m, To: n})
		m = n
	}
	// A window through midnight is one.
	if len(windows) > 1 && windows[0].From == 0 && windows[len(windows)-1].To == day {
		last := windows[len(windows)-1]
		windows = append([]Window{{From: last.From, To: windows[0].To}}, windows[1:len(windows)-1]...)
	}
	// Drop what is still shorter than windowShortest (also at the day's
	// edges).
	var kept []Window
	for _, w := range windows {
		length := w.To - w.From
		if length < 0 {
			length += day
		}
		if length >= short {
			kept = append(kept, w)
		}
	}
	windows = kept
	var expected [day]bool
	for _, w := range windows {
		length := w.To - w.From
		if length <= 0 {
			length += day
		}
		for k := 0; k < length; k++ {
			expected[(w.From+k)%day] = true
		}
	}
	for _, mask := range masks {
		off := 0
		for m := 0; m < day; m++ {
			if mask[m] != expected[m] {
				off++
			}
		}
		if time.Duration(off)*time.Minute <= routineOffBy {
			following++
		}
	}
	return windows, days, following
}
