package calendar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

// docs/spec/calendar.md, Adding: one building block, one tool.

type acted struct {
	service string
	data    map[string]any
}

// createSetup: the calendars Anna, Ben and Familie (writable), Feiertage
// (read-only); Anna and Ben are persons' calendars by convention.
func createSetup(t *testing.T, cals ...Info) (*Calendar, *[]acted, *bytes.Buffer, *error) {
	t.Helper()
	berlin, _ := time.LoadLocation("Europe/Berlin")
	if len(cals) == 0 {
		cals = []Info{{Entity: "calendar.anna", Name: "Anna", Writable: true}, {Entity: "calendar.ben", Name: "Ben", Writable: true},
			{Entity: "calendar.familie", Name: "Familie", Writable: true}, {Entity: "calendar.feiertage", Name: "Feiertage"}}
	}
	var calls []acted
	var refuse error
	var log bytes.Buffer
	c := New(Options{Lang: i18n.DE, Location: berlin, Logger: slog.New(slog.NewTextHandler(&log, nil)),
		Clock:     func() time.Time { return time.Date(2026, 11, 12, 9, 0, 0, 0, berlin) },
		Calendars: func(context.Context) ([]Info, error) { return cals, nil },
		Owner: func(_ context.Context, name string) string {
			switch name {
			case "Anna":
				return "anna"
			case "Ben":
				return "ben"
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
	return c, &calls, &log, &refuse
}

func create(t *testing.T, c *Calendar, person, args string) string {
	t.Helper()
	out, err := c.CreateTool(i18n.DE, "postfach").Run(context.Background(),
		tools.Invocation{Args: json.RawMessage(args), Person: person})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCA01_AnEventWithATimeIsEntered(t *testing.T) {
	c, calls, _, _ := createSetup(t)
	out := create(t, c, "anna", `{"title":"Elternabend","start":"2026-12-03 19:30","location":"Aula"}`)
	if len(*calls) != 1 {
		t.Fatalf("%d calls", len(*calls))
	}
	got := (*calls)[0]
	if got.service != "calendar.create_event" || got.data["entity_id"] != "calendar.anna" || got.data["summary"] != "Elternabend" ||
		got.data["start_date_time"] != "2026-12-03T19:30:00+01:00" || got.data["end_date_time"] != "2026-12-03T20:30:00+01:00" ||
		got.data["location"] != "Aula" {
		t.Fatalf("call = %+v", got)
	}
	for _, want := range []string{"Elternabend", "19:30", "Anna"} {
		if !strings.Contains(out, want) {
			t.Errorf("answer %q lacks %q", out, want)
		}
	}
}

func TestCA02_AWholeDay(t *testing.T) {
	c, calls, _, _ := createSetup(t)
	create(t, c, "anna", `{"title":"schulfrei","day":"2026-12-22"}`)
	got := (*calls)[0].data
	if got["start_date"] != "2026-12-22" || got["end_date"] != "2026-12-23" || got["start_date_time"] != nil {
		t.Fatalf("data = %+v", got)
	}
}

func TestCA03_DatesThatDoNotFitEnterNothing(t *testing.T) {
	c, calls, log, _ := createSetup(t)
	for _, args := range []string{
		`{"title":"A","start":"2026-12-03 19:30","day":"2026-12-03"}`,
		`{"title":"A","start":"2026-12-03 19:30","end":"2026-12-03 18:00"}`,
		`{"title":"A","start":"2026-13-40 19:30"}`,
		`{"title":"A"}`,
		`{"title":"","day":"2026-12-03"}`,
	} {
		out := create(t, c, "anna", args)
		if !strings.HasPrefix(out, "Nicht eingetragen") {
			t.Errorf("%s: %q", args, out)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("%d calls", len(*calls))
	}
	if out := create(t, c, "anna", `{"title":"A","start":"2026-13-40 19:30"}`); !strings.Contains(out, "2026-13-40 19:30") {
		t.Errorf("the value is not named: %q", out)
	}
	if !strings.Contains(log.String(), "calendar.create refused") {
		t.Errorf("no log line: %s", log)
	}
}

func TestCA04_TheCalendarByTheOneRule(t *testing.T) {
	c, calls, _, _ := createSetup(t)
	create(t, c, "anna", `{"title":"A","day":"2026-12-03"}`)
	create(t, c, "anna", `{"title":"B","day":"2026-12-03","calendar":"Familie"}`)
	create(t, c, "anna", `{"title":"C","day":"2026-12-03","calendar":"standard"}`)
	if len(*calls) != 3 || (*calls)[0].data["entity_id"] != "calendar.anna" || (*calls)[1].data["entity_id"] != "calendar.familie" ||
		(*calls)[2].data["entity_id"] != "calendar.anna" {
		t.Fatalf("calls = %+v", *calls)
	}
	// Without an own calendar, the only free one.
	c, calls, _, _ = createSetup(t, Info{Entity: "calendar.ben", Name: "Ben", Writable: true},
		Info{Entity: "calendar.familie", Name: "Familie", Writable: true})
	create(t, c, "anna", `{"title":"A","day":"2026-12-03"}`)
	if len(*calls) != 1 || (*calls)[0].data["entity_id"] != "calendar.familie" {
		t.Fatalf("calls = %+v", *calls)
	}
}

func TestCA05_AnotherPersonsCalendarIsRefused(t *testing.T) {
	c, calls, _, _ := createSetup(t)
	out := create(t, c, "anna", `{"title":"A","day":"2026-12-03","calendar":"Ben"}`)
	if len(*calls) != 0 || !strings.Contains(out, "Anna") || !strings.Contains(out, "Familie") || strings.Contains(out, "Ben,") {
		t.Fatalf("calls %d, answer %q", len(*calls), out)
	}
	// Mira's own code (no person) may name any writable calendar.
	if _, err := c.Create(context.Background(), NewEvent{Title: "Paket", Day: time.Date(2026, 12, 3, 0, 0, 0, 0, time.UTC),
		Calendar: "calendar.ben"}); err != nil || len(*calls) != 1 {
		t.Fatalf("own code: %v, %d calls", err, len(*calls))
	}
}

func TestCA06_WithoutAnOwnCalendarAndSeveralOthersItAsks(t *testing.T) {
	c, calls, _, _ := createSetup(t, Info{Entity: "calendar.familie", Name: "Familie", Writable: true},
		Info{Entity: "calendar.garten", Name: "Garten", Writable: true})
	out := create(t, c, "anna", `{"title":"A","day":"2026-12-03"}`)
	if len(*calls) != 0 || !strings.Contains(out, "Welcher Kalender") || !strings.Contains(out, "Familie") ||
		!strings.Contains(out, "Garten") {
		t.Fatalf("calls %d, answer %q", len(*calls), out)
	}
}

func TestCA07_HomeAssistantRefuses(t *testing.T) {
	c, _, log, refuse := createSetup(t)
	*refuse = errors.New("service not found")
	out := create(t, c, "anna", `{"title":"Elternabend","day":"2026-12-03"}`)
	if !strings.HasPrefix(out, "Nicht eingetragen") || !strings.Contains(out, "service not found") {
		t.Fatalf("answer %q", out)
	}
	for _, want := range []string{"calendar.anna", "Elternabend", "service not found"} {
		if !strings.Contains(log.String(), want) {
			t.Errorf("log lacks %q: %s", want, log)
		}
	}
}

// The skill termin's finish (action: calendar.create) with its fields.
func TestCreateActionOfTheSkillTermin(t *testing.T) {
	c, calls, _, _ := createSetup(t)
	err := c.CreateAction(context.Background(), map[string]any{"entity_id": "calendar.familie", "title": "Zahnarzt",
		"start": "2026-11-14 15:00", "person": "anna"})
	if err != nil || len(*calls) != 1 || (*calls)[0].data["start_date_time"] != "2026-11-14T15:00:00+01:00" {
		t.Fatalf("err %v, calls %+v", err, *calls)
	}
	if err := c.CreateAction(context.Background(), map[string]any{"entity_id": "calendar.ben", "title": "Zahnarzt",
		"day": "2026-11-14", "person": "anna"}); err == nil {
		t.Fatal("another person's calendar was taken")
	}
}

func TestCreatePastIsSaid(t *testing.T) {
	c, _, _, _ := createSetup(t)
	if out := create(t, c, "anna", `{"title":"A","day":"2026-11-01"}`); !strings.Contains(out, "Vergangenheit") {
		t.Fatalf("answer %q", out)
	}
}
