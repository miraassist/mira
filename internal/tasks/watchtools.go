package tasks

import (
	"context"
	"fmt"

	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
	"github.com/miraassist/mira/internal/watch"
)

var notSeen = map[i18n.Lang]string{
	i18n.DE: "%s gehört nicht zu den Entitäten, die Mira sehen darf",
	i18n.EN: "%s is not among the entities Mira may see",
}

// sensorOf is what the watch's tools know of a state.
func sensorOf(s haws.State) watch.Sensor {
	str := func(k string) string {
		v, _ := s.Attributes[k].(string)
		return v
	}
	return watch.Sensor{ID: s.EntityID, Name: s.FriendlyName(), DeviceClass: str("device_class"), Unit: str("unit_of_measurement")}
}

// Sensor looks up an entity Mira may see (exposed to Assist), for the
// device profile.
func (m *Manager) Sensor(ctx context.Context, id string) (watch.Sensor, error) {
	if !m.opts.Snapshot().ExposedEntities[id] {
		return watch.Sensor{}, fmt.Errorf(i18n.Pick(notSeen, m.opts.Lang), id)
	}
	states, err := m.opts.Source.GetStates(ctx, haws.StateFilter{EntityIDs: []string{id}})
	if err != nil {
		return watch.Sensor{}, fmt.Errorf("the state of %s: %w", id, err)
	}
	for _, s := range states {
		if s.EntityID == id {
			return sensorOf(s), nil
		}
	}
	return watch.Sensor{}, fmt.Errorf("the state of %s: home assistant answered without it", id)
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
