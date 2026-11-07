package main

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// A skill whose settings need a variable testenv/.env lacks is left out of
// the run and named; the others stay (docs/spec/evals.md).
func TestEvalLeavesOutSkillsWithoutTheirVariables(t *testing.T) {
	var cfg map[string]any
	if err := yaml.Unmarshal([]byte(`
skills:
  dirs: [skills]
  paperless: {url: "http://x", token: "${PAPERLESS_API_TOKEN}"}
  x: {client_id: abc, client_secret: "${MIRA_SKILL_X_CLIENT_SECRET}"}
  abfall: {calendar: calendar.abfuhr}
`), &cfg); err != nil {
		t.Fatal(err)
	}
	r := &evalRun{env: env{"PAPERLESS_API_TOKEN": "t"}}
	got := r.withoutUnsetSkills(cfg)
	if strings.Join(got, "|") != "x (MIRA_SKILL_X_CLIENT_SECRET)" {
		t.Errorf("left out %v", got)
	}
	skills := cfg["skills"].(map[string]any)
	for _, kept := range []string{"dirs", "paperless", "abfall"} {
		if _, ok := skills[kept]; !ok {
			t.Errorf("%s was left out", kept)
		}
	}
	if _, ok := skills["x"]; ok {
		t.Error("x is still in the run")
	}
	if md := (Report{Setup: "s", LeftOut: got}).markdown(); !strings.Contains(md, "Without these skills (variable missing in testenv/.env): x (MIRA_SKILL_X_CLIENT_SECRET)") {
		t.Errorf("report:\n%s", md)
	}
}

// A scenario measured again replaces its row in the setup's last report; the
// others stay, and the report says what was measured again.
func TestAScenarioMeasuredAgainGoesIntoTheLastReport(t *testing.T) {
	dir := t.TempDir()
	last := Report{Setup: "s", Date: "2026-11-05 22:20", Mira: "0.16.0", Runs: 3, Scenarios: []ScenarioResult{
		{Name: "abfall", Passed: true}, {Name: "termin-absagen", Passed: false}}}
	if err := last.write(dir); err != nil {
		t.Fatal(err)
	}
	again := Report{Setup: "s", Date: "2026-11-05 23:00", Mira: "0.16.0", Runs: 3, Scenarios: []ScenarioResult{{Name: "termin-absagen", Passed: true}}}
	got, err := again.into(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p, n := got.passed(); p != 2 || n != 2 || got.Date != "2026-11-05 22:20" {
		t.Errorf("passed %d of %d, date %s", p, n, got.Date)
	}
	if !strings.Contains(got.markdown(), "Measured again on their own: termin-absagen (2026-11-05 23:00, Mira 0.16.0, 3 runs)") {
		t.Errorf("report:\n%s", got.markdown())
	}
}
