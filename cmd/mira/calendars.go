package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/miraassist/mira/internal/calendar"
	"github.com/miraassist/mira/internal/config"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/todo"
	"github.com/miraassist/mira/internal/trace"
)

// Calendars and to-do lists Mira may use: those exposed to Assist
// (docs/spec/calendar.md). What a calendar can do comes from its features.
const (
	calendarCreate = 1 // CalendarEntityFeature.CREATE_EVENT
	calendarDelete = 2 // CalendarEntityFeature.DELETE_EVENT
)

func (a *app) calendarList(ctx context.Context) ([]calendar.Info, error) {
	exposed := a.structure.Snapshot().ExposedEntities
	states, err := a.ha.GetStates(ctx, haws.StateFilter{Domains: []string{"calendar"}})
	if err != nil {
		return nil, err
	}
	var out []calendar.Info
	for _, s := range states {
		if !strings.HasPrefix(s.EntityID, "calendar.") || !exposed[s.EntityID] {
			continue
		}
		f := features(s.Attributes)
		out = append(out, calendar.Info{Entity: s.EntityID, Name: s.FriendlyName(),
			Writable: f&calendarCreate != 0, Deletable: f&calendarDelete != 0})
	}
	return out, nil
}

func (a *app) deleteEvent(ctx context.Context, entity, uid, recurrenceID string) error {
	trace.Add(ctx, trace.Action, "calendar.delete", entity)
	cmd := map[string]any{"type": "calendar/event/delete", "entity_id": entity, "uid": uid}
	if recurrenceID != "" {
		cmd["recurrence_id"] = recurrenceID
	}
	_, err := a.ha.Call(ctx, cmd)
	return err
}

// todoLists are the exposed to-do lists with what they can hold
// (docs/spec/todo-due.md).
func (a *app) todoLists(ctx context.Context) ([]todo.List, error) {
	exposed := a.structure.Snapshot().ExposedEntities
	states, err := a.ha.GetStates(ctx, haws.StateFilter{Domains: []string{"todo"}})
	if err != nil {
		return nil, err
	}
	var out []todo.List
	for _, s := range states {
		if !strings.HasPrefix(s.EntityID, "todo.") || !exposed[s.EntityID] {
			continue
		}
		out = append(out, todo.List{Entity: s.EntityID, Name: s.FriendlyName(), Features: features(s.Attributes)})
	}
	return out, nil
}

// todoItems are a list's open to-dos, from Home Assistant's todo.get_items
// (docs/spec/todo-due.md, Reading).
func (a *app) todoItems(ctx context.Context, entity string) ([]todo.Item, error) {
	trace.Add(ctx, trace.Action, "todo.get_items", entity)
	raw, err := a.ha.CallServiceResponse(ctx, "todo", "get_items",
		map[string]any{"entity_id": entity, "status": "needs_action"})
	if err != nil {
		return nil, err
	}
	var resp map[string]struct {
		Items []todo.Item `json:"items"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("todo.get_items for %s: %w (answer starts %q)", entity, err, todo.Cut(string(raw), 80))
	}
	return resp[entity].Items, nil
}

// checkTodoList is the check "todo_list": an exposed to-do list by name or
// id; "standard" is the only one, or asks which.
func (a *app) checkTodoList(ctx context.Context, value string) (string, error) {
	exposed := a.structure.Snapshot().ExposedEntities
	states, err := a.ha.GetStates(ctx, haws.StateFilter{Domains: []string{"todo"}})
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	var lists, names []string
	for _, s := range states {
		if !strings.HasPrefix(s.EntityID, "todo.") || !exposed[s.EntityID] {
			continue
		}
		names = append(names, s.FriendlyName())
		if calendar.IsDefault(value) || strings.EqualFold(value, s.EntityID) || strings.EqualFold(value, s.FriendlyName()) {
			lists = append(lists, s.EntityID)
		}
	}
	switch {
	case len(names) == 0:
		return "", fmt.Errorf("there is no to-do list I may see; in Home Assistant it must be exposed to Assist")
	case len(lists) == 1:
		return lists[0], nil
	case len(lists) == 0:
		return "", fmt.Errorf("there is no list %q; possible: %s", value, strings.Join(names, ", "))
	}
	return "", fmt.Errorf("which list? %s", strings.Join(names, ", "))
}

func features(attrs map[string]any) int {
	f, _ := attrs["supported_features"].(float64)
	return int(f)
}

// briefingPeople are the persons a per_person skill schedule runs for: those
// a personal message can reach, by a channel of their own or because "alone
// at home is me" lets a satellite count as them. Each with their own
// calendar, by convention one named like the person in Home Assistant ("Ben"
// is Ben's; docs/spec/calendar.md), and their personal profile.
func (a *app) briefingPeople(ctx context.Context) []skills.Person {
	cals, err := a.calendarList(ctx)
	if err != nil {
		a.log.Warn("could not read the calendars for personal schedules", "error", err)
	}
	lists, err := a.todoLists(ctx)
	if err != nil {
		a.log.Warn("could not read the to-do lists for personal schedules", "error", err)
	}
	names := make([]string, len(lists))
	for i, l := range lists {
		names[i] = l.Name
	}
	out := reachablePeople(a.persons(), a.logins(ctx), cals, names)
	for i := range out {
		if p, _, err := a.store.Profile(ctx, out[i].Key); err == nil {
			out[i].Profile = p
		}
	}
	return out
}

// reachablePeople are the persons a personal message reaches, with their
// own calendar and to-do list, if any: one named like the login of their
// Home Assistant user ("stefan"), or like the person ("Stefan Rinke"), case
// and spaces aside. logins is person entity: login; lists are the to-do
// lists' names.
func reachablePeople(persons map[string]config.Person, logins map[string]string, cals []calendar.Info, lists []string) []skills.Person {
	norm := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	byName := map[string]string{}
	for _, c := range cals {
		if n := norm(c.Name); n != "" {
			byName[n] = c.Name
		}
	}
	listByName := map[string]string{}
	for _, l := range lists {
		if n := norm(l); n != "" {
			listByName[n] = l
		}
	}
	var out []skills.Person
	for key, p := range persons {
		if len(p.Channels) == 0 && !p.AloneIsMe {
			continue
		}
		name := p.Name
		if name == "" {
			name = key
		}
		cal := byName[norm(logins[p.Entity])]
		if cal == "" {
			cal = byName[norm(name)]
		}
		list := listByName[norm(logins[p.Entity])]
		if list == "" {
			list = listByName[norm(name)]
		}
		out = append(out, skills.Person{Key: key, Name: name, Login: logins[p.Entity], Calendar: cal, List: list,
			Channel: len(p.Channels) > 0})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
