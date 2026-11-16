package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/miraassist/mira/internal/trace"
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

// The eval skills the scenarios name are switched on from evals/skills,
// after the config's own folders (docs/spec/evals.md, Remote agents and
// delegate).
func TestEvalSwitchesOnTheScenariosEvalSkills(t *testing.T) {
	var cfg map[string]any
	if err := yaml.Unmarshal([]byte("skills:\n  dirs: [skills]\n  abfall: {calendar: calendar.abfuhr}\n"), &cfg); err != nil {
		t.Fatal(err)
	}
	withEvalSkills(cfg, []string{"hermes"}, evalSkillsDir)
	skills := cfg["skills"].(map[string]any)
	if dirs := skills["dirs"].([]any); len(dirs) != 2 || dirs[0] != "skills" || dirs[1] != evalSkillsDir {
		t.Errorf("dirs %v", dirs)
	}
	if _, ok := skills["hermes"]; !ok {
		t.Error("hermes is not on")
	}
	if _, ok := skills["abfall"]; !ok {
		t.Error("abfall is gone")
	}
	untouched := map[string]any{}
	withEvalSkills(untouched, nil, evalSkillsDir)
	if len(untouched) != 0 {
		t.Errorf("without eval skills the config changed: %v", untouched)
	}
}

// skill_open names the skill in the trace, so a scenario can forbid one.
func TestEvalTraceNamesTheOpenedSkill(t *testing.T) {
	if got := traceKey(trace.Event{Kind: trace.Tool, Name: "skill_open", Detail: `{"name":"hermes"}`}); got != "tool:skill_open:hermes" {
		t.Errorf("skill_open: %q", got)
	}
	if got := traceKey(trace.Event{Kind: trace.Tool, Name: "nachdenken", Detail: `{"input":"x"}`}); got != "tool:nachdenken" {
		t.Errorf("an agent's tool: %q", got)
	}
}

// The shipped skill recherche is left out of an eval run: its agent would
// research for real, and the eval skill hermes stands for it.
func TestEvalLeavesOutTheRealResearchSkill(t *testing.T) {
	var cfg map[string]any
	if err := yaml.Unmarshal([]byte("skills:\n  dirs: [skills]\n  recherche: {url: http://127.0.0.1:9900}\n  abfall: {}\n"), &cfg); err != nil {
		t.Fatal(err)
	}
	withEvalSkills(cfg, []string{"hermes"}, evalSkillsDir)
	if got := withoutRealRemoteAgents(cfg); len(got) != 1 || got[0] != "recherche" {
		t.Errorf("left out %v", got)
	}
	skills := cfg["skills"].(map[string]any)
	if _, ok := skills["recherche"]; ok {
		t.Error("recherche is still on")
	}
	for _, kept := range []string{"hermes", "abfall"} {
		if _, ok := skills[kept]; !ok {
			t.Errorf("%s is gone", kept)
		}
	}
}

// An entry of must with "|" is met by one of its alternatives
// (docs/spec/evals.md, Remote agents and delegate, decision 6).
func TestEvalMustWithAlternatives(t *testing.T) {
	b := Behaviour{Must: []string{"thread:hermes|tool:nachdenken"}}
	for _, c := range []struct {
		trace []string
		ok    bool
	}{
		{[]string{"tool:skill_open:hermes", "thread:hermes:collecting"}, true},
		{[]string{"tool:skill_open:nachdenken", "tool:nachdenken"}, true},
		{[]string{"tool:skill_open:nachdenken"}, false},
	} {
		got := checkBehaviour(b, StepResult{Trace: c.trace})
		if (len(got) == 0) != c.ok {
			t.Errorf("%v: %v", c.trace, got)
		}
	}
}

// --evals names a folder laid out as evals/: its scenarios, eval skills and
// results are used; a folder without scenarios ends the run (docs/spec/evals.md,
// Evals of private skills).
func TestEvalsFolderFromTheFlag(t *testing.T) {
	root := t.TempDir()
	if ev, sk, err := evalsFolders(root, ""); err != nil || ev != filepath.Join(root, "evals") || sk != evalSkillsDir {
		t.Errorf("default: %q %q %v", ev, sk, err)
	}
	private := filepath.Join(t.TempDir(), "evals")
	if _, _, err := evalsFolders(root, private); err == nil || !strings.Contains(err.Error(), private) {
		t.Errorf("a folder without scenarios: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(private, "scenarios"), 0o755); err != nil {
		t.Fatal(err)
	}
	if ev, sk, err := evalsFolders(root, private); err != nil || ev != private || sk != filepath.Join(private, "skills") {
		t.Errorf("private: %q %q %v", ev, sk, err)
	}
}

// A reference with a fallback, as the private skills' folder in the test
// config, is not missing when testenv/.env lacks it (docs/spec/evals.md,
// Evals of private skills, decision 2).
func TestEvalEnvTakesAFallbackAsSet(t *testing.T) {
	r := &evalRun{env: env{"HA_TOKEN": "t"}}
	got, err := r.agentEnv("token: ${HA_TOKEN}\nskills: {dirs: [skills, \"${MIRA_PRIVATE_SKILLS:-skills}\"]}\n")
	if err != nil {
		t.Fatalf("a fallback counted as missing: %v", err)
	}
	for _, e := range got {
		if strings.HasPrefix(e, "MIRA_PRIVATE_SKILLS=") {
			t.Errorf("an unset variable was passed: %s", e)
		}
	}
	if _, err := r.agentEnv("token: ${NOPE}\n"); err == nil {
		t.Error("a reference without a fallback is missing")
	}
}
