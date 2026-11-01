package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/ui"
)

// The page "Schedules" (docs/spec/zeitplan.md, step 3).

const planSkill = `---
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
      - name: stuendlich
        cron: "0 * * * *"
        task: Jede Stunde.
      - cron: "0 12 * * *"
        task: Ohne Namen.
---
Anleitung.
`

type stepTasker struct{ answer string }

func (s stepTasker) RunTask(context.Context, string, string) (string, error) { return s.answer, nil }

// scheduleApp is an app with a mira.yaml, the skill plan and Mira's own
// profile schedule, its scheduler not running.
func scheduleApp(t *testing.T) (*app, schedules, string) {
	t.Helper()
	a, _, path := editApp(t)
	dir := t.TempDir()
	writeSkill(t, dir, "plan", planSkill)
	writeSkill(t, dir, "aus", "---\nname: aus\ndescription: Ein Skill, der aus ist.\nmetadata:\n  mira:\n    schedules:\n      - name: frueh\n        description: Früh\n        cron: \"0 6 * * *\"\n        task: Früh.\n---\nText.\n")
	writeSkill(t, dir, "frei", "---\nname: frei\ndescription: Ein Skill ohne Zeitplan, der aus ist.\n---\nText.\n")
	a.cfg.Skills.Dirs = []string{dir}
	a.cfg.Skills.Enabled = map[string]map[string]any{"plan": nil}
	loaded, problems := skills.Load(a.cfg.Skills.Dirs, a.cfg.Skills.Enabled)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	a.skills = &skills.Set{Skills: loaded}
	a.runner = &skills.Runner{Set: a.skills, Store: a.store, Logger: a.log,
		Tasker: stepTasker{`{"importance":1,"spoken":"Guten Morgen."}`}}
	job, err := skills.NewSystemJob(profileJob, "Profile neu bauen", "30 3 * * *", func(context.Context) (string, error) {
		return "1 Profile neu gebaut", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	a.runner.System = []*skills.SystemJob{job}
	return a, schedules{a}, path
}

func item(t *testing.T, s schedules, skill, name string) ui.ScheduleItem {
	t.Helper()
	list, err := s.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range list {
		if it.Skill == skill && it.Name == name {
			return it
		}
	}
	t.Fatalf("no schedule %s/%s in %+v", skill, name, list)
	return ui.ScheduleItem{}
}

func on(b bool) *bool { return &b }

func TestZP20_the_list_has_every_schedule(t *testing.T) {
	_, s, _ := scheduleApp(t)
	list, err := s.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, it := range list {
		ids = append(ids, it.Skill+"/"+it.Name)
	}
	if want := []string{"plan/morgen", "plan/abend", "plan/stuendlich", "plan/", "aus/frueh", "mira/profile"}; !slices.Equal(ids, want) {
		t.Fatalf("list %v, want %v", ids, want)
	}
	m := list[0]
	if m.Description != "Am Morgen" || !m.Enabled || !m.Editable || m.Time != "05:30" || len(m.Days) != 7 || m.Next == "" {
		t.Errorf("morgen: %+v", m)
	}
	if aus := list[4]; !aus.SkillOff || aus.Editable || aus.Next != "" {
		t.Errorf("a skill that is off: %+v", aus)
	}
	if own := list[5]; !own.Own || !own.Editable || own.Next == "" {
		t.Errorf("Mira's own: %+v", own)
	}
}

func TestZP21_the_switch_writes_mira_yaml(t *testing.T) {
	a, s, path := scheduleApp(t)
	if err := s.Change(context.Background(), "plan", "abend", ui.ScheduleEdit{Enabled: on(false)}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, path), "schedules:\n      abend:\n        enabled: false") {
		t.Errorf("mira.yaml:\n%s", read(t, path))
	}
	if it := item(t, s, "plan", "abend"); it.Enabled || it.Next != "" {
		t.Errorf("abend: %+v", it)
	}
	if a.edit.changed {
		t.Error("a schedule asks for a restart")
	}
	// on again: the line goes
	if err := s.Change(context.Background(), "plan", "abend", ui.ScheduleEdit{Enabled: on(true)}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read(t, path), "abend") {
		t.Errorf("mira.yaml:\n%s", read(t, path))
	}
	if err := s.Change(context.Background(), skills.SystemSkill, profileJob, ui.ScheduleEdit{Enabled: on(false)}); err == nil {
		t.Error("Mira's own schedule was switched off")
	}
}

func TestZP22_time_and_weekdays_become_a_cron_expression(t *testing.T) {
	_, s, path := scheduleApp(t)
	if err := s.Change(context.Background(), "plan", "morgen", ui.ScheduleEdit{Time: "06:45", Days: []int{1, 2, 3, 4, 5}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, path), `cron: "45 6 * * 1-5"`) && !strings.Contains(read(t, path), `cron: 45 6 * * 1-5`) {
		t.Errorf("mira.yaml:\n%s", read(t, path))
	}
	if it := item(t, s, "plan", "morgen"); it.Cron != "45 6 * * 1-5" || it.Time != "06:45" || !slices.Equal(it.Days, []int{1, 2, 3, 4, 5}) {
		t.Errorf("morgen: %+v", it)
	}
	// back to the skill's own time: no line
	if err := s.Change(context.Background(), "plan", "morgen", ui.ScheduleEdit{Time: "05:30", Days: []int{0, 1, 2, 3, 4, 5, 6}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read(t, path), "morgen") {
		t.Errorf("mira.yaml:\n%s", read(t, path))
	}
	// Mira's own goes to memory.profile_schedule
	if err := s.Change(context.Background(), skills.SystemSkill, profileJob, ui.ScheduleEdit{Time: "02:15", Days: []int{0, 6}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, path), "profile_schedule: 15 2 * * 0,6") && !strings.Contains(read(t, path), `profile_schedule: "15 2 * * 0,6"`) {
		t.Errorf("mira.yaml:\n%s", read(t, path))
	}
	for cron, want := range map[string]string{"0 7 * * *": "07:00 [0 1 2 3 4 5 6]", "5 22 * * 6,0": "22:05 [0 6]", "0 9 * * 1-3,5": "09:00 [1 2 3 5]"} {
		at, days, ok := timeOfCron(cron)
		if got := fmt.Sprint(at, " ", days); !ok || got != want {
			t.Errorf("%s: %q, want %q", cron, got, want)
		}
	}
	if _, err := cronOfTime("06:45", nil); err == nil {
		t.Error("no weekday was taken")
	}
}

func TestZP23_a_time_the_fields_cannot_show_is_not_changed_on_the_page(t *testing.T) {
	_, s, _ := scheduleApp(t)
	it := item(t, s, "plan", "stuendlich")
	if it.Editable || it.Time != "" || it.Cron != "0 * * * *" {
		t.Errorf("stuendlich: %+v", it)
	}
	if err := s.Change(context.Background(), "plan", "stuendlich", ui.ScheduleEdit{Time: "07:00", Days: []int{1}}); err == nil {
		t.Error("an hourly schedule took a time")
	}
	if err := s.Change(context.Background(), "plan", "stuendlich", ui.ScheduleEdit{Enabled: on(false)}); err != nil {
		t.Errorf("its switch: %v", err)
	}
}

func TestZP24_run_now_runs_the_job_and_shows_its_result(t *testing.T) {
	a, s, _ := scheduleApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- a.runner.Run(ctx) }()
	defer func() { cancel(); <-done }()
	for run := 1; run <= 2; run++ {
		if err := s.RunNow("plan", "morgen"); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			it := item(t, s, "plan", "morgen")
			if it.State == "" && len(it.Last) == 1 {
				if it.Last[0].Status != store.RunDone || it.Last[0].Text != "Guten Morgen." {
					t.Errorf("last run: %+v", it.Last)
				}
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("run %d did not end: %+v", run, it)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	msgs, err := a.store.ListMessages(context.Background(), store.MessageFilter{States: []string{store.MessagePending}, IncludeExpired: true})
	if err != nil || len(msgs) != 1 {
		t.Errorf("two runs of one day: %d messages (%v)", len(msgs), err)
	}
}

func TestZP25_run_now_on_a_running_schedule_is_refused(t *testing.T) {
	a, s, _ := scheduleApp(t) // the scheduler does not run: the first waits
	if err := s.RunNow("plan", "morgen"); err != nil {
		t.Fatal(err)
	}
	if it := item(t, s, "plan", "morgen"); it.State != skills.StateWaiting {
		t.Errorf("state %q", it.State)
	}
	if err := s.RunNow("plan", "morgen"); !errors.Is(err, ui.ErrScheduleRunning) {
		t.Errorf("second run now: %v", err)
	}
	if err := a.runner.RunNow("plan", "abend"); err != nil {
		t.Errorf("another schedule: %v", err)
	}
}

func TestZP26_a_schedule_without_a_name_is_shown_not_changed(t *testing.T) {
	_, s, _ := scheduleApp(t)
	it := item(t, s, "plan", "")
	if it.Editable || it.Cron != "0 12 * * *" || !it.Enabled {
		t.Errorf("without a name: %+v", it)
	}
	if err := s.Change(context.Background(), "plan", "", ui.ScheduleEdit{Enabled: on(false)}); err == nil {
		t.Error("a schedule without a name was changed")
	}
}
