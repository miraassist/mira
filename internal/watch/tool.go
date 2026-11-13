package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/miraassist/mira/internal/excerpt"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

// ToolDeviceProfile is the tool that gives the model a power sensor's device
// profile (docs/spec/device-watch.md, decision 13).
const ToolDeviceProfile = "device_profile"

// Skill is the skill whose turns get the watch's tools: the one that makes
// tasks.
const Skill = "aufgabe"

// Sensor is what the tool needs to know of an entity Mira may see.
type Sensor struct {
	ID, Name, DeviceClass, Unit string
}

// Power says whether the sensor reports a power in watts: the blueprints'
// numbers are watts.
func (s Sensor) Power() bool { return s.DeviceClass == "power" && s.Unit == "W" }

type toolTexts struct {
	desc, entity, notPower string
}

var toolCatalog = map[i18n.Lang]toolTexts{
	i18n.DE: {
		desc:     "Geräteprofil eines Leistungssensors (device_class power, in W) aus seinem Verlauf: ob das Gerät einen Tagesablauf hat (jeden Tag zu festen Zeiten an, z. B. Aquariumlicht), im Takt läuft (an und aus den ganzen Tag, z. B. Kühlschrank) oder Geräteläufe hat (ein Programm ab und zu, z. B. Spülmaschine), mit den Zahlen: die Zeiten eines Tagesablaufs, Läufe und Pausen, und ob sie für einen Wächter reichen. Liest die Statistik der letzten 90 Tage und die Zustände der letzten 10 Tage.",
		entity:   "Die id des Leistungssensors (z. B. sensor.aquarium_leistung) oder sein Name",
		notPower: "%s (%s) ist kein Leistungssensor in W (device_class %q, Einheit %q): ein Geräteprofil gibt es nur dafür",
	},
	i18n.EN: {
		desc:     "Device profile of a power sensor (device_class power, in W) from its history: whether the device follows a daily routine (on at fixed times every day, e.g. an aquarium's light), runs in a cycle (on and off all day, e.g. a fridge) or has device runs (a programme now and then, e.g. a dishwasher), with the numbers: a daily routine's times, runs and pauses, and whether they are enough for a watch. Reads the statistics of the last 90 days and the states of the last 10 days.",
		entity:   "The power sensor's id (e.g. sensor.aquarium_power) or its name",
		notPower: "%s (%s) is not a power sensor in W (device_class %q, unit %q): a device profile is only made for one",
	},
}

// Tool is device_profile: find looks up an entity Mira may see (exposed to
// Assist) by its id or name and fails with why it cannot. Its error names
// the sensor and Home Assistant's answer (the spec's error table).
func (p *Profiler) Tool(find func(ctx context.Context, id string) (Sensor, error)) tools.InternalTool {
	t := i18n.Pick(toolCatalog, p.opts.Lang)
	return tools.InternalTool{
		Name: ToolDeviceProfile, Description: t.desc, ReadOnly: true, Skill: Skill,
		Schema: fmt.Sprintf(`{"type":"object","properties":{"entity":{"type":"string","description":%q}},"required":["entity"]}`, t.entity),
		Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
			var a struct {
				Entity string `json:"entity"`
			}
			if err := json.Unmarshal(inv.Args, &a); err != nil {
				return "", fmt.Errorf("%q not understood: %w", excerpt.Bytes(inv.Args), err)
			}
			s, err := find(ctx, strings.TrimSpace(a.Entity))
			if err != nil {
				return "", err
			}
			if !s.Power() {
				return "", fmt.Errorf(t.notPower, s.Name, s.ID, s.DeviceClass, s.Unit)
			}
			pr, err := p.Profile(ctx, s.ID)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("%s (%s): %s", s.Name, s.ID, p.Describe(pr, StatsDays)), nil
		},
	}
}
