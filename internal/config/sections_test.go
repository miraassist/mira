package config

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

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

// The persona is a section of its own: plain text, trimmed, bounded.
func TestPersonaSection(t *testing.T) {
	c := Default()
	y, err := SectionYAML("persona", json.RawMessage(`"  Trocken und frech.\nKeine Floskeln.  "`))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ApplySection("persona", y); err != nil {
		t.Fatal(err)
	}
	if c.Prompt.Persona != "Trocken und frech.\nKeine Floskeln." {
		t.Errorf("persona = %q", c.Prompt.Persona)
	}
	raw, _ := c.SectionJSON("persona")
	if string(raw) != `"Trocken und frech.\nKeine Floskeln."` {
		t.Errorf("persona as JSON: %s", raw)
	}
	c.Prompt.Persona = strings.Repeat("x", MaxPersonaChars+1)
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "prompt.persona") {
		t.Errorf("want a length error, got %v", err)
	}
}

// A held-back own message is on by default and may be turned off, not below 0.
func TestOwnMessageHoldConfig(t *testing.T) {
	if got := Default().Notifier.OwnMessageHold.Duration(); got != 10*time.Minute {
		t.Errorf("default: %v", got)
	}
	c := Default()
	c.Notifier.OwnMessageHold = Duration(-time.Minute)
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "own_message_hold") {
		t.Errorf("negative accepted: %v", err)
	}
	c.Notifier.OwnMessageHold = 0
	if err := c.Validate(); err != nil {
		t.Errorf("0 rejected: %v", err)
	}
}

// What WithoutDefaults leaves is read exactly as the whole section: the
// parts left out come back as their defaults, also inside a part.
func TestWithoutDefaultsReadsAsTheWhole(t *testing.T) {
	c := Default()
	c.Notifier.Cooldown = Duration(10 * time.Minute)
	c.Notifier.QuietHours.From = mustClock(t, "23:00")
	c.Persons = map[string]Person{"anna": {Entity: "person.anna", AloneIsMe: true}}
	c.Satellites = map[string]string{"bad": "assist_satellite.bad"}
	c.Prompt.Persona = "Kurz und freundlich."
	for _, name := range UISections {
		whole, err := c.SectionJSON(name)
		if err != nil {
			t.Fatal(err)
		}
		set, err := WithoutDefaults(name, whole)
		if err != nil {
			t.Fatal(err)
		}
		read := func(raw json.RawMessage) string {
			y, err := SectionYAML(name, raw)
			if err != nil {
				t.Fatal(err)
			}
			r := Default()
			if err := r.ApplySection(name, y); err != nil {
				t.Fatal(err)
			}
			out, _ := r.SectionJSON(name)
			return string(out)
		}
		if a, b := read(whole), read(set); a != b {
			t.Errorf("%s: whole reads %s, without defaults %s (%s)", name, a, b, set)
		}
	}
	set, _ := WithoutDefaults("notifier", mustJSON(t, c, "notifier"))
	if string(set) != `{"cooldown":"10m","quiet_hours":{"from":"23:00"}}` {
		t.Errorf("notifier without defaults: %s", set)
	}
	def := Default()
	if set, _ := WithoutDefaults("notifier", mustJSON(t, def, "notifier")); string(set) != "null" {
		t.Errorf("all defaults: %s", set)
	}
	// "30m" as the page may send it is the default "30m0s".
	if set, _ := WithoutDefaults("notifier", json.RawMessage(`{"cooldown":"30m"}`)); string(set) != "null" {
		t.Errorf("30m: %s", set)
	}
}

func mustClock(t *testing.T, s string) ClockTime {
	t.Helper()
	c, err := ParseClockTime(s)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func mustJSON(t *testing.T, c Config, name string) json.RawMessage {
	t.Helper()
	raw, err := c.SectionJSON(name)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
