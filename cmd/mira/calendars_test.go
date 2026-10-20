package main

import (
	"testing"

	"github.com/miraassist/mira/internal/calendar"
	"github.com/miraassist/mira/internal/config"
)

// Personal schedules run for whom a personal message reaches (a channel, or
// "alone at home is me"), with the calendar named like them, case and spaces
// aside; without one the calendar is empty.
func TestReachablePeopleWithTheirOwnCalendar(t *testing.T) {
	persons := map[string]config.Person{
		"ben":  {Name: "Ben", Channels: []string{"notify.ben_phone"}},
		"anna": {Name: "Anna", AloneIsMe: true},
		"oma":  {Name: "Oma Hilde", Channels: []string{"notify.oma"}},
		"gast": {Name: "Gast"}, // no channel: a personal message would not reach them
	}
	cals := []calendar.Info{{Name: "ben "}, {Name: "Familie"}, {Name: "OMA HILDE"}}
	got := reachablePeople(persons, cals)
	want := []struct{ key, cal string }{{"anna", ""}, {"ben", "ben "}, {"oma", "OMA HILDE"}}
	if len(got) != len(want) {
		t.Fatalf("people: %+v", got)
	}
	for i, w := range want {
		if got[i].Key != w.key || got[i].Calendar != w.cal {
			t.Errorf("person %d: %+v, want %s with calendar %q", i, got[i], w.key, w.cal)
		}
	}
}
