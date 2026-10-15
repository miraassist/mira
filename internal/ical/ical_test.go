package ical

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	data, err := os.ReadFile("testdata/abfall.ics")
	if err != nil {
		t.Fatal(err)
	}
	berlin, _ := time.LoadLocation("Europe/Berlin")
	from := time.Date(2026, 10, 15, 0, 0, 0, 0, berlin)
	events, err := Parse(data, berlin, from, from.AddDate(0, 0, 14))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range events {
		got = append(got, e.Start.Format("01-02 15:04")+" "+e.Summary)
	}
	want := []string{
		"10-15 00:00 Restabfall (privat) 2-Rad 02-woechentl.",
		"10-22 00:00 Wertstoff (privat) 2-Rad 02-woechentl.",
		"10-22 00:00 Papier", // every two weeks since Jan 1st; same day keeps file order
		"10-24 09:00 Mobile Schadstoffsammlung, Marktplatz",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("events:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !events[0].AllDay || events[3].AllDay {
		t.Error("all-day flags wrong")
	}
	if _, err := Parse([]byte("<html>"), berlin, from, from); err == nil {
		t.Error("HTML accepted as a calendar")
	}
}

func TestCacheHidesTheURL(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Query().Get("secret") == "down" {
			http.Error(w, "no", http.StatusInternalServerError)
			return
		}
		w.Write([]byte("BEGIN:VCALENDAR\nEND:VCALENDAR\n"))
	}))
	defer srv.Close()
	c := &Cache{}
	for range 3 {
		if _, err := c.Get(context.Background(), srv.URL+"/?secret=ok"); err != nil {
			t.Fatal(err)
		}
	}
	if hits != 1 {
		t.Errorf("downloaded %d times, want once", hits)
	}
	_, err := c.Get(context.Background(), srv.URL+"/?secret=down")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("error leaks the URL or is missing: %v", err)
	}
	_, err = c.Get(context.Background(), "http://127.0.0.1:1/?secret=x")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("network error leaks the URL: %v", err)
	}
}
