package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sker65/mira/internal/i18n"
	"github.com/sker65/mira/internal/ical"
	"github.com/sker65/mira/internal/tools"
)

const (
	ToolSkillOpen      = "skill_open"
	ToolCalendarEvents = "calendar_events"
)

// Set is the enabled skills with what their tools need.
type Set struct {
	Lang     i18n.Lang
	Skills   []Skill
	Calendar *ical.Cache
	Clock    func() time.Time
	Location *time.Location
}

func (s *Set) clock() time.Time {
	if s.Clock != nil {
		return s.Clock()
	}
	return time.Now()
}

func (s *Set) loc() *time.Location {
	if s.Location != nil {
		return s.Location
	}
	return time.Local
}

// Get returns a skill by name.
func (s *Set) Get(name string) (Skill, bool) {
	for _, sk := range s.Skills {
		if sk.Name == name {
			return sk, true
		}
	}
	return Skill{}, false
}

// calendars maps every calendar name of the enabled skills to its URL.
func (s *Set) calendars() map[string]string {
	out := map[string]string{}
	for _, sk := range s.Skills {
		for name, url := range sk.Calendars {
			out[name] = url
		}
	}
	return out
}

// Tools returns skill_open and, when a skill has a calendar,
// calendar_events. Both only read. With no skill enabled there are none,
// and the prompt stays as it was.
func (s *Set) Tools() []tools.InternalTool {
	if len(s.Skills) == 0 {
		return nil
	}
	t := i18n.Pick(catalog, s.Lang)
	var list strings.Builder
	for _, sk := range s.Skills {
		fmt.Fprintf(&list, "\n- %s: %s", sk.Name, sk.Description)
	}
	out := []tools.InternalTool{{
		Name:        ToolSkillOpen,
		Description: t.openDesc + list.String(),
		Schema:      fmt.Sprintf(`{"type":"object","properties":{"name":{"type":"string","description":%q}},"required":["name"]}`, t.openName),
		ReadOnly:    true,
		Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
			var a struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(inv.Args, &a); err != nil {
				return "", err
			}
			sk, ok := s.Get(strings.TrimSpace(a.Name))
			if !ok {
				return "", fmt.Errorf(t.noSkill, a.Name)
			}
			return sk.Body, nil
		},
	}}
	cals := s.calendars()
	if len(cals) == 0 {
		return out
	}
	names := make([]string, 0, len(cals))
	for n := range cals {
		names = append(names, n)
	}
	sort.Strings(names)
	out = append(out, tools.InternalTool{
		Name:        ToolCalendarEvents,
		Description: fmt.Sprintf(t.calDesc, strings.Join(names, ", ")),
		Schema: fmt.Sprintf(`{"type":"object","properties":{"calendar":{"type":"string","enum":%s},"days":{"type":"integer","description":%q},"day":{"type":"string","enum":["today","tomorrow"],"description":%q},"match":{"type":"string","description":%q}},"required":["calendar"]}`,
			mustJSON(names), t.calDays, t.calDay, t.calMatch),
		ReadOnly: true,
		Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
			var a struct {
				Calendar string `json:"calendar"`
				Days     int    `json:"days"`
				Day      string `json:"day"`
				Match    string `json:"match"`
			}
			if err := json.Unmarshal(inv.Args, &a); err != nil {
				return "", err
			}
			url, ok := cals[a.Calendar]
			if !ok {
				return "", fmt.Errorf(t.noCalendar, a.Calendar)
			}
			switch a.Day {
			case "today":
				return s.eventsOn(ctx, t, url, 0, a.Match)
			case "tomorrow":
				return s.eventsOn(ctx, t, url, 1, a.Match)
			}
			return s.events(ctx, t, url, a.Days, a.Match)
		},
	})
	return out
}

// events lists the events from today on, for days days.
func (s *Set) events(ctx context.Context, t texts, url string, days int, match string) (string, error) {
	if days <= 0 {
		days = 14
	}
	if days > 366 {
		days = 366
	}
	if s.Calendar == nil {
		return "", errors.New("no calendar cache")
	}
	data, err := s.Calendar.Get(ctx, url)
	if err != nil {
		return "", err
	}
	now := s.clock().In(s.loc())
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.loc())
	// Read further than asked: when the window holds fewer than
	// minEvents matches, the next ones follow. A model asking for 14 days
	// about a fortnightly bin otherwise sees only today and makes up the
	// date after.
	windowEnd := from.AddDate(0, 0, days)
	events, err := ical.Parse(data, s.loc(), from, from.AddDate(0, 0, max(days, lookAheadDays)))
	if err != nil {
		return "", err
	}
	words := matchWords(match)
	var b strings.Builder
	fmt.Fprintf(&b, t.calToday, i18n.FormatDate(s.Lang, now))
	n := 0
	for _, e := range events {
		if len(words) > 0 && !containsAny(strings.ToLower(e.Summary), words) {
			continue
		}
		if !e.Start.Before(windowEnd) && n >= minEvents {
			break
		}
		start := e.Start.In(s.loc())
		when := i18n.FormatDate(s.Lang, start)
		if !e.AllDay {
			when = i18n.FormatTime(s.Lang, start)
		}
		// Models miscount days ("today" read as "tomorrow"); the tool says
		// it outright.
		day := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, s.loc())
		if d := int(day.Sub(from).Hours()/24 + 0.5); d >= 0 && d < len(t.relDays) {
			when = t.relDays[d] + ", " + when
		}
		fmt.Fprintf(&b, "\n- %s: %s", when, e.Summary)
		n++
	}
	if n == 0 {
		fmt.Fprintf(&b, "\n"+t.calNone, max(days, lookAheadDays))
	}
	return b.String(), nil
}

// eventsOn lists only the events of one day, offset days from today, and
// says so when there are none. "Is anything collected tomorrow?" is then
// answered by the code: given today's collection and the next one, the
// model announced today's as tomorrow's.
func (s *Set) eventsOn(ctx context.Context, t texts, url string, offset int, match string) (string, error) {
	if s.Calendar == nil {
		return "", errors.New("no calendar cache")
	}
	data, err := s.Calendar.Get(ctx, url)
	if err != nil {
		return "", err
	}
	now := s.clock().In(s.loc())
	from := time.Date(now.Year(), now.Month(), now.Day()+offset, 0, 0, 0, 0, s.loc())
	events, err := ical.Parse(data, s.loc(), from, from.AddDate(0, 0, 1))
	if err != nil {
		return "", err
	}
	words := matchWords(match)
	label := t.relDays[offset] + ", " + i18n.FormatDate(s.Lang, from)
	var b strings.Builder
	fmt.Fprintf(&b, t.calToday, i18n.FormatDate(s.Lang, now))
	n := 0
	for _, e := range events {
		start := e.Start.In(s.loc())
		if start.Before(from) || !start.Before(from.AddDate(0, 0, 1)) {
			continue // a multi-day event begun earlier, or the parser's slack
		}
		if len(words) > 0 && !containsAny(strings.ToLower(e.Summary), words) {
			continue
		}
		when := label
		if !e.AllDay {
			when = t.relDays[offset] + ", " + i18n.FormatTime(s.Lang, start)
		}
		fmt.Fprintf(&b, "\n- %s: %s", when, e.Summary)
		n++
	}
	if n == 0 {
		fmt.Fprintf(&b, "\n"+t.calNoneOn, label)
	}
	return b.String(), nil
}

func matchWords(match string) []string {
	var words []string
	for _, w := range strings.Split(strings.ToLower(match), ",") {
		if w = strings.TrimSpace(w); w != "" {
			words = append(words, w)
		}
	}
	return words
}

const (
	minEvents     = 2
	lookAheadDays = 90
)

func containsAny(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

func mustJSON(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

type texts struct {
	openDesc, openName, noSkill                               string
	calDesc, calDays, calMatch, noCalendar, calToday, calNone string
	calDay, calNoneOn                                         string
	taskIntro, taskRules                                      string
	relDays                                                   []string
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		openDesc:   "Öffnet einen Skill: liefert seine Anleitung, die du dann befolgst. Öffne den passenden Skill, bevor du eine Frage aus seinem Bereich beantwortest. Verfügbare Skills:",
		openName:   "Name des Skills.",
		noSkill:    "keinen Skill %q gibt es nicht",
		calDesc:    "Liest Termine aus einem Kalender eines Skills, ab heute. Kalender: %s.",
		calDays:    "Wie viele Tage ab heute, Standard 14.",
		calMatch:   "Optional: nur Termine, deren Name einen dieser Wortteile enthält, mit Komma getrennt.",
		noCalendar: "keinen Kalender %q gibt es nicht",
		calToday:   "Heute ist %s.",
		calNone:    "Keine passenden Termine in den nächsten %d Tagen.",
		calDay:     "Optional: nur die Termine dieses einen Tages, today oder tomorrow; days zählt dann nicht.",
		calNoneOn:  "%s: keine passenden Termine.",
		relDays:    []string{"heute", "morgen", "übermorgen"},
		taskIntro:  "Geplante Aufgabe des Skills %q. Anleitung des Skills:\n\n",
		taskRules: `

Antworte am Ende nur mit einem JSON-Objekt:
- importance: 0 = nichts zu melden, 1 = nebenbei erwähnen, 2 = wichtig
- spoken: ein bis zwei kurze Sätze zum Vorlesen, ohne Markdown; leer bei importance 0
- details: optional die Langfassung
- expires_in_hours: wie lange die Meldung relevant bleibt`,
	},
	i18n.EN: {
		openDesc:   "Opens a skill: returns its instructions, which you then follow. Open the matching skill before answering a question in its area. Available skills:",
		openName:   "Name of the skill.",
		noSkill:    "there is no skill %q",
		calDesc:    "Reads events from a skill's calendar, from today on. Calendars: %s.",
		calDays:    "How many days from today, default 14.",
		calMatch:   "Optional: only events whose name contains one of these word parts, comma separated.",
		noCalendar: "there is no calendar %q",
		calToday:   "Today is %s.",
		calNone:    "No matching events in the next %d days.",
		calDay:     "Optional: only the events of this one day, today or tomorrow; days does not count then.",
		calNoneOn:  "%s: no matching events.",
		relDays:    []string{"today", "tomorrow", "the day after tomorrow"},
		taskIntro:  "Scheduled task of the skill %q. The skill's instructions:\n\n",
		taskRules: `

At the end, answer only with a JSON object:
- importance: 0 = nothing to report, 1 = mention in passing, 2 = important
- spoken: one or two short sentences to be read aloud, no Markdown; empty when importance is 0
- details: optionally the long form
- expires_in_hours: how long the notice stays relevant`,
	},
}
