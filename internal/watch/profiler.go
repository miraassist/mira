package watch

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/retry"
)

// Recorder is what the device profile reads of Home Assistant's recorder:
// the hourly statistics and one entity's states.
type Recorder interface {
	Statistics(ctx context.Context, ids []string, start time.Time, period string) (map[string][]haws.StatRow, error)
	History(ctx context.Context, id string, start, end time.Time) ([]haws.Change, error)
}

type ProfilerOptions struct {
	Recorder Recorder
	Lang     i18n.Lang
	Clock    func() time.Time
	// Location is the house's time zone: the hours and days of a daily
	// routine.
	Location *time.Location
	Logger   *slog.Logger
	// Policy replaces the package's Policy (tests: no waits).
	Policy *retry.Policy
}

// Profiler computes device profiles (docs/spec/device-watch.md, "The device
// profile"): never stored, computed when a watch is made or offered.
type Profiler struct {
	opts   ProfilerOptions
	log    *slog.Logger
	policy retry.Policy
}

func NewProfiler(opts ProfilerOptions) *Profiler {
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	if opts.Location == nil {
		opts.Location = time.Local
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	p := Policy
	if opts.Policy != nil {
		p = *opts.Policy
	}
	return &Profiler{opts: opts, log: opts.Logger.With("component", "watch"), policy: p}
}

// statistics reads the hourly statistics of ids since start, tried again by
// decision 17 (Home Assistant's refusal classifies itself).
func (p *Profiler) statistics(ctx context.Context, ids []string, start time.Time) (map[string][]haws.StatRow, error) {
	var out map[string][]haws.StatRow
	err := retry.Do(ctx, p.policy, func(ctx context.Context) error {
		var err error
		out, err = p.opts.Recorder.Statistics(ctx, ids, start, "hour")
		return err
	}, retry.Warn(p.log, "reading the statistics", p.policy, "sensors", len(ids), "first", ids[0]))
	return out, err
}

// history reads one entity's states since start, tried again by decision
// 17.
func (p *Profiler) history(ctx context.Context, id string, start, end time.Time) ([]haws.Change, error) {
	var out []haws.Change
	err := retry.Do(ctx, p.policy, func(ctx context.Context) error {
		var err error
		out, err = p.opts.Recorder.History(ctx, id, start, end)
		return err
	}, retry.Warn(p.log, "reading the states", p.policy, "entity", id))
	return out, err
}

// Profile is one power sensor's device profile: its kind from the hourly
// statistics of the last StatsDays days, its numbers from its states of
// the last StatesDays days. An error names the sensor and Home Assistant's
// answer, also when it answered with nothing for it.
func (p *Profiler) Profile(ctx context.Context, entity string) (Profile, error) {
	now := p.opts.Clock()
	stats, err := p.statistics(ctx, []string{entity}, now.AddDate(0, 0, -StatsDays))
	if err != nil {
		return Profile{}, fmt.Errorf("the statistics of %s: %w", entity, err)
	}
	if len(stats[entity]) == 0 {
		return Profile{}, fmt.Errorf("the statistics of %s: home assistant answered with none for the last %d days", entity, StatsDays)
	}
	return p.Numbers(ctx, entity, stats[entity], KindNone)
}

// Numbers is the device profile from statistics already read: the kind,
// unless given, and the numbers from the states of the last StatesDays
// days.
func (p *Profiler) Numbers(ctx context.Context, entity string, stats []haws.StatRow, kind Kind) (Profile, error) {
	now := p.opts.Clock()
	from := now.AddDate(0, 0, -StatesDays)
	changes, err := p.history(ctx, entity, from, now)
	if err != nil {
		return Profile{}, fmt.Errorf("the states of %s: %w", entity, err)
	}
	if len(changes) == 0 {
		return Profile{}, fmt.Errorf("the states of %s: home assistant answered with none for the last %d days", entity, StatesDays)
	}
	return Compute(entity, stats, kind, changes, from, now, p.opts.Location), nil
}

// Statistics reads the hourly statistics of the last RequestDays days for
// many sensors in one call (the request, step 5). missing are the ones Home
// Assistant answered with nothing for.
func (p *Profiler) Statistics(ctx context.Context, ids []string) (stats map[string][]haws.StatRow, missing []string, err error) {
	if len(ids) == 0 {
		return nil, nil, nil
	}
	stats, err = p.statistics(ctx, ids, p.opts.Clock().AddDate(0, 0, -RequestDays))
	if err != nil {
		return nil, nil, fmt.Errorf("the statistics of %d power sensors (%s): %w", len(ids), strings.Join(ids, ", "), err)
	}
	for _, id := range ids {
		if len(stats[id]) == 0 {
			missing = append(missing, id)
		}
	}
	return stats, missing, nil
}

// KindOf is the kind of statistics read by Statistics, in the house's time
// zone.
func (p *Profiler) KindOf(rows []haws.StatRow) Kind { return KindOf(rows, p.opts.Location) }

type profileTexts struct {
	routine, routineFew, routineNone        string
	cycle, cycleFew                         string
	run, runRow, runNone                    string
	none, never, enough, notEnough, typical string
	dateTime                                string
}

var profileCatalog = map[i18n.Lang]profileTexts{
	i18n.DE: {
		routine:     "Tagesablauf: an über %v W%s jeden Tag zu diesen Zeiten: %s; %d von %d vollen Tagen der letzten %d folgen ihnen (je höchstens %d min Abweichung).",
		routineFew:  "Tagesablauf nach den Stunden der letzten %d Tage, aber in den Zuständen der letzten %d Tage folgen nur %d von %d vollen Tagen den Zeiten %s (%d nötig): für einen Wächter seines Tagesablaufs fehlen die Zeiten.",
		routineNone: "Tagesablauf nach den Stunden der letzten %d Tage, aber in den Zuständen der letzten %d Tage keine wiederkehrenden Zeiten: für einen Wächter seines Tagesablaufs fehlen die Zeiten.",
		cycle:       "Takt: in den letzten %d Tagen %d Läufe über %v W%s, meist %d min, der längste %d min; Pausen meist %d min, die längste %d min.",
		cycleFew:    "Takt nach den Stunden der letzten %d Tage, aber Zustände erst seit %s (%d Tage nötig, %d ganze Läufe).",
		run:         "Gerätelauf: Spitze %.0f W in den Stunden der letzten %d Tage; in den letzten %d Tagen %d Läufe:",
		runRow:      " %s, %d min, Spitze %.0f W, längste Pause darin %d min;",
		runNone:     "Gerätelauf: Spitze %.0f W in den Stunden der letzten %d Tage, aber in den letzten %d Tagen kein ganzer Lauf: es fehlen Läufe; nach dem nächsten Lauf reicht es.",
		none:        "Kein Tagesablauf, kein Takt und kein Gerätelauf in den Stunden der letzten %d Tage (Spitze %.0f W): für einen Wächter seines Tagesablaufs fehlen die Zeiten.",
		never:       "In den Stunden der letzten %d Tage nie über %v W: kein Kandidat.",
		enough:      " Genug für einen Wächter (Blueprint %s).",
		notEnough:   " Noch zu wenig für einen Wächter.",
		typical:     " (meist %.0f W)",
		dateTime:    "02.01. 15:04",
	},
	i18n.EN: {
		routine:     "Daily routine: on above %v W%s every day at these times: %s; %d of %d full days of the last %d follow them (each off by at most %d min).",
		routineFew:  "Daily routine by the hours of the last %d days, but in the states of the last %d days only %d of %d full days follow the times %s (%d needed): for a watch of its daily routine the times are missing.",
		routineNone: "Daily routine by the hours of the last %d days, but no recurring times in the states of the last %d days: for a watch of its daily routine the times are missing.",
		cycle:       "Cycle: in the last %d days %d runs above %v W%s, mostly %d min, the longest %d min; pauses mostly %d min, the longest %d min.",
		cycleFew:    "Cycle by the hours of the last %d days, but states only since %s (%d days needed, %d whole runs).",
		run:         "Device run: peak %.0f W in the hours of the last %d days; in the last %d days %d runs:",
		runRow:      " %s, %d min, peak %.0f W, longest pause within %d min;",
		runNone:     "Device run: peak %.0f W in the hours of the last %d days, but no whole run in the last %d days: runs are missing; after its next run there is enough.",
		none:        "No daily routine, no cycle and no device run in the hours of the last %d days (peak %.0f W): for a watch of its daily routine the times are missing.",
		never:       "Never above %[2]v W in the hours of the last %[1]d days: no candidate.",
		enough:      " Enough for a watch (blueprint %s).",
		notEnough:   " Not enough for a watch yet.",
		typical:     " (mostly %.0f W)",
		dateTime:    "01/02 15:04",
	},
}

func minutes(d time.Duration) int { return int(d.Round(time.Minute) / time.Minute) }

func medianOf(ds []time.Duration) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2]
}

// Describe says a device profile for the model, in the house's language:
// the kind, the numbers, and whether they are enough or what is missing.
// statsDays is how many days of statistics it was found from.
func (p *Profiler) Describe(pr Profile, statsDays int) string {
	return Describe(pr, statsDays, p.opts.Lang, p.opts.Location)
}

// Describe says a device profile in l (see Profiler.Describe).
func Describe(pr Profile, statsDays int, l i18n.Lang, loc *time.Location) string {
	t := i18n.Pick(profileCatalog, l)
	typ := ""
	if pr.Typical > 0 {
		typ = fmt.Sprintf(t.typical, pr.Typical)
	}
	var b strings.Builder
	switch pr.Kind {
	case KindRoutine:
		switch {
		case pr.Enough:
			fmt.Fprintf(&b, t.routine, OnAbove, typ, Windows(pr.Windows), pr.Following, pr.Days, StatesDays, minutes(routineOffBy))
		case len(pr.Windows) > 0:
			fmt.Fprintf(&b, t.routineFew, statsDays, StatesDays, pr.Following, pr.Days, Windows(pr.Windows), routineDays)
		default:
			fmt.Fprintf(&b, t.routineNone, statsDays, StatesDays)
		}
	case KindCycle:
		if pr.Enough {
			var runs, pauses []time.Duration
			var longest, longestPause time.Duration
			for i, r := range pr.Runs {
				runs = append(runs, r.Duration)
				longest = max(longest, r.Duration)
				if i > 0 {
					pauses = append(pauses, r.PauseBefore)
					longestPause = max(longestPause, r.PauseBefore)
				}
			}
			fmt.Fprintf(&b, t.cycle, StatesDays, len(pr.Runs), OnAbove, typ, minutes(medianOf(runs)), minutes(longest),
				minutes(medianOf(pauses)), minutes(longestPause))
		} else {
			fmt.Fprintf(&b, t.cycleFew, statsDays, pr.StatesFrom.In(loc).Format(t.dateTime), cycleDays, len(pr.Runs))
		}
	case KindRun:
		if len(pr.Runs) == 0 {
			fmt.Fprintf(&b, t.runNone, pr.Peak, statsDays, StatesDays)
		} else {
			fmt.Fprintf(&b, t.run, pr.Peak, statsDays, StatesDays, len(pr.Runs))
			for _, r := range pr.Runs {
				fmt.Fprintf(&b, t.runRow, r.Start.In(loc).Format(t.dateTime), minutes(r.Duration), r.Peak, minutes(r.LongestPause))
			}
			s := strings.TrimSuffix(b.String(), ";") + "."
			b.Reset()
			b.WriteString(s)
		}
	default:
		if pr.Peak > OnAbove {
			fmt.Fprintf(&b, t.none, statsDays, pr.Peak)
		} else {
			fmt.Fprintf(&b, t.never, statsDays, OnAbove)
		}
		return b.String()
	}
	if pr.Enough {
		fmt.Fprintf(&b, t.enough, pr.Kind.Blueprint())
	} else if pr.Kind != KindRun || len(pr.Runs) > 0 {
		b.WriteString(t.notEnough)
	}
	return b.String()
}
