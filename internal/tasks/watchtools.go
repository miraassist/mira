package tasks

import (
	"context"
	"fmt"
	"slices"
	"strings"

	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
	"github.com/miraassist/mira/internal/watch"
)

var notSeen = map[i18n.Lang]string{
	i18n.DE: "%q nennt keinen Leistungssensor in W unter den Entitäten, die Mira sehen darf (nach id oder Name)",
	i18n.EN: "%q names no power sensor in W among the entities Mira may see (by id or name)",
}

var severalSeen = map[i18n.Lang]string{
	i18n.DE: "%q passt auf mehrere Leistungssensoren: %s; nimm die id",
	i18n.EN: "%q fits several power sensors: %s; take the id",
}

// sensorOf is what the watch's tools know of a state.
func sensorOf(s haws.State) watch.Sensor {
	str := func(k string) string {
		v, _ := s.Attributes[k].(string)
		return v
	}
	return watch.Sensor{ID: s.EntityID, Name: s.FriendlyName(), DeviceClass: str("device_class"), Unit: str("unit_of_measurement")}
}

// Sensor finds the entity the device profile is asked for among the ones
// Mira may see (exposed to Assist): by its id, else by a name Home
// Assistant knows for a power sensor in W (display name, alias, device
// name), as the field check "entities" finds one; the longest name wins.
func (m *Manager) Sensor(ctx context.Context, given string) (watch.Sensor, error) {
	states, err := m.exposedStates(ctx)
	if err != nil {
		return watch.Sensor{}, fmt.Errorf("the states of the entities Mira may see: %w", err)
	}
	for _, s := range states {
		if s.EntityID == given {
			return sensorOf(s), nil
		}
	}
	text := strings.ToLower(strings.ReplaceAll(given, "_", " "))
	names := m.opts.Snapshot().EntityNames
	var found []watch.Sensor
	longest := 0
	for _, s := range states {
		sensor := sensorOf(s)
		if !strings.HasPrefix(s.EntityID, "sensor.") || !sensor.Power() {
			continue
		}
		for _, n := range append([]string{strings.ToLower(sensor.Name)}, names[s.EntityID]...) {
			if len(n) < 3 || n == s.EntityID || !strings.Contains(text, n) {
				continue
			}
			switch {
			case len(n) > longest:
				found, longest = []watch.Sensor{sensor}, len(n)
			case len(n) == longest && !slices.ContainsFunc(found, func(f watch.Sensor) bool { return f.ID == sensor.ID }):
				found = append(found, sensor)
			}
		}
	}
	switch len(found) {
	case 0:
		return watch.Sensor{}, fmt.Errorf(i18n.Pick(notSeen, m.opts.Lang), given)
	case 1:
		return found[0], nil
	}
	var list []string
	for _, f := range found {
		list = append(list, f.Name+" ("+f.ID+")")
	}
	return watch.Sensor{}, fmt.Errorf(i18n.Pick(severalSeen, m.opts.Lang), given, strings.Join(list, ", "))
}

// watchTools are the watch's tools, with the skill that makes tasks:
// device_profile and watch_candidates. None without blueprints or a
// profiler.
func (m *Manager) watchTools() []tools.InternalTool {
	if m.opts.Watch == nil || m.opts.Profiler == nil {
		return nil
	}
	t := m.candidateTexts()
	return []tools.InternalTool{
		m.opts.Profiler.Tool(m.Sensor),
		{
			Name: ToolWatchCandidates, Description: t.desc, ReadOnly: true, Skill: watch.Skill,
			Schema: `{"type":"object","properties":{}}`,
			Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
				return m.Candidates(ctx)
			},
		},
	}
}

// checkNumbers checks a cycle's and a device run's numbers against the
// device profile of its power sensor (docs/spec/device-watch.md, decision
// 45): one read of its states of the last days. Without a profiler nothing
// is checked; a sensor whose states cannot be read refuses the draft with
// Home Assistant's answer.
func (m *Manager) checkNumbers(ctx context.Context, b watch.Blueprint, inputs map[string]any) error {
	kind := watch.KindOfBlueprint(b.Name)
	if kind == watch.KindNone || m.opts.Profiler == nil {
		return nil
	}
	entity, _ := inputs["power"].(string)
	pr, err := m.opts.Profiler.Numbers(ctx, entity, nil, kind)
	if err != nil {
		return err
	}
	return watch.CheckNumbers(b, inputs, pr, m.opts.Lang, m.opts.Location)
}
