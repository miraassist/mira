package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/miraassist/mira/internal/calendar"
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
