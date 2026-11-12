package inbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/tools"
)

// ToolEnter enters findings of the inbox thread into a calendar.
const ToolEnter = "inbox_enter"

// The fields of an inbox thread.
const (
	FieldDoc      = "doc" // the key of the document or mail
	FieldTitle    = "title"
	FieldRef      = "ref"
	FieldFindings = "findings"
)

// Calendar is a calendar the tool may write to.
type Calendar struct {
	Entity, Name string
	Writable     bool
	// Of is the person whose calendar it is by convention; "" for one of
	// the house.
	Of string
}

// Thread is the turn's inbox thread as the tool needs it.
type Thread struct {
	ID, Person string
	Fields     map[string]string
}

// ToolOptions are what inbox_enter needs besides the service.
type ToolOptions struct {
	// Thread is the turn's open inbox thread; false for none.
	Thread func(ctx context.Context, inv tools.Invocation) (Thread, bool)
	// Calendars are the calendars, with whose they are.
	Calendars func(ctx context.Context) ([]Calendar, error)
	Create    func(ctx context.Context, entity string, e Entry) error
	// Tick ticks a step of the turn's plan with a note.
	Tick func(ctx context.Context, inv tools.Invocation, step int, note string) error
}

// Tools is inbox_enter, a tool of the skill postfach.
func (s *Service) Tools(o ToolOptions) []tools.InternalTool {
	t := s.t
	schema := fmt.Sprintf(`{"type":"object","properties":{
"steps":{"type":"array","items":{"type":"integer"},"description":%q},
"calendar":{"type":"string","description":%q},
"changes":{"type":"array","description":%q,"items":{"type":"object","properties":{
"step":{"type":"integer"},"title":{"type":"string"},"day":{"type":"string"},"start":{"type":"string"},"end":{"type":"string"},"location":{"type":"string"}},
"required":["step"]}}},
"required":["steps"]}`, t.argSteps, t.argCalendar, t.argChanges)
	return []tools.InternalTool{{Name: ToolEnter, Description: t.toolDesc, Skill: Target, Schema: schema,
		Run: func(ctx context.Context, inv tools.Invocation) (string, error) { return s.enter(ctx, inv, o) }}}
}

type change struct {
	Step     int     `json:"step"`
	Title    *string `json:"title"`
	Day      *string `json:"day"`
	Start    *string `json:"start"`
	End      *string `json:"end"`
	Location *string `json:"location"`
}

func (s *Service) enter(ctx context.Context, inv tools.Invocation, o ToolOptions) (string, error) {
	t := s.t
	var args struct {
		Steps    []int    `json:"steps"`
		Calendar string   `json:"calendar"`
		Changes  []change `json:"changes"`
	}
	if err := json.Unmarshal(inv.Args, &args); err != nil {
		return "", fmt.Errorf("arguments of %s: %w", ToolEnter, err)
	}
	s.log.Info("inbox_enter called", "steps", args.Steps, "calendar", args.Calendar, "changes", len(args.Changes), "person", inv.Person)
	th, ok := o.Thread(ctx, inv)
	if !ok {
		s.log.Info("inbox_enter without an inbox thread", "session", inv.SessionKey)
		return t.noThread, nil
	}
	var findings []Finding
	if err := json.Unmarshal([]byte(th.Fields[FieldFindings]), &findings); err != nil {
		return "", fmt.Errorf("the inbox thread %s holds no readable findings: %w", th.ID, err)
	}
	cals, err := o.Calendars(ctx)
	if err != nil {
		s.log.Warn("could not read the calendars for inbox_enter", "thread", th.ID, "error", err)
		return fmt.Sprintf(t.calendarsFailed, err), nil
	}
	cal, refusal := s.pick(cals, th.Person, args.Calendar)
	if refusal != "" {
		s.log.Info("inbox_enter refused", "thread", th.ID, "person", th.Person, "calendar", args.Calendar, "why", refusal)
		return refusal, nil
	}
	base := Doc{Key: th.Fields[FieldDoc], Title: th.Fields[FieldTitle], Ref: th.Fields[FieldRef]}
	changes := map[int]change{}
	for _, c := range args.Changes {
		changes[c.Step] = c
	}
	var out []string
	for _, step := range args.Steps {
		if step < 1 || step > len(findings) {
			s.log.Info("inbox_enter names a step the plan does not have", "thread", th.ID, "step", step, "steps", len(findings))
			out = append(out, fmt.Sprintf(t.noStep, step))
			continue
		}
		f := findings[step-1]
		if c, ok := changes[step]; ok {
			changed, err := s.apply(f, c)
			if err != nil {
				s.log.Info("a change of an inbox step does not fit", "thread", th.ID, "step", step, "error", err)
				out = append(out, fmt.Sprintf(t.badChange, step, err))
				continue
			}
			f = changed
		}
		if err := o.Create(ctx, cal.Entity, s.entry(f, base)); err != nil {
			s.log.Warn("could not enter a finding of the inbox", "thread", th.ID, "step", step, "calendar", cal.Entity, "error", err)
			out = append(out, fmt.Sprintf(t.failed, step, err))
			continue
		}
		s.log.Info("inbox step entered", "thread", th.ID, "step", step, "calendar", cal.Entity, "title", f.Title, "day", f.Day, "start", f.Start)
		out = append(out, fmt.Sprintf(t.entered, step, s.Describe(f)))
		if err := o.Tick(ctx, inv, step, t.note); err != nil {
			s.log.Warn("could not tick an entered step of the inbox plan", "thread", th.ID, "step", step, "error", err)
		}
	}
	out = append(out, fmt.Sprintf(t.enteredSummary, cal.Name))
	return strings.Join(out, "\n"), nil
}

// pick is the calendar to write to: the one named, if it is the person's or
// the house's and writable; else the person's own. A refusal is the tool's
// answer.
func (s *Service) pick(cals []Calendar, person, name string) (Calendar, string) {
	t := s.t
	if name = strings.TrimSpace(name); name != "" {
		var names []string
		for _, c := range cals {
			if !c.Writable {
				continue
			}
			names = append(names, c.Name)
			if strings.EqualFold(c.Name, name) || strings.EqualFold(c.Entity, name) {
				if c.Of != "" && c.Of != person {
					return Calendar{}, fmt.Sprintf(t.foreignCalendar, c.Name)
				}
				return c, ""
			}
		}
		return Calendar{}, fmt.Sprintf(t.unknownCalendar, name, strings.Join(names, ", "))
	}
	for _, c := range cals {
		if c.Of == person && person != "" && c.Writable {
			return c, ""
		}
	}
	return Calendar{}, t.noCalendar
}

// apply takes the user's change of a finding, under the same checks as the
// reading.
func (s *Service) apply(f Finding, c change) (Finding, error) {
	if c.Title != nil {
		f.Title = *c.Title
	}
	if c.Day != nil {
		f.Day = *c.Day
	}
	if c.Start != nil {
		f.Start = *c.Start
		if c.End == nil {
			f.End = ""
		}
	}
	if c.End != nil {
		f.End = *c.End
	}
	if c.Location != nil {
		f.Location = *c.Location
	}
	day, _ := time.ParseInLocation("2006-01-02", f.Day, s.o.Location)
	checked, err := s.checkOne(f, midnight(day))
	if err != nil {
		return f, err
	}
	if c.Start != nil && strings.TrimSpace(*c.Start) != "" && checked.Start == "" {
		return f, errors.New("start " + *c.Start)
	}
	checked.Past = f.Past
	return checked, nil
}

// entry is a finding as a calendar entry: an hour without an end.
func (s *Service) entry(f Finding, doc Doc) Entry {
	day, _ := time.ParseInLocation("2006-01-02", f.Day, s.o.Location)
	e := Entry{Summary: f.Title, Location: f.Location, Day: day}
	if f.For != "" {
		e.Summary += " (" + f.For + ")"
	}
	if doc.Title != "" {
		e.Description = fmt.Sprintf(s.t.from, doc.Ref, doc.Title)
	}
	if start, ok := at(day, f.Start); ok {
		e.Start = start
		if end, ok := at(day, f.End); ok && end.After(start) {
			e.End = end
		} else {
			e.End = start.Add(time.Hour)
		}
	}
	return e
}
