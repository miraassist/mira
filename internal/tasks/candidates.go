package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/miraassist/mira/internal/excerpt"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/watch"
)

// ToolWatchCandidates lists the house's candidates for a watch
// (docs/spec/device-watch.md, decision 4: offering on request).
const ToolWatchCandidates = "watch_candidates"

type candidateTexts struct {
	desc, intro, none, unreadable, skipped, power string
}

var candidateCatalog = map[i18n.Lang]candidateTexts{
	i18n.DE: {
		desc:       "Listet die Geräte des Hauses, die Mira überwachen könnte (Kandidaten), mit dem, was sie an jedem überwachen würde: Wasserzähler und Leistungssensoren nach ihrem Geräteprofil; ohne die schon überwachten und die abgelehnten. Für „Gibt's Geräte, die du überwachen könntest?“. Liest die Statistik der letzten 30 Tage aller Leistungssensoren und die Zustände der letzten 10 Tage der Kandidaten: dauert einige Sekunden.",
		intro:      "Kandidaten für einen Wächter:",
		none:       "Nichts Neues zu überwachen: kein Gerät des Hauses, das ein Blueprint überwacht und das noch nicht überwacht oder abgelehnt ist.",
		unreadable: "Nicht lesbar, daher nicht geprüft:",
		skipped:    "Nicht aufgeführt: schon überwacht %d, abgelehnt %d.",
		power:      "die %d Leistungssensoren",
	},
	i18n.EN: {
		desc:       "Lists the house's devices Mira could watch (candidates), with what she would watch on each: water meters, and power sensors by their device profile; without the ones watched already and the declined ones. For \"Are there devices you could watch?\". Reads the statistics of the last 30 days of all power sensors and the states of the last 10 days of the candidates: takes a few seconds.",
		intro:      "Candidates for a watch:",
		none:       "Nothing new to watch: no device of the house a blueprint watches that is not watched or declined already.",
		unreadable: "Could not be read, so not checked:",
		skipped:    "Not listed: watched already %d, declined %d.",
		power:      "the %d power sensors",
	},
}

func (m *Manager) candidateTexts() candidateTexts { return i18n.Pick(candidateCatalog, m.opts.Lang) }

// watchedEntities are the entities Mira's automations watch with one of her
// blueprints: every entity among their inputs.
func watchedEntities(items []Item) map[string]bool {
	out := map[string]bool{}
	for _, it := range items {
		var cfg struct {
			Use struct {
				Path  string         `json:"path"`
				Input map[string]any `json:"input"`
			} `json:"use_blueprint"`
		}
		if json.Unmarshal(it.Automation, &cfg) != nil || !strings.HasPrefix(cfg.Use.Path, watch.Folder) {
			continue
		}
		for _, v := range cfg.Use.Input {
			if s, ok := v.(string); ok && strings.Contains(s, ".") && !strings.Contains(s, " ") {
				out[s] = true
			}
		}
	}
	return out
}

// Candidates is the request "Gibt's Geräte, die du überwachen könntest?"
// (docs/spec/device-watch.md, step 5): the entities Mira may see that a
// ready blueprint could watch, without the ones Mira's automations watch
// already and the declined ones. Water meters by their device class; power
// sensors (in W) by their kind, from one read of the hourly statistics of
// all of them, and their numbers from the states of each candidate. A
// sensor that cannot be read is logged and named in the answer; the others
// are listed.
func (m *Manager) Candidates(ctx context.Context) (string, error) {
	t := m.candidateTexts()
	if m.opts.Watch == nil || m.opts.Profiler == nil {
		return t.none, nil
	}
	states, err := m.exposedStates(ctx)
	if err != nil {
		return "", fmt.Errorf("the states of the entities Mira may see: %w", err)
	}
	items, err := m.List(ctx)
	if err != nil {
		return "", err
	}
	watched := watchedEntities(items)
	declines, err := m.opts.Store.WatchDeclines(ctx)
	if err != nil {
		return "", fmt.Errorf("the declined watches: %w", err)
	}
	declined := map[string]bool{}
	for _, d := range declines {
		declined[d.EntityID+" "+d.Blueprint] = true
	}
	sort.Slice(states, func(i, j int) bool { return states[i].EntityID < states[j].EntityID })

	type candidate struct {
		sensor watch.Sensor
		lines  []string
	}
	var found []candidate
	var unreadable []string
	skippedWatched, skippedDeclined := map[string]bool{}, map[string]bool{}
	byID := map[string]int{}
	add := func(s watch.Sensor, line string) {
		i, ok := byID[s.ID]
		if !ok {
			i = len(found)
			byID[s.ID] = i
			found = append(found, candidate{sensor: s})
		}
		found[i].lines = append(found[i].lines, line)
	}
	// open says whether a blueprint may be offered for a sensor, counting
	// what is left out.
	open := func(id, blueprint string) bool {
		switch {
		case watched[id]:
			skippedWatched[id] = true
			return false
		case declined[id+" "+blueprint]:
			skippedDeclined[id+" "+blueprint] = true
			return false
		}
		return true
	}
	powerBlueprints := map[string]bool{}
	for _, b := range m.opts.Watch.Available(ctx) {
		var in watch.Input
		for _, i := range b.Inputs {
			if i.Kind == "entity" {
				in = i
				break
			}
		}
		if in.DeviceClass == "power" {
			powerBlueprints[b.Name] = true
			continue
		}
		for _, s := range states {
			sensor := sensorOf(s)
			if domain, _, _ := strings.Cut(s.EntityID, "."); domain != in.Domain || sensor.DeviceClass != in.DeviceClass || !open(s.EntityID, b.Name) {
				continue
			}
			add(sensor, b.Name+": "+m.opts.Watch.About(b.Name))
		}
	}
	if len(powerBlueprints) > 0 {
		var power []watch.Sensor
		var ids []string
		for _, s := range states {
			sensor := sensorOf(s)
			if !strings.HasPrefix(s.EntityID, "sensor.") || !sensor.Power() {
				continue
			}
			if watched[s.EntityID] {
				skippedWatched[s.EntityID] = true
				continue
			}
			power = append(power, sensor)
			ids = append(ids, s.EntityID)
		}
		stats, missing, err := m.opts.Profiler.Statistics(ctx, ids)
		if err != nil {
			m.log.Warn("could not read the statistics of the power sensors; the candidates are listed without them", "sensors", len(ids), "error", err)
			unreadable = append(unreadable, fmt.Sprintf(t.power, len(ids))+": "+excerpt.Of(err.Error()))
			power = nil
		}
		for _, id := range missing {
			m.log.Warn("home assistant has no statistics of a power sensor; it is named in the candidates' answer as not read", "entity", id, "days", watch.RequestDays)
		}
		for _, s := range power {
			rows := stats[s.ID]
			if len(rows) == 0 {
				unreadable = append(unreadable, fmt.Sprintf("%s (%s): the statistics of %s: home assistant answered with none for the last %d days", s.Name, s.ID, s.ID, watch.RequestDays))
				continue
			}
			kind := m.opts.Profiler.KindOf(rows)
			b := kind.Blueprint()
			if !powerBlueprints[b] || !open(s.ID, b) {
				continue
			}
			pr, err := m.opts.Profiler.Numbers(ctx, s.ID, rows, kind)
			if err != nil {
				m.log.Warn("could not read the states of a candidate; it is named in the answer as not read", "entity", s.ID, "error", err)
				unreadable = append(unreadable, fmt.Sprintf("%s (%s): %v", s.Name, s.ID, err))
				continue
			}
			add(s, m.opts.Profiler.Describe(pr, watch.RequestDays))
		}
	}
	var b strings.Builder
	if len(found) == 0 {
		b.WriteString(t.none)
	} else {
		b.WriteString(t.intro)
		for _, c := range found {
			fmt.Fprintf(&b, "\n- %s (%s): %s", c.sensor.Name, c.sensor.ID, strings.Join(c.lines, " | "))
		}
	}
	if len(unreadable) > 0 {
		b.WriteString("\n" + t.unreadable)
		for _, u := range unreadable {
			b.WriteString("\n- " + u)
		}
	}
	if len(skippedWatched)+len(skippedDeclined) > 0 {
		b.WriteString("\n" + fmt.Sprintf(t.skipped, len(skippedWatched), len(skippedDeclined)))
	}
	m.log.Info("watch candidates listed", "candidates", len(found), "unreadable", len(unreadable),
		"watched", len(skippedWatched), "declined", len(skippedDeclined))
	return b.String(), nil
}
