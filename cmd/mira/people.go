package main

import (
	"context"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/sker65/mira/internal/config"
	haws "github.com/sker65/mira/internal/ha/ws"
)

// people is the directory of Home Assistant's persons: with Home Assistant
// every HA person is a person for Mira, keyed by the HA person's id. Mira
// creates nothing and keeps nothing in sync; the config only adds Mira's
// settings per person (docs/spec/benutzer.md).
type people struct {
	mu      sync.Mutex
	list    []haPerson
	loaded  bool
	checked time.Time
}

// haPerson is a person entity as Home Assistant has it.
type haPerson struct {
	ID, Entity, Name, UserID string
}

// peopleRefresh is how often the directory is read again; an unknown HA
// user asks sooner, but not more often than peopleRecheck.
const (
	peopleRefresh = time.Minute
	peopleRecheck = 10 * time.Second
)

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
// by its entity. Without Home Assistant's persons the config is all there is.
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

// refresh reads the persons again; changed says whether the set differs.
func (pp *people) refresh(ctx context.Context, ha *haws.Client) (changed bool, err error) {
	states, err := ha.States(ctx)
	if err != nil {
		return false, err
	}
	list := haPersonsFrom(states)
	pp.mu.Lock()
	defer pp.mu.Unlock()
	pp.checked = time.Now()
	changed = !pp.loaded || !slicesEqual(pp.list, list)
	pp.list, pp.loaded = list, true
	return changed, nil
}

func (pp *people) snapshot() []haPerson {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	if !pp.loaded {
		return nil
	}
	return append([]haPerson{}, pp.list...)
}

// due says whether a recheck for an unknown user is allowed now.
func (pp *people) due() bool {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	return time.Since(pp.checked) > peopleRecheck
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
// settings, or the config's alone without Home Assistant.
func (a *app) persons() map[string]config.Person {
	settings := a.liveConfig().Persons
	if settings == nil {
		settings = a.cfg.Persons // the chat command has no live config
	}
	if a.ha == nil {
		return settings
	}
	return mergePersons(a.people.snapshot(), settings)
}

// personOfHAUser maps a HA user to a person key; an unknown user makes the
// directory read Home Assistant again, as a new person may just have come.
func (a *app) personOfHAUser(ctx context.Context, user string) string {
	if user == "" {
		return ""
	}
	if key := personOfUser(a.persons(), user); key != "" || a.ha == nil || !a.people.due() {
		return key
	}
	if changed, err := a.people.refresh(ctx, a.ha); err != nil {
		a.log.Warn("could not read the persons", "error", err)
	} else if changed {
		a.peopleChanged()
	}
	return personOfUser(a.persons(), user)
}

// watchPeople reads Home Assistant's persons now and every minute. A
// change restarts the notifier, which holds the persons it delivers to.
func (a *app) watchPeople(ctx context.Context) error {
	t := time.NewTicker(peopleRefresh)
	defer t.Stop()
	for {
		if a.ha.Connected() {
			changed, err := a.people.refresh(ctx, a.ha)
			if err != nil {
				a.log.Warn("could not read the persons", "error", err)
			} else if changed {
				a.peopleChanged()
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
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
