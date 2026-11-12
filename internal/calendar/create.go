package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

// ToolCreate is the tool on the building block calendar.create.
const ToolCreate = "calendar_create"

// ActionCreate is the building block's name where a skill's finish names it.
const ActionCreate = "calendar.create"

// The limits of the building block (docs/spec/calendar.md, Adding).
const (
	maxTitle       = 80
	maxLocation    = 100
	maxDescription = 1000
)

// NewEvent is what the building block calendar.create enters: with a time
// (Start, End) or for whole days (Day, DayAfter), never both.
type NewEvent struct {
	Title, Location, Description string
	Start, End                   time.Time
	Day, DayAfter                time.Time
	// Calendar is a calendar's name or entity, "standard", or empty.
	Calendar string
	// Person is whose turn or thread it is; empty for Mira's own code (the
	// mail calendar), which may name any writable calendar.
	Person string
}

// Created is what was entered, and where.
type Created struct {
	Entity, Name string
	Event        NewEvent
}

// Refusal is the building block's no: nothing was entered, and Text says
// why in words the caller passes on.
type Refusal struct{ Text string }

func (r *Refusal) Error() string { return r.Text }

// Create is the building block calendar.create (docs/spec/calendar.md,
// Adding): it checks the dates and the calendar, then calls Home
// Assistant's calendar.create_event. A *Refusal is a no before Home
// Assistant was asked; any other error is Home Assistant's or the list of
// calendars'.
func (c *Calendar) Create(ctx context.Context, e NewEvent) (Created, error) {
	t := c.texts(ctx)
	if err := c.checkEvent(t, &e); err != nil {
		c.log.Info("calendar.create refused", "title", e.Title, "person", e.Person, "why", err.Error())
		return Created{}, err
	}
	cal, err := c.calendarFor(ctx, e)
	if err != nil {
		var r *Refusal
		if errors.As(err, &r) {
			c.log.Info("calendar.create refused", "title", e.Title, "calendar", e.Calendar, "person", e.Person, "why", r.Text)
		} else {
			c.log.Warn("could not read the calendars for calendar.create", "title", e.Title, "error", err)
		}
		return Created{}, err
	}
	data := map[string]any{"entity_id": cal.Entity, "summary": e.Title}
	if e.Start.IsZero() {
		data["start_date"] = e.Day.Format(time.DateOnly)
		data["end_date"] = e.DayAfter.Format(time.DateOnly)
	} else {
		data["start_date_time"] = e.Start.Format(time.RFC3339)
		data["end_date_time"] = e.End.Format(time.RFC3339)
	}
	if e.Location != "" {
		data["location"] = e.Location
	}
	if e.Description != "" {
		data["description"] = e.Description
	}
	if err := c.opts.Act(ctx, "calendar", "create_event", data); err != nil {
		c.log.Warn("Home Assistant did not enter the event", "calendar", cal.Entity, "title", e.Title, "error", err)
		return Created{}, fmt.Errorf("calendar.create_event in %s for %q: %w", cal.Entity, e.Title, err)
	}
	c.log.Info("event entered", "calendar", cal.Entity, "title", e.Title, "person", e.Person)
	return Created{Entity: cal.Entity, Name: cal.Name, Event: e}, nil
}

// checkEvent checks and completes the dates and the texts.
func (c *Calendar) checkEvent(t texts, e *NewEvent) error {
	e.Title = strings.TrimSpace(e.Title)
	e.Location = strings.TrimSpace(e.Location)
	e.Description = strings.TrimSpace(e.Description)
	switch {
	case e.Title == "":
		return &Refusal{t.createNoTitle}
	case utf8.RuneCountInString(e.Title) > maxTitle:
		return &Refusal{fmt.Sprintf(t.createTooLong, "title", maxTitle, e.Title)}
	case utf8.RuneCountInString(e.Location) > maxLocation:
		return &Refusal{fmt.Sprintf(t.createTooLong, "location", maxLocation, e.Location)}
	case utf8.RuneCountInString(e.Description) > maxDescription:
		return &Refusal{fmt.Sprintf(t.createTooLong, "description", maxDescription, cut(e.Description, 60))}
	}
	timed, whole := !e.Start.IsZero(), !e.Day.IsZero()
	switch {
	case timed && whole:
		return &Refusal{t.createBoth}
	case !timed && !whole:
		return &Refusal{t.createNoDate}
	case timed:
		if e.End.IsZero() {
			e.End = e.Start.Add(time.Hour)
		}
		if !e.End.After(e.Start) {
			return &Refusal{fmt.Sprintf(t.createEndBefore, e.End.Format("2006-01-02 15:04"), e.Start.Format("2006-01-02 15:04"))}
		}
	default:
		if e.DayAfter.IsZero() {
			e.DayAfter = e.Day.AddDate(0, 0, 1)
		}
		if !e.DayAfter.After(e.Day) {
			return &Refusal{fmt.Sprintf(t.createEndBefore, e.DayAfter.Format(time.DateOnly), e.Day.Format(time.DateOnly))}
		}
	}
	return nil
}

// calendarFor is the calendar by the one rule (docs/spec/calendar.md,
// Adding): the one named, if writable and no other person's; else the
// person's own; else the only writable one that is no other person's; else
// a refusal that asks which.
func (c *Calendar) calendarFor(ctx context.Context, e NewEvent) (Info, error) {
	t := c.texts(ctx)
	all, err := c.opts.Calendars(ctx)
	if err != nil {
		return Info{}, err
	}
	owner := func(cal Info) string {
		if c.opts.Owner == nil {
			return ""
		}
		return c.opts.Owner(ctx, cal.Name)
	}
	// What this person (or Mira's own code) may write to.
	var may []Info
	for _, cal := range all {
		if !cal.Writable {
			continue
		}
		if o := owner(cal); e.Person != "" && o != "" && o != e.Person {
			continue
		}
		may = append(may, cal)
	}
	names := func() string {
		n := make([]string, len(may))
		for i, cal := range may {
			n[i] = cal.Name
		}
		return strings.Join(n, ", ")
	}
	if len(may) == 0 {
		return Info{}, &Refusal{t.createNoCalendar}
	}
	if name := strings.TrimSpace(e.Calendar); !IsDefault(name) {
		for _, cal := range may {
			if strings.EqualFold(cal.Name, name) || strings.EqualFold(cal.Entity, name) {
				return cal, nil
			}
		}
		return Info{}, &Refusal{fmt.Sprintf(t.createNotThat, name, names())}
	}
	if e.Person != "" {
		for _, cal := range may {
			if owner(cal) == e.Person {
				return cal, nil
			}
		}
	}
	var free []Info
	for _, cal := range may {
		if owner(cal) == "" {
			free = append(free, cal)
		}
	}
	if len(free) == 1 {
		return free[0], nil
	}
	return Info{}, &Refusal{fmt.Sprintf(t.whichCalendar, names())}
}

// CreateAction is the building block as a skill's finish names it
// (action: calendar.create): the fields of the skill termin (title, start,
// end, day, day_after, location, description, entity_id, person) as
// strings.
func (c *Calendar) CreateAction(ctx context.Context, data map[string]any) error {
	str := func(k string) string { s, _ := data[k].(string); return strings.TrimSpace(s) }
	e, err := c.parseNew(ctx, str("title"), str("start"), str("end"), str("day"), str("day_after"))
	if err != nil {
		return err
	}
	e.Location, e.Description = str("location"), str("description")
	e.Calendar, e.Person = str("entity_id"), str("person")
	_, err = c.Create(ctx, e)
	return err
}

// parseNew reads the dates as the skill and the tool give them:
// "YYYY-MM-DD HH:MM" and "YYYY-MM-DD".
func (c *Calendar) parseNew(ctx context.Context, title, start, end, day, dayAfter string) (NewEvent, error) {
	t := c.texts(ctx)
	e := NewEvent{Title: title}
	read := func(field, s string, whole bool) (time.Time, error) {
		if s == "" {
			return time.Time{}, nil
		}
		v, isDay, err := parseWhen(s, c.opts.Location)
		if err != nil || isDay != whole {
			want := "2026-10-20 15:00"
			if whole {
				want = "2026-10-20"
			}
			return time.Time{}, &Refusal{fmt.Sprintf(t.createBadDate, field, s, want)}
		}
		return v, nil
	}
	var err error
	if e.Start, err = read("start", start, false); err != nil {
		return e, err
	}
	if e.End, err = read("end", end, false); err != nil {
		return e, err
	}
	if e.Day, err = read("day", day, true); err != nil {
		return e, err
	}
	if e.DayAfter, err = read("day_after", dayAfter, true); err != nil {
		return e, err
	}
	return e, nil
}

// CreateTool is calendar_create, the tool on the building block for the
// skills that name it (docs/spec/calendar.md, Who uses it).
func (c *Calendar) CreateTool(lang i18n.Lang, skills ...string) tools.InternalTool {
	t := i18n.Pick(catalog, lang)
	return tools.InternalTool{
		Name: ToolCreate, Description: t.createDesc, Skills: skills,
		Schema: fmt.Sprintf(`{"type":"object","properties":{
"title":{"type":"string","description":%q},
"start":{"type":"string","description":%q},
"end":{"type":"string","description":%q},
"day":{"type":"string","description":%q},
"day_after":{"type":"string","description":%q},
"location":{"type":"string","description":%q},
"description":{"type":"string","description":%q},
"calendar":{"type":"string","description":%q}},
"required":["title"]}`, t.createTitleDesc, t.createStartDesc, t.createEndDesc, t.createDayDesc, t.createDayAfterDesc,
			t.createLocationDesc, t.createDescriptionDesc, t.createCalendarDesc),
		Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
			var a struct {
				Title, Start, End, Day, Location, Description, Calendar string
				DayAfter                                                string `json:"day_after"`
			}
			if err := json.Unmarshal(inv.Args, &a); err != nil {
				return "", fmt.Errorf("arguments of %s: %w", ToolCreate, err)
			}
			tt := c.texts(ctx)
			e, err := c.parseNew(ctx, a.Title, a.Start, a.End, a.Day, a.DayAfter)
			if err == nil {
				e.Location, e.Description, e.Calendar, e.Person = a.Location, a.Description, a.Calendar, inv.Person
				var done Created
				if done, err = c.Create(ctx, e); err == nil {
					out := fmt.Sprintf(tt.createDone, c.describeNew(ctx, done))
					if c.past(done.Event) {
						out += " " + tt.createPast
					}
					return out, nil
				}
			}
			var r *Refusal
			if errors.As(err, &r) {
				return fmt.Sprintf(tt.createRefused, r.Text), nil
			}
			return fmt.Sprintf(tt.createFailed, err), nil
		},
	}
}

// describeNew says an entered event in words: title, day, time, calendar.
func (c *Calendar) describeNew(ctx context.Context, d Created) string {
	l := c.lang(ctx)
	e := d.Event
	if e.Start.IsZero() {
		when := i18n.FormatDate(l, e.Day)
		if last := e.DayAfter.AddDate(0, 0, -1); last.After(e.Day) {
			when += " – " + i18n.FormatDate(l, last)
		}
		return fmt.Sprintf("%s, %s, %s", e.Title, when, d.Name)
	}
	start := e.Start.In(c.opts.Location)
	return fmt.Sprintf("%s, %s %s–%s, %s", e.Title, i18n.FormatDate(l, start), start.Format("15:04"),
		e.End.In(c.opts.Location).Format("15:04"), d.Name)
}

// past: the event begins before today.
func (c *Calendar) past(e NewEvent) bool {
	now := c.opts.Clock().In(c.opts.Location)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, c.opts.Location)
	start := e.Start
	if start.IsZero() {
		start = e.Day
	}
	return start.Before(today)
}

func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
