// Package structure turns the Home Assistant registries into prompt layer 4.
//
// The block lists areas, devices and exposed entities with their aliases and
// deliberately contains no states: the model asks for those through a tool.
// Everything here is sorted by stable keys so the same house always renders
// to the same bytes, which is what keeps the prompt cache warm.
package structure

import (
	"fmt"
	"github.com/miraassist/mira/internal/i18n"
	"slices"
	"sort"
	"strings"

	haws "github.com/miraassist/mira/internal/ha/ws"
)

// Registry is the raw input of a build.
type Registry struct {
	// Lang is the language of the block's labels.
	Lang     i18n.Lang
	Areas    []haws.Area
	Devices  []haws.Device
	Entities []haws.Entity
	// Exposed is the set of entity ids exposed to the conversation assistant.
	// A nil map means exposure could not be determined.
	Exposed map[string]bool
}

// Snapshot is a built structure block plus the lookups the rest of the agent
// needs from the same data.
type Snapshot struct {
	// Block is prompt layer 4.
	Block string
	// AreaNames maps area id to its display name.
	AreaNames map[string]string
	// DeviceArea maps device id to area id, which is how a voice request from
	// a satellite is resolved to the room the user is standing in.
	DeviceArea map[string]string
	// EntityArea maps entity id to area id.
	EntityArea map[string]string
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
	// (LabelPrivate): what starts in one stays there (docs/spec/agenten.md,
	// What a turn belongs to).
	Private map[string]bool
	// ExposureKnown is false when Home Assistant could not tell us which
	// entities are exposed and the build fell back to including everything.
	ExposureKnown bool
	// SatelliteDevices are the device ids of the voice satellites (Assist
	// satellite entities), sorted. The MCP host asks Home Assistant for its
	// tools as each of them would get them.
	SatelliteDevices []string
	// Counts for logging and telemetry.
	AreaCount, EntityCount int
}

// entityRow is one line of the block, prepared so sorting is explicit.
type entityRow struct {
	entityID   string
	name       string
	deviceName string
	aliases    []string
}

// LabelConfirm is the id of the label "Mira: nachfragen", which the
// integration creates; the label's name may be anything.
const LabelConfirm = "mira_confirm"

// LabelPrivate is the id of the label "Mira: privater Bereich" on areas.
const LabelPrivate = "mira_private"

// Build renders the structure block. It is a pure function of its input:
// no clock, no randomness, no map iteration order.
func Build(reg Registry) Snapshot {
	t := i18n.Pick(catalog, reg.Lang)
	snap := Snapshot{
		AreaNames:       make(map[string]string, len(reg.Areas)),
		DeviceArea:      make(map[string]string, len(reg.Devices)),
		EntityArea:      make(map[string]string, len(reg.Entities)),
		ExposedEntities: map[string]bool{},
		EntityNames:     map[string][]string{},
		EntityLabel:     map[string]string{},
		AreaKeys:        map[string]string{},
		Confirm:         map[string]bool{},
		Private:         map[string]bool{},
		ExposureKnown:   reg.Exposed != nil,
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

	areaByID := make(map[string]haws.Area, len(reg.Areas))
	for _, a := range reg.Areas {
		areaByID[a.AreaID] = a
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

	// Group entities by area. The empty area id collects what has no room.
	byArea := map[string][]entityRow{}
	for _, e := range reg.Entities {
		if !e.Usable() {
			continue
		}
		if !isExposed(e, reg.Exposed) {
			continue
		}
		area := e.AreaID
		device := deviceByID[e.DeviceID]
		if area == "" {
			area = device.AreaID
		}
		snap.EntityArea[e.EntityID] = area
		snap.ExposedEntities[e.EntityID] = true

		name := e.DisplayName()
		if name == "" {
			name = friendlyFromEntityID(e.EntityID)
		}
		row := entityRow{entityID: e.EntityID, name: name, aliases: sortedCopy(e.Aliases)}
		// The device name only earns a mention when it adds something the
		// entity name does not already say.
		if dn := device.DisplayName(); dn != "" && !strings.EqualFold(dn, name) {
			row.deviceName = dn
		}
		byArea[area] = append(byArea[area], row)
		snap.EntityNames[e.EntityID] = nameKeys(e.EntityID, name, device.DisplayName(), e.Aliases)
		snap.EntityLabel[e.EntityID] = name
	}

	areaIDs := make([]string, 0, len(byArea))
	for id := range byArea {
		if id != "" {
			areaIDs = append(areaIDs, id)
		}
	}
	sort.Strings(areaIDs)

	var b strings.Builder
	for _, id := range areaIDs {
		a := areaByID[id]
		name := a.Name
		if name == "" {
			name = id
		}
		fmt.Fprintf(&b, t.area, name, id)
		if aliases := sortedCopy(a.Aliases); len(aliases) > 0 {
			fmt.Fprintf(&b, t.aliases, strings.Join(aliases, ", "))
		}
		b.WriteByte('\n')
		writeRows(&b, t, byArea[id])
		snap.AreaCount++
	}
	if rows := byArea[""]; len(rows) > 0 {
		b.WriteString(t.noArea + "\n")
		writeRows(&b, t, rows)
	}

	snap.Block = strings.TrimRight(b.String(), "\n")
	snap.EntityCount = len(snap.ExposedEntities)
	return snap
}

func writeRows(b *strings.Builder, t texts, rows []entityRow) {
	sort.Slice(rows, func(i, j int) bool { return rows[i].entityID < rows[j].entityID })
	for _, r := range rows {
		fmt.Fprintf(b, "  %s — %s", r.entityID, r.name)
		if r.deviceName != "" {
			fmt.Fprintf(b, t.device, r.deviceName)
		}
		if len(r.aliases) > 0 {
			fmt.Fprintf(b, t.aliases, strings.Join(r.aliases, ", "))
		}
		b.WriteByte('\n')
	}
}

// isExposed decides whether an entity belongs in the block.
//
// Only what the user exposed to Assist counts, and only the list the Mira
// integration sent (docs/spec/ha-anbindung.md). Without it nothing is
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

func sortedCopy(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// normalizeName is the form names are compared in.
func normalizeName(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

func nameKeys(entityID, name, deviceName string, aliases []string) []string {
	seen := map[string]bool{}
	var out []string
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

// texts are the labels of the structure block.
type texts struct{ area, aliases, noArea, device string }

var catalog = map[i18n.Lang]texts{
	i18n.DE: {area: "Bereich %s (%s)", aliases: ", auch genannt: %s", noArea: "Ohne Bereich", device: ", Gerät: %s"},
	i18n.EN: {area: "Area %s (%s)", aliases: ", also called: %s", noArea: "No area", device: ", device: %s"},
}
