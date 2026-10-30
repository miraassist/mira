package tasks

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/tools"
)

// editHA plays the integration's commands for changing tasks.
type editHA struct {
	fakeHA
	listed  []listed
	configs map[string]map[string]any // by entity id; missing: not editable
	old     bool                      // an integration without get and list all
	saved   []map[string]any
}

func (h *editHA) Call(ctx context.Context, cmd map[string]any) (json.RawMessage, error) {
	h.commands = append(h.commands, cmd)
	switch cmd["type"] {
	case "mira/automation/list":
		if h.old {
			return nil, &haws.Error{Command: "mira/automation/list", Code: "invalid_format", Message: "extra keys not allowed"}
		}
		raw, _ := json.Marshal(map[string]any{"automations": h.listed})
		return raw, nil
	case "mira/automation/get":
		if h.old {
			return nil, &haws.Error{Command: "mira/automation/get", Code: "unknown_command", Message: "Unknown command."}
		}
		entity := cmd["entity_id"].(string)
		cfg, ok := h.configs[entity]
		if !ok {
			return nil, &haws.Error{Command: "mira/automation/get", Code: "not_editable", Message: "not in automations.yaml"}
		}
		raw, _ := json.Marshal(map[string]any{"id": cfg["id"], "entity_id": entity, "config": cfg})
		return raw, nil
	case "mira/automation/save":
		h.saved = append(h.saved, cmd)
		return json.RawMessage(`{"id":"x"}`), nil
	}
	return h.fakeHA.Call(ctx, cmd)
}

func editSetup(t *testing.T) (*env, *editHA) {
	e := setup(t)
	ha := &editHA{fakeHA: fakeHA{automations: map[string]map[string]any{}}, configs: map[string]map[string]any{}}
	e.m.opts.HA = ha
	return e, ha
}

func jsonMap(s string) map[string]any {
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		panic(err)
	}
	return m
}

const abendlicht = `{"id":"abendlicht","alias":"Abendlicht Wohnzimmer",
	"triggers":[{"trigger":"sun","event":"sunset"}],
	"actions":[{"action":"light.turn_on","target":{"entity_id":"light.wohnzimmer"}}]}`

// editThread is a thread of aufgabe-aendern with both fields.
func editThread(task, change string) store.Thread {
	return store.Thread{ID: "t1", Target: "aufgabe-aendern", Person: "anna", Place: "person:anna",
		Fields: map[string]string{"aufgabe": task, "aenderung": change}}
}

// AE-10 A task is found by its name
func TestAE10_ATaskIsFoundByItsName(t *testing.T) {
	e, ha := editSetup(t)
	ha.listed = []listed{
		{EntityID: "automation.abendlicht_wohnzimmer", ID: "abendlicht", Name: "Abendlicht Wohnzimmer", Editable: true},
		{EntityID: "automation.abendlicht_kueche", ID: "kueche", Name: "Abendlicht Küche", Editable: true},
	}
	v, err := e.m.CheckTask(context.Background(), "das Abendlicht im Wohnzimmer")
	if err != nil || !strings.HasSuffix(v, "(automation.abendlicht_wohnzimmer)") {
		t.Fatalf("%q %v", v, err)
	}
	if v, err := e.m.CheckTask(context.Background(), "automation.abendlicht_kueche"); err != nil || v != "automation.abendlicht_kueche" {
		t.Errorf("by id: %q %v", v, err)
	}
	if _, err := e.m.CheckTask(context.Background(), "die Bewässerung"); err == nil || !strings.Contains(err.Error(), "Abendlicht Küche") {
		t.Errorf("nothing fits: the tasks are named so the model can pick: %v", err)
	}
}

// AE-11 Two matching tasks are asked about
func TestAE11_TwoMatchingTasksAreAskedAbout(t *testing.T) {
	e, ha := editSetup(t)
	ha.listed = []listed{
		{EntityID: "automation.abendlicht_wohnzimmer", Name: "Abendlicht Wohnzimmer", Editable: true},
		{EntityID: "automation.abendlicht_kueche", Name: "Abendlicht Küche", Editable: true},
	}
	_, err := e.m.CheckTask(context.Background(), "das Abendlicht")
	if err == nil || !strings.Contains(err.Error(), "Abendlicht Wohnzimmer") || !strings.Contains(err.Error(), "Abendlicht Küche") {
		t.Fatalf("%v", err)
	}
}

// AE-12 A task Mira cannot change ends the thread
func TestAE12_ATaskMiraCannotChangeEndsTheThread(t *testing.T) {
	e, ha := editSetup(t)
	ha.listed = []listed{{EntityID: "automation.aus_yaml", Name: "Aus YAML"}}
	v, err := e.m.CheckTask(context.Background(), "die Aufgabe aus YAML")
	if err != nil {
		t.Fatalf("found: %v", err)
	}
	res, err := e.m.EditFinisher().Work(context.Background(), editThread(v, "um 8"))
	if err != nil || !strings.Contains(res.Cancel, "YAML") || res.Proposal != "" {
		t.Fatalf("%+v %v", res, err)
	}
}

// AE-20 The proposal says the condition before and after
func TestAE20_TheProposalSaysTheConditionBeforeAndAfter(t *testing.T) {
	e, ha := editSetup(t)
	ha.configs["automation.abendlicht"] = jsonMap(abendlicht)
	e.model.answers = []string{`{"title":"Abendlicht Wohnzimmer","summary":"Bisher bei Sonnenuntergang, neu 30 Minuten danach.",
		"automation":{"alias":"Abendlicht Wohnzimmer","triggers":[{"trigger":"sun","event":"sunset","offset":"00:30:00"}],
		"conditions":[{"condition":"time","after":"17:00:00"}],
		"actions":[{"action":"light.turn_on","target":{"entity_id":"light.wohnzimmer"}}]}}`}
	res, err := e.m.EditFinisher().Work(context.Background(), editThread("Abendlicht (automation.abendlicht)", "erst 30 Minuten danach"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Abendlicht Wohnzimmer", "neu 30 Minuten danach", "Bedingung bisher: keine; neu: nur ab 17:00 Uhr.", "Soll ich das so ändern?"} {
		if !strings.Contains(res.Proposal, want) {
			t.Errorf("proposal lacks %q: %s", want, res.Proposal)
		}
	}
	if strings.Contains(res.Proposal, "Neu schaltet") || strings.Contains(res.Proposal, "nicht mehr") {
		t.Errorf("the actions did not change: %s", res.Proposal)
	}
	// The drafting model got the task as it is, and the check knew it.
	if !strings.Contains(e.model.instructions[0], `"light.wohnzimmer"`) {
		t.Error("the instruction lacks the task as it is")
	}
	for _, c := range ha.commands {
		if c["type"] == "mira/automation/validate" && c["automation_id"] != "abendlicht" {
			t.Errorf("validate without the task: %v", c)
		}
	}
}

// AE-21 A new action is named by the code
func TestAE21_ANewActionIsNamedByTheCode(t *testing.T) {
	e, ha := editSetup(t)
	ha.configs["automation.abendlicht"] = jsonMap(abendlicht)
	e.model.answers = []string{`{"title":"Abendlicht Wohnzimmer","summary":"Neu läuft dazu das Radio.",
		"automation":{"alias":"Abendlicht Wohnzimmer","triggers":[{"trigger":"sun","event":"sunset"}],
		"actions":[{"action":"light.turn_on","target":{"entity_id":"light.wohnzimmer"}},
		{"action":"media_player.media_play","target":{"entity_id":"media_player.radio"}}]}}`}
	res, err := e.m.EditFinisher().Work(context.Background(), editThread("automation.abendlicht", "dazu das Radio"))
	if err != nil || !strings.Contains(res.Proposal, "Neu schaltet sie außerdem: media_player.media_play (media_player.radio).") {
		t.Fatalf("%+v %v", res, err)
	}
}

// AE-22 A new critical action is refused
func TestAE22_ANewCriticalActionIsRefused(t *testing.T) {
	e, ha := editSetup(t)
	ha.configs["automation.abendlicht"] = jsonMap(abendlicht)
	// The integration refuses what is new and critical; both drafts unlock.
	ha.invalid = "lock.haustuer"
	e.model.answers = []string{`{"title":"Abendlicht Wohnzimmer","summary":"Dazu schließe ich die Tür auf.",
		"automation":{"triggers":[{"trigger":"sun","event":"sunset"}],
		"actions":[{"action":"lock.unlock","target":{"entity_id":"lock.haustuer"}}]}}`}
	th := editThread("automation.abendlicht", "und schließ die Tür auf")
	th.Request = "Abendlicht ändern"
	res, err := e.m.EditFinisher().Work(context.Background(), th)
	if err != nil || res.Proposal != "" {
		t.Fatalf("proposed: %+v %v", res, err)
	}
	if !strings.Contains(res.Cancel, "lock.haustuer") || strings.Contains(res.Cancel, "no valid") {
		t.Errorf("the reason is lost: %q", res.Cancel)
	}
}

// AE-23 An action that was there stays allowed
func TestAE23_AnActionThatWasThereStaysAllowed(t *testing.T) {
	e, ha := editSetup(t)
	ha.configs["automation.morgens"] = jsonMap(`{"id":"morgens","alias":"Tür morgens",
		"triggers":[{"trigger":"time","at":"07:00:00"}],
		"actions":[{"action":"lock.unlock","target":{"entity_id":"lock.haustuer"}}]}`)
	e.model.answers = []string{`{"title":"Tür morgens","summary":"Bisher um 7:00, neu um 7:30.",
		"automation":{"alias":"Tür morgens","triggers":[{"trigger":"time","at":"07:30:00"}],
		"actions":[{"action":"lock.unlock","target":{"entity_id":"lock.haustuer"}}]}}`}
	res, err := e.m.EditFinisher().Work(context.Background(), editThread("automation.morgens", "erst um 7:30"))
	if err != nil || !strings.Contains(res.Proposal, "neu um 7:30") || strings.Contains(res.Proposal, "Neu schaltet") {
		t.Fatalf("%+v %v", res, err)
	}
}

// AE-24 A draft that changes nothing ends the thread
func TestAE24_ADraftThatChangesNothingEndsTheThread(t *testing.T) {
	e, ha := editSetup(t)
	ha.configs["automation.abendlicht"] = jsonMap(abendlicht)
	e.model.answers = []string{`{"title":"Abendlicht Wohnzimmer","summary":"Bleibt bei Sonnenuntergang.",
		"automation":{"alias":"Abendlicht Wohnzimmer","triggers":[{"trigger":"sun","event":"sunset"}],
		"actions":[{"action":"light.turn_on","target":{"entity_id":"light.wohnzimmer"}}]}}`}
	res, err := e.m.EditFinisher().Work(context.Background(), editThread("automation.abendlicht", "bei Sonnenuntergang"))
	if err != nil || !strings.Contains(res.Cancel, "ändert sich damit nichts") {
		t.Fatalf("%+v %v", res, err)
	}
}

// AE-30 After the yes the task is saved under its id
func TestAE30_AfterTheYesTheTaskIsSavedUnderItsID(t *testing.T) {
	e, ha := editSetup(t)
	ha.configs["automation.morgens"] = jsonMap(`{"id":"morgens","alias":"Tür morgens",
		"triggers":[{"trigger":"time","at":"07:00:00"}],
		"actions":[{"action":"lock.unlock","target":{"area_id":"flur"}},{"action":"light.turn_on","target":{"entity_id":"light.flur"}}]}`)
	e.model.answers = []string{`{"title":"Tür morgens","summary":"Bisher um 7:00, neu um 7:30.",
		"automation":{"alias":"Tür morgens","triggers":[{"trigger":"time","at":"07:30:00"}],
		"actions":[{"action":"lock.unlock","target":{"area_id":"flur"}},{"action":"light.turn_on","target":{"entity_id":"light.flur"}}]}}`}
	th := editThread("automation.morgens", "erst um 7:30")
	res, err := e.m.EditFinisher().Work(context.Background(), th)
	if err != nil {
		t.Fatal(err)
	}
	var seen map[string]any
	e.m.opts.Hooks = func(_ context.Context, got store.Thread, args json.RawMessage) tools.Decision {
		_ = json.Unmarshal(args, &seen)
		// A critical target asks for a yes: the thread's yes is it.
		return tools.Decision{Outcome: tools.NeedsConfirmation}
	}
	th.Content = res.Content
	say, err := e.m.EditFinisher().Commit(context.Background(), th)
	if err != nil || !strings.Contains(say, "Tür morgens") {
		t.Fatalf("%q %v", say, err)
	}
	if len(ha.saved) != 1 || ha.saved[0]["automation_id"] != "morgens" || ha.saved[0]["keep_labels"] != true {
		t.Fatalf("saved: %v", ha.saved)
	}
	// The hooks saw the entities and, for the area, the domain.
	if ids, _ := seen["entity_id"].([]any); len(ids) != 1 || ids[0] != "light.flur" {
		t.Errorf("entities: %v", seen)
	}
	if domains, _ := seen["domain"].([]any); len(domains) != 1 || domains[0] != "lock" {
		t.Errorf("domains: %v", seen)
	}

	// A refusal of the hooks saves nothing.
	ha.saved = nil
	e.m.opts.Hooks = func(context.Context, store.Thread, json.RawMessage) tools.Decision {
		return tools.Decision{Outcome: tools.Deny, Reason: "Abgelehnt"}
	}
	if _, err := e.m.EditFinisher().Commit(context.Background(), th); err == nil || len(ha.saved) != 0 {
		t.Fatalf("saved despite the hooks: %v", err)
	}
}

// AE-33 An older integration says it needs an update
func TestAE33_AnOlderIntegrationSaysItNeedsAnUpdate(t *testing.T) {
	e, ha := editSetup(t)
	ha.old = true
	res, err := e.m.EditFinisher().Work(context.Background(), editThread("automation.abendlicht", "um 8"))
	if err != nil || !strings.Contains(res.Cancel, "Update") {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := e.m.CheckTask(context.Background(), "das Abendlicht"); err == nil || !strings.Contains(err.Error(), "Update") {
		t.Fatalf("check: %v", err)
	}
}

// AE-40 The skill aufgabe-aendern starts with its building blocks
func TestAE40_TheSkillAufgabeAendernStartsWithItsBuildingBlocks(t *testing.T) {
	list, problems := skills.Load([]string{"../../skills"}, map[string]map[string]any{"aufgabe-aendern": nil})
	if len(problems) != 0 || len(list) != 1 || list[0].Target == nil {
		t.Fatalf("skill: %v %v", list, problems)
	}
	target := list[0].Target
	if target.Finish.Name != FinishEdit || !target.Confirm {
		t.Errorf("finish: %+v", target)
	}
	checks := map[string]string{}
	for _, f := range target.Fields {
		checks[f.Name] = f.Check
	}
	if checks["aufgabe"] != CheckFind || len(checks) != 2 {
		t.Errorf("fields: %v", checks)
	}
}
