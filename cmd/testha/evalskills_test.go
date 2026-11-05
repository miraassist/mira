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
	if md := (Report{Setup: "s", LeftOut: got}).markdown(); !strings.Contains(md, "Ohne diese Skills (Variable fehlt in testenv/.env): x (MIRA_SKILL_X_CLIENT_SECRET)") {
		t.Errorf("report:\n%s", md)
	}
}
