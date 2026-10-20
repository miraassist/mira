package main

import (
	"testing"

	"github.com/miraassist/mira/internal/calendar"
	"github.com/miraassist/mira/internal/config"
)

// Personal schedules run for whom a personal message reaches (a channel, or
// "alone at home is me"). Their own calendar is the one named like the
// login of their Home Assistant user, else like the person, case and spaces
// aside; without one the calendar is empty.
func TestReachablePeopleWithTheirOwnCalendar(t *testing.T) {
	persons := map[string]config.Person{
		"stefan_rinke": {Name: "Stefan Rinke", Entity: "person.stefan_rinke", AloneIsMe: true},
		"ben":          {Name: "Ben", Entity: "person.ben", Channels: []string{"notify.ben_phone"}},
		"anna":         {Name: "Anna", Entity: "person.anna", AloneIsMe: true},
		"gast":         {Name: "Gast", Entity: "person.gast"}, // no channel: a message would not reach them
	}
	logins := map[string]string{"person.stefan_rinke": "stefan", "person.anna": "anna.m"}
	cals := []calendar.Info{{Name: "Stefan"}, {Name: "ben "}, {Name: "Familie"}, {Name: ""}}
	got := reachablePeople(persons, logins, cals)
	want := []struct{ key, login, cal string }{{"anna", "anna.m", ""}, {"ben", "", "ben "}, {"stefan_rinke", "stefan", "Stefan"}}
	if len(got) != len(want) {
		t.Fatalf("people: %+v", got)
	}
	for i, w := range want {
		if got[i].Key != w.key || got[i].Login != w.login || got[i].Calendar != w.cal {
			t.Errorf("person %d: %+v, want %s with calendar %q", i, got[i], w.key, w.cal)
		}
	}
}

// A first name alone is no claim: "Stefan Rinke" without a login "stefan"
// does not own the calendar "Stefan".
func TestNoCalendarByFirstName(t *testing.T) {
	got := reachablePeople(map[string]config.Person{
		"stefan_rinke": {Name: "Stefan Rinke", Entity: "person.stefan_rinke", AloneIsMe: true},
	}, nil, []calendar.Info{{Name: "Stefan"}})
	if len(got) != 1 || got[0].Calendar != "" {
		t.Errorf("matched by first name: %+v", got)
	}
}
