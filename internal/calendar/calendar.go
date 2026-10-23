// Package calendar lets Mira read and change Home Assistant's calendars
// (docs/spec/kalender.md): the tool calendar_events, which shows events the
// way Mira decides (package timeline), the checks for the skills termin and
// termin_absagen, and the building block calendar.delete, for which Home
// Assistant has no action.
package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/threads"
	"github.com/miraassist/mira/internal/timeline"
	"github.com/miraassist/mira/internal/tools"
)

const ToolEvents = "calendar_events"

// Info is a calendar Mira may see: exposed to Assist.
type Info struct {
	Entity, Name string
	// Writable calendars take new events; Deletable ones let events go.
	Writable, Deletable bool
}

// Event is one event as Home Assistant's REST API returns it.
type Event struct {
	Start, End   time.Time
	AllDay       bool
	Summary      string
	Description  string
	Location     string
	UID          string
	RecurrenceID string
	Recurring    bool
}

type Options struct {
	// HAURL and Token reach Home Assistant's REST API.
	HAURL, Token string
	HTTP         *http.Client
	// Calendars lists the exposed calendars.
	Calendars func(ctx context.Context) ([]Info, error)
	// Delete removes an event (WebSocket calendar/event/delete); an empty
	// recurrenceID removes a whole series.
	Delete   func(ctx context.Context, entity, uid, recurrenceID string) error
	Lang     i18n.Lang
	Location *time.Location
	Clock    func() time.Time
	Logger   *slog.Logger
}

type Calendar struct {
	opts Options
	log  *slog.Logger

	mu     sync.Mutex
	next   int
	labels map[string]ref
	order  []string
}

// ref is what a label stands for; the model sees only the label.
type ref struct {
	Entity, Calendar string
	Event            Event
}

// maxLabels bounds the labels kept; the oldest go first.
const maxLabels = 500

func New(opts Options) *Calendar {
	if opts.HTTP == nil {
		opts.HTTP = &http.Client{Timeout: 15 * time.Second}
	}
	if opts.Location == nil {
		opts.Location = time.Local
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Calendar{opts: opts, log: opts.Logger.With("component", "calendar"), labels: map[string]ref{}}
}

func (c *Calendar) lang(ctx context.Context) i18n.Lang {
	if l, ok := i18n.FromContext(ctx); ok {
		return l
	}
	return c.opts.Lang
}

func (c *Calendar) texts(ctx context.Context) texts { return i18n.Pick(catalog, c.lang(ctx)) }

// Query selects events.
type Query struct {
	From, To time.Time
	// Calendar is a name or entity id; empty means all.
	Calendar string
	// Text must appear in the title (case does not matter).
	Text    string
	Details bool
	Limit   int
}

// Render reads the events and writes them for the model, each with a label
// the other building blocks understand.
func (c *Calendar) Render(ctx context.Context, q Query) (string, error) {
	cals, err := c.pick(ctx, q.Calendar, false)
	if err != nil {
		return "", err
	}
	var entries []timeline.Entry
	for _, cal := range cals {
		events, err := c.Events(ctx, cal.Entity, q.From, q.To)
		if err != nil {
			return "", err
		}
		for _, e := range events {
			if q.Text != "" && !strings.Contains(strings.ToLower(e.Summary), strings.ToLower(q.Text)) {
				continue
			}
			entries = append(entries, timeline.Entry{Start: e.Start, End: e.End, AllDay: e.AllDay, Title: e.Summary,
				Place: e.Location, Details: e.Description, Source: cal.Name,
				Ref: ref{Entity: cal.Entity, Calendar: cal.Name, Event: e}})
		}
	}
	return timeline.Render(entries, timeline.Options{
		Lang: c.lang(ctx), Location: c.opts.Location, Now: c.opts.Clock(), Limit: q.Limit, Details: q.Details,
		Label: func(e timeline.Entry) string { return c.label(e.Ref.(ref)) },
	}), nil
}

// label hands out the next short label for an event.
func (c *Calendar) label(r ref) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	l := fmt.Sprintf("t%d", c.next)
	c.labels[l] = r
	c.order = append(c.order, l)
	if len(c.order) > maxLabels {
		delete(c.labels, c.order[0])
		c.order = c.order[1:]
	}
	return l
}

func (c *Calendar) lookup(label string) (ref, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.labels[strings.TrimSpace(label)]
	return r, ok
}

// pick returns the calendars a name means: one by name or id, else all;
// writable only when asked.
func (c *Calendar) pick(ctx context.Context, name string, writable bool) ([]Info, error) {
	all, err := c.opts.Calendars(ctx)
	if err != nil {
		return nil, err
	}
	var out []Info
	for _, cal := range all {
		if writable && !cal.Writable {
			continue
		}
		if name == "" || strings.EqualFold(cal.Entity, name) || strings.EqualFold(cal.Name, name) {
			out = append(out, cal)
		}
	}
	if len(out) == 0 {
		var names []string
		for _, cal := range all {
			if !writable || cal.Writable {
				names = append(names, cal.Name)
			}
		}
		t := c.texts(ctx)
		if len(names) == 0 {
			return nil, errors.New(t.noCalendars)
		}
		return nil, fmt.Errorf(t.unknownCalendar, name, strings.Join(names, ", "))
	}
	return out, nil
}

// Events reads a calendar's events in a window over the REST API, which
// gives their ids.
func (c *Calendar) Events(ctx context.Context, entity string, from, to time.Time) ([]Event, error) {
	u := fmt.Sprintf("%s/api/calendars/%s?start=%s&end=%s", strings.TrimRight(c.opts.HAURL, "/"), url.PathEscape(entity),
		url.QueryEscape(from.Format(time.RFC3339)), url.QueryEscape(to.Format(time.RFC3339)))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.opts.Token)
	resp, err := c.opts.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("calendar %s: HTTP %d", entity, resp.StatusCode)
	}
	var raw []struct {
		Start        when   `json:"start"`
		End          when   `json:"end"`
		Summary      string `json:"summary"`
		Description  string `json:"description"`
		Location     string `json:"location"`
		UID          string `json:"uid"`
		RecurrenceID string `json:"recurrence_id"`
		RRule        string `json:"rrule"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]Event, 0, len(raw))
	for _, r := range raw {
		start, allDay, err := r.Start.time(c.opts.Location)
		if err != nil {
			continue
		}
		end, _, _ := r.End.time(c.opts.Location)
		out = append(out, Event{Start: start, End: end, AllDay: allDay, Summary: r.Summary, Description: r.Description,
			Location: r.Location, UID: r.UID, RecurrenceID: r.RecurrenceID, Recurring: r.RRule != "" || r.RecurrenceID != ""})
	}
	return out, nil
}

// when is HA's {"dateTime": …} or {"date": …}.
type when struct {
	DateTime string `json:"dateTime"`
	Date     string `json:"date"`
}

func (w when) time(loc *time.Location) (time.Time, bool, error) {
	if w.DateTime != "" {
		t, err := time.Parse(time.RFC3339, w.DateTime)
		return t, false, err
	}
	t, err := time.ParseInLocation("2006-01-02", w.Date, loc)
	return t, true, err
}

// Tool is calendar_events. It only reads.
func (c *Calendar) Tool(lang i18n.Lang) tools.InternalTool {
	t := i18n.Pick(catalog, lang)
	return tools.InternalTool{
		Name: ToolEvents, Description: t.toolDesc, ReadOnly: true,
		Schema: fmt.Sprintf(`{"type":"object","properties":{"from":{"type":"string","description":%q},"to":{"type":"string","description":%q},"calendar":{"type":"string","description":%q},"query":{"type":"string","description":%q},"details":{"type":"boolean","description":%q}}}`,
			t.fromDesc, t.toDesc, t.calendarDesc, t.queryDesc, t.detailsDesc),
		Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
			var a struct {
				From, To, Calendar, Query string
				Details                   bool
			}
			if err := json.Unmarshal(inv.Args, &a); err != nil {
				return "", err
			}
			from, to, err := c.window(a.From, a.To)
			if err != nil {
				return "", err
			}
			return c.Render(ctx, Query{From: from, To: to, Calendar: a.Calendar, Text: a.Query, Details: a.Details})
		},
	}
}

// window reads from and to: a day (2026-10-20) or a time (2026-10-20
// 15:00); default from the start of today, for 7 days. A day as "to" counts
// whole.
func (c *Calendar) window(fromS, toS string) (time.Time, time.Time, error) {
	now := c.opts.Clock().In(c.opts.Location)
	y, m, d := now.Date()
	from := time.Date(y, m, d, 0, 0, 0, 0, c.opts.Location)
	if fromS != "" {
		t, _, err := parseWhen(fromS, c.opts.Location)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		from = t
	}
	to := from.AddDate(0, 0, 7)
	if toS != "" {
		t, day, err := parseWhen(toS, c.opts.Location)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		if day {
			t = t.AddDate(0, 0, 1)
		}
		to = t
	}
	if !to.After(from) {
		return time.Time{}, time.Time{}, errors.New("to must be after from")
	}
	return from, to, nil
}

func parseWhen(s string, loc *time.Location) (time.Time, bool, error) {
	s = strings.TrimSpace(strings.Replace(s, "T", " ", 1))
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, false, nil
		}
	}
	t, err := time.ParseInLocation("2006-01-02", s, loc)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("%q is no date like 2026-10-20 or 2026-10-20 15:00", s)
	}
	return t, true, nil
}

// CheckWritable is the check "calendar_writable": a writable calendar by
// name or id, completed to its entity id.
func (c *Calendar) CheckWritable(ctx context.Context, value string) (string, error) {
	value = strings.TrimSpace(value)
	if IsDefault(value) {
		value = "" // the only writable one, or ask which
	}
	cals, err := c.pick(ctx, value, true)
	if err != nil {
		return "", err
	}
	if len(cals) != 1 {
		names := make([]string, len(cals))
		for i, cal := range cals {
			names[i] = cal.Name
		}
		return "", fmt.Errorf(c.texts(ctx).whichCalendar, strings.Join(names, ", "))
	}
	return cals[0].Entity, nil
}

// CheckEvent is the check "calendar_event": a label calendar_events gave,
// completed with what it stands for.
func (c *Calendar) CheckEvent(ctx context.Context, value string) (string, error) {
	label := strings.Fields(value + " ")[0]
	r, ok := c.lookup(label)
	if !ok {
		return "", errors.New(c.texts(ctx).unknownEvent)
	}
	return label + " (" + c.describe(ctx, r) + ")", nil
}

func (c *Calendar) describe(ctx context.Context, r ref) string {
	l := c.lang(ctx)
	start := r.Event.Start.In(c.opts.Location)
	when := i18n.FormatDate(l, start)
	if !r.Event.AllDay {
		when += " " + start.Format("15:04")
	}
	return fmt.Sprintf("%s, %s, %s", r.Event.Summary, when, r.Calendar)
}

// Delete is the building block "calendar.delete": the field "event" holds
// a label; the user's yes is asked first. A series loses only this event,
// unless the field "all" says so.
func (c *Calendar) Delete() threads.Finisher { return deleteFinish{c} }

type deleteFinish struct{ c *Calendar }

func (deleteFinish) Async() bool { return false }

func (f deleteFinish) Work(ctx context.Context, th store.Thread) (threads.Result, error) {
	t := f.c.texts(ctx)
	label := strings.Fields(th.Fields["event"] + " ")[0]
	r, ok := f.c.lookup(label)
	if !ok {
		return threads.Result{Cancel: t.unknownEvent}, nil
	}
	whole := r.Event.Recurring && yes(th.Fields["all"])
	content, _ := json.Marshal(struct {
		Ref   ref
		Whole bool
	}{r, whole})
	q := t.confirmDelete
	if whole {
		q = t.confirmDeleteAll
	}
	return threads.Result{Proposal: fmt.Sprintf(q, f.c.describe(ctx, r)), Content: content}, nil
}

func (f deleteFinish) Commit(ctx context.Context, th store.Thread) (string, error) {
	var c struct {
		Ref   ref
		Whole bool
	}
	if err := json.Unmarshal(th.Content, &c); err != nil || c.Ref.Event.UID == "" {
		return "", errors.New("the thread holds no event")
	}
	rec := c.Ref.Event.RecurrenceID
	if c.Whole {
		rec = ""
	}
	if err := f.c.opts.Delete(ctx, c.Ref.Entity, c.Ref.Event.UID, rec); err != nil {
		return "", err
	}
	f.c.log.Info("event deleted", "calendar", c.Ref.Entity, "series", c.Whole)
	return fmt.Sprintf(f.c.texts(ctx).deleted, f.c.describe(ctx, c.Ref)), nil
}

// IsDefault reports the placeholder a skill uses when no calendar or list
// was named and none is known as the person's default.
func IsDefault(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "standard", "default":
		return true
	}
	return false
}

func yes(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "ja", "yes", "true", "alle", "all":
		return true
	}
	return false
}

type texts struct {
	toolDesc, fromDesc, toDesc, calendarDesc, queryDesc, detailsDesc string
	noCalendars, unknownCalendar, whichCalendar, unknownEvent        string
	confirmDelete, confirmDeleteAll, deleted                         string
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		toolDesc:         "Liest Termine aus den Kalendern des Hauses, knapp, nach Tag, jeder mit einer Kennung wie t3 (zum Absagen). Nenne nur Termine, die dieses Werkzeug geliefert hat.",
		fromDesc:         "Beginn: Tag (2026-10-20) oder Zeit (2026-10-20 15:00); Standard heute",
		toDesc:           "Ende: Tag (zählt ganz) oder Zeit; Standard eine Woche nach Beginn",
		calendarDesc:     "Ein Kalender (Name); leer für alle",
		queryDesc:        "Nur Termine, deren Titel das enthält, z. B. Arzt",
		detailsDesc:      "Auch Beschreibungen (nur wenn danach gefragt wird)",
		noCalendars:      "Es gibt keinen Kalender, den ich sehen darf. In Home Assistant müssen Kalender für Assist freigegeben sein.",
		unknownCalendar:  "Kalender %q gibt es nicht. Möglich: %s",
		whichCalendar:    "Welcher Kalender? %s",
		unknownEvent:     "Diese Kennung kenne ich nicht; suche den Termin zuerst mit calendar_events.",
		confirmDelete:    "Den Termin %s absagen?",
		confirmDeleteAll: "Den Serientermin %s absagen, alle Termine der Serie?",
		deleted:          "Abgesagt: %s. Sag es dem Nutzer kurz.",
	},
	i18n.EN: {
		toolDesc:         "Reads events from the household's calendars, briefly, by day, each with a label like t3 (to cancel it). Only name events this tool returned.",
		fromDesc:         "Start: a day (2026-10-20) or a time (2026-10-20 15:00); default today",
		toDesc:           "End: a day (counts whole) or a time; default a week after the start",
		calendarDesc:     "One calendar (name); empty for all",
		queryDesc:        "Only events whose title contains this, e.g. dentist",
		detailsDesc:      "Descriptions too (only when asked for)",
		noCalendars:      "There is no calendar I may see. In Home Assistant, calendars must be exposed to Assist.",
		unknownCalendar:  "There is no calendar %q. Possible: %s",
		whichCalendar:    "Which calendar? %s",
		unknownEvent:     "I do not know this label; look the event up with calendar_events first.",
		confirmDelete:    "Cancel the event %s?",
		confirmDeleteAll: "Cancel the recurring event %s, every event of the series?",
		deleted:          "Cancelled: %s. Tell the user briefly.",
	},
}

// DeleteCreated is the building block "calendar.delete_created", the undo
// of the skill termin (docs/spec/kalender.md): Home Assistant returns no ID
// for an event it added, so it finds the event in the calendar (entity_id)
// by its title (summary) and start (start_date_time "2006-01-02 15:04", or
// start_date for a whole day) and deletes it. Exactly one match is deleted;
// with none or several nothing is.
func (c *Calendar) DeleteCreated(ctx context.Context, data map[string]any) error {
	str := func(k string) string { s, _ := data[k].(string); return strings.TrimSpace(s) }
	entity, summary := str("entity_id"), str("summary")
	if entity == "" || summary == "" {
		return errors.New("calendar.delete_created needs entity_id and summary")
	}
	var start time.Time
	allDay := false
	if s := str("start_date_time"); s != "" {
		t, err := parseLocal(s, c.opts.Location)
		if err != nil {
			return fmt.Errorf("start_date_time %q: %w", s, err)
		}
		start = t
	} else if s := str("start_date"); s != "" {
		t, err := time.ParseInLocation("2006-01-02", s, c.opts.Location)
		if err != nil {
			return fmt.Errorf("start_date %q: %w", s, err)
		}
		start, allDay = t, true
	} else {
		return errors.New("calendar.delete_created needs start_date_time or start_date")
	}
	events, err := c.Events(ctx, entity, start.Add(-time.Minute), start.Add(24*time.Hour))
	if err != nil {
		return err
	}
	var match []Event
	for _, ev := range events {
		if !strings.EqualFold(strings.TrimSpace(ev.Summary), summary) || ev.AllDay != allDay {
			continue
		}
		same := ev.Start.Equal(start)
		if allDay {
			same = ev.Start.In(c.opts.Location).Format("2006-01-02") == start.Format("2006-01-02")
		}
		if same {
			match = append(match, ev)
		}
	}
	if len(match) != 1 {
		return fmt.Errorf("found %d events %q at %s in %s, so nothing was taken back", len(match), summary,
			start.Format("2006-01-02 15:04"), entity)
	}
	if err := c.opts.Delete(ctx, entity, match[0].UID, match[0].RecurrenceID); err != nil {
		return err
	}
	c.log.Info("added event taken back", "calendar", entity)
	return nil
}

// parseLocal reads "2006-01-02 15:04" (with or without seconds, or with a T)
// in the given time zone.
func parseLocal(s string, loc *time.Location) (time.Time, error) {
	s = strings.Replace(s, "T", " ", 1)
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, errors.New("not a time like 2026-10-25 16:00")
}
