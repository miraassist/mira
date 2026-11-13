package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/ha/structure"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/retry"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/tools"
	"github.com/miraassist/mira/internal/watch"
)

// blueprintHA plays Home Assistant's blueprint commands: every blueprint is
// there in its version; substitute renders the inputs into a trigger and
// Mira's action, or refuses as refuse says.
type blueprintHA struct {
	commands []string
	substs   []map[string]any
	refuse   string
}

func (h *blueprintHA) Call(_ context.Context, cmd map[string]any) (json.RawMessage, error) {
	h.commands = append(h.commands, fmt.Sprint(cmd["type"]))
	switch cmd["type"] {
	case "blueprint/list":
		out := map[string]any{}
		for _, b := range watch.All {
			out[b.Path()] = map[string]any{"metadata": map[string]any{"description": fmt.Sprintf("mira-version: %d", b.Version)}}
		}
		return json.Marshal(out)
	case "blueprint/substitute":
		input := cmd["input"].(map[string]any)
		h.substs = append(h.substs, input)
		if h.refuse != "" {
			return nil, &haws.Error{Command: "blueprint/substitute", Code: "unknown_error", Message: h.refuse}
		}
		return json.Marshal(map[string]any{"substituted_config": map[string]any{
			"mode":     "single",
			"triggers": []any{map[string]any{"trigger": "template", "value_template": "{{ states('" + fmt.Sprint(input["meter"]) + "') }}"}},
			"actions":  []any{map[string]any{"action": "mira.send_message", "data": map[string]any{"text": input["message"], "task": "{{ this.attributes.id }}"}}},
		}})
	}
	return nil, fmt.Errorf("unexpected %v", cmd["type"])
}

// watchSetup is setup with a water meter of device class water and Mira's
// blueprints ready in Home Assistant.
func watchSetup(t *testing.T) (*env, *blueprintHA) {
	t.Helper()
	e := setup(t)
	e.src.states = append(e.src.states, haws.State{EntityID: "sensor.wasserzaehler", State: "81240",
		Attributes: map[string]any{"friendly_name": "Wasserzähler Keller", "device_class": "water", "state_class": "total_increasing", "unit_of_measurement": "L"}})
	exposed := map[string]bool{"sensor.wasserzaehler": true}
	for k, v := range exposedInTests {
		exposed[k] = v
	}
	snap := e.m.opts.Snapshot()
	snap.ExposedEntities = exposed
	bp := &blueprintHA{}
	opts := e.m.opts
	opts.Snapshot = func() structure.Snapshot { return snap }
	opts.Watch = watch.New(watch.Options{HA: bp, Lang: i18n.DE, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Policy: &retry.Policy{Attempts: 1}})
	e.m = New(opts)
	return e, bp
}

const waterWatch = `{"title":"Wasser läuft","summary":"Wenn der Wasserzähler eine halbe Stunde ohne Pause läuft, sag ich Bescheid.","blueprint":"water_runs_on","inputs":{"meter":"sensor.wasserzaehler","duration":30}}`

// CW-05 A wish that fits a blueprint becomes a task from it
func TestCW05_AWishThatFitsABlueprintBecomesATaskFromIt(t *testing.T) {
	// Given the house has a water meter
	e, bp := watchSetup(t)
	// When the user says "Pass auf, ob bei uns Wasser dauernd läuft"
	e.model.answers = []string{waterWatch}
	d, err := e.m.DraftAutomation(context.Background(), "Pass auf, ob bei uns Wasser dauernd läuft", nil, Origin{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.model.instructions[0], "water_runs_on") || !strings.Contains(e.model.instructions[0], "duration (Zahl 5–1440 min, Standard 30)") {
		t.Errorf("the planner did not get the catalog:\n%s", e.model.instructions[0])
	}
	// The rendered automation is what the integration checked.
	var validated map[string]any
	for _, c := range e.ha.commands {
		if c["type"] == "mira/automation/validate" {
			validated = c["config"].(map[string]any)
		}
	}
	if validated == nil || validated["use_blueprint"] != nil || validated["triggers"] == nil {
		t.Errorf("validated: %v", validated)
	}
	// And agrees to the proposal
	content, _ := json.Marshal(d)
	if _, err := e.m.Finisher().Commit(context.Background(), store.Thread{Content: content}); err != nil {
		t.Fatal(err)
	}
	// Then an automation with use_blueprint "mira/water_runs_on.yaml" and the water meter as input is saved
	if len(e.ha.automations) != 1 {
		t.Fatalf("automations: %v", e.ha.automations)
	}
	for id, cfg := range e.ha.automations {
		use, _ := cfg["use_blueprint"].(map[string]any)
		input, _ := use["input"].(map[string]any)
		if use["path"] != "mira/water_runs_on.yaml" || input["meter"] != "sensor.wasserzaehler" || input["duration"] != 30.0 ||
			!strings.Contains(fmt.Sprint(input["message"]), "Wasser ohne Pause") || cfg["triggers"] != nil || cfg["alias"] != "Wasser läuft" {
			t.Errorf("saved: %v", cfg)
		}
		// And it is labelled "Mira" and has a row of kind task
		if e.ha.labels[id] != "tasks" {
			t.Errorf("label: %v", e.ha.labels)
		}
		if own, err := e.st.IsOwnAutomation(context.Background(), id); err != nil || !own {
			t.Errorf("own: %v %v", own, err)
		}
	}
	if len(bp.substs) != 1 {
		t.Errorf("substitutes: %v", bp.substs)
	}
}

// CW-06 The proposal comes from the rendered automation
func TestCW06_TheProposalComesFromTheRenderedAutomation(t *testing.T) {
	// Given the planner chose "water_runs_on" with a duration of 30 minutes
	e, _ := watchSetup(t)
	e.model.answers = []string{strings.Replace(waterWatch, "eine halbe Stunde", "lange", 1)}
	d, err := e.m.DraftAutomation(context.Background(), "Pass auf, ob Wasser dauernd läuft", nil, Origin{})
	if err != nil {
		t.Fatal(err)
	}
	// When the proposal is made
	text := e.m.ProposalText(d)
	// Then it says the water meter and the 30 minutes
	if !strings.Contains(text, "Zähler sensor.wasserzaehler") || !strings.Contains(text, "Dauer 30 min") || !strings.Contains(text, "Pause bis 11 min") {
		t.Errorf("proposal: %s", text)
	}
}

// CW-07 Home Assistant refuses the inputs
func TestCW07_HomeAssistantRefusesTheInputs(t *testing.T) {
	// Given the planner chose a blueprint with an input Home Assistant refuses
	e, bp := watchSetup(t)
	bp.refuse = "Missing input meter"
	e.model.answers = []string{waterWatch}
	// When the draft is checked
	_, err := e.m.DraftAutomation(context.Background(), "Pass auf, ob Wasser dauernd läuft", nil, Origin{})
	// Then the planner gets Home Assistant's answer once
	if len(e.model.instructions) != 2 || !strings.Contains(e.model.instructions[1], "Missing input meter") {
		t.Errorf("instructions: %d", len(e.model.instructions))
	}
	// And if it is refused again, the tool's error names the blueprint and the answer
	if err == nil || !strings.Contains(err.Error(), "mira/water_runs_on.yaml") || !strings.Contains(err.Error(), "Missing input meter") {
		t.Errorf("err: %v", err)
	}
	// And nothing is saved
	if len(e.ha.automations) != 0 || len(bp.substs) != 2 {
		t.Errorf("automations %v, substitutes %d", e.ha.automations, len(bp.substs))
	}
}

// CW-08 A blueprint that does not exist
func TestCW08_ABlueprintThatDoesNotExist(t *testing.T) {
	// Given the planner names a blueprint that is not in the catalog
	e, bp := watchSetup(t)
	e.model.answers = []string{strings.Replace(waterWatch, "water_runs_on", "water_leak", 1), waterWatch}
	// When the draft is checked
	d, err := e.m.DraftAutomation(context.Background(), "Pass auf, ob Wasser dauernd läuft", nil, Origin{})
	if err != nil {
		t.Fatal(err)
	}
	// Then the code refuses it before Home Assistant is asked
	if len(bp.substs) != 1 {
		t.Errorf("substitutes: %d", len(bp.substs))
	}
	// And the planner gets the reason once
	if len(e.model.instructions) != 2 || !strings.Contains(e.model.instructions[1], `den Blueprint "water_leak" gibt es nicht`) {
		t.Errorf("instructions: %v", e.model.instructions[1:])
	}
	if d.Blueprint != "water_runs_on" {
		t.Errorf("draft: %+v", d)
	}
	// An input the blueprint does not have is refused the same way.
	e.model.instructions, bp.substs = nil, nil
	e.model.answers = []string{strings.Replace(waterWatch, `"duration"`, `"minutes"`, 1), waterWatch}
	if _, err := e.m.DraftAutomation(context.Background(), "x", nil, Origin{}); err != nil || len(bp.substs) != 1 ||
		!strings.Contains(e.model.instructions[1], `keine Eingabe "minutes"`) {
		t.Errorf("unknown input: %v %d", err, len(bp.substs))
	}
}

// CW-09 A wish without a blueprint is drafted as before
func TestCW09_AWishWithoutABlueprintIsDraftedAsBefore(t *testing.T) {
	e, bp := watchSetup(t)
	// When the user says "Sag Bescheid, wenn die Waschmaschine fertig ist"
	e.model.answers = []string{draft}
	d, err := e.m.DraftAutomation(context.Background(), "Sag Bescheid, wenn die Waschmaschine fertig ist", nil, Origin{})
	// Then the planner drafts an automation of its own as today
	if err != nil || d.Blueprint != "" || d.Rendered != nil || d.Automation["triggers"] == nil {
		t.Fatalf("%+v %v", d, err)
	}
	if !strings.Contains(e.model.instructions[0], "Blueprints:") || len(bp.substs) != 0 {
		t.Errorf("catalog offered %v, substitutes %d", strings.Contains(e.model.instructions[0], "Blueprints:"), len(bp.substs))
	}
	content, _ := json.Marshal(d)
	if _, err := e.m.Finisher().Commit(context.Background(), store.Thread{Content: content}); err != nil || len(e.ha.automations) != 1 {
		t.Fatalf("commit: %v %v", err, e.ha.automations)
	}
}

// saveWatch drafts and saves the water watch; its automation id.
func saveWatch(t *testing.T, e *env) string {
	t.Helper()
	e.model.answers = []string{waterWatch}
	d, err := e.m.DraftAutomation(context.Background(), "Pass auf, ob Wasser dauernd läuft", nil, Origin{})
	if err != nil {
		t.Fatal(err)
	}
	content, _ := json.Marshal(d)
	if _, err := e.m.Finisher().Commit(context.Background(), store.Thread{Content: content}); err != nil {
		t.Fatal(err)
	}
	for id := range e.ha.automations {
		return id
	}
	t.Fatal("nothing saved")
	return ""
}

func runTool(t *testing.T, e *env, name, args string) string {
	t.Helper()
	for _, tool := range e.m.Tools() {
		if tool.Name == name {
			out, err := tool.Run(context.Background(), tools.Invocation{Args: json.RawMessage(args)})
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			return out
		}
	}
	t.Fatalf("no tool %s", name)
	return ""
}

// CW-10 A watch is deleted like a task
func TestCW10_AWatchIsDeletedLikeATask(t *testing.T) {
	// Given a watch made from a blueprint
	e, bp := watchSetup(t)
	id := saveWatch(t, e)
	// When the user says "Pass nicht mehr aufs Wasser auf"
	runTool(t, e, ToolTaskDelete, `{"id":"`+id+`"}`)
	// Then its automation is deleted
	if len(e.ha.automations) != 0 {
		t.Errorf("automations: %v", e.ha.automations)
	}
	// And the blueprint stays in Home Assistant
	for _, c := range bp.commands {
		if c != "blueprint/list" && c != "blueprint/substitute" {
			t.Errorf("blueprint command %s", c)
		}
	}
}

// CW-21 A watch is switched off and on by voice
func TestCW21_AWatchIsSwitchedOffAndOnByVoice(t *testing.T) {
	// Given a watch "water_never_rests"
	e, _ := watchSetup(t)
	id := saveWatch(t, e)
	// When the user says "Pass erstmal nicht aufs Wasser auf"
	runTool(t, e, ToolTaskEnable, `{"id":"`+id+`","on":false}`)
	// Then its automation is switched off, not deleted
	if len(e.ha.services) != 1 || e.ha.services[0] != "automation.turn_off automation."+id || len(e.ha.automations) != 1 {
		t.Errorf("services %v, automations %d", e.ha.services, len(e.ha.automations))
	}
	// And "Pass wieder aufs Wasser auf" switches it on
	runTool(t, e, ToolTaskEnable, `{"id":"`+id+`","on":true}`)
	if len(e.ha.services) != 2 || e.ha.services[1] != "automation.turn_on automation."+id {
		t.Errorf("services %v", e.ha.services)
	}
}

// A watch's inputs are changed by drafting again with the same blueprint
// (docs/spec/device-watch.md, C): the proposal says the values before and
// after, the rendered automation is checked, the task saved as a blueprint's.
func TestWatchIsChangedWithItsBlueprint(t *testing.T) {
	e, bp := watchSetup(t)
	ha := &editHA{fakeHA: fakeHA{automations: map[string]map[string]any{}}, configs: map[string]map[string]any{}}
	e.m.opts.HA = ha
	b, _ := watch.ByName("water_runs_on")
	before := map[string]any{"id": "mira_w", "alias": "Wasser läuft", "description": "Pass auf",
		"use_blueprint": map[string]any{"path": b.Path(), "input": map[string]any{"meter": "sensor.wasserzaehler", "duration": 30, "message": "x"}}}
	ha.configs["automation.wasser_lauft"] = before
	after := `{"title":"Wasser läuft","summary":"bisher nach 30 Minuten, neu nach 45","automation":{"alias":"Wasser läuft","description":"Pass auf","use_blueprint":{"path":"mira/water_runs_on.yaml","input":{"meter":"sensor.wasserzaehler","duration":45,"message":"x"}}}}`
	e.model.answers = []string{after}
	res, err := e.m.EditFinisher().Work(context.Background(), editThread("Wasser läuft (automation.wasser_lauft)", "erst nach 45 Minuten"))
	if err != nil || res.Proposal == "" {
		t.Fatalf("%+v %v", res, err)
	}
	if !strings.Contains(res.Proposal, "Werte bisher: Zähler sensor.wasserzaehler, Pause bis 11 min, Dauer 30 min; neu: Zähler sensor.wasserzaehler, Pause bis 11 min, Dauer 45 min") {
		t.Errorf("proposal: %s", res.Proposal)
	}
	th := editThread("Wasser läuft (automation.wasser_lauft)", "erst nach 45 Minuten")
	th.Content = res.Content
	if _, err := e.m.EditFinisher().Commit(context.Background(), th); err != nil {
		t.Fatal(err)
	}
	saved := ha.saved[0]["config"].(map[string]any)
	use := saved["use_blueprint"].(map[string]any)
	if use["input"].(map[string]any)["duration"] != 45.0 || !strings.Contains(fmt.Sprint(use["input"]), "Wasser ohne Pause") {
		t.Errorf("saved: %v", saved)
	}
	if len(bp.substs) != 1 {
		t.Errorf("substitutes: %d", len(bp.substs))
	}
}

// CW-36 A declined offer: "Pass doch auf …" makes the watch all the same and
// forgets the decline (the offer's half: cmd/mira, TestCW36_ADeclinedOffer).
func TestCW36_MakingTheWatchByVoiceForgetsTheDecline(t *testing.T) {
	e, _ := watchSetup(t)
	ctx := context.Background()
	if err := e.st.DeclineWatch(ctx, "sensor.wasserzaehler", "water_runs_on"); err != nil {
		t.Fatal(err)
	}
	if err := e.st.DeclineWatch(ctx, "sensor.wasserzaehler", "water_never_rests"); err != nil {
		t.Fatal(err)
	}
	e.model.answers = []string{waterWatch}
	d, err := e.m.DraftAutomation(ctx, "Pass doch aufs Wasser auf", nil, Origin{})
	if err != nil {
		t.Fatal(err)
	}
	content, _ := json.Marshal(d)
	if _, err := e.m.Finisher().Commit(ctx, store.Thread{Content: content}); err != nil {
		t.Fatal(err)
	}
	declines, err := e.st.WatchDeclines(ctx)
	if err != nil || len(declines) != 1 || declines[0].Blueprint != "water_never_rests" {
		t.Errorf("declines = %+v %v", declines, err)
	}
}
