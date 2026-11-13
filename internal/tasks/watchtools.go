package tasks

import (
	"context"
	"encoding/json"
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

// Watched are the entities Mira's watches watch with one of her blueprints
// (decision 39): a waiting offer of one of them ends.
func (m *Manager) Watched(ctx context.Context) (map[string]bool, error) {
	items, err := m.List(ctx)
	if err != nil {
		return nil, err
	}
	return watchedEntities(items), nil
}

// ToolWatchException sets or removes the label "Mira: nicht überwachen" on
// the user's word (docs/spec/group-watch.md, A3 outside a drafting, and D).
const ToolWatchException = "watch_exception"

var exceptionTexts = map[i18n.Lang]struct{ desc, set, removed, notFound, several string }{
	i18n.DE: {
		desc:     "Setzt das Label „Mira: nicht überwachen“ auf eine Lampe, ein Fenster, einen Melder oder Temperatursensor (watch false: dann in keinem Wächter für Gruppen und nicht in „Ist alles zu?“) oder nimmt es wieder ab (watch true, „Überwach die Terrasse wieder“). Nur nach dem Ja des Nutzers. Beim Einrichten eines Wächters nicht nötig: dort nimmt der Entwurf die Ausnahmen mit. entity: die id oder der Name. Ein Aufruf an Home Assistant, Millisekunden.",
		set:      "%s trägt jetzt das Label „Mira: nicht überwachen“: in keinem Wächter für Gruppen mehr.",
		removed:  "%s wird wieder überwacht: das Label „Mira: nicht überwachen“ ist ab.",
		notFound: "%q nennt keine Lampe, kein Fenster, keine Tür, keinen Melder und keinen Temperatursensor des Hauses (nach id oder Name)",
		several:  "%q passt auf mehrere: %s; nimm die id",
	},
	i18n.EN: {
		desc:     "Puts the label \"Mira: don't watch\" on a light, a window, a sensor (watch false: then in no group watch and not in \"Is everything closed?\") or takes it off again (watch true, \"watch the terrace again\"). Only after the user's yes. Not needed when a watch is made: the draft takes its exceptions along. entity: the id or the name. One call to Home Assistant, milliseconds.",
		set:      "%s now carries the label \"Mira: don't watch\": in no group watch any more.",
		removed:  "%s is watched again: the label \"Mira: don't watch\" is off.",
		notFound: "%q names no light, window, door, sensor or temperature sensor of the house (by id or name)",
		several:  "%q fits several: %s; take the id",
	},
}

// exceptionTool is watch_exception; none without blueprints.
func (m *Manager) exceptionTool() []tools.InternalTool {
	if m.opts.Watch == nil {
		return nil
	}
	t := i18n.Pick(exceptionTexts, m.opts.Lang)
	return []tools.InternalTool{{
		Name: ToolWatchException, Description: t.desc, Skill: watch.Skill,
		Schema: `{"type":"object","properties":{"entity":{"type":"string"},"watch":{"type":"boolean"}},"required":["entity","watch"]}`,
		Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
			var args struct {
				Entity string `json:"entity"`
				Watch  bool   `json:"watch"`
			}
			if err := json.Unmarshal(inv.Args, &args); err != nil {
				return "", fmt.Errorf("%s: %w", ToolWatchException, err)
			}
			id, name, err := m.watchedMember(ctx, strings.TrimSpace(args.Entity))
			if err != nil {
				return "", err
			}
			if err := m.opts.Watch.SetLabel(ctx, id, !args.Watch); err != nil {
				return "", fmt.Errorf("%s (%s): %w", name, id, m.haError(err))
			}
			if args.Watch {
				return fmt.Sprintf(t.removed, name), nil
			}
			return fmt.Sprintf(t.set, name), nil
		},
	}}
}

// watchedMember finds an entity a watch group could hold, by its id or a
// name: the members of every scope, labelled ones included (rendered by Home
// Assistant, one render per scope); the one whose name's words the user said
// most of wins.
func (m *Manager) watchedMember(ctx context.Context, given string) (id, name string, err error) {
	t := i18n.Pick(exceptionTexts, m.opts.Lang)
	text := strings.ToLower(strings.ReplaceAll(given, "_", " "))
	var found []watch.Member
	longest := 0
	for _, scope := range watch.Scopes {
		members, err := m.opts.Watch.Members(ctx, watch.Selection{Scope: scope, WithLabelled: true})
		if err != nil {
			return "", "", err
		}
		for _, mb := range members {
			if mb.ID == given {
				return mb.ID, mb.Name, nil
			}
			// The words of its name the user said: "die Terrasse" is the
			// light "Licht Terrasse"; the most said wins.
			l := 0
			for _, word := range strings.Fields(strings.ToLower(mb.Name)) {
				if len(word) >= 3 && strings.Contains(text, word) {
					l += len(word)
				}
			}
			if l == 0 {
				continue
			}
			switch {
			case l > longest:
				found, longest = []watch.Member{mb}, l
			case l == longest && !slices.ContainsFunc(found, func(f watch.Member) bool { return f.ID == mb.ID }):
				found = append(found, mb)
			}
		}
	}
	switch len(found) {
	case 0:
		return "", "", fmt.Errorf(t.notFound, given)
	case 1:
		return found[0].ID, found[0].Name, nil
	}
	var list []string
	for _, f := range found {
		list = append(list, f.Name+" ("+f.ID+")")
	}
	return "", "", fmt.Errorf(t.several, given, strings.Join(list, ", "))
}
