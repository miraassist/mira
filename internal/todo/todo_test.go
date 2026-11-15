package todo

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/skills"
)

// docs/spec/todo-due.md, The building block todo.create.

type acted struct {
	service string
	data    map[string]any
}

const all = 1 | 2 | 4 | FeatureDueDate | FeatureDescription

// setup: Anna and Ben are persons' lists by convention, stefan is Stefan's
// by his login.
func setup(t *testing.T, lists ...List) (*Todo, *[]acted, *bytes.Buffer, *error) {
	t.Helper()
	berlin, _ := time.LoadLocation("Europe/Berlin")
	var calls []acted
	var refuse error
	var log bytes.Buffer
	td := New(Options{Lang: i18n.DE, Location: berlin, Logger: slog.New(slog.NewTextHandler(&log, nil)),
		Clock: func() time.Time { return time.Date(2026, 11, 15, 9, 0, 0, 0, berlin) },
		Lists: func(context.Context) ([]List, error) { return lists, nil },
		Owner: func(_ context.Context, name string) string {
			switch strings.ToLower(name) {
			case "anna":
				return "anna"
			case "ben":
				return "ben"
			case "stefan":
				return "stefan"
			}
			return ""
		},
		Act: func(_ context.Context, domain, service string, data map[string]any) error {
			if refuse != nil {
				return refuse
			}
			calls = append(calls, acted{domain + "." + service, data})
			return nil
		}})
	return td, &calls, &log, &refuse
}

func act(td *Todo, person string, data map[string]any) error {
	data["person"] = person
	return td.CreateAction(context.Background(), data)
}

func refusal(t *testing.T, err error) string {
	t.Helper()
	var r *Refusal
	if !errors.As(err, &r) {
		t.Fatalf("err = %v, want a refusal", err)
	}
	return r.Text
}

func TestTD02_AToDoWithADueDateGoesOntoThePersonsOwnList(t *testing.T) {
	td, calls, _, _ := setup(t, List{"todo.anna", "Anna", all}, List{"todo.einkauf", "Einkauf", all})
	if err := act(td, "anna", map[string]any{"item": "Rechnung Stadtwerke bezahlen", "due_date": "2026-11-30"}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 {
		t.Fatalf("%d calls", len(*calls))
	}
	got := (*calls)[0]
	if got.service != "todo.add_item" || got.data["entity_id"] != "todo.anna" || got.data["item"] != "Rechnung Stadtwerke bezahlen" ||
		got.data["due_date"] != "2026-11-30" {
		t.Fatalf("call = %+v", got)
	}
	if _, ok := got.data["description"]; ok {
		t.Fatal("an empty description is sent")
	}
}

func TestTD03_TheOwnListByTheLoginName(t *testing.T) {
	td, calls, _, _ := setup(t, List{"todo.stefan", "stefan", all}, List{"todo.einkauf", "Einkauf", all})
	if err := act(td, "stefan", map[string]any{"item": "Steuererklärung abgeben"}); err != nil {
		t.Fatal(err)
	}
	if (*calls)[0].data["entity_id"] != "todo.stefan" {
		t.Fatalf("call = %+v", (*calls)[0])
	}
}

func TestTD04_AnotherPersonsListIsRefused(t *testing.T) {
	td, calls, log, _ := setup(t, List{"todo.anna", "Anna", all}, List{"todo.ben", "Ben", all})
	text := refusal(t, act(td, "anna", map[string]any{"item": "Batterien", "entity_id": "Ben"}))
	if len(*calls) != 0 {
		t.Fatal("entered on Ben's list")
	}
	if !strings.Contains(text, "Anna") || strings.Contains(text, "Ben,") {
		t.Fatalf("refusal = %q", text)
	}
	if !strings.Contains(log.String(), "todo.create refused") {
		t.Fatalf("no log line: %s", log)
	}
}

func TestTD05_WithoutAnOwnListAndSeveralListsMiraAsks(t *testing.T) {
	td, calls, _, _ := setup(t, List{"todo.einkauf", "Einkauf", all}, List{"todo.haushalt", "Haushalt", all})
	text := refusal(t, act(td, "ben", map[string]any{"item": "Batterien"}))
	if len(*calls) != 0 {
		t.Fatal("entered without knowing the list")
	}
	if !strings.HasPrefix(text, "Welche Liste?") || !strings.Contains(text, "Einkauf") || !strings.Contains(text, "Haushalt") {
		t.Fatalf("refusal = %q", text)
	}
}

func TestTD06_FieldsThatDoNotFitEnterNothing(t *testing.T) {
	td, calls, log, _ := setup(t, List{"todo.anna", "Anna", all})
	for _, c := range []struct {
		data map[string]any
		want string
	}{
		{map[string]any{"item": ""}, "Eintrag fehlt"},
		{map[string]any{"item": strings.Repeat("x", 81)}, "item"},
		{map[string]any{"item": "Formular", "due_date": "2026-13-40"}, "2026-13-40"},
	} {
		if text := refusal(t, act(td, "anna", c.data)); !strings.Contains(text, c.want) {
			t.Errorf("refusal = %q, want %q", text, c.want)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("%d calls", len(*calls))
	}
	if strings.Count(log.String(), "todo.create refused") != 3 {
		t.Fatalf("log = %s", log)
	}
}

func TestTD07_HomeAssistantRefuses(t *testing.T) {
	td, calls, log, refuse := setup(t, List{"todo.anna", "Anna", all})
	*refuse = errors.New("service unavailable")
	err := act(td, "anna", map[string]any{"item": "Formular"})
	var r *Refusal
	if err == nil || errors.As(err, &r) || !strings.Contains(err.Error(), "todo.anna") {
		t.Fatalf("err = %v", err)
	}
	if len(*calls) != 0 {
		t.Fatal("repeated or recorded")
	}
	for _, want := range []string{"todo.anna", "Formular", "service unavailable"} {
		if !strings.Contains(log.String(), want) {
			t.Fatalf("log misses %q: %s", want, log)
		}
	}
}

func TestTD08_TheSkillTodoFinishesThroughTheBuildingBlock(t *testing.T) {
	loaded, problems := skills.Load([]string{"../../skills"}, map[string]map[string]any{"todo": nil})
	if len(problems) > 0 {
		t.Fatalf("problems: %v", problems)
	}
	for _, sk := range loaded {
		if sk.Name != "todo" {
			continue
		}
		fin := sk.Target.Finish
		if fin.Action == nil || fin.Action.Action != ActionCreate {
			t.Fatalf("finish = %+v", fin)
		}
		for _, f := range sk.Target.Fields {
			if f.Check != "" {
				t.Fatalf("field %s keeps the check %s", f.Name, f.Check)
			}
		}
		return
	}
	t.Fatal("skill todo not loaded")
}

func TestTD18_AListThatCannotHoldADueDateIsNotTakenForOne(t *testing.T) {
	td, calls, log, _ := setup(t, List{"todo.anna", "Anna", 1 | 2 | 4 | 8}, List{"todo.haushalt", "Haushalt", all})
	text := refusal(t, act(td, "anna", map[string]any{"item": "Formular", "due_date": "2026-11-28", "entity_id": "Anna"}))
	if len(*calls) != 0 {
		t.Fatal("entered on a list without due dates")
	}
	if !strings.Contains(text, "Haushalt") {
		t.Fatalf("refusal = %q", text)
	}
	if !strings.Contains(log.String(), "todo.create refused") {
		t.Fatalf("no log line: %s", log)
	}
	// Without a list named, the own list that cannot is passed over.
	if err := act(td, "anna", map[string]any{"item": "Formular", "due_date": "2026-11-28"}); err != nil {
		t.Fatal(err)
	}
	if (*calls)[0].data["entity_id"] != "todo.haushalt" {
		t.Fatalf("call = %+v", (*calls)[0])
	}
}
