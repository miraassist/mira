package timer

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// The timer's automation as the service builds it, kept in testdata for the
// integration's tests: Home Assistant checks it there
// (tests/ha/test_built_automations.py). UPDATE=1 writes it anew.
func TestTheBuiltTimerIsKeptForTheIntegrationsCheck(t *testing.T) {
	berlin := time.FixedZone("CET", 3600)
	config := buildOnce(once{ID: "mira_0123837deda7", Title: "Schlaf-Timer Wohnzimmer", Owner: "anna", Area: "wohnzimmer",
		Kind: kindSleep, Due: time.Date(2099, 1, 1, 22, 35, 0, 0, berlin), Actions: []map[string]any{
			{"action": "media_player.media_stop", "target": map[string]any{"entity_id": []string{"media_player.show_speaker"}}},
			{"action": "light.turn_off", "target": map[string]any{"entity_id": []string{"light.farblampe"}}},
		}}, berlin)
	golden(t, "testdata/once.json", []any{config})
}

func golden(t *testing.T, path string, v any) {
	t.Helper()
	got, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if os.Getenv("UPDATE") == "1" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil || string(want) != string(got) {
		t.Errorf("%s differs from what is built; run the test with UPDATE=1 and check the diff (%v)", path, err)
	}
}
