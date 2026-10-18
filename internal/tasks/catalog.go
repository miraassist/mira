// Package tasks makes Mira's tasks ("Achte auf den Wasserverbrauch"). A
// task is a Home Assistant automation labelled "Mira": Home Assistant's
// triggers and conditions decide when, Mira's actions decide what is worth
// saying (docs/spec/aufgaben.md).
//
// The package has two halves. Assess and Decide are what mira.assess and
// mira.decide call: they condense data from Home Assistant into a few
// numbers, add context (memory, who is home, the time, the task's earlier
// judgements), let the model judge in an isolated call and deliver the
// result as a message. The other half drafts automations in conversation and
// saves them through the Mira integration once the user agrees.
package tasks

import (
	"sort"
	"strconv"
	"strings"

	"github.com/miraassist/mira/internal/ha/structure"
	haws "github.com/miraassist/mira/internal/ha/ws"
)

// Entity is what the planner may choose from.
type Entity struct {
	ID, Name, Area, Domain, DeviceClass, StateClass, Unit, State string
}

func (e Entity) counter() bool {
	return e.StateClass == "total" || e.StateClass == "total_increasing"
}

func (e Entity) numeric() bool {
	_, err := strconv.ParseFloat(e.State, 64)
	return err == nil || e.StateClass != ""
}

// Catalog lists the entities Mira may watch: the ones exposed to Assist,
// with what a plan needs to know about them.
func Catalog(states []haws.State, snap structure.Snapshot) []Entity {
	var out []Entity
	for _, s := range states {
		if !snap.ExposedEntities[s.EntityID] {
			continue
		}
		domain, _, _ := strings.Cut(s.EntityID, ".")
		attr := func(k string) string {
			v, _ := s.Attributes[k].(string)
			return v
		}
		out = append(out, Entity{
			ID: s.EntityID, Name: s.FriendlyName(), Area: snap.AreaNames[snap.EntityArea[s.EntityID]],
			Domain: domain, DeviceClass: attr("device_class"), StateClass: attr("state_class"),
			Unit: attr("unit_of_measurement"), State: s.State,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
