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

// "Stefan Rinke" owns the calendar "Stefan" by first name, unless another
// person has that first name too.
func TestOwnCalendarByFirstName(t *testing.T) {
	cals := []calendar.Info{{Name: "Stefan"}, {Name: "Familie"}}
	got := reachablePeople(map[string]config.Person{
		"stefan_rinke": {Name: "Stefan Rinke", AloneIsMe: true},
	}, cals)
	if len(got) != 1 || got[0].Calendar != "Stefan" {
		t.Errorf("by first name: %+v", got)
	}
	got = reachablePeople(map[string]config.Person{
		"stefan_rinke": {Name: "Stefan Rinke", AloneIsMe: true},
		"stefan_meier": {Name: "Stefan Meier", Channels: []string{"notify.x"}},
	}, cals)
	for _, p := range got {
		if p.Calendar != "" {
			t.Errorf("ambiguous first name still matched: %+v", got)
		}
	}
}
