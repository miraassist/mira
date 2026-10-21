package mailcal

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

type fakeCal struct {
	events map[string]Event // uid -> event
	next   int
}

func (f *fakeCal) Find(_ context.Context, name string) (string, bool) {
	return "calendar.pakete", strings.EqualFold(name, "Pakete")
}
func (f *fakeCal) Names(context.Context) ([]string, error) { return []string{"Pakete", "Familie"}, nil }
func (f *fakeCal) Events(_ context.Context, _ string, from, to time.Time) ([]Event, error) {
	var out []Event
	for _, e := range f.events {
		if !e.Day.Before(from.Truncate(24*time.Hour)) && e.Day.Before(to) {
			out = append(out, e)
		}
	}
	return out, nil
}
func (f *fakeCal) put(uid string, e Entry) {
	f.events[uid] = Event{UID: uid, Summary: e.Summary, Description: e.Description, Location: e.Location,
		Day: e.Day, Start: e.Start, End: e.End, AllDay: e.Start.IsZero()}
}
func (f *fakeCal) Create(_ context.Context, _ string, e Entry) error {
	f.next++
	f.put(fmt.Sprintf("u%d", f.next), e)
	return nil
}
func (f *fakeCal) Update(_ context.Context, _ string, uid string, e Entry) error {
	f.put(uid, e)
	return nil
}
func (f *fakeCal) Delete(_ context.Context, _ string, uid string) error {
	delete(f.events, uid)
	return nil
}

// fakeModel answers extraction with the next item and matching with same.
type fakeModel struct {
	items        []item
	same         int
	instructions []string
}

func (f *fakeModel) Ask(_ context.Context, system, instruction string, _ json.RawMessage) (string, error) {
	f.instructions = append(f.instructions, instruction)
	if system == matchSystem {
		return fmt.Sprintf(`{"same":%d}`, f.same), nil
	}
	it := f.items[0]
	f.items = f.items[1:]
	raw, _ := json.Marshal(it)
	return string(raw), nil
}

var berlin, _ = time.LoadLocation("Europe/Berlin")
var now = time.Date(2026, 10, 20, 9, 0, 0, 0, berlin)

func service(m *fakeModel) (*Service, *fakeCal) {
	cal := &fakeCal{events: map[string]Event{}}
	return New(Options{Calendar: cal, Model: m, Location: berlin, Clock: func() time.Time { return now },
		Apps: func() map[string]App { return map[string]App{"pakete": {Extract: "Pakete lesen.", Done: "●"}} }}), cal
}

func mail(entry string, at time.Time) Mail {
	return Mail{Entry: entry, Sender: "noreply@dhl.de", Subject: "Ihr Paket", Date: at, Text: "<p>Hallo&nbsp;<b>du</b></p><style>x{}</style>"}
}

func only(t *testing.T, cal *fakeCal) Event {
	t.Helper()
	if len(cal.events) != 1 {
		t.Fatalf("events: %+v", cal.events)
	}
	for _, e := range cal.events {
		return e
	}
	return Event{}
}

func TestAnnouncedThenTodayThenDelivered(t *testing.T) {
	m := &fakeModel{items: []item{
		{Relevant: true, Title: "Paket von Amazon: Druckerpatronen", Keys: []string{"00340434"}, Status: "scheduled", Day: "2026-10-21", Details: []string{"DHL, Sendung 00340434", "Bestellnummer: ", "Abholung bis:"}},
		{Relevant: true, Title: "Paket von Amazon: Druckerpatronen", Keys: []string{"00340434"}, Status: "scheduled", Day: "2026-10-21", Start: "10:15", End: "12:15"},
		{Relevant: true, Title: "Paket von Amazon zugestellt", Keys: []string{"00340434"}, Status: "done", Day: "2026-10-21"},
	}}
	s, cal := service(m)
	ctx := context.Background()

	out, err := s.Handle(ctx, mail("Pakete", now.Add(-time.Hour)))
	if err != nil || out.Action != "created" {
		t.Fatalf("first: %+v %v", out, err)
	}
	e := only(t, cal)
	if !e.AllDay || e.Day.Format("2006-01-02") != "2026-10-21" || !strings.Contains(e.Description, "mira-mail: 00340434") || !strings.Contains(e.Description, "DHL, Sendung") {
		t.Errorf("entry: %+v", e)
	}
	if strings.Contains(e.Description, "Bestellnummer") || strings.Contains(e.Description, "Abholung") {
		t.Errorf("empty detail lines kept: %q", e.Description)
	}
	// The mail reached the model as data: plain text, with sender and date.
	if in := m.instructions[0]; !strings.Contains(in, "Hallo du") || strings.Contains(in, "<p>") || !strings.Contains(in, "Absender: noreply@dhl.de") || !strings.Contains(in, "Pakete lesen.") {
		t.Errorf("instruction:\n%s", in)
	}

	out, _ = s.Handle(ctx, mail("Pakete", now))
	e = only(t, cal)
	if out.Action != "updated" || e.AllDay || e.Start.Format("15:04") != "10:15" || e.End.Format("15:04") != "12:15" {
		t.Errorf("today: %+v %+v", out, e)
	}

	out, _ = s.Handle(ctx, mail("Pakete", now.Add(2*time.Hour)))
	e = only(t, cal)
	if out.Action != "updated" || !strings.Contains(e.Description, "Status: done") {
		t.Errorf("delivered: %+v %+v", out, e)
	}
}

// An older mail never overwrites a newer one, whatever the order of arrival.
func TestAnOlderMailIsDropped(t *testing.T) {
	m := &fakeModel{items: []item{
		{Relevant: true, Title: "Paket heute", Keys: []string{"K1"}, Status: "scheduled", Day: "2026-10-20", Start: "10:00", End: "12:00"},
		{Relevant: true, Title: "Paket angekündigt", Keys: []string{"K1"}, Status: "scheduled", Day: "2026-10-22"},
	}}
	s, cal := service(m)
	s.Handle(context.Background(), mail("Pakete", now))
	out, err := s.Handle(context.Background(), mail("Pakete", now.Add(-24*time.Hour)))
	if err != nil || out.Action != "ignored" || out.Reason != "older than the entry" {
		t.Errorf("older: %+v %v", out, err)
	}
	if e := only(t, cal); e.Summary != "Paket heute" {
		t.Errorf("overwritten: %+v", e)
	}
}

func TestCancelledRemovesAndUnknownCancelIsIgnored(t *testing.T) {
	m := &fakeModel{items: []item{
		{Relevant: true, Title: "Termin", Keys: []string{"B7"}, Status: "scheduled", Day: "2026-10-23"},
		{Relevant: true, Title: "Termin", Keys: []string{"B7"}, Status: "cancelled", Day: "2026-10-23"},
		{Relevant: true, Title: "Anderer", Keys: []string{"B8"}, Status: "cancelled", Day: "2026-10-23"},
	}}
	s, cal := service(m)
	ctx := context.Background()
	s.Handle(ctx, mail("Pakete", now))
	if out, _ := s.Handle(ctx, mail("Pakete", now.Add(time.Minute))); out.Action != "deleted" || len(cal.events) != 0 {
		t.Errorf("cancel: %+v %+v", out, cal.events)
	}
	if out, _ := s.Handle(ctx, mail("Pakete", now.Add(2*time.Minute))); out.Action != "ignored" || len(cal.events) != 0 {
		t.Errorf("cancel of nothing: %+v", out)
	}
}

// What is not valid is dropped; no title or no day, no entry.
func TestChecks(t *testing.T) {
	s, _ := service(&fakeModel{})
	cases := []struct {
		it   item
		ok   bool
		want func(Entry) bool
	}{
		{item{Title: "x", Day: "2026-13-01"}, false, nil},
		{item{Title: "", Day: "2026-10-21"}, false, nil},
		{item{Title: "x", Day: "2026-10-01"}, false, nil}, // more than 7 days before the mail
		{item{Title: "x", Day: "2028-01-01"}, false, nil}, // more than a year after
		{item{Title: "x", Day: "2026-10-21", Start: "25:00"}, true, func(e Entry) bool { return e.Start.IsZero() }},
		{item{Title: "x", Day: "2026-10-21", Start: "10:00", End: "09:00"}, true, func(e Entry) bool { return e.End.Sub(e.Start) == time.Hour }},
		{item{Title: strings.Repeat("a", 200), Day: "2026-10-21"}, true, func(e Entry) bool { return len(e.Summary) == 80 }},
	}
	for i, c := range cases {
		e, ok := s.check(&c.it, now)
		if ok != c.ok || (ok && !c.want(e)) {
			t.Errorf("case %d: %+v %v", i, e, ok)
		}
	}
	m := &fakeModel{items: []item{{Relevant: true, Title: "x", Keys: []string{"a b/c"}, Day: "2026-10-21"}}}
	s2, cal := service(m)
	s2.Handle(context.Background(), mail("Pakete", now))
	if e := only(t, cal); !strings.Contains(e.Description, "mira-mail: -") {
		t.Errorf("bad key kept: %q", e.Description)
	}
}

func TestIrrelevantUnknownAppAndMissingCalendar(t *testing.T) {
	s, cal := service(&fakeModel{items: []item{{Relevant: false}}})
	ctx := context.Background()
	if out, err := s.Handle(ctx, mail("Pakete", now)); err != nil || out.Reason != "not relevant" || len(cal.events) != 0 {
		t.Errorf("irrelevant: %+v %v", out, err)
	}
	if _, err := s.Handle(ctx, mail("Rechnungen", now)); err == nil {
		t.Error("unknown application accepted")
	}
	if out, _ := s.Handle(ctx, mail("Pakete stefan", now)); out.Reason != "no calendar Pakete stefan" {
		t.Errorf("missing calendar: %+v", out)
	}
	if s.App("pakete  Stefan") != "pakete" || s.App("Familie") != "" {
		t.Error("app from the entry's name")
	}
}

// Without a key the model says which open entry a mail is about; in doubt a
// new entry.
func TestWithoutKeyTheModelMatches(t *testing.T) {
	m := &fakeModel{items: []item{
		{Relevant: true, Title: "Paket von Oma", Status: "scheduled", Day: "2026-10-22"},
		{Relevant: true, Title: "Paket von Oma, heute", Status: "scheduled", Day: "2026-10-22", Start: "14:00"},
		{Relevant: true, Title: "Paket von Shop", Status: "scheduled", Day: "2026-10-23"},
	}, same: 1}
	s, cal := service(m)
	ctx := context.Background()
	s.Handle(ctx, mail("Pakete", now))
	if out, _ := s.Handle(ctx, mail("Pakete", now.Add(time.Hour))); out.Action != "updated" || len(cal.events) != 1 {
		t.Errorf("matched: %+v %+v", out, cal.events)
	}
	m.same = 0
	if out, _ := s.Handle(ctx, mail("Pakete", now.Add(2*time.Hour))); out.Action != "created" || len(cal.events) != 2 {
		t.Errorf("new: %+v", out)
	}
}

func TestCleanup(t *testing.T) {
	s, cal := service(&fakeModel{})
	day := func(d int) time.Time { return time.Date(2026, 10, 20+d, 0, 0, 0, 0, berlin) }
	desc := func(status string) string { return "x\n\nmira-mail: k\nStand: 2026-10-01T00:00:00Z\nStatus: " + status }
	cal.events = map[string]Event{
		"done-yesterday":   {UID: "done-yesterday", Day: day(-1), Description: desc("done")},
		"done-today":       {UID: "done-today", Day: day(0), Description: desc("done")},
		"open-3-days-ago":  {UID: "open-3-days-ago", Day: day(-3), Description: desc("scheduled")},
		"open-10-days-ago": {UID: "open-10-days-ago", Day: day(-10), Description: desc("scheduled")},
		"not-mira":         {UID: "not-mira", Day: day(-10), Description: "von Hand"},
	}
	if err := s.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, uid := range []string{"done-today", "open-3-days-ago", "not-mira"} {
		if _, ok := cal.events[uid]; !ok {
			t.Errorf("%s removed", uid)
		}
	}
	for _, uid := range []string{"done-yesterday", "open-10-days-ago"} {
		if _, ok := cal.events[uid]; ok {
			t.Errorf("%s kept", uid)
		}
	}
}

// An update keeps what the newer mail does not name; any shared key finds
// the entry, and the keys add up.
func TestUpdateKeepsWhatTheMailDoesNotName(t *testing.T) {
	m := &fakeModel{items: []item{
		{Relevant: true, Title: "Paket von Shop: Druckerpatronen", Keys: []string{"S1"}, Status: "scheduled", Day: "2026-10-21",
			Start: "10:00", End: "12:00", Location: "Kiosk", Details: []string{"Paketdienst: DHL", "Bestellnummer: O9"}},
		{Relevant: true, Title: "Paket von Shop", Keys: []string{"s1", "O9"}, Status: "scheduled", Day: "2026-10-21",
			Details: []string{"Sendungsnummer: S1", "paketdienst: DHL Express"}},
		{Relevant: true, Title: "Paket von Shop zugestellt", Keys: []string{"O9"}, Status: "done", Day: "2026-10-21"},
	}}
	s, cal := service(m)
	ctx := context.Background()
	s.Handle(ctx, mail("Pakete", now))
	out, _ := s.Handle(ctx, mail("Pakete", now.Add(time.Hour)))
	e := only(t, cal)
	want := "Sendungsnummer: S1\npaketdienst: DHL Express\nBestellnummer: O9\n\nmira-mail: S1, O9\n"
	if out.Action != "updated" || e.Summary != "Paket von Shop: Druckerpatronen" || e.Location != "Kiosk" ||
		e.Start.Format("15:04") != "10:00" || e.End.Format("15:04") != "12:00" || !strings.HasPrefix(e.Description, want) {
		t.Errorf("merged: %+v %+v", out, e)
	}
	// The order number alone finds it too; a new title replaces the old.
	out, _ = s.Handle(ctx, mail("Pakete", now.Add(2*time.Hour)))
	if e = only(t, cal); out.Action != "updated" || e.Summary != "● Paket von Shop zugestellt" || !strings.Contains(e.Description, "Status: done") {
		t.Errorf("by the second key: %+v %+v", out, e)
	}
}

// Without a day there is no entry until a mail names one; a cancellation
// needs no day.
func TestNoDayNoEntryButCancelWithoutDay(t *testing.T) {
	m := &fakeModel{items: []item{
		{Relevant: true, Title: "Paket von Shop", Keys: []string{"A1"}, Status: "scheduled"},
		{Relevant: true, Title: "Paket von Shop", Keys: []string{"A1"}, Status: "scheduled", Day: "2026-10-22"},
		{Relevant: true, Keys: []string{"A1"}, Status: "cancelled"},
	}}
	s, cal := service(m)
	ctx := context.Background()
	if out, _ := s.Handle(ctx, mail("Pakete", now)); out.Action != "ignored" || len(cal.events) != 0 {
		t.Errorf("without day: %+v", out)
	}
	s.Handle(ctx, mail("Pakete", now.Add(time.Hour)))
	if out, _ := s.Handle(ctx, mail("Pakete", now.Add(2*time.Hour))); out.Action != "deleted" || len(cal.events) != 0 {
		t.Errorf("cancel without day: %+v %+v", out, cal.events)
	}
}
