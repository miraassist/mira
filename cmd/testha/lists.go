package main

import (
	"context"
	"fmt"
	"net/url"
	"time"

	haws "github.com/miraassist/mira/internal/ha/ws"
)

// The test house's calendars and to-do list (docs/spec/kalender.md): a
// writable family calendar, the waste calendar the abfall skill reads (the
// made-up iCal file under /local/, through Home Assistant's remote
// calendar), and a shopping list with due dates and descriptions.
var lists = []struct {
	handler, entity string
	data            map[string]any
}{
	{"local_calendar", "calendar.familie", map[string]any{"calendar_name": "Familie", "import": "create_empty"}},
	{"local_calendar", "calendar.ben", map[string]any{"calendar_name": "Ben", "import": "create_empty"}},
	{"remote_calendar", "calendar.abfuhrtermine", map[string]any{"calendar_name": "Abfuhrtermine",
		"url": "http://127.0.0.1:8123/local/abfall-test.ics", "verify_ssl": false}},
	{"local_todo", "todo.einkauf", map[string]any{"todo_list_name": "Einkauf"}},
}

// ensureLists sets up the calendars and the list once, and fills the empty
// ones with examples around today, among them an event with a long
// description and a link, to see that Mira shortens it.
func ensureLists(ctx context.Context, admin *haws.Client, token string) error {
	var entries []struct {
		Domain string `json:"domain"`
		Title  string `json:"title"`
	}
	if err := api(ctx, "GET", "/api/config/config_entries/entry", token, nil, &entries); err != nil {
		return err
	}
	have := map[string]bool{}
	for _, e := range entries {
		have[e.Domain+"/"+e.Title] = true
	}
	for _, l := range lists {
		title, _ := l.data["calendar_name"].(string)
		if title == "" {
			title, _ = l.data["todo_list_name"].(string)
		}
		if have[l.handler+"/"+title] {
			continue
		}
		var flow struct {
			FlowID string `json:"flow_id"`
			Type   string `json:"type"`
		}
		if err := api(ctx, "POST", "/api/config/config_entries/flow", token, map[string]any{"handler": l.handler}, &flow); err != nil {
			return err
		}
		if err := api(ctx, "POST", "/api/config/config_entries/flow/"+flow.FlowID, token, l.data, &flow); err != nil {
			return err
		}
		if flow.Type != "create_entry" {
			return fmt.Errorf("%s setup ended with %q", l.handler, flow.Type)
		}
	}
	for _, l := range lists {
		if err := waitFor(ctx, admin, l.entity); err != nil {
			return err
		}
	}
	return fillExamples(ctx, token)
}

// waitFor waits until Home Assistant has the entity, watching it.
func waitFor(ctx context.Context, admin *haws.Client, entity string) error {
	if !admin.WaitState(ctx, entity, 10*time.Second, func(s string) bool { return s != "" }) {
		return fmt.Errorf("%s did not appear", entity)
	}
	return nil
}

func fillExamples(ctx context.Context, token string) error {
	now := time.Now()
	day := func(d int) time.Time {
		y, m, dd := now.AddDate(0, 0, d).Date()
		return time.Date(y, m, dd, 0, 0, 0, 0, time.Local)
	}
	var events []any
	path := fmt.Sprintf("/api/calendars/calendar.familie?start=%s&end=%s",
		url.QueryEscape(day(-1).Format(time.RFC3339)), url.QueryEscape(day(30).Format(time.RFC3339)))
	if err := api(ctx, "GET", path, token, nil, &events); err != nil {
		return err
	}
	if len(events) == 0 {
		at := func(d, h, m int) string {
			return day(d).Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute).Format("2006-01-02 15:04:05")
		}
		for _, e := range []map[string]any{
			{"summary": "Fußballtraining Ben", "start_date_time": at(0, 16, 0), "end_date_time": at(0, 17, 30)},
			{"summary": "Arzt Dr. Berger", "start_date_time": at(3, 10, 0), "end_date_time": at(3, 10, 30),
				"location":    "Praxis Dr. Berger, Hauptstraße 12, 76275 Ettlingen, 2. OG, Eingang über den Hof",
				"description": "Kontrolltermin. Bitte Versichertenkarte und Überweisung mitbringen. Bei Verhinderung mindestens 24 Stunden vorher absagen. Online: https://example.org/termin?id=123456789&token=abcdef"},
			{"summary": "Elternabend", "start_date_time": at(5, 19, 30), "end_date_time": at(5, 21, 0), "location": "Grundschule"},
			{"summary": "Geburtstag Oma", "start_date": day(7).Format("2006-01-02"), "end_date": day(8).Format("2006-01-02")},
		} {
			e["entity_id"] = "calendar.familie"
			if err := api(ctx, "POST", "/api/services/calendar/create_event", token, e, nil); err != nil {
				return err
			}
		}
	}
	var items struct {
		Response map[string]struct {
			Items []any `json:"items"`
		} `json:"service_response"`
	}
	if err := api(ctx, "POST", "/api/services/todo/get_items?return_response", token, map[string]any{"entity_id": "todo.einkauf"}, &items); err != nil {
		return err
	}
	if len(items.Response["todo.einkauf"].Items) == 0 {
		for _, it := range []map[string]any{
			{"item": "Milch"},
			{"item": "Brot", "due_date": day(1).Format("2006-01-02")},
			{"item": "Batterien AA", "description": "für die Fernbedienung"},
		} {
			it["entity_id"] = "todo.einkauf"
			if err := api(ctx, "POST", "/api/services/todo/add_item", token, it, nil); err != nil {
				return err
			}
		}
	}
	return nil
}
