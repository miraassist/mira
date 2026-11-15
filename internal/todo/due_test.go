package todo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

// docs/spec/todo-due.md, Reading: the tool todo_due. Today is Sun 15.11.2026.

func dueSetup(t *testing.T, items []Item, fail error) (*Todo, *bytes.Buffer) {
	t.Helper()
	berlin, _ := time.LoadLocation("Europe/Berlin")
	var log bytes.Buffer
	td := New(Options{Lang: i18n.DE, Location: berlin, Logger: slog.New(slog.NewTextHandler(&log, nil)),
		Clock: func() time.Time { return time.Date(2026, 11, 15, 5, 35, 0, 0, berlin) },
		Lists: func(context.Context) ([]List, error) {
			return []List{{"todo.anna", "Anna", all}, {"todo.einkauf", "Einkauf", all}}, nil
		},
		Items: func(_ context.Context, entity string) ([]Item, error) {
			if fail != nil {
				return nil, fail
			}
			if entity != "todo.anna" {
				return nil, fmt.Errorf("asked for %s", entity)
			}
			return items, nil
		}})
	return td, &log
}

func due(t *testing.T, td *Todo, args string) string {
	t.Helper()
	out, err := td.DueTool(i18n.DE, "briefing").Run(context.Background(), tools.Invocation{Args: json.RawMessage(args)})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTD11_TodoDueNamesOverdueAndTodaysToDos(t *testing.T) {
	td, _ := dueSetup(t, []Item{
		{Summary: "Anmeldung Schwimmkurs abgeben", Status: "needs_action", Due: "2026-11-15"},
		{Summary: "Rechnung Stadtwerke bezahlen", Status: "needs_action", Due: "2026-11-12"},
		{Summary: "Reifen wechseln", Status: "needs_action", Due: "2026-11-20"},
		{Summary: "Batterien", Status: "needs_action"},
	}, nil)
	out := due(t, td, `{"list":"Anna"}`)
	want := "Überfällig\n- Rechnung Stadtwerke bezahlen · fällig seit Donnerstag 12.11. (Anna)\nHeute\n- Anmeldung Schwimmkurs abgeben (Anna)"
	if out != want {
		t.Fatalf("out =\n%s\nwant\n%s", out, want)
	}
	// until widens the window ahead.
	if out := due(t, td, `{"list":"Anna","until":"2026-11-20"}`); !strings.Contains(out, "Reifen wechseln") || strings.Contains(out, "Batterien") {
		t.Fatalf("until: %s", out)
	}
}

func TestTD12_TickedToDosAreNotNamed(t *testing.T) {
	td, _ := dueSetup(t, []Item{{Summary: "Formular", Status: "completed", Due: "2026-11-14"}}, nil)
	if out := due(t, td, `{"list":"Anna"}`); out != "Auf der Liste Anna ist nichts fällig." {
		t.Fatalf("out = %q", out)
	}
}

func TestTD13_AtMost20TheRestCounted(t *testing.T) {
	var items []Item
	for i := 0; i < 23; i++ {
		items = append(items, Item{Summary: fmt.Sprintf("Punkt %d", i), Status: "needs_action", Due: "2026-11-10"})
	}
	td, _ := dueSetup(t, items, nil)
	out := due(t, td, `{"list":"Anna"}`)
	if strings.Count(out, "\n- ") != 20 || !strings.HasSuffix(out, "und 3 weitere") {
		t.Fatalf("out =\n%s", out)
	}
}

func TestTD14_AListThatCannotBeRead(t *testing.T) {
	td, log := dueSetup(t, nil, errors.New("connection lost"))
	if out := due(t, td, `{"list":"Anna"}`); out != "Die Liste Anna konnte ich nicht lesen." {
		t.Fatalf("out = %q", out)
	}
	for _, want := range []string{"todo.anna", "connection lost"} {
		if !strings.Contains(log.String(), want) {
			t.Fatalf("log misses %q: %s", want, log)
		}
	}
	// An unknown list names the exposed ones.
	if out := due(t, td, `{"list":"Ben"}`); !strings.Contains(out, "Anna, Einkauf") {
		t.Fatalf("unknown list: %q", out)
	}
}

func TestTD15_TodoDueIsOfferedToBackgroundRuns(t *testing.T) {
	td, _ := dueSetup(t, nil, nil)
	r := tools.NewRegistry(nil, td.DueTool(i18n.DE, "briefing"))
	found := false
	for _, d := range r.SkillDefs("briefing") {
		found = found || d.Name == ToolDue
	}
	if !found {
		t.Fatal("the briefing's schedules do not get todo_due")
	}
	for _, d := range r.SkillDefs("wecker") {
		if d.Name == ToolDue {
			t.Fatal("another skill gets todo_due")
		}
	}
	if s, ok := r.Lookup(ToolDue); !ok || !s.ReadOnly {
		t.Fatalf("spec = %+v", s)
	}
}

func TestTD17_AToDoOverdueForMoreThan30DaysIsNotNamed(t *testing.T) {
	td, log := dueSetup(t, []Item{
		{Summary: "Alt", Status: "needs_action", Due: "2026-10-15"},
		{Summary: "Gerade noch", Status: "needs_action", Due: "2026-10-16"},
		{Summary: "Kaputt", Status: "needs_action", Due: "morgen"},
	}, nil)
	out := due(t, td, `{"list":"Anna"}`)
	if !strings.Contains(out, "Gerade noch") || strings.Contains(out, "Alt") || strings.Contains(out, "Kaputt") {
		t.Fatalf("out =\n%s", out)
	}
	if !strings.Contains(log.String(), "Kaputt") || !strings.Contains(log.String(), "morgen") {
		t.Fatalf("no log line for the due date that does not parse: %s", log)
	}
}
