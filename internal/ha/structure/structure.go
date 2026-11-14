// Package structure keeps what Mira needs to know of the Home Assistant
// registries: areas, devices and exposed entities with their names and
// aliases, for looking them up. The house structure the model reads is Home
// Assistant's own prompt (docs/spec/context.md, 1.2); there is no text here.
package structure

import (
	"reflect"
	"slices"
	"sort"
	"strings"

	haws "github.com/miraassist/mira/internal/ha/ws"
)

// Registry is the raw input of a build.
type Registry struct {
	Areas    []haws.Area
	Devices  []haws.Device
	Entities []haws.Entity
	// Exposed is the set of entity ids exposed to the conversation assistant.
	// A nil map means exposure could not be determined.
	Exposed map[string]bool
}

// Snapshot is the lookups the rest of the service needs from the registries.
type Snapshot struct {
	// AreaNames maps area id to its display name.
	AreaNames map[string]string
	// DeviceArea maps device id to area id, which is how a voice request from
	// a satellite is resolved to the room the user is standing in.
	DeviceArea map[string]string
	// EntityArea maps entity id to area id.
	EntityArea map[string]string
	// EntityDevice maps an exposed entity id to its device id, if it has one.
	EntityDevice map[string]string
	// ExposedEntities is the set the model was told about.
	ExposedEntities map[string]bool
	// EntityNames maps entity id to every lower-cased name the model might
	// use for it: entity id, display name, aliases and device name. The
	// security policy resolves tool arguments through it.
	EntityNames map[string][]string
	// EntityLabel is the name an entity goes by, as Home Assistant shows
	// it, for telling the user which one Mira means.
	EntityLabel map[string]string
	// AreaKeys maps a lower-cased area id, name or alias to the area id.
	AreaKeys map[string]string
	// Confirm are the entities labelled "Mira: nachfragen" (LabelConfirm):
	// Mira switches them only after the user confirmed.
	Confirm map[string]bool
	// Private are the areas labelled "Mira: privater Bereich"
	// (LabelPrivate): what starts in one stays there (docs/spec/agents.md,
	// What a turn belongs to).
	Private map[string]bool
	// ExposureKnown is false while the integration has not sent which
	// entities are exposed; then nothing counts as exposed.
	ExposureKnown bool
	// Devices are every device by id; Entities every entity not switched
	// off, by id; DeviceEntities those of each device, sorted. A
	// satellite's speaker, node name or phone line is looked up here, kept
	// current by the registry events, never read from a registry per call
	// (docs/spec/ha-integration.md, Watching states).
	Devices        map[string]haws.Device
	Entities       map[string]haws.Entity
	DeviceEntities map[string][]string
	// SatelliteDevices are the device ids of the voice satellites (Assist
	// satellite entities), sorted. The MCP host asks Home Assistant for its
	// tools as each of them would get them.
	SatelliteDevices []string
	// AskingDevices are the device ids a turn can come from, sorted: the
	// satellites and the companion apps (devices of the integration
	// mobile_app, not disabled). Home Assistant's prompt and tools are
	// fetched as each of them (docs/spec/context.md, 1.1).
	AskingDevices []string
	// Counts for logging and telemetry.
	AreaCount, EntityCount int
}

// DeviceEntity is the first entity of a domain on a device, not switched
// off; "" for none.
func (s Snapshot) DeviceEntity(deviceID, domain string) string {
	for _, id := range s.DeviceEntities[deviceID] {
		if Domain(id) == domain {
			return id
		}
	}
	return ""
}

// LabelConfirm is the id of the label "Mira: nachfragen", which the
// integration creates; the label's name may be anything.
const LabelConfirm = "mira_confirm"

// LabelPrivate is the id of the label "Mira: privater Bereich" on areas.
const LabelPrivate = "mira_private"

// Build makes the lookups. It is a pure function of its input: no clock, no
// randomness, no map iteration order.
func Build(reg Registry) Snapshot {
	snap := Snapshot{
		AreaNames:       make(map[string]string, len(reg.Areas)),
		DeviceArea:      make(map[string]string, len(reg.Devices)),
		EntityArea:      make(map[string]string, len(reg.Entities)),
		EntityDevice:    map[string]string{},
		ExposedEntities: map[string]bool{},
		EntityNames:     map[string][]string{},
		EntityLabel:     map[string]string{},
		AreaKeys:        map[string]string{},
		Confirm:         map[string]bool{},
		Private:         map[string]bool{},
		ExposureKnown:   reg.Exposed != nil,
		Devices:         make(map[string]haws.Device, len(reg.Devices)),
		Entities:        make(map[string]haws.Entity, len(reg.Entities)),
		DeviceEntities:  map[string][]string{},
	}
	for _, d := range reg.Devices {
		snap.Devices[d.ID] = d
	}
	for _, e := range reg.Entities {
		if e.DisabledBy != "" { // hidden and diagnostic ones count: a phone line is one
			continue
		}
		snap.Entities[e.EntityID] = e
		if e.DeviceID != "" {
			snap.DeviceEntities[e.DeviceID] = append(snap.DeviceEntities[e.DeviceID], e.EntityID)
		}
	}
	for _, ids := range snap.DeviceEntities {
		slices.Sort(ids)
	}
	for _, e := range reg.Entities {
		if slices.Contains(e.Labels, LabelConfirm) {
			snap.Confirm[e.EntityID] = true
		}
		if Domain(e.EntityID) == "assist_satellite" && e.Usable() && e.DeviceID != "" &&
			!slices.Contains(snap.SatelliteDevices, e.DeviceID) {
			snap.SatelliteDevices = append(snap.SatelliteDevices, e.DeviceID)
		}
	}
	slices.Sort(snap.SatelliteDevices)
	snap.AskingDevices = slices.Clone(snap.SatelliteDevices)
	for _, d := range reg.Devices {
		if d.DisabledBy == "" && d.HasIdentifierOf("mobile_app") && !slices.Contains(snap.AskingDevices, d.ID) {
			snap.AskingDevices = append(snap.AskingDevices, d.ID)
		}
	}
	slices.Sort(snap.AskingDevices)

	for _, a := range reg.Areas {
		snap.AreaNames[a.AreaID] = a.Name
		if slices.Contains(a.Labels, LabelPrivate) {
			snap.Private[a.AreaID] = true
		}
		for _, key := range append([]string{a.AreaID, a.Name}, a.Aliases...) {
			if key = normalizeName(key); key != "" {
				snap.AreaKeys[key] = a.AreaID
			}
		}
	}
	deviceByID := make(map[string]haws.Device, len(reg.Devices))
	for _, d := range reg.Devices {
		if d.DisabledBy != "" {
			continue
		}
		deviceByID[d.ID] = d
		if d.AreaID != "" {
			snap.DeviceArea[d.ID] = d.AreaID
		}
	}
	// A satellite's area is its entity's, else its device's, as the
	// integration finds it (docs/spec/satellites-rooms.md, decision 8).
	for _, e := range reg.Entities {
		if Domain(e.EntityID) == "assist_satellite" && e.DisabledBy == "" && e.AreaID != "" {
			if _, ok := deviceByID[e.DeviceID]; ok {
				snap.DeviceArea[e.DeviceID] = e.AreaID
			}
		}
	}

	areas := map[string]bool{}
	for _, e := range reg.Entities {
		// The exposed list is taken as it is: what the user exposed counts,
		// diagnostic and hidden ones too (docs/spec/ha-integration.md).
		if !isExposed(e, reg.Exposed) {
			continue
		}
		area := e.AreaID
		device := deviceByID[e.DeviceID]
		if area == "" {
			area = device.AreaID
		}
		snap.EntityArea[e.EntityID] = area
		if e.DeviceID != "" {
			snap.EntityDevice[e.EntityID] = e.DeviceID
		}
		snap.ExposedEntities[e.EntityID] = true

		name := e.DisplayName()
		if name == "" {
			name = friendlyFromEntityID(e.EntityID)
		}
		snap.EntityNames[e.EntityID] = nameKeys(e.EntityID, name, device.DisplayName(), e.Aliases)
		snap.EntityLabel[e.EntityID] = name
		if area != "" {
			areas[area] = true
		}
	}
	snap.AreaCount = len(areas)
	snap.EntityCount = len(snap.ExposedEntities)
	return snap
}

// isExposed decides whether an entity is one Mira may see.
//
// Only what the user exposed to Assist counts, and only the list the Mira
// integration sent (docs/spec/ha-integration.md). Without it nothing is
// exposed: not knowing what was shared never means everything is.
func isExposed(e haws.Entity, exposed map[string]bool) bool {
	return exposed[e.EntityID]
}

// friendlyFromEntityID builds a readable name when the registry has none:
// "light.kueche_decke" becomes "Kueche Decke".
func friendlyFromEntityID(entityID string) string {
	object := entityID
	if i := strings.Index(entityID, "."); i >= 0 {
		object = entityID[i+1:]
	}
	parts := strings.Split(object, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

// normalizeName is the form names are compared in.
func normalizeName(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// nameKeys are an entity's names, its aliases sorted: the same house in any
// order has the same names (Same).
func nameKeys(entityID, name, deviceName string, aliases []string) []string {
	seen := map[string]bool{}
	var out []string
	aliases = slices.Sorted(slices.Values(aliases))
	for _, k := range append([]string{entityID, name, deviceName}, aliases...) {
		if k = normalizeName(k); k != "" && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// ResolveName returns the exposed entities a name could refer to, the way
// Home Assistant's intent matching would: by entity id, name, alias or device
// name, optionally restricted to an area. The result is sorted.
func (s Snapshot) ResolveName(name, area string) []string {
	key := normalizeName(name)
	if key == "" {
		return nil
	}
	areaID := s.ResolveArea(area)
	var out []string
	for id, names := range s.EntityNames {
		if area != "" && areaID != "" && s.EntityArea[id] != areaID {
			continue
		}
		for _, n := range names {
			if n == key {
				out = append(out, id)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// ResolveArea maps an area id, name or alias to the area id, or "".
func (s Snapshot) ResolveArea(area string) string {
	return s.AreaKeys[normalizeName(area)]
}

// EntitiesInArea lists the exposed entities of an area given by id, name or
// alias. The result is sorted.
func (s Snapshot) EntitiesInArea(area string) []string {
	areaID := s.ResolveArea(area)
	if areaID == "" {
		return nil
	}
	var out []string
	for id, a := range s.EntityArea {
		if a == areaID {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// Domain returns the domain part of an entity id.
func Domain(entityID string) string {
	if i := strings.Index(entityID, "."); i >= 0 {
		return entityID[:i]
	}
	return ""
}

// Same reports whether two snapshots know the same house: the same areas,
// names, aliases, exposure, labels, satellites and asking devices. Only then is a rebuild a
// change worth fetching Home Assistant's prompt again for.
func Same(a, b Snapshot) bool {
	return a.ExposureKnown == b.ExposureKnown &&
		reflect.DeepEqual(a.AreaNames, b.AreaNames) && reflect.DeepEqual(a.AreaKeys, b.AreaKeys) &&
		reflect.DeepEqual(a.DeviceArea, b.DeviceArea) && reflect.DeepEqual(a.EntityArea, b.EntityArea) &&
		reflect.DeepEqual(a.EntityDevice, b.EntityDevice) && reflect.DeepEqual(a.ExposedEntities, b.ExposedEntities) &&
		reflect.DeepEqual(a.EntityNames, b.EntityNames) && reflect.DeepEqual(a.EntityLabel, b.EntityLabel) &&
		reflect.DeepEqual(a.Confirm, b.Confirm) && reflect.DeepEqual(a.Private, b.Private) &&
		slices.Equal(a.SatelliteDevices, b.SatelliteDevices) && slices.Equal(a.AskingDevices, b.AskingDevices)
}
