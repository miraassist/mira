package main

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/ui"
)

// Scheduled jobs on the page: docs/spec/scheduled-jobs.md, step 2.

const jobsListe = `---
name: liste
description: Eine Liste.
metadata:
  mira:
    jobs:
      liste:
        description: Neue Videos zu Themen
        parameters:
          themen:     { description: Die Themen, required: true }
          empfaenger: { description: Wer sie bekommt }
        to: "{{empfaenger}}"
        task: Such seit {{since}} zu {{themen}}.
        first:
          - { name: morgens, cron: "0 7 * * *", parameters: { themen: A } }
          - { name: leer, cron: "0 9 * * *" }
---
Anleitung.
`

const jobsAlt = `---
name: alt
description: Ein Skill von vorher.
metadata:
  mira:
    schedules:
      - name: morgen
        cron: "0 8 * * *"
        task: Morgens.
      - name: abend
        cron: "0 18 * * *"
        task: Abends.
---
Anleitung.
`

// jobsApp is an app with mira.yaml (its skills section as given), the
// skills liste and alt, the ones named in skillsYAML switched on.
func jobsApp(t *testing.T, skillsYAML string) (*app, string, *bytes.Buffer) {
	t.Helper()
	a, _, path := editApp(t)
	var logs bytes.Buffer
	a.log = slog.New(slog.NewTextHandler(&logs, nil))
	dir := t.TempDir()
	writeSkill(t, dir, "liste", jobsListe)
	writeSkill(t, dir, "alt", jobsAlt)
	if err := os.WriteFile(path, []byte(editConfig+skillsYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadEnabled(path)
	if err != nil {
		t.Fatal(err)
	}
	a.cfg.Skills.Dirs = []string{dir}
	a.cfg.Skills.Enabled = cfg
	loaded, problems := skills.Load(a.cfg.Skills.Dirs, a.cfg.Skills.Enabled)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	a.skills = &skills.Set{Skills: loaded}
	a.skillsOn = loaded
	a.runner = &skills.Runner{Set: a.skills, Store: a.store, Logger: a.log}
	return a, path, &logs
}

// loadEnabled reads the skills section of a mira.yaml as Mira does.
func loadEnabled(path string) (map[string]map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Skills map[string]map[string]any `yaml:"skills"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	out := map[string]map[string]any{}
	for k, v := range doc.Skills {
		if v == nil {
			v = map[string]any{}
		}
		if on, ok := v["enabled"].(bool); ok && !on {
			continue
		}
		delete(v, "enabled")
		out[k] = v
	}
	return out, nil
}

// SJ-10 The skill's entry shows its templates and jobs
func TestSJ10_TheSkillsEntryShowsItsTemplatesAndJobs(t *testing.T) {
	a, _, _ := jobsApp(t, `skills:
  liste:
    jobs:
      morgens: { template: liste, cron: "0 7 * * *", themen: A }
      abends: { template: liste, cron: "0 18 * * 1-5", themen: B, enabled: false }
`)
	var info ui.SkillInfo
	a.jobsInfo(a.skillsOn[0], a.runner.Schedules("liste"), &info)
	if len(info.Templates) != 1 || info.Templates[0].Key != "liste" || len(info.Templates[0].Parameters) != 2 ||
		info.Templates[0].Parameters[1].Name != "themen" || !info.Templates[0].Parameters[1].Required {
		t.Errorf("templates %+v", info.Templates)
	}
	if len(info.Jobs) != 2 || info.Jobs[0].Name != "abends" || info.Jobs[0].Enabled || info.Jobs[0].Time != "18:00" ||
		len(info.Jobs[0].Days) != 5 || info.Jobs[0].Params["themen"] != "B" || info.Jobs[1].Next == "" {
		t.Errorf("jobs %+v", info.Jobs)
	}
}

// SJ-11 Saving and deleting a job writes mira.yaml
func TestSJ11_SavingAndDeletingAJobWritesMiraYaml(t *testing.T) {
	a, path, _ := jobsApp(t, "skills:\n  liste: {}\n")
	j := jobs{a}
	err := j.SaveJob(context.Background(), "liste", "abends", ui.JobEdit{Template: "liste", Time: "18:00",
		Days: []int{0, 1, 2, 3, 4, 5, 6}, Enabled: true, Params: map[string]string{"themen": "lokale KI", "empfaenger": ""}})
	if err != nil {
		t.Fatal(err)
	}
	out := read(t, path)
	for _, want := range []string{"jobs:", "morgens:", "abends:", "template: liste", `cron: "0 18 * * *"`, "themen: lokale KI"} {
		if !strings.Contains(out, want) {
			t.Errorf("mira.yaml lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "empfaenger") || strings.Contains(out, "leer:") {
		t.Errorf("an empty value or the first job without a value is written:\n%s", out)
	}
	if !a.edit.changed {
		t.Error("a job does not ask for a restart")
	}
	// A required value missing is refused.
	if err := j.SaveJob(context.Background(), "liste", "x", ui.JobEdit{Template: "liste", Time: "07:00", Days: []int{1}}); err == nil ||
		!strings.Contains(err.Error(), "themen") {
		t.Errorf("without themen: %v", err)
	}
	for _, name := range []string{"morgens", "abends"} {
		if err := j.DeleteJob(context.Background(), "liste", name); err != nil {
			t.Fatal(err)
		}
	}
	if out := read(t, path); !strings.Contains(out, "jobs: {}") || strings.Contains(out, "morgens") {
		t.Errorf("after deleting the last:\n%s", out)
	}
}

// SJ-12 Writing the skill's entry moves schedules to jobs
func TestSJ12_WritingTheSkillsEntryMovesSchedulesToJobs(t *testing.T) {
	a, path, logs := jobsApp(t, "skills:\n  alt:\n    schedules:\n      abend: { cron: \"0 19 * * *\" }\n")
	if err := a.edit.SaveSkill(context.Background(), "alt", ui.SkillEdit{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	out := read(t, path)
	if strings.Contains(out, "schedules") || !strings.Contains(out, "morgen:") || !strings.Contains(out, `cron: "0 19 * * *"`) ||
		!strings.Contains(out, "template: abend") {
		t.Errorf("mira.yaml:\n%s", out)
	}
	if !strings.Contains(logs.String(), `msg="schedules moved to jobs" skill=alt jobs=2`) {
		t.Errorf("log %s", logs)
	}
	// What was moved loads as before.
	cfg, _ := loadEnabled(path)
	loaded, problems := skills.Load(a.cfg.Skills.Dirs, map[string]map[string]any{"alt": cfg["alt"]})
	if len(problems) > 0 || loaded[0].Schedules[0].Name != "abend" || loaded[0].Schedules[0].Cron != "0 19 * * *" {
		t.Errorf("%v %+v", problems, loaded)
	}
}

// SJ-13 Switching a skill on writes its first jobs
func TestSJ13_SwitchingASkillOnWritesItsFirstJobs(t *testing.T) {
	a, path, _ := jobsApp(t, "skills:\n  liste: { enabled: false }\n")
	if err := a.edit.SaveSkill(context.Background(), "liste", ui.SkillEdit{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	out := read(t, path)
	if !strings.Contains(out, "morgens:") || !strings.Contains(out, "themen: A") || strings.Contains(out, "leer:") ||
		strings.Contains(out, "enabled: false") {
		t.Errorf("mira.yaml:\n%s", out)
	}
}

// SJ-14 The page Zeitplan changes a job's time in jobs
func TestSJ14_ThePageZeitplanChangesAJobsTimeInJobs(t *testing.T) {
	a, path, _ := jobsApp(t, "skills:\n  liste: {}\n")
	if err := (schedules{a}).Change(context.Background(), "liste", "morgens", ui.ScheduleEdit{Time: "06:30", Days: []int{1, 2, 3, 4, 5}}); err != nil {
		t.Fatal(err)
	}
	out := read(t, path)
	if !strings.Contains(out, "morgens:") || !strings.Contains(out, `cron: "30 6 * * 1-5"`) || strings.Contains(out, "schedules") {
		t.Errorf("mira.yaml:\n%s", out)
	}
	if sc := a.runner.Schedules("liste"); sc[0].Cron != "30 6 * * 1-5" {
		t.Errorf("running at %s", sc[0].Cron)
	}
}
