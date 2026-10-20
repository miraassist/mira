package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/miraassist/mira/internal/calendar"
	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/trace"
)

// Calendars and to-do lists Mira may use: those exposed to Assist
// (docs/spec/kalender.md). What a calendar can do comes from its features.
const (
	calendarCreate = 1 // CalendarEntityFeature.CREATE_EVENT
	calendarDelete = 2 // CalendarEntityFeature.DELETE_EVENT
)

func (a *app) calendarList(ctx context.Context) ([]calendar.Info, error) {
	exposed := a.structure.Snapshot().ExposedEntities
	states, err := a.ha.States(ctx)
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

// checkTodoList is the check "todo_list": an exposed to-do list by name or
// id; "standard" is the only one, or asks which.
func (a *app) checkTodoList(ctx context.Context, value string) (string, error) {
	exposed := a.structure.Snapshot().ExposedEntities
	states, err := a.ha.States(ctx)
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

// peopleWithCalendars are the persons with a calendar of their own, by
// convention: an exposed calendar named like the person in Home Assistant
// ("Ben" is Ben's), case and spaces aside. Skill schedules with per_person
// run for them (docs/spec/kalender.md).
func (a *app) peopleWithCalendars(ctx context.Context) []skills.Person {
	cals, err := a.calendarList(ctx)
	if err != nil {
		a.log.Warn("could not read the calendars for personal schedules", "error", err)
		return nil
	}
	return ownCalendars(a.persons(), cals)
}

// ownCalendars matches persons and calendars by name.
func ownCalendars(persons map[string]config.Person, cals []calendar.Info) []skills.Person {
	byName := map[string]string{}
	for _, c := range cals {
		byName[strings.ToLower(strings.TrimSpace(c.Name))] = c.Name
	}
	var out []skills.Person
	for key, p := range persons {
		if name, ok := byName[strings.ToLower(strings.TrimSpace(p.Name))]; ok && p.Name != "" {
			out = append(out, skills.Person{Key: key, Name: p.Name, Calendar: name})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
