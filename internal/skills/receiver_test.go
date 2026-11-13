package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/store/storetest"
)

// A schedule for one person: docs/spec/schedule-receiver.md, step 2.

const receiverSkill = `---
name: posts
description: Posts für eine Person.
metadata:
  mira:
    settings:
      empfaenger: { description: Wer die Liste bekommt, required: true }
    schedules:
      - name: morgen
        cron: "0 8 * * *"
        to: "{{empfaenger}}"
        task: Such die Posts.
---
Anleitung.
`

func loadReceiverSkill(t *testing.T, skill, to string) ([]Skill, []Problem) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "posts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "posts", "SKILL.md"), []byte(skill), 0o644); err != nil {
		t.Fatal(err)
	}
	return Load([]string{dir}, map[string]map[string]any{"posts": {"empfaenger": to}})
}

func receiverRunner(t *testing.T, sk Skill, tasker Tasker) (*Runner, *store.Store) {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{Path: storetest.Path(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return &Runner{Set: berlinSet(t, sk), Tasker: tasker, Store: st, People: func(context.Context) []Person {
		return []Person{
			{Key: "anna", Name: "Anna", Channel: true},
			{Key: "stefan", Name: "Stefan Rinke", Login: "srinke", Channel: true},
			{Key: "gast", Name: "Gast"}, // only "alone at home is me"
		}
	}}, st
}

func pendingMessages(t *testing.T, st *store.Store) []store.Message {
	t.Helper()
	list, err := st.ListMessages(context.Background(), store.MessageFilter{States: []string{store.MessagePending}, IncludeExpired: true})
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestPS05_AScheduleWithToSendsItsResultPrivatelyToThatPerson(t *testing.T) {
	list, problems := loadReceiverSkill(t, receiverSkill, "Stefan Rinke")
	if len(problems) != 0 || len(list) != 1 {
		t.Fatalf("load: %+v %+v", list, problems)
	}
	tasker := &countingTasker{answer: `{"importance":1,"spoken":"Neue Posts.","details":"1. @a: x https://x.com/a/1"}`}
	r, st := receiverRunner(t, list[0], tasker)
	if queued, err := r.RunJob(context.Background(), list[0], 0); err != nil || !queued {
		t.Fatalf("run: %v %v", queued, err)
	}
	if len(tasker.seen) != 1 {
		t.Fatalf("the job ran %d times, want once", len(tasker.seen))
	}
	msgs := pendingMessages(t, st)
	if len(msgs) != 1 || msgs[0].Receiver != "stefan" || !msgs[0].Private() || msgs[0].Details != "1. @a: x https://x.com/a/1" {
		t.Fatalf("messages: %+v", msgs)
	}
}

func TestPS06_AnUnknownPersonInToFailsTheRun(t *testing.T) {
	for _, to := range []string{"Niemand", "Gast"} {
		list, problems := loadReceiverSkill(t, receiverSkill, to)
		if len(problems) != 0 || len(list) != 1 {
			t.Fatalf("load: %+v %+v", list, problems)
		}
		tasker := &countingTasker{answer: `{"importance":1,"spoken":"Neue Posts."}`}
		r, st := receiverRunner(t, list[0], tasker)
		_, err := r.RunJob(context.Background(), list[0], 0)
		if err == nil || !strings.Contains(err.Error(), `no person "`+to+`" with a channel of their own`) {
			t.Errorf("%s: err = %v", to, err)
		}
		if len(tasker.seen) != 0 || len(pendingMessages(t, st)) != 0 {
			t.Errorf("%s: ran %d times, messages %+v", to, len(tasker.seen), pendingMessages(t, st))
		}
		run, _, _ := st.LastRun(context.Background(), store.RunKey{Skill: "posts", Schedule: "morgen"})
		if run.Status != store.RunFailed {
			t.Errorf("%s: last run = %+v", to, run)
		}
	}
}

func TestPS07_ToIsFilledFromTheSettingsAndExcludesPerPerson(t *testing.T) {
	list, problems := loadReceiverSkill(t, receiverSkill, "stefan")
	if len(problems) != 0 || len(list) != 1 || list[0].Schedules[0].To != "stefan" {
		t.Fatalf("load: %+v %+v", list, problems)
	}
	both := strings.Replace(receiverSkill, `to: "{{empfaenger}}"`, "to: \"{{empfaenger}}\"\n        per_person: true", 1)
	list, problems = loadReceiverSkill(t, both, "stefan")
	if len(list) != 0 || len(problems) != 1 || !strings.Contains(problems[0].Text(i18n.DE), "to und per_person") {
		t.Fatalf("to with per_person: %+v %+v", list, problems)
	}
}

func TestPS08_ThePersonIsFoundByKeyNameOrLogin(t *testing.T) {
	for _, to := range []string{"stefan", "stefan rinke", " SRinke "} {
		list, _ := loadReceiverSkill(t, receiverSkill, to)
		if len(list) != 1 {
			t.Fatalf("%q: not loaded", to)
		}
		tasker := &countingTasker{answer: `{"importance":1,"spoken":"Neue Posts."}`}
		r, st := receiverRunner(t, list[0], tasker)
		if _, err := r.RunJob(context.Background(), list[0], 0); err != nil {
			t.Fatalf("%q: %v", to, err)
		}
		if msgs := pendingMessages(t, st); len(msgs) != 1 || msgs[0].Receiver != "stefan" {
			t.Errorf("%q: messages %+v", to, msgs)
		}
	}
}

type countingTasker struct {
	answer string
	seen   []string
}

func (f *countingTasker) RunTask(_ context.Context, _, instruction string) (string, error) {
	f.seen = append(f.seen, instruction)
	return f.answer, nil
}
