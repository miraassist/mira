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

// Every HA person is a person, keyed by its id; Mira's settings attach by
// that id, or by the entity for an older entry, and a HA user finds its
// person through Home Assistant's link.
func TestPersonsFromHomeAssistant(t *testing.T) {
	states := []haws.State{
		{EntityID: "person.anna", Attributes: map[string]any{"id": "anna", "friendly_name": "Anna", "user_id": "u-anna"}},
		{EntityID: "person.ben_renamed", Attributes: map[string]any{"id": "ben", "friendly_name": "Ben"}},
		{EntityID: "person.oma", Attributes: map[string]any{"id": "oma_1", "friendly_name": "Oma"}},
		{EntityID: "light.kueche"},
	}
	settings := map[string]config.Person{
		"anna":   {Channels: []string{"notify.anna"}},
		"benjie": {Entity: "person.ben_renamed", AloneIsMe: true}, // older entry, by entity
	}
	persons := mergePersons(haPersonsFrom(states), settings)
	if len(persons) != 3 {
		t.Fatalf("persons: %v", persons)
	}
	if p := persons["anna"]; len(p.Channels) != 1 || p.Name != "Anna" || p.Entity != "person.anna" || p.UserID != "u-anna" {
		t.Errorf("anna: %+v", p)
	}
	if p := persons["ben"]; !p.AloneIsMe || p.Entity != "person.ben_renamed" {
		t.Errorf("ben: %+v", p)
	}
	if p, ok := persons["oma_1"]; !ok || p.Name != "Oma" {
		t.Errorf("oma, without settings, is a person too: %+v", persons)
	}
	if got := personOfUser(persons, "u-anna"); got != "anna" {
		t.Errorf("u-anna: got %q", got)
	}
	if got := personOfUser(persons, "u-guest"); got != "" {
		t.Errorf("a user without a person: got %q", got)
	}
	// Until Home Assistant's persons were read, the config is all there is.
	if got := mergePersons(nil, settings); len(got) != 2 {
		t.Errorf("before reading HA's persons: %v", got)
	}
}
