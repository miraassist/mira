// Package tasks runs standing orders ("Aufträge"): "Achte auf den
// Wasserverbrauch", "Sag Bescheid, wenn nachts ungewöhnlich viel Strom
// läuft", "Pass auf das Haus auf".
//
// The work is split by what each side is good at. When a task is created,
// the model plans once which entities matter and what would be unusual; the
// user confirms the plan. From then on the code watches: it compares the
// last hour with the same hour on the previous fourteen days from Home
// Assistant's statistics, and checks states and thresholds. Only a candidate
// — something the numbers say is out of the ordinary — goes to the model,
// which judges whether it is worth a notice and words it. That keeps it
// cheap and grounded in data rather than in the model's guesses.
package tasks

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/sker65/mira/internal/ha/structure"
	haws "github.com/sker65/mira/internal/ha/ws"
)

// Rule kinds.
const (
	// KindUsage watches a counter (water, energy): what it rose by in an
	// hour, against the same hour on earlier days.
	KindUsage = "usage"
	// KindLevel watches a measurement (temperature, power): its hourly mean
	// against the same hour on earlier days.
	KindLevel = "level"
	// KindState fires when an entity is in one of the given states.
	KindState = "state"
	// KindThreshold fires when a value is above or below a limit.
	KindThreshold = "threshold"
)

// When a rule applies: always, at night (the notifier's quiet hours) or by
// day.
const (
	WhenAlways = "always"
	WhenNight  = "night"
	WhenDay    = "day"
)

const maxRules = 8

// Plan is what the model makes of a goal.
type Plan struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Rules   []Rule `json:"rules"`
}

type Rule struct {
	EntityID  string   `json:"entity_id"`
	Kind      string   `json:"kind"`
	When      string   `json:"when"`
	Direction string   `json:"direction,omitempty"` // level: high, low or both
	States    []string `json:"states,omitempty"`    // state
	Above     *float64 `json:"above,omitempty"`     // threshold
	Below     *float64 `json:"below,omitempty"`     // threshold
	Why       string   `json:"why"`
}

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

// Catalog lists the entities Mira may watch: the ones exposed to Assist
// (all usable ones when exposure is unknown), with what a plan needs to
// know about them.
func Catalog(states []haws.State, snap structure.Snapshot) []Entity {
	var out []Entity
	for _, s := range states {
		if len(snap.ExposedEntities) > 0 && !snap.ExposedEntities[s.EntityID] {
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

// Validate checks a plan against the catalog, so the code never watches an
// entity that does not exist or reads a counter as a level.
func Validate(p Plan, catalog []Entity) error {
	if strings.TrimSpace(p.Title) == "" || strings.TrimSpace(p.Summary) == "" {
		return fmt.Errorf("plan needs title and summary")
	}
	if len(p.Rules) == 0 || len(p.Rules) > maxRules {
		return fmt.Errorf("plan needs 1 to %d rules, has %d", maxRules, len(p.Rules))
	}
	byID := map[string]Entity{}
	for _, e := range catalog {
		byID[e.ID] = e
	}
	for i, r := range p.Rules {
		e, ok := byID[r.EntityID]
		if !ok {
			return fmt.Errorf("rule %d: unknown entity %s", i+1, r.EntityID)
		}
		switch r.When {
		case WhenAlways, WhenNight, WhenDay:
		default:
			return fmt.Errorf("rule %d: when must be always, night or day", i+1)
		}
		switch r.Kind {
		case KindUsage:
			if !e.counter() {
				return fmt.Errorf("rule %d: %s is not a counter (state_class total)", i+1, r.EntityID)
			}
		case KindLevel:
			if e.StateClass != "measurement" {
				return fmt.Errorf("rule %d: %s has no statistics (state_class measurement)", i+1, r.EntityID)
			}
			if r.Direction != "high" && r.Direction != "low" && r.Direction != "both" {
				return fmt.Errorf("rule %d: direction must be high, low or both", i+1)
			}
		case KindState:
			if len(r.States) == 0 {
				return fmt.Errorf("rule %d: states are missing", i+1)
			}
		case KindThreshold:
			if !e.numeric() || (r.Above == nil && r.Below == nil) {
				return fmt.Errorf("rule %d: threshold needs a numeric entity and above or below", i+1)
			}
		default:
			return fmt.Errorf("rule %d: unknown kind %q", i+1, r.Kind)
		}
	}
	return nil
}

// repair fixes what the model often mixes up and the catalog makes clear:
// a counter planned as a level or the other way round, a level without a
// direction.
func repair(p *Plan, catalog []Entity) {
	byID := map[string]Entity{}
	for _, e := range catalog {
		byID[e.ID] = e
	}
	for i := range p.Rules {
		r := &p.Rules[i]
		e, ok := byID[r.EntityID]
		if !ok {
			continue
		}
		switch {
		case r.Kind == KindUsage && e.StateClass == "measurement":
			r.Kind = KindLevel
		case r.Kind == KindLevel && e.counter():
			r.Kind = KindUsage
		}
		if r.Kind == KindLevel && r.Direction == "" {
			r.Direction = "high"
		}
		if r.When == "" {
			r.When = WhenAlways
		}
	}
}

// planSchema constrains the planner's answer.
const planSchema = `{"type":"object","properties":{
"title":{"type":"string"},"summary":{"type":"string"},
"rules":{"type":"array","items":{"type":"object","properties":{
"entity_id":{"type":"string"},"kind":{"type":"string","enum":["usage","level","state","threshold"]},
"when":{"type":"string","enum":["always","night","day"]},"direction":{"type":"string","enum":["high","low","both"]},
"states":{"type":"array","items":{"type":"string"}},"above":{"type":"number"},"below":{"type":"number"},
"why":{"type":"string"}},"required":["entity_id","kind","when","why"]}}},
"required":["title","summary","rules"]}`

// judgeSchema constrains the verdict on a candidate.
const judgeSchema = `{"type":"object","properties":{
"importance":{"type":"integer","minimum":0,"maximum":3},"spoken":{"type":"string"},"details":{"type":"string"}},
"required":["importance","spoken"]}`
