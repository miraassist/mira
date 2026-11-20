package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/store"
)

// Scheduled jobs: docs/spec/scheduled-jobs.md, step 1.

const jobSkill = `---
name: liste
description: Eine Liste.
metadata:
  mira:
    settings:
      quelle: { description: Woher, required: true }
    jobs:
      liste:
        description: Neue Videos zu Themen
        parameters:
          themen:     { description: Die Themen, required: true }
          empfaenger: { description: Wer sie bekommt }
        to: "{{empfaenger}}"
        window: 12h
        task: Such seit {{since}} zu {{themen}} aus {{quelle}}.
        first:
          - { name: morgens, cron: "0 7 * * *", parameters: { themen: A } }
          - { name: leer, cron: "0 9 * * *" }
---
Anleitung.
`

func loadJobs(t *testing.T, skill string, values map[string]any) (Skill, []Problem) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "liste"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "liste", "SKILL.md"), []byte(skill), 0o644); err != nil {
		t.Fatal(err)
	}
	if values == nil {
		values = map[string]any{}
	}
	if _, ok := values["quelle"]; !ok {
		values["quelle"] = "YouTube"
	}
	list, problems := Load([]string{dir}, map[string]map[string]any{"liste": values})
	if len(list) == 0 {
		return Skill{}, problems
	}
	return list[0], problems
}

func names(scheds []Schedule) []string {
	var out []string
	for _, s := range scheds {
		out = append(out, s.Name)
	}
	return out
}

// SJ-01 A skill's first job runs while mira.yaml has no jobs
func TestSJ01_ASkillsFirstJobRunsWhileMiraYamlHasNoJobs(t *testing.T) {
	sk, problems := loadJobs(t, jobSkill, nil)
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	if len(sk.Templates) != 1 || sk.Templates[0].Key != "liste" || len(sk.Schedules) != 1 {
		t.Fatalf("templates %+v, schedules %v", sk.Templates, names(sk.Schedules))
	}
	m := sk.Schedules[0]
	if m.Name != "morgens" || m.Cron != "0 7 * * *" || m.Template != "liste" || m.Description != "Neue Videos zu Themen" ||
		m.Task != "Such seit {{since}} zu A aus YouTube." || !m.Runs() || m.To != "" {
		t.Errorf("schedule %+v", m)
	}
}

// SJ-02 The jobs in mira.yaml are the ones that run
func TestSJ02_TheJobsInMiraYamlAreTheOnesThatRun(t *testing.T) {
	sk, problems := loadJobs(t, jobSkill, map[string]any{"jobs": map[string]any{
		"morgens": map[string]any{"template": "liste", "cron": "0 7 * * *", "themen": "A"},
		"abends":  map[string]any{"template": "liste", "cron": "0 18 * * *", "themen": "B", "enabled": false},
	}})
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	got := map[string]Schedule{}
	for _, s := range sk.Schedules {
		got[s.Name] = s
	}
	if len(sk.Schedules) != 2 || got["abends"].Cron != "0 18 * * *" || !got["abends"].Off ||
		!strings.Contains(got["abends"].Task, "zu B") || !got["morgens"].Runs() {
		t.Errorf("schedules %+v", sk.Schedules)
	}
	sk, _ = loadJobs(t, jobSkill, map[string]any{"jobs": map[string]any{}})
	if len(sk.Schedules) != 0 {
		t.Errorf("jobs: {} has %v", names(sk.Schedules))
	}
}

// SJ-03 A job's parameters fill its task and its recipient
func TestSJ03_AJobsParametersFillItsTaskAndItsRecipient(t *testing.T) {
	sk, _ := loadJobs(t, jobSkill, map[string]any{"jobs": map[string]any{
		"haus":   map[string]any{"template": "liste", "cron": "0 7 * * *", "themen": "Voice", "empfaenger": ""},
		"stefan": map[string]any{"template": "liste", "cron": "0 7 * * *", "themen": "Voice", "empfaenger": "Stefan"},
	}})
	got := map[string]Schedule{}
	for _, s := range sk.Schedules {
		got[s.Name] = s
	}
	if h := got["haus"]; h.Task != "Such seit {{since}} zu Voice aus YouTube." || h.To != "" {
		t.Errorf("haus %+v", h)
	}
	if got["stefan"].To != "Stefan" {
		t.Errorf("stefan %+v", got["stefan"])
	}
}

// SJ-04 A job that is wrong does not run, the skill does
func TestSJ04_AJobThatIsWrongDoesNotRunTheSkillDoes(t *testing.T) {
	sk, problems := loadJobs(t, jobSkill, map[string]any{"jobs": map[string]any{
		"gut":    map[string]any{"template": "liste", "cron": "0 7 * * *", "themen": "A"},
		"fremd":  map[string]any{"template": "andere", "cron": "0 7 * * *"},
		"ohne":   map[string]any{"template": "liste", "cron": "0 8 * * *"},
		"falsch": map[string]any{"template": "liste", "cron": "um sieben", "themen": "A"},
		"zuviel": map[string]any{"template": "liste", "cron": "0 7 * * *", "themen": "A", "farbe": "rot"},
	}})
	if len(problems) != 0 || sk.Name != "liste" {
		t.Fatalf("the skill did not start: %v", problems)
	}
	issues := map[string]string{}
	for _, s := range sk.Schedules {
		if s.Issue != nil {
			issues[s.Name] = s.Issue.Text(i18n.DE)
		} else if s.Name != "gut" {
			t.Errorf("%s runs", s.Name)
		}
	}
	for name, want := range map[string]string{"fremd": "die Vorlage „andere“", "ohne": "fehlt „themen“", "falsch": "ungültige Zeit",
		"zuviel": "den Wert „farbe“"} {
		if !strings.Contains(issues[name], want) || !strings.Contains(issues[name], "„"+name+"“") {
			t.Errorf("%s: %q", name, issues[name])
		}
	}
	r := &Runner{Set: berlinSet(t, sk)}
	if err := r.RunNow("liste", "fremd"); err == nil {
		t.Error("a wrong job runs now")
	}
	for _, j := range r.jobs(false) {
		if j.sched.Name != "gut" {
			t.Errorf("job %s is scheduled", j.sched.Name)
		}
	}
}

// SJ-05 A first job without a required value is not made
func TestSJ05_AFirstJobWithoutARequiredValueIsNotMade(t *testing.T) {
	sk, problems := loadJobs(t, jobSkill, nil)
	if len(problems) != 0 || strings.Join(names(sk.Schedules), ",") != "morgens" {
		t.Errorf("schedules %v, problems %v", names(sk.Schedules), problems)
	}
}

// SJ-06 {{since}} is the job's last run
func TestSJ06_SinceIsTheJobsLastRun(t *testing.T) {
	sk, _ := loadJobs(t, jobSkill, nil)
	tasker := &countingTasker{answer: `{"importance":0}`}
	r, st := receiverRunner(t, sk, tasker)
	// At its first run: the window (12h) before now (18:00 Berlin).
	if _, err := r.RunJob(context.Background(), sk, 0); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tasker.seen[0], "Such seit 2026-10-21 06:00 zu A") {
		t.Errorf("first run: %s", tasker.seen[0])
	}
	// With a last run at 07:00: that time.
	berlin, _ := time.LoadLocation("Europe/Berlin")
	at := time.Date(2026, 10, 21, 7, 0, 0, 0, berlin)
	if err := st.SetLastRun(context.Background(), store.RunKey{Skill: "liste", Schedule: "morgens"},
		store.LastRun{Started: at, Ended: at, Status: store.RunNothing}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.task(context.Background(), sk, 0, Person{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tasker.seen[1], "Such seit 2026-10-21 07:00 zu A") {
		t.Errorf("second run: %s", tasker.seen[1])
	}
}

const legacySkill = `---
name: liste
description: Ein Skill von vorher.
metadata:
  mira:
    settings:
      quelle: { description: Woher, required: true }
    schedules:
      - name: morgen
        description: Morgens
        cron: "0 8 * * *"
        task: Posts der letzten 14 Stunden aus {{quelle}}.
      - name: abend
        cron: "0 18 * * *"
        task: Posts der letzten 10 Stunden.
---
Anleitung.
`

// SJ-07 schedules in a SKILL.md still work
func TestSJ07_SchedulesInASkillMdStillWork(t *testing.T) {
	sk, problems := loadJobs(t, legacySkill, nil)
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	if strings.Join(names(sk.Schedules), ",") != "morgen,abend" || sk.Schedules[0].Cron != "0 8 * * *" ||
		sk.Schedules[0].Task != "Posts der letzten 14 Stunden aus YouTube." || sk.Schedules[0].Description != "Morgens" ||
		sk.Schedules[1].Template != "abend" || !sk.Templates[0].Legacy {
		t.Errorf("schedules %+v", sk.Schedules)
	}
	both := strings.Replace(legacySkill, "    schedules:", "    jobs:\n      x: { task: y }\n    schedules:", 1)
	if _, problems := loadJobs(t, both, nil); len(problems) != 1 || !strings.Contains(problems[0].Text(i18n.EN), "jobs and schedules") {
		t.Errorf("both: %v", problems)
	}
	// Moved by the page: jobs naming the schedules of before.
	sk, problems = loadJobs(t, legacySkill, map[string]any{"jobs": map[string]any{
		"abend": map[string]any{"template": "abend", "cron": "0 19 * * *"}}})
	if len(problems) != 0 || len(sk.Schedules) != 1 || sk.Schedules[0].Cron != "0 19 * * *" || sk.Schedules[0].Task != "Posts der letzten 10 Stunden." {
		t.Errorf("moved: %+v %v", sk.Schedules, problems)
	}
}

// SJ-08 schedules in mira.yaml change the job of that name
func TestSJ08_SchedulesInMiraYamlChangeTheJobOfThatName(t *testing.T) {
	sk, problems := loadJobs(t, legacySkill, map[string]any{"schedules": map[string]any{
		"abend": map[string]any{"cron": "0 19 * * *", "enabled": false}}})
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	if a := sk.Schedules[1]; a.Name != "abend" || a.Cron != "0 19 * * *" || !a.Off || a.Default != "0 18 * * *" {
		t.Errorf("abend %+v", a)
	}
	if _, problems := loadJobs(t, legacySkill, map[string]any{"schedules": map[string]any{
		"mittag": map[string]any{"cron": "0 12 * * *"}}}); len(problems) != 1 || !strings.Contains(problems[0].Error(), `no schedule "mittag"`) {
		t.Errorf("unknown: %v", problems)
	}
}
