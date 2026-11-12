package tasks

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/ha/automation"
)

// The tasks are saved and deleted through Home Assistant's automation config
// API with the admin's token Mira was given; she deletes only what she
// created (docs/spec/hacs-default.md, 3 and 4).

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

// HD-40 A new task or timer is recorded as Mira's
func TestHD40_ANewTaskIsRecordedAsMiras(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	if err := e.m.SaveAutomation(ctx, "mira_wasser", waterTask); err != nil {
		t.Fatal(err)
	}
	if mine, err := e.st.IsOwnAutomation(ctx, "mira_wasser"); err != nil || !mine {
		t.Errorf("new task not noted: %v %v", mine, err)
	}
	// A household task changed by Mira keeps its labels and is not hers.
	if err := automation.Save(ctx, e.ha, e.st, e.m.log, "abendlicht", waterTask, automation.Keep); err != nil {
		t.Fatal(err)
	}
	if mine, _ := e.st.IsOwnAutomation(ctx, "abendlicht"); mine {
		t.Error("a household task was noted as Mira's")
	}
}

// HD-41 Only automations with a row are deleted
func TestHD41_OnlyAutomationsWithARowAreDeleted(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	// Labelled "Mira" (listed by the integration), but not created by her.
	e.ha.automations["abendlicht"] = waterTask
	err := e.m.Delete(ctx, "abendlicht")
	if err == nil || err.Error() != "Das ist keine Aufgabe von Mira." || e.ha.automations["abendlicht"] == nil {
		t.Errorf("deleted an automation Mira did not create: %v", err)
	}
	if err := e.m.SaveAutomation(ctx, "mira_wasser", waterTask); err != nil {
		t.Fatal(err)
	}
	if err := e.m.Delete(ctx, "mira_wasser"); err != nil || e.ha.automations["mira_wasser"] != nil {
		t.Errorf("Mira's task: %v %v", err, e.ha.automations)
	}
	if mine, _ := e.st.IsOwnAutomation(ctx, "mira_wasser"); mine {
		t.Error("the row stays after the delete")
	}
}

// failingOwn cannot write its rows.
type failingOwn struct{ automation.Own }

func (failingOwn) RecordOwnAutomation(context.Context, string, string) error {
	return errors.New("database is locked")
}

// HD-43 A failed record leaves a trace
func TestHD43_AFailedRecordLeavesATrace(t *testing.T) {
	e := setup(t)
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	if err := automation.Save(context.Background(), e.ha, failingOwn{e.st}, log, "mira_wasser", waterTask, automation.LabelTask); err != nil {
		t.Fatalf("the task is saved, the tool must say so: %v", err)
	}
	if e.ha.automations["mira_wasser"] == nil {
		t.Error("not saved")
	}
	if l := logs.String(); !strings.Contains(l, "not noted as Mira's") || !strings.Contains(l, "automation=mira_wasser") || !strings.Contains(l, "database is locked") {
		t.Errorf("log: %s", l)
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
