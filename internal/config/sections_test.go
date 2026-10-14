package config

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSectionRoundTrip(t *testing.T) {
	c := Default()
	raw, err := c.SectionJSON("notifier")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"quiet_hours":{"from":"22:30","to":"06:30"}`) || !strings.Contains(string(raw), `"cooldown":"30m"`) {
		t.Fatalf("notifier as JSON: %s", raw)
	}

	var edited map[string]any
	json.Unmarshal(raw, &edited)
	edited["quiet_hours"] = map[string]any{"from": "23:30", "to": "06:00"}
	edited["cooldown"] = "10m"
	back, _ := json.Marshal(edited)
	y, err := SectionYAML("notifier", back)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ApplySection("notifier", y); err != nil {
		t.Fatal(err)
	}
	if c.Notifier.QuietHours.From.String() != "23:30" || c.Notifier.Cooldown.Duration().String() != "10m0s" {
		t.Errorf("notifier = %+v", c.Notifier)
	}
	if c.Notifier.Urgent.MinImportance != 3 {
		t.Error("untouched fields lost their values")
	}
}

func TestApplySectionFillsDefaults(t *testing.T) {
	c := Default()
	c.Notifier.Cooldown = Duration(0)
	if err := c.ApplySection("notifier", []byte("quiet_hours: {from: \"21:00\", to: \"07:00\"}\n")); err != nil {
		t.Fatal(err)
	}
	if c.Notifier.Cooldown != Default().Notifier.Cooldown {
		t.Errorf("missing field did not get its default: %v", c.Notifier.Cooldown)
	}
}

func TestWatchersSection(t *testing.T) {
	c := Default()
	y, err := SectionYAML("watchers", json.RawMessage(`[{"name":"leck","trigger":{"entity_id":"binary_sensor.leck","to":["on"]},"importance":3}]`))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ApplySection("watchers", y); err != nil {
		t.Fatal(err)
	}
	if len(c.Watchers) != 1 || c.Watchers[0].Trigger.EntityID[0] != "binary_sensor.leck" || *c.Watchers[0].Importance != 3 {
		t.Fatalf("watchers = %+v", c.Watchers)
	}
	if _, err := SectionYAML("llm", json.RawMessage(`{}`)); err == nil {
		t.Error("the llm section must not be editable")
	}
}

func TestCompactDurations(t *testing.T) {
	for d, want := range map[string]string{"30m": "30m", "1h": "1h", "1h30m": "1h30m", "90s": "1m30s", "0s": "0s", "2h0m30s": "2h0m30s"} {
		var v Duration
		if err := v.UnmarshalYAML(&yaml.Node{Kind: yaml.ScalarNode, Value: d}); err != nil {
			t.Fatal(err)
		}
		if got, _ := v.MarshalYAML(); got != want {
			t.Errorf("%s -> %v, want %s", d, got, want)
		}
	}
}
