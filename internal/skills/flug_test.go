package skills

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The shipped skill flug (docs/spec/flight-status.md): Home Assistant's
// Flightradar24 integration through the script "Mira Flug", and a skill
// automation that reports a landing.

func flugSkill(t *testing.T) Skill {
	t.Helper()
	sk, p := loadDir("../../skills/flug", nil)
	if p != nil {
		t.Fatalf("flug did not load: %v", p)
	}
	return sk
}

func TestFS01_TheSkillDeclaresItsLandingAutomation(t *testing.T) {
	sk := flugSkill(t)
	if len(sk.Automations) != 1 || sk.Automations[0].ID != "gelandet" || sk.Automations[0].Blueprint != "gelandet.yaml" {
		t.Fatalf("automations: %+v", sk.Automations)
	}
	// Its script, which Mira makes and exposes (docs/spec/skill-files.md,
	// step 5), with the description the model reads as the tool's.
	if len(sk.Scripts) != 1 || sk.Scripts[0].ID != "abfrage" || sk.Scripts[0].Blueprint != "flug.yaml" ||
		!strings.Contains(sk.Scripts[0].Description, "aktion") || len(sk.Scripts[0].Description) > 200 {
		t.Fatalf("scripts: %+v", sk.Scripts)
	}
	if len(sk.Settings) != 0 {
		t.Errorf("a setting: %+v", sk.Settings)
	}
}

// flugBlueprint is the skill's blueprint as Home Assistant reads it.
type flugBlueprint struct {
	Blueprint struct {
		Description string         `yaml:"description"`
		Domain      string         `yaml:"domain"`
		Input       map[string]any `yaml:"input"`
	} `yaml:"blueprint"`
	Triggers []map[string]any `yaml:"triggers"`
	Actions  []map[string]any `yaml:"actions"`
}

func TestFS02_ALandingIsAMessageAndTheFlightIsNoLongerTracked(t *testing.T) {
	raw, err := os.ReadFile("../../skills/flug/blueprints/gelandet.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var b flugBlueprint
	if err := yaml.Unmarshal(raw, &b); err != nil {
		t.Fatalf("blueprints/gelandet.yaml: %v", err)
	}
	if b.Blueprint.Domain != "automation" || !strings.Contains(b.Blueprint.Description, "mira-version: ") || len(b.Blueprint.Input) != 0 {
		t.Errorf("blueprint head: %+v", b.Blueprint)
	}
	if len(b.Triggers) != 1 || b.Triggers[0]["trigger"] != "event" || b.Triggers[0]["event_type"] != "flightradar24_tracked_landed" {
		t.Errorf("triggers: %v", b.Triggers)
	}
	var calls []string
	for _, a := range b.Actions {
		if s, ok := a["action"].(string); ok {
			calls = append(calls, s)
		}
	}
	if strings.Join(calls, ",") != "mira.send_message,text.set_value" {
		t.Fatalf("actions: %v", calls)
	}
	if target := b.Actions[len(b.Actions)-1]["target"].(map[string]any); target["entity_id"] != "text.flightradar24_remove_from_track" {
		t.Errorf("stops tracking: %v", target)
	}
}

func TestFS03_TheModelIsToldTheScriptAndItsAnswer(t *testing.T) {
	sk := flugSkill(t)
	for _, want := range []string{"flug__abfrage", "verspaetung_minuten", "aktion: verfolgen", "aktion: stand", "aktion: vergessen",
		"verfolgt", "ankunft_erwartet", "ankunft_geplant", "noch keine Daten", "Gate", "Datum"} {
		if !strings.Contains(sk.Body, want) {
			t.Errorf("the skill does not say %q", want)
		}
	}
}

// The script blueprint: a script, versioned, with the two fields, and the
// delay in its answer (FS-03's other half: the answer the prose names).
func TestFS04_TheScriptBlueprintGivesTheDelay(t *testing.T) {
	raw, err := os.ReadFile("../../skills/flug/blueprints/flug.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var b struct {
		Blueprint struct {
			Description string `yaml:"description"`
			Domain      string `yaml:"domain"`
		} `yaml:"blueprint"`
		Fields   map[string]any   `yaml:"fields"`
		Sequence []map[string]any `yaml:"sequence"`
	}
	if err := yaml.Unmarshal(raw, &b); err != nil {
		t.Fatalf("blueprints/flug.yaml: %v", err)
	}
	if b.Blueprint.Domain != "script" || !strings.Contains(b.Blueprint.Description, "mira-version: ") {
		t.Errorf("blueprint head: %+v", b.Blueprint)
	}
	if _, ok := b.Fields["aktion"]; !ok || b.Fields["flugnummer"] == nil {
		t.Errorf("fields: %v", b.Fields)
	}
	for _, want := range []string{"heute ", "['Mo','Di','Mi','Do','Fr','Sa','So'][d.weekday()]", "verspaetung_minuten", "text.flightradar24_add_to_track", "text.flightradar24_remove_from_track", "response_variable: antwort"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("the script lacks %q", want)
		}
	}
}

func TestFS05_AFlightThatHasLandedAlreadyIsNotTracked(t *testing.T) {
	raw, err := os.ReadFile("../../skills/flug/blueprints/flug.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var b struct {
		Sequence []map[string]any `yaml:"sequence"`
	}
	if err := yaml.Unmarshal(raw, &b); err != nil {
		t.Fatalf("blueprints/flug.yaml: %v", err)
	}
	// The step after the answer's data: when it shows the flight landed,
	// the remove entity is set again, and the answer says so.
	removes, landed := 0, false
	for _, step := range b.Sequence {
		cond, _ := step["if"].(string)
		then, _ := step["then"].([]any)
		for _, a := range then {
			m, _ := a.(map[string]any)
			if target, _ := m["target"].(map[string]any); target["entity_id"] == "text.flightradar24_remove_from_track" {
				removes++
				if strings.Contains(cond, "schon_gelandet") {
					landed = true
				}
			}
		}
	}
	if removes != 2 || !landed {
		t.Errorf("removes %d, one on schon_gelandet %v", removes, landed)
	}
	if !strings.Contains(string(raw), "'schon_gelandet': schon_gelandet") || !strings.Contains(string(raw), "verfolgt': info.verfolgt and not schon_gelandet") {
		t.Error("the answer does not say schon_gelandet")
	}
	sk := flugSkill(t)
	for _, want := range []string{"schon_gelandet", "nicht verfolgt", "nicht mehr im Auge"} {
		if !strings.Contains(sk.Body, want) {
			t.Errorf("the skill does not say %q", want)
		}
	}
}
