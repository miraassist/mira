package main

import (
	"context"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/mailcal"
	"github.com/miraassist/mira/internal/skills"
)

// The mail calendar (docs/spec/mail-kalender.md) keeps its entries in Home
// Assistant's calendars: read over REST like every calendar, written over
// the WebSocket commands calendar/event/create, update and delete.

// builtinMail are the mail applications that are on when their folder is
// there; the name of the IMAP entry's device is the switch.
func builtinMail() []string {
	return []string{"pakete"}
}

// mailApps are the mail applications of the loaded skills: name to their
// mail.extract.
func mailApps(loaded []skills.Skill) map[string]string {
	out := map[string]string{}
	for _, sk := range loaded {
		if sk.Mail != nil {
			out[strings.ToLower(sk.Name)] = sk.Mail.Extract
		}
	}
	return out
}

type mailCalendar struct{ a *app }

func (c mailCalendar) Find(ctx context.Context, name string) (string, bool) {
	cals, err := c.a.calendarList(ctx)
	if err != nil {
		return "", false
	}
	for _, cal := range cals {
		if strings.EqualFold(cal.Name, name) {
			return cal.Entity, true
		}
	}
	return "", false
}

func (c mailCalendar) Names(ctx context.Context) ([]string, error) {
	cals, err := c.a.calendarList(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(cals))
	for _, cal := range cals {
		out = append(out, cal.Name)
	}
	return out, nil
}

func (c mailCalendar) Events(ctx context.Context, entity string, from, to time.Time) ([]mailcal.Event, error) {
	events, err := c.a.calendar.Events(ctx, entity, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]mailcal.Event, 0, len(events))
	for _, e := range events {
		if e.Recurring {
			continue // never one of Mira's
		}
		ev := mailcal.Event{UID: e.UID, Summary: e.Summary, Description: e.Description, Location: e.Location,
			Day: time.Date(e.Start.Year(), e.Start.Month(), e.Start.Day(), 0, 0, 0, 0, e.Start.Location()), AllDay: e.AllDay}
		if !e.AllDay {
			ev.Start, ev.End = e.Start, e.End
		}
		out = append(out, ev)
	}
	return out, nil
}

func (c mailCalendar) Create(ctx context.Context, entity string, e mailcal.Entry) error {
	_, err := c.a.ha.Call(ctx, map[string]any{"type": "calendar/event/create", "entity_id": entity, "event": haEvent(e)})
	return err
}

func (c mailCalendar) Update(ctx context.Context, entity, uid string, e mailcal.Entry) error {
	_, err := c.a.ha.Call(ctx, map[string]any{"type": "calendar/event/update", "entity_id": entity, "uid": uid,
		"event": haEvent(e)})
	return err
}

func (c mailCalendar) Delete(ctx context.Context, entity, uid string) error {
	return c.a.deleteEvent(ctx, entity, uid, "")
}

// haEvent is an entry as Home Assistant takes it: dates for a whole day (the
// end is the next day), times with their offset otherwise.
func haEvent(e mailcal.Entry) map[string]any {
	ev := map[string]any{"summary": e.Summary, "description": e.Description}
	if e.Location != "" {
		ev["location"] = e.Location
	}
	if e.Start.IsZero() {
		ev["dtstart"] = e.Day.Format(time.DateOnly)
		ev["dtend"] = e.Day.AddDate(0, 0, 1).Format(time.DateOnly)
	} else {
		ev["dtstart"] = e.Start.Format(time.RFC3339)
		ev["dtend"] = e.End.Format(time.RFC3339)
	}
	return ev
}
