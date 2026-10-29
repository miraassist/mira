package main

import (
	"context"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/miraassist/mira/internal/config"
	haws "github.com/miraassist/mira/internal/ha/ws"
)

// people is the directory of Home Assistant's persons: with Home Assistant
// every HA person is a person for Mira, keyed by the HA person's id. Mira
// creates nothing and keeps nothing in sync; the config only adds Mira's
// settings per person (docs/spec/benutzer.md).
type people struct {
	mu     sync.Mutex
	list   []haPerson
	states []haws.State
	loaded bool
}

// haPerson is a person entity as Home Assistant has it.
type haPerson struct {
	ID, Entity, Name, UserID string
}

// personStates is what Mira reads of the persons: Home Assistant filters
// to the domain person (docs/spec/ha-anbindung.md, Watching states).
var personStates = haws.StateFilter{Domains: []string{"person"}}

// haPersonsFrom reads the person entities from the states. The id is the
// HA person's own (attributes.id), which stays when the entity is renamed.
func haPersonsFrom(states []haws.State) []haPerson {
	var out []haPerson
	for _, s := range states {
		object, ok := strings.CutPrefix(s.EntityID, "person.")
		if !ok {
			continue
		}
		p := haPerson{ID: object, Entity: s.EntityID, Name: object}
		if id, _ := s.Attributes["id"].(string); id != "" {
			p.ID = id
		}
		if name, _ := s.Attributes["friendly_name"].(string); name != "" {
			p.Name = name
		}
		p.UserID, _ = s.Attributes["user_id"].(string)
		out = append(out, p)
	}
	return out
}

// mergePersons lays Mira's settings over Home Assistant's persons. A
// setting belongs to a person by the person's id, or, for an older entry,
// by its entity. Until Home Assistant's persons were read, the config is all
// there is.
func mergePersons(ha []haPerson, settings map[string]config.Person) map[string]config.Person {
	if ha == nil {
		return settings
	}
	byEntity := map[string]config.Person{}
	for _, p := range settings {
		if p.Entity != "" {
			byEntity[p.Entity] = p
		}
	}
	out := make(map[string]config.Person, len(ha))
	for _, h := range ha {
		p, ok := settings[h.ID]
		if !ok {
			p = byEntity[h.Entity]
		}
		p.Entity, p.Name, p.UserID = h.Entity, h.Name, h.UserID
		out[h.ID] = p
	}
	return out
}

// refresh reads the persons once, for a command that runs without
// watching them; changed says whether the set differs.
func (pp *people) refresh(ctx context.Context, ha *haws.Client) (changed bool, err error) {
	states, err := ha.GetStates(ctx, personStates)
	if err != nil {
		return false, err
	}
	return pp.set(states), nil
}

// set takes the person states as Home Assistant has them now; changed says
// whether the set of persons differs (a state alone does not change it).
func (pp *people) set(states []haws.State) (changed bool) {
	list := haPersonsFrom(states)
	pp.mu.Lock()
	defer pp.mu.Unlock()
	changed = !pp.loaded || !slicesEqual(pp.list, list)
	pp.list, pp.states, pp.loaded = list, states, true
	return changed
}

// personStates are the persons' states as last reported: home, away.
func (pp *people) personStates() []haws.State {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	return pp.states
}

func (pp *people) snapshot() []haPerson {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	if !pp.loaded {
		return nil
	}
	return append([]haPerson{}, pp.list...)
}

func slicesEqual(a, b []haPerson) bool {
	if len(a) != len(b) {
		return false
	}
	ma := map[string]haPerson{}
	for _, p := range a {
		ma[p.ID] = p
	}
	for _, p := range b {
		if ma[p.ID] != p {
			return false
		}
	}
	return true
}

// persons are the persons Mira knows now: Home Assistant's with Mira's
// settings.
func (a *app) persons() map[string]config.Person {
	settings := a.liveConfig().Persons
	if settings == nil {
		settings = a.cfg.Persons // the chat command has no live config
	}
	return mergePersons(a.people.snapshot(), settings)
}

// personOfHAUser maps a HA user to a person key.
func (a *app) personOfHAUser(ctx context.Context, user string) string {
	if user == "" {
		return ""
	}
	return personOfUser(a.persons(), user)
}

// watchPeople keeps Home Assistant's persons and their states current:
// Home Assistant reports every change of a person (a new one, a renamed one,
// home or away), nothing is read again and again. A change of the set
// restarts the notifier, which holds the persons it delivers to.
func (a *app) watchPeople(ctx context.Context) error {
	changed := make(chan struct{}, 1)
	w, err := a.ha.WatchStates(ctx, personStates, func() {
		select { // on the connection's read loop: only pass it on
		case changed <- struct{}{}:
		default:
		}
	})
	if err != nil {
		return err
	}
	defer w.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
			if a.people.set(w.States()) {
				a.peopleChanged()
			}
		}
	}
}

// peopleChanged logs the persons and lets the notifier pick them up.
func (a *app) peopleChanged() {
	a.logPeople()
	if a.runCtx != nil {
		go a.restartProactive(a.runCtx)
	}
}

func (a *app) logPeople() {
	keys := slices.Sorted(maps.Keys(a.persons()))
	a.log.Info("persons from home assistant", "persons", strings.Join(keys, ","))
}
