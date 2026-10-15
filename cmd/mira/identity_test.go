package main

import (
	"testing"

	"github.com/sker65/mira/internal/config"
	haws "github.com/sker65/mira/internal/ha/ws"
)

func TestAloneAtHome(t *testing.T) {
	persons := map[string]config.Person{
		"anna": {Entity: "person.anna", AloneIsMe: true},
		"ben":  {Entity: "person.ben"},
	}
	states := func(anna, ben string) []haws.State {
		return []haws.State{{EntityID: "person.anna", State: anna}, {EntityID: "person.ben", State: ben}}
	}
	for _, c := range []struct {
		anna, ben, want string
	}{
		{"home", "not_home", "anna"},
		{"home", "home", ""},         // not alone
		{"not_home", "home", ""},     // Ben is alone but did not switch it on
		{"home", "unknown", ""},      // Ben might be there
		{"not_home", "not_home", ""}, // nobody
	} {
		if got := aloneAtHome(persons, states(c.anna, c.ben)); got != c.want {
			t.Errorf("anna %s, ben %s: got %q, want %q", c.anna, c.ben, got, c.want)
		}
	}
}

func TestPersonOfUser(t *testing.T) {
	persons := map[string]config.Person{"anna": {Entity: "person.anna"}, "ben": {Entity: "person.ben"}}
	states := []haws.State{
		{EntityID: "person.anna", Attributes: map[string]any{"user_id": "u-anna"}},
		{EntityID: "person.ben", Attributes: map[string]any{"user_id": "u-ben"}},
	}
	if got := personOfUser(persons, states, "u-ben"); got != "ben" {
		t.Errorf("got %q", got)
	}
	if got := personOfUser(persons, states, "u-guest"); got != "" {
		t.Errorf("a user without a person: got %q", got)
	}
}
