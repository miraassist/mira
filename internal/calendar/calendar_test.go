package calendar

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/tools"
)

func TestTextsComplete(t *testing.T) {
	if m := i18n.Missing(catalog); len(m) > 0 {
		t.Errorf("missing texts: %v", m)
	}
}

// A Home Assistant with a writable family calendar and read-only holidays.
func setup(t *testing.T) (*Calendar, *[]string) {
	t.Helper()
	berlin, _ := time.LoadLocation("Europe/Berlin")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/api/calendars/calendar.familie":
			w.Write([]byte(`[
 {"start":{"dateTime":"2026-10-20T10:00:00+02:00"},"end":{"dateTime":"2026-10-20T10:30:00+02:00"},"summary":"Arzt Dr. Berger","location":"Praxis","description":"Karte mitbringen https://x.org/a","uid":"u1"},
 {"start":{"dateTime":"2026-10-21T18:00:00+02:00"},"end":{"dateTime":"2026-10-21T19:00:00+02:00"},"summary":"Chor","uid":"u2","recurrence_id":"20261021T180000","rrule":"FREQ=WEEKLY"}]`))
		case "/api/calendars/calendar.feiertage":
			w.Write([]byte(`[{"start":{"date":"2026-10-31"},"end":{"date":"2026-11-01"},"summary":"Reformationstag","uid":"f1"}]`))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	var deleted []string
	c := New(Options{HAURL: srv.URL, Token: "tok", Lang: i18n.DE, Location: berlin,
		Clock: func() time.Time { return time.Date(2026, 10, 19, 9, 0, 0, 0, berlin) },
		Calendars: func(context.Context) ([]Info, error) {
			return []Info{{Entity: "calendar.familie", Name: "Familie", Writable: true, Deletable: true},
				{Entity: "calendar.feiertage", Name: "Feiertage"}}, nil
		},
		Delete: func(_ context.Context, entity, uid, rec string) error {
			deleted = append(deleted, entity+"/"+uid+"/"+rec)
			return nil
		}})
	return c, &deleted
}

func run(t *testing.T, c *Calendar, args string) string {
	t.Helper()
	out, err := c.Tool(i18n.DE).Run(context.Background(), tools.Invocation{Args: json.RawMessage(args)})
	if err != nil {
		t.Fatalf("%s: %v", args, err)
	}
	return out
}

func TestEventsFromAllCalendars(t *testing.T) {
	c, _ := setup(t)
	out := run(t, c, `{"to":"2026-10-31"}`)
	for _, want := range []string{"[t1] 10:00–10:30 Arzt Dr. Berger · Praxis (Familie)", "Chor (Familie)", "ganztägig Reformationstag (Feiertage)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "u1") || strings.Contains(out, "Karte") {
		t.Errorf("ids or details leaked:\n%s", out)
	}
	if out := run(t, c, `{"query":"arzt","calendar":"Familie"}`); strings.Contains(out, "Chor") || !strings.Contains(out, "Arzt") {
		t.Errorf("query:\n%s", out)
	}
}

func TestChecks(t *testing.T) {
	c, _ := setup(t)
	ctx := context.Background()
	if got, err := c.CheckWritable(ctx, "familie"); err != nil || got != "calendar.familie" {
		t.Errorf("familie: %q %v", got, err)
	}
	if _, err := c.CheckWritable(ctx, "Feiertage"); err == nil || !strings.Contains(err.Error(), "Familie") {
		t.Errorf("read-only calendar accepted: %v", err)
	}
	if _, err := c.CheckWritable(ctx, ""); err != nil {
		t.Errorf("one writable calendar: %v", err)
	}
	run(t, c, `{}`)
	if got, err := c.CheckEvent(ctx, "t1"); err != nil || !strings.Contains(got, "Arzt Dr. Berger") {
		t.Errorf("t1: %q %v", got, err)
	}
	if _, err := c.CheckEvent(ctx, "t99"); err == nil {
		t.Error("unknown label accepted")
	}
}

// Cancelling asks first; a series loses only this event unless all.
func TestDeleteAsksAndKeepsTheSeries(t *testing.T) {
	c, deleted := setup(t)
	ctx := context.Background()
	run(t, c, `{}`) // t1 Arzt, t2 Chor
	fin := c.Delete()
	for _, tc := range []struct {
		fields map[string]string
		want   string
	}{
		{map[string]string{"event": "t1 (Arzt …)"}, "calendar.familie/u1/"},
		{map[string]string{"event": "t2"}, "calendar.familie/u2/20261021T180000"},
		{map[string]string{"event": "t2", "all": "ja"}, "calendar.familie/u2/"},
	} {
		th := store.Thread{Fields: tc.fields}
		res, err := fin.Work(ctx, th)
		if err != nil || res.Proposal == "" || !strings.HasSuffix(res.Proposal, "?") {
			t.Fatalf("work: %+v %v", res, err)
		}
		th.Content = res.Content
		if _, err := fin.Commit(ctx, th); err != nil {
			t.Fatal(err)
		}
		if got := (*deleted)[len(*deleted)-1]; got != tc.want {
			t.Errorf("deleted %q, want %q", got, tc.want)
		}
	}
}

// The undo of the skill termin finds the event just added by calendar, title
// and start, and deletes exactly that one; with no match it deletes nothing.
func TestDeleteCreated(t *testing.T) {
	c, deleted := setup(t)
	ctx := context.Background()
	if err := c.DeleteCreated(ctx, map[string]any{"entity_id": "calendar.familie", "summary": "arzt dr. berger",
		"start_date_time": "2026-10-20 10:00"}); err != nil {
		t.Fatal(err)
	}
	if len(*deleted) != 1 || (*deleted)[0] != "calendar.familie/u1/" {
		t.Fatalf("deleted %v", *deleted)
	}
	for _, data := range []map[string]any{
		{"entity_id": "calendar.familie", "summary": "Arzt Dr. Berger", "start_date_time": "2026-10-20 11:00"},
		{"entity_id": "calendar.familie", "summary": "Zahnarzt", "start_date_time": "2026-10-20 10:00"},
		{"entity_id": "calendar.familie", "start_date_time": "2026-10-20 10:00"},
	} {
		if err := c.DeleteCreated(ctx, data); err == nil {
			t.Errorf("%v: deleted", data)
		}
	}
	if err := c.DeleteCreated(ctx, map[string]any{"entity_id": "calendar.feiertage", "summary": "Reformationstag",
		"start_date": "2026-10-31"}); err != nil || len(*deleted) != 2 || (*deleted)[1] != "calendar.feiertage/f1/" {
		t.Errorf("a whole day: %v %v", err, *deleted)
	}
}
