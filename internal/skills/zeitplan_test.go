package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Schedules on Mira's page (docs/spec/zeitplan.md): names and mira.yaml.

const scheduledSkill = `---
name: plan
description: Ein Skill mit Zeitplänen.
metadata:
  mira:
    schedules:
      - name: morgen
        description: Am Morgen
        cron: "30 5 * * *"
        task: Etwas am Morgen.
      - name: abend
        cron: "0 18 * * *"
        task: Etwas am Abend.
%s
---
Anleitung.
`

func loadPlan(t *testing.T, extra string, settings map[string]any) (Skill, []Problem) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "plan"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Replace(scheduledSkill, "%s\n", extra, 1)
	if err := os.WriteFile(filepath.Join(dir, "plan", "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	list, problems := Load([]string{dir}, map[string]map[string]any{"plan": settings})
	if len(list) == 1 {
		return list[0], problems
	}
	return Skill{}, problems
}

func TestZP01_an_entry_in_mira_yaml_replaces_the_skills_time(t *testing.T) {
	sk, problems := loadPlan(t, "", map[string]any{"schedules": map[string]any{
		"morgen": map[string]any{"cron": "45 6 * * 1-5"},
	}})
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	m := sk.Schedules[0]
	if m.Name != "morgen" || m.Description != "Am Morgen" || m.Cron != "45 6 * * 1-5" || m.Default != "30 5 * * *" {
		t.Fatalf("schedule: %+v", m)
	}
	r := &Runner{Set: berlinSet(t, sk)} // Wednesday 2026-10-21, 18:00
	if got := r.NextRun(m).Format("Mon 15:04"); got != "Thu 06:45" {
		t.Errorf("next run %s", got)
	}
}

func TestZP02_a_schedule_switched_off_does_not_run_and_keeps_its_time(t *testing.T) {
	sk, problems := loadPlan(t, "", map[string]any{"schedules": map[string]any{
		"abend": map[string]any{"enabled": false},
	}})
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	a := sk.Schedules[1]
	if !a.Off || a.Cron != "0 18 * * *" {
		t.Fatalf("schedule: %+v", a)
	}
	r := &Runner{Set: berlinSet(t, sk)}
	if !r.NextRun(a).IsZero() {
		t.Errorf("an off schedule has a next run")
	}
	for _, j := range r.jobs() {
		if j.sched.Name == "abend" {
			t.Errorf("an off schedule is a job")
		}
	}
}

func TestZP03_without_an_entry_the_skills_own_time_holds(t *testing.T) {
	sk, problems := loadPlan(t, "", nil)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	if m := sk.Schedules[0]; m.Cron != "30 5 * * *" || m.Off {
		t.Errorf("schedule: %+v", m)
	}
}

func TestZP04_an_entry_for_a_schedule_the_skill_has_not_is_refused(t *testing.T) {
	_, problems := loadPlan(t, "", map[string]any{"schedules": map[string]any{
		"mittag": map[string]any{"cron": "0 12 * * *"},
	}})
	if len(problems) != 1 || !strings.Contains(problems[0].Error(), `no schedule "mittag"`) {
		t.Errorf("problems: %v", problems)
	}
	for _, bad := range []map[string]any{
		{"morgen": map[string]any{"cron": "jeden Tag"}},
		{"morgen": map[string]any{"enabled": "nein"}},
		{"morgen": map[string]any{"zeit": "06:45"}},
	} {
		if _, problems := loadPlan(t, "", map[string]any{"schedules": bad}); len(problems) != 1 {
			t.Errorf("%v: problems %v", bad, problems)
		}
	}
}

func TestZP05_two_schedules_of_a_skill_with_one_name_stop_the_skill(t *testing.T) {
	_, problems := loadPlan(t, `      - name: morgen
        cron: "0 7 * * *"
        task: Noch einmal am Morgen.
`, nil)
	if len(problems) != 1 || !strings.Contains(problems[0].Error(), `"morgen"`) {
		t.Errorf("problems: %v", problems)
	}
}

func TestZP06_a_change_holds_at_once(t *testing.T) {
	sk, _ := loadPlan(t, "", nil)
	r := &Runner{Set: berlinSet(t, sk)}
	wake := r.wakeup()
	if err := r.Change("plan", "morgen", ScheduleChange{Cron: "45 6 * * *"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-wake:
	case <-time.After(time.Second):
		t.Fatal("the scheduler was not woken")
	}
	if !r.changed["plan/0"] {
		t.Errorf("changed: %v", r.changed)
	}
	if got := r.NextRun(r.Schedules("plan")[0]).Format("15:04"); got != "06:45" {
		t.Errorf("next run %s", got)
	}
	if err := r.Change("plan", "abend", ScheduleChange{Off: true}); err != nil || !r.Schedules("plan")[1].Off {
		t.Errorf("off: %v", err)
	}
	if err := r.Change("plan", "mittag", ScheduleChange{}); err == nil {
		t.Error("an unknown schedule was changed")
	}
}
