package calendar

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// docs/spec/calendar.md, Conflicts when adding. Friday is 2026-11-13.

// conflictSetup serves each calendar's events as given (JSON by entity; a
// missing entity answers 500) and counts the reads.
func conflictSetup(t *testing.T, events map[string]string) (*Calendar, *int) {
	t.Helper()
	c, _, _, _ := createSetup(t)
	reads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads++
		body, ok := events[strings.TrimPrefix(r.URL.Path, "/api/calendars/")]
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c.opts.HAURL, c.opts.HTTP, c.opts.Token = srv.URL, srv.Client(), "tok"
	return c, &reads
}

func timed(summary, from, to string) string {
	return `{"start":{"dateTime":"2026-11-13T` + from + `:00+01:00"},"end":{"dateTime":"2026-11-13T` + to + `:00+01:00"},"summary":"` + summary + `"}`
}

func zahnarzt(start, end string) map[string]any {
	d := map[string]any{"title": "Zahnarzt", "start": "2026-11-13 " + start, "entity_id": "Familie", "person": "anna"}
	if end != "" {
		d["end"] = "2026-11-13 " + end
	}
	return d
}

func TestCA10_WithoutAConflictTheEventIsEnteredWithoutAQuestion(t *testing.T) {
	c, reads := conflictSetup(t, map[string]string{"calendar.familie": `[` + timed("Friseur", "10:00", "11:00") + `]`})
	if concern, note := c.ConcernCreate(context.Background(), zahnarzt("15:00", "")); concern != "" || note != "" {
		t.Errorf("concern %q, note %q", concern, note)
	}
	if *reads != 1 {
		t.Errorf("reads %d", *reads)
	}
}

func TestCA11_AConflictAsksBeforeEntering(t *testing.T) {
	c, _ := conflictSetup(t, map[string]string{"calendar.familie": `[` + timed("Elternabend", "15:30", "16:30") + `]`})
	concern, note := c.ConcernCreate(context.Background(), zahnarzt("15:00", ""))
	if !strings.Contains(concern, "Familie") || !strings.Contains(concern, "Elternabend 15:30–16:30") ||
		!strings.Contains(concern, "thread_confirm") || note != "" {
		t.Errorf("concern %q, note %q", concern, note)
	}
}

func TestCA12_OnlyTheCalendarItGoesIntoCounts(t *testing.T) {
	c, _ := conflictSetup(t, map[string]string{"calendar.familie": `[]`,
		"calendar.anna": `[` + timed("Training", "15:00", "16:00") + `]`})
	if concern, _ := c.ConcernCreate(context.Background(), zahnarzt("15:00", "")); concern != "" {
		t.Errorf("concern %q", concern)
	}
}

func TestCA13_WholeDaysAreNoConflict(t *testing.T) {
	c, reads := conflictSetup(t, map[string]string{"calendar.familie": `[{"start":{"date":"2026-11-13"},"end":{"date":"2026-11-14"},"summary":"Geburtstag Oma"}]`})
	if concern, _ := c.ConcernCreate(context.Background(), zahnarzt("15:00", "")); concern != "" {
		t.Errorf("an all-day event is a conflict: %q", concern)
	}
	*reads = 0
	whole := map[string]any{"title": "Ausflug", "day": "2026-11-13", "entity_id": "Familie", "person": "anna"}
	if concern, note := c.ConcernCreate(context.Background(), whole); concern != "" || note != "" || *reads != 0 {
		t.Errorf("a new whole day: concern %q, note %q, reads %d", concern, note, *reads)
	}
}

func TestCA14_TouchingIsNoConflict(t *testing.T) {
	c, _ := conflictSetup(t, map[string]string{"calendar.familie": `[` + timed("Friseur", "14:00", "15:00") + `,` + timed("Kino", "16:00", "18:00") + `]`})
	if concern, _ := c.ConcernCreate(context.Background(), zahnarzt("15:00", "16:00")); concern != "" {
		t.Errorf("concern %q", concern)
	}
}

func TestCA15_ACalendarThatCannotBeReadDoesNotStopTheEntering(t *testing.T) {
	c, _, log, _ := createSetup(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	c.opts.HAURL, c.opts.HTTP, c.opts.Token = srv.URL, srv.Client(), "tok"
	concern, note := c.ConcernCreate(context.Background(), zahnarzt("15:00", ""))
	if concern != "" || !strings.Contains(note, "nicht prüfen") || !strings.Contains(note, "HTTP 500") {
		t.Errorf("concern %q, note %q", concern, note)
	}
	out := log.String()
	for _, want := range []string{"could not check the calendar for conflicts", "calendar=calendar.familie", "title=Zahnarzt", "HTTP 500"} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %q: %s", want, out)
		}
	}
}
