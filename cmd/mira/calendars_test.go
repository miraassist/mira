package main

import (
	"testing"

	"github.com/miraassist/mira/internal/calendar"
	"github.com/miraassist/mira/internal/config"
)

// A calendar named like the person in Home Assistant is theirs, case and
// spaces aside; a person without one is left out, and so is a calendar of
// nobody.
func TestOwnCalendarsByConvention(t *testing.T) {
	persons := map[string]config.Person{
		"ben":  {Name: "Ben"},
		"anna": {Name: "Anna"},
		"oma":  {Name: "Oma Hilde"},
		"x":    {},
	}
	cals := []calendar.Info{{Name: "ben "}, {Name: "Familie"}, {Name: "OMA HILDE"}, {Name: ""}}
	got := ownCalendars(persons, cals)
	if len(got) != 2 || got[0].Key != "ben" || got[0].Calendar != "ben " || got[1].Key != "oma" || got[1].Name != "Oma Hilde" {
		t.Errorf("own calendars: %+v", got)
	}
}
