package watch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/miraassist/mira/internal/excerpt"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/retry"
)

// The group watch (docs/spec/group-watch.md): a watch group is a selection
// over what Home Assistant knows, held as a blueprint's inputs areas,
// floors and group; its members are computed by Home Assistant's templates,
// in the blueprints and, with the same text, for a proposal and a check.

// LabelDontWatch is the label "Mira: nicht überwachen" (decision 5): an
// entity carrying it is in no selection. The integration creates it.
const LabelDontWatch = "mira_dont_watch"

// Templates renders a template in Home Assistant once (render_template).
type Templates interface {
	RenderTemplate(ctx context.Context, template string, variables map[string]any) (json.RawMessage, error)
}

// Scope is a kind of watch group: its domains (one, or several: decision
// 42) and their device classes (decision 8); no classes for every entity of
// the domains.
type Scope struct {
	Name    string
	Domains []string
	Classes []string
}

// Scopes are the watch groups (decisions 8, 36 and 38). Every group
// blueprint's trigger_variables name one of them.
var Scopes = []Scope{
	{Name: "lights", Domains: []string{"light"}},
	{Name: "openings", Domains: []string{"binary_sensor"}, Classes: []string{"window", "door", "opening", "garage_door"}},
	{Name: "motion", Domains: []string{"binary_sensor"}, Classes: []string{"motion", "occupancy", "presence"}},
	{Name: "temperatures", Domains: []string{"sensor"}, Classes: []string{"temperature"}},
	{Name: ScopeBatteries, Domains: []string{"sensor", "binary_sensor"}, Classes: []string{"battery"}},
	{Name: ScopeDevices, Domains: []string{"sensor", "binary_sensor", "switch", "light", "cover", "climate", "lock", "fan", "valve"}},
}

// The scopes of the battery and the availability watch: their members are
// named by device (decision 40).
const (
	ScopeBatteries = "batteries"
	ScopeDevices   = "devices"
)

// ByDevice says whether the scope's members are named by their devices.
func (s Scope) ByDevice() bool { return s.Name == ScopeBatteries || s.Name == ScopeDevices }

// domainVariable is the scope's domains as the shared selection takes them:
// one domain, or a list.
func (s Scope) domainVariable() any {
	if len(s.Domains) == 1 {
		return s.Domains[0]
	}
	return s.Domains
}

// ScopeByName is the scope of that name.
func ScopeByName(name string) (Scope, bool) {
	for _, s := range Scopes {
		if s.Name == name {
			return s, true
		}
	}
	return Scope{}, false
}

// ScopeOf is the scope of a group blueprint.
func ScopeOf(b Blueprint) (Scope, bool) {
	for _, s := range Scopes {
		if slices.Equal(s.Domains, b.Domains) && slices.Equal(s.Classes, b.Classes) {
			return s, true
		}
	}
	return Scope{}, false
}

// Selection is a watch group: a scope, and either areas and floors (none:
// the whole house) or a group helper.
type Selection struct {
	Scope         Scope
	Areas, Floors []string
	Group         string
	// WithLabelled finds every entity of the scope in the house, the
	// labelled ones too (an exception to be taken back); not a watch group.
	WithLabelled bool
}

func (s Selection) variables() map[string]any {
	classes := s.Classes()
	return map[string]any{"domain": s.Scope.domainVariable(), "classes": classes, "areas": nonNil(s.Areas),
		"floors": nonNil(s.Floors), "group": s.Group}
}

// Classes are the scope's device classes, never nil.
func (s Selection) Classes() []string { return nonNil(s.Scope.Classes) }

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// SelectionOf is the selection a group blueprint's inputs hold.
func SelectionOf(b Blueprint, inputs map[string]any) Selection {
	scope, _ := ScopeOf(b)
	sel := Selection{Scope: scope}
	sel.Areas = stringList(inputs["areas"])
	sel.Floors = stringList(inputs["floors"])
	sel.Group, _ = inputs[GroupInput].(string)
	return sel
}

// stringList reads a list of ids as the planner or a saved automation gives
// it: a list, or one id.
func stringList(v any) []string {
	var out []string
	switch x := v.(type) {
	case []string:
		out = append(out, x...)
	case []any:
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
	case string:
		out = append(out, strings.Split(x, ",")...)
	}
	var clean []string
	for _, s := range out {
		if s = strings.TrimSpace(s); s != "" {
			clean = append(clean, s)
		}
	}
	return clean
}

// Member is one member of a watch group as Home Assistant rendered it.
type Member struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
	Area  string `json:"area"`
}

// Readable says whether the member's state can be read (decision: an
// unavailable or unknown member breaks no rule).
func (m Member) Readable() bool { return m.State != "unavailable" && m.State != "unknown" }

// Floor is a floor of the house with its areas.
type Floor struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Areas []string `json:"areas"`
}

// GroupHelper is a group helper of the house: an entity holding others.
type GroupHelper struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DeviceClass string `json:"device_class"`
}

// Domain is the group helper's domain.
func (g GroupHelper) Domain() string { d, _, _ := strings.Cut(g.ID, "."); return d }

// House is what a selection may name: the floors, the areas (id to name)
// and the group helpers.
type House struct {
	Floors []Floor           `json:"floors"`
	Areas  map[string]string `json:"areas"`
	Groups []GroupHelper     `json:"groups"`
}

// FloorName is a floor's name, its id when unknown.
func (h House) FloorName(id string) string {
	for _, f := range h.Floors {
		if f.ID == id {
			return f.Name
		}
	}
	return id
}

// AreaName is an area's name, its id when unknown.
func (h House) AreaName(id string) string {
	if n := h.Areas[id]; n != "" {
		return n
	}
	return id
}

// houseTemplate lists the floors with their areas, the areas, and the
// group helpers of the domains Mira watches groups of, as JSON.
const houseTemplate = `{"floors": [{%- for f in floors() -%}{"id": {{ f | tojson }}, "name": {{ floor_name(f) | tojson }}, "areas": {{ floor_areas(f) | tojson }}}{{ ", " if not loop.last }}{%- endfor -%}],
"areas": { {%- for a in areas() -%}{{ a | tojson }}: {{ area_name(a) | tojson }}{{ ", " if not loop.last }}{%- endfor -%} },
"groups": [{%- for s in states if s.attributes.entity_id is defined and s.domain in ['light', 'binary_sensor', 'sensor'] -%}{"id": {{ s.entity_id | tojson }}, "name": {{ s.name | tojson }}, "device_class": {{ (s.attributes.device_class or '') | tojson }}}{{ ", " if not loop.last }}{%- endfor -%}]}`

// everyEntityOfScope is every entity of a scope in the house, labelled or
// not, without group helpers: where an exception is found to be taken back.
const everyEntityOfScope = `{%- set ns = namespace(members=[]) -%}
{%- for d in ([domain] if domain is string else domain) -%}
{%- for s in states[d] if (not classes or s.attributes.device_class in classes) and s.attributes.entity_id is not defined -%}
{%- set ns.members = ns.members + [s] -%}
{%- endfor -%}
{%- endfor -%}`

// membersOutput turns the selection's members into JSON.
const membersOutput = `[{%- for s in ns.members -%}{"id": {{ s.entity_id | tojson }}, "name": {{ s.name | tojson }}, "state": {{ s.state | tojson }}, "area": {{ (area_name(s.entity_id) or '') | tojson }}}{{ ", " if not loop.last }}{%- endfor -%}]`

// SelectionTemplate is the selection shared by the group blueprints and
// the check: with the variables domain, classes, areas, floors and group it
// leaves the members' states in ns.members.
func SelectionTemplate() string {
	raw, err := files.ReadFile("blueprints/selection.jinja")
	if err != nil {
		panic(err)
	}
	return strings.TrimSpace(string(raw))
}

// decodeResult reads what render_template answered: Home Assistant sends a
// result it could parse as a list or an object, else the string.
func decodeResult(raw json.RawMessage, v any) error {
	var text string
	if trimmed := bytes.TrimSpace(raw); len(trimmed) > 0 && trimmed[0] == '"' && json.Unmarshal(trimmed, &text) == nil {
		raw = json.RawMessage(text)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("home assistant rendered %q: %w", excerpt.Bytes(raw), err)
	}
	return nil
}

// render renders a template once, tried again by decision 17 (Retries): a
// lost connection or a timeout is tried again, a template Home Assistant
// refuses is not.
func (w *Blueprints) render(ctx context.Context, what, template string, vars map[string]any, v any) error {
	if w.opts.Templates == nil {
		return errors.New("no way to render templates in home assistant")
	}
	var raw json.RawMessage
	err := retry.Do(ctx, w.policy, func(ctx context.Context) error {
		out, err := w.opts.Templates.RenderTemplate(ctx, template, vars)
		raw = out
		return err
	}, retry.Warn(w.log, what, w.policy, "template", excerpt.Of(template)))
	if err != nil {
		return err
	}
	return decodeResult(raw, v)
}

// House renders what a selection may name: one render_template.
func (w *Blueprints) House(ctx context.Context) (House, error) {
	var h House
	if err := w.render(ctx, "reading the house's floors, areas and group helpers", houseTemplate, nil, &h); err != nil {
		w.log.Warn("could not read the house's floors, areas and group helpers", "error", err)
		return House{}, err
	}
	sort.Slice(h.Groups, func(i, j int) bool { return h.Groups[i].ID < h.Groups[j].ID })
	return h, nil
}

// Members renders a selection's members as the blueprints compute them:
// one render_template with the shared selection.
func (w *Blueprints) Members(ctx context.Context, sel Selection) ([]Member, error) {
	var out []Member
	tpl := SelectionTemplate()
	if sel.WithLabelled {
		tpl = everyEntityOfScope
	}
	if err := w.render(ctx, "rendering a watch group", tpl+"\n"+membersOutput, sel.variables(), &out); err != nil {
		w.log.Warn("could not render a watch group", "scope", sel.Scope.Name, "areas", sel.Areas, "floors", sel.Floors,
			"group", sel.Group, "error", err)
		return nil, err
	}
	return out, nil
}

type groupTexts struct {
	intro, house, floor, groupHelper, members              string
	scopes                                                 map[string]string
	unknownArea, unknownFloor, unknownGroup, groupAndPlace string
	noLimit, belowAbove                                    string
}

var groupCatalog = map[i18n.Lang]groupTexts{
	i18n.DE: {
		intro:       "\n\nWächter für Gruppen: Die Wächtergruppe ist eine Auswahl über das, was Home Assistant kennt; die Art (Lichter, Fenster und Türen, Bewegungsmelder, Temperaturen) bestimmt der Blueprint. Wähle sie mit areas (Liste von Bereichs-ids) und floors (Liste von Etagen-ids): zusammen alles in diesen Bereichen und auf diesen Etagen, ohne beide das ganze Haus; oder mit group (die id eines Gruppen-Helfers, dann ohne areas und floors). Was das Label „Mira: nicht überwachen“ trägt, ist in keiner Wächtergruppe. Was der Nutzer ausnimmt („die Terrasse nicht“), genau das; sagt er bei Lichtern nichts dazu, als Vorschlag die Außenbeleuchtung (nach Bereich und Name: Terrasse, Garten, Einfahrt …) und die Anzeigen der Sprachgeräte (LED-Ring, Bildschirm): gib ihre ids im Feld \"exclude\" deiner Antwort an (nur Mitglieder der Wächtergruppe, nicht bei group); sie bekommen beim Ja das Label und sind dann in keinem Wächter, und dein summary nennt sie. Nennt der Nutzer etwas, das es nicht gibt (eine Etage, einen Bereich), antworte {\"error\": was es gibt}.",
		house:       "\nEtagen und Bereiche des Hauses:",
		floor:       "\n- Etage %s (%s): %s",
		groupHelper: "\nGruppen-Helfer: %s",
		members:     "\nIm ganzen Haus heute, %s (id, Name, Bereich): %s",
		scopes:      map[string]string{"lights": "Lichter", "openings": "Fenster und Türen", "motion": "Bewegungsmelder", "temperatures": "Temperaturen"},
		unknownArea: "einen Bereich %q gibt es nicht; die Bereiche: %s", unknownFloor: "eine Etage %q gibt es nicht; die Etagen: %s",
		unknownGroup:  "%q ist kein Gruppen-Helfer für diesen Wächter; die Gruppen-Helfer: %s",
		groupAndPlace: "entweder group oder areas und floors, nicht beides",
		noLimit:       "gib below oder above an (oder beide)", belowAbove: "below (%v) muss unter above (%v) liegen",
	},
	i18n.EN: {
		intro:       "\n\nWatches for groups: the watch group is a selection over what Home Assistant knows; its kind (lights, windows and doors, motion sensors, temperatures) is the blueprint's. Choose it with areas (a list of area ids) and floors (a list of floor ids): together everything in these areas and on these floors, without both the whole house; or with group (the id of a group helper, then without areas and floors). What carries the label \"Mira: don't watch\" is in no watch group. What the user leaves out (\"not the terrace\"), exactly that; when the user says nothing about it for lights, as a proposal the outside lighting (by area and name: terrace, garden, driveway …) and the voice devices' indicators (an LED ring, a screen): give their ids in the field \"exclude\" of your answer (only members of the watch group, not with group); on the yes they get the label and are then in no watch, and your summary names them. When the user names something that does not exist (a floor, an area), answer {\"error\": what there is}.",
		house:       "\nFloors and areas of the house:",
		floor:       "\n- floor %s (%s): %s",
		groupHelper: "\nGroup helpers: %s",
		members:     "\nIn the whole house today, %s (id, name, area): %s",
		scopes:      map[string]string{"lights": "lights", "openings": "windows and doors", "motion": "motion sensors", "temperatures": "temperatures"},
		unknownArea: "there is no area %q; the areas: %s", unknownFloor: "there is no floor %q; the floors: %s",
		unknownGroup:  "%q is no group helper for this watch; the group helpers: %s",
		groupAndPlace: "either group or areas and floors, not both",
		noLimit:       "give below or above (or both)", belowAbove: "below (%v) must be under above (%v)",
	},
}

func (w *Blueprints) groupTexts() groupTexts { return i18n.Pick(groupCatalog, w.opts.Lang) }

// GroupCatalog is what the planner learns of watch groups besides the
// blueprints: how to choose one, the house's floors and areas, its group
// helpers, and the members of each scope's watch group over the whole house
// (the ones it may leave out; one render each). Empty when no group
// blueprint is ready or the house cannot be read (logged by House and
// Members).
func (w *Blueprints) GroupCatalog(ctx context.Context, list []Blueprint) string {
	if !slices.ContainsFunc(list, Blueprint.Group) {
		return ""
	}
	h, err := w.House(ctx)
	if err != nil {
		return ""
	}
	t := w.groupTexts()
	var b strings.Builder
	b.WriteString(t.intro)
	b.WriteString(t.house)
	inFloor := map[string]bool{}
	for _, f := range h.Floors {
		var names []string
		for _, a := range f.Areas {
			inFloor[a] = true
			names = append(names, h.AreaName(a)+" ("+a+")")
		}
		fmt.Fprintf(&b, t.floor, f.Name, f.ID, strings.Join(names, ", "))
	}
	var rest []string
	for id, name := range h.Areas {
		if !inFloor[id] {
			rest = append(rest, name+" ("+id+")")
		}
	}
	sort.Strings(rest)
	if len(rest) > 0 {
		fmt.Fprintf(&b, t.floor, "–", "–", strings.Join(rest, ", "))
	}
	if len(h.Groups) > 0 {
		var g []string
		for _, x := range h.Groups {
			g = append(g, x.Name+" ("+x.ID+")")
		}
		fmt.Fprintf(&b, t.groupHelper, strings.Join(g, ", "))
	}
	for _, scope := range Scopes {
		members, err := w.Members(ctx, Selection{Scope: scope})
		if err != nil {
			return ""
		}
		var list []string
		for _, mb := range members {
			list = append(list, fmt.Sprintf("%s (%s, %s)", mb.ID, mb.Name, mb.Area))
		}
		fmt.Fprintf(&b, t.members, t.scopes[scope.Name], strings.Join(list, "; "))
	}
	return b.String()
}

// CheckSelection is the code's check of a group blueprint's selection
// (docs/spec/group-watch.md, A2): the areas, floors and group helper it
// names exist, a group helper of the blueprint's domain, not with areas or
// floors; for the temperatures a lower or an upper limit, the lower under
// the upper.
func (w *Blueprints) CheckSelection(b Blueprint, inputs map[string]any, h House) (Selection, error) {
	t := w.groupTexts()
	sel := SelectionOf(b, inputs)
	if sel.Group != "" && (len(sel.Areas) > 0 || len(sel.Floors) > 0) {
		return Selection{}, errors.New(t.groupAndPlace)
	}
	for _, a := range sel.Areas {
		if _, ok := h.Areas[a]; !ok {
			var names []string
			for id, n := range h.Areas {
				names = append(names, n+" ("+id+")")
			}
			sort.Strings(names)
			return Selection{}, fmt.Errorf(t.unknownArea, a, strings.Join(names, ", "))
		}
	}
	for _, f := range sel.Floors {
		if !slices.ContainsFunc(h.Floors, func(x Floor) bool { return x.ID == f }) {
			var names []string
			for _, x := range h.Floors {
				names = append(names, x.Name+" ("+x.ID+")")
			}
			return Selection{}, fmt.Errorf(t.unknownFloor, f, strings.Join(names, ", "))
		}
	}
	if sel.Group != "" {
		if !slices.ContainsFunc(h.Groups, func(g GroupHelper) bool {
			return g.ID == sel.Group && slices.Contains(b.Domains, g.Domain()) && (len(b.Classes) == 0 || slices.Contains(b.Classes, g.DeviceClass) || g.DeviceClass == "")
		}) {
			var names []string
			for _, g := range h.Groups {
				if slices.Contains(b.Domains, g.Domain()) {
					names = append(names, g.Name+" ("+g.ID+")")
				}
			}
			return Selection{}, fmt.Errorf(t.unknownGroup, sel.Group, strings.Join(names, ", "))
		}
	}
	if b.Name == "temperature_out_of_range" {
		below, hasBelow := number(inputs["below"])
		above, hasAbove := number(inputs["above"])
		if !hasBelow && !hasAbove {
			return Selection{}, errors.New(t.noLimit)
		}
		if hasBelow && hasAbove && below >= above {
			return Selection{}, fmt.Errorf(t.belowAbove, below, above)
		}
	}
	return sel, nil
}

// ErrNoLabel is the label "Mira: nicht überwachen" missing in Home
// Assistant: an integration older than the group watch.
var ErrNoLabel = errors.New("home assistant has no label mira_dont_watch: the Mira integration needs an update")

// SetLabel puts the label "Mira: nicht überwachen" on an entity (on), or
// takes it off (docs/spec/group-watch.md, A3 and D): the label registry
// first (Home Assistant takes an unknown label id without a word, decision
// 27), then the entity's labels from the entity registry, then the update
// with the label added or removed. A refusal (a token without admin) ends at
// once, a lost connection is tried again (Retries); a failure is logged with
// the entity and Home Assistant's answer.
func (w *Blueprints) SetLabel(ctx context.Context, entity string, on bool) error {
	err := w.setLabel(ctx, entity, on)
	if err != nil {
		w.log.Warn("could not change the label mira_dont_watch of an entity", "entity", entity, "set", on, "error", err)
	}
	return err
}

func (w *Blueprints) setLabel(ctx context.Context, entity string, on bool) error {
	raw, err := w.callAbout(ctx, "reading the labels", "entity", entity, map[string]any{"type": "config/label_registry/list"}, false)
	if err != nil {
		return err
	}
	var labels []struct {
		LabelID string `json:"label_id"`
	}
	if err := json.Unmarshal(raw, &labels); err != nil {
		return fmt.Errorf("config/label_registry/list answered %q: %w", excerpt.Bytes(raw), err)
	}
	if !slices.ContainsFunc(labels, func(l struct {
		LabelID string `json:"label_id"`
	}) bool {
		return l.LabelID == LabelDontWatch
	}) {
		return ErrNoLabel
	}
	raw, err = w.callAbout(ctx, "reading an entity's labels", "entity", entity, map[string]any{"type": "config/entity_registry/get", "entity_id": entity}, false)
	if err != nil {
		return err
	}
	var entry struct {
		Labels []string `json:"labels"`
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		return fmt.Errorf("config/entity_registry/get of %s answered %q: %w", entity, excerpt.Bytes(raw), err)
	}
	has := slices.Contains(entry.Labels, LabelDontWatch)
	if has == on {
		return nil
	}
	next := slices.DeleteFunc(slices.Clone(entry.Labels), func(l string) bool { return l == LabelDontWatch })
	if on {
		next = append(next, LabelDontWatch)
	}
	_, err = w.callAbout(ctx, "setting an entity's labels", "entity", entity, map[string]any{"type": "config/entity_registry/update",
		"entity_id": entity, "labels": nonNil(next)}, false)
	if err == nil {
		w.log.Info("label mira_dont_watch changed", "entity", entity, "set", on)
	}
	return err
}

// CheckRequest is a check of a watch group once ("Ist alles zu?", the
// action mira.check; docs/spec/group-watch.md, C): a scope (default the
// openings, decision 17) and optionally areas and floors, by id or name.
type CheckRequest struct {
	Scope  string   `json:"scope"`
	Areas  []string `json:"areas"`
	Floors []string `json:"floors"`
}

// CheckResult is what a check found: the members that break the scope's
// rule now (on: open, on, motion), the ones that cannot be read, and how
// many were checked.
type CheckResult struct {
	Scope      string   `json:"scope"`
	Open       []Member `json:"open"`
	Unreadable []Member `json:"unreadable"`
	Checked    int      `json:"checked"`
}

type checkTexts struct {
	unknownScope, unknownArea, unknownFloor string
}

var checkCatalog = map[i18n.Lang]checkTexts{
	i18n.DE: {
		unknownScope: "eine Art %q gibt es hier nicht; die Arten: openings, lights, motion",
		unknownArea:  "einen Bereich %q gibt es nicht; die Bereiche: %s",
		unknownFloor: "eine Etage %q gibt es nicht; die Etagen: %s",
	},
	i18n.EN: {
		unknownScope: "there is no kind %q here; the kinds: openings, lights, motion",
		unknownArea:  "there is no area %q; the areas: %s",
		unknownFloor: "there is no floor %q; the floors: %s",
	},
}

// ErrBadCheck is a check that names what does not exist (a scope, an area,
// a floor): the caller's error, with what there is.
type ErrBadCheck struct{ Reason string }

func (e ErrBadCheck) Error() string { return e.Reason }

// HouseCheck checks a watch group once (C1, C2): the house's floors and
// areas when the request names some (one render), then the selection's
// members as the blueprints compute them (one render), split into what is
// on (open) and what cannot be read; the label counts as in every watch
// (decision 13). Temperatures have no "open" and are no scope of a check.
func (w *Blueprints) HouseCheck(ctx context.Context, req CheckRequest) (CheckResult, error) {
	t := i18n.Pick(checkCatalog, w.opts.Lang)
	name := req.Scope
	if name == "" {
		name = "openings"
	}
	scope, ok := ScopeByName(name)
	// Temperatures have no "open"; batteries and devices are the daily
	// watches' (decision 46).
	if !ok || !slices.Contains([]string{"openings", "lights", "motion"}, scope.Name) {
		return CheckResult{}, ErrBadCheck{fmt.Sprintf(t.unknownScope, name)}
	}
	sel := Selection{Scope: scope}
	if len(req.Areas) > 0 || len(req.Floors) > 0 {
		h, err := w.House(ctx)
		if err != nil {
			return CheckResult{}, err
		}
		for _, a := range req.Areas {
			id, ok := h.areaID(a)
			if !ok {
				var names []string
				for _, n := range h.Areas {
					names = append(names, n)
				}
				sort.Strings(names)
				return CheckResult{}, ErrBadCheck{fmt.Sprintf(t.unknownArea, a, strings.Join(names, ", "))}
			}
			sel.Areas = append(sel.Areas, id)
		}
		for _, f := range req.Floors {
			id, ok := h.floorID(f)
			if !ok {
				var names []string
				for _, x := range h.Floors {
					names = append(names, x.Name)
				}
				return CheckResult{}, ErrBadCheck{fmt.Sprintf(t.unknownFloor, f, strings.Join(names, ", "))}
			}
			sel.Floors = append(sel.Floors, id)
		}
	}
	members, err := w.Members(ctx, sel)
	if err != nil {
		return CheckResult{}, err
	}
	res := CheckResult{Scope: scope.Name, Checked: len(members), Open: []Member{}, Unreadable: []Member{}}
	for _, m := range members {
		switch {
		case !m.Readable():
			res.Unreadable = append(res.Unreadable, m)
		case m.State == "on":
			res.Open = append(res.Open, m)
		}
	}
	return res, nil
}

// areaID finds an area by its id or its name (any case).
func (h House) areaID(given string) (string, bool) {
	if _, ok := h.Areas[given]; ok {
		return given, true
	}
	for id, n := range h.Areas {
		if strings.EqualFold(n, strings.TrimSpace(given)) {
			return id, true
		}
	}
	return "", false
}

// floorID finds a floor by its id or its name (any case).
func (h House) floorID(given string) (string, bool) {
	for _, f := range h.Floors {
		if f.ID == given || strings.EqualFold(f.Name, strings.TrimSpace(given)) {
			return f.ID, true
		}
	}
	return "", false
}
