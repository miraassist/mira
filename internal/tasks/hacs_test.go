package tasks

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/ha/automation"
)

// The tasks are saved and deleted through Home Assistant's automation config
// API with the owner's token (docs/spec/hacs-default.md, 3).

var waterTask = map[string]any{
	"alias":    "Wasser",
	"triggers": []any{map[string]any{"trigger": "time_pattern", "minutes": 15}},
	"actions":  []any{map[string]any{"action": "mira.assess", "data": map[string]any{"goal": "Wasser"}}},
}

// HD-23 A task is saved through Home Assistant's API
func TestHD23_ATaskIsSavedThroughHomeAssistantsAPI(t *testing.T) {
	e := setup(t)
	if err := e.m.SaveAutomation(context.Background(), "mira_wasser", waterTask); err != nil {
		t.Fatal(err)
	}
	if e.ha.commands[0]["type"] != "mira/automation/validate" {
		t.Errorf("not checked first: %v", e.ha.commands)
	}
	if e.ha.automations["mira_wasser"] == nil || len(e.ha.reloads) != 1 || e.ha.reloads[0] != "mira_wasser" {
		t.Errorf("saved %v, reloaded %v", e.ha.automations, e.ha.reloads)
	}
	if e.ha.labels["mira_wasser"] != automation.LabelTask {
		t.Errorf("labels: %v", e.ha.labels)
	}

	// A config the integration refuses is not saved.
	e.ha.invalid = "sensor.gibtsnicht"
	bad := map[string]any{"alias": "x", "triggers": waterTask["triggers"],
		"actions": []any{map[string]any{"action": "light.turn_on", "target": map[string]any{"entity_id": "sensor.gibtsnicht"}}}}
	if err := e.m.SaveAutomation(context.Background(), "mira_x", bad); err == nil || e.ha.automations["mira_x"] != nil {
		t.Errorf("saved despite the check: %v", err)
	}
}

// HD-24 Only Mira's automations are deleted
func TestHD24_OnlyMirasAutomationsAreDeleted(t *testing.T) {
	e := setup(t)
	e.ha.automations["mira_wasser"] = waterTask // listed by the integration: labelled "Mira"
	err := e.m.Delete(context.Background(), "abendlicht")
	if err == nil || err.Error() != "Das ist keine Aufgabe von Mira." {
		t.Errorf("deleted a household automation: %v", err)
	}
	if err := e.m.Delete(context.Background(), "mira_wasser"); err != nil || e.ha.automations["mira_wasser"] != nil {
		t.Errorf("Mira's task: %v %v", err, e.ha.automations)
	}
}

// HD-26 Without the new access, saving says what to do
func TestHD26_WithoutTheNewAccessSavingSaysWhatToDo(t *testing.T) {
	e := setup(t)
	var logs bytes.Buffer
	e.m.log = slog.New(slog.NewTextHandler(&logs, nil))
	e.ha.notAdmin = true
	err := e.m.SaveAutomation(context.Background(), "mira_wasser", waterTask)
	if err == nil || err.Error() != "Mira braucht erst wieder einen Zugang: Einstellungen → Reparaturen." {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(logs.String(), "could not save an automation") || !strings.Contains(logs.String(), "401") {
		t.Errorf("log: %s", logs.String())
	}
}
