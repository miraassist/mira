package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/ui"
)

// schedules is the page "Schedules" (docs/spec/schedules.md): the skills'
// schedules and Mira's own, from the running scheduler; a change goes into
// mira.yaml and to the scheduler at once.
type schedules struct{ a *app }

func (a *app) schedules() ui.Schedules {
	if a.runner == nil {
		return nil
	}
	return schedules{a}
}

func (s schedules) List(ctx context.Context) ([]ui.ScheduleItem, error) {
	a := s.a
	r := a.runner
	var out []ui.ScheduleItem
	var names []string
	if a.skills != nil {
		for _, sk := range a.skills.Skills {
			if len(sk.Schedules) > 0 {
				names = append(names, sk.Name)
			}
		}
	}
	sort.Strings(names)
	for _, name := range names {
		for i, sc := range r.Schedules(name) {
			it := scheduleItem(name, sc)
			it.Enabled = !sc.Off
			if next := r.NextRun(sc); !next.IsZero() {
				it.Next = next.Format("2006-01-02 15:04")
			}
			if sc.Name != "" {
				it.State = r.State(name, sc.Name)
			}
			k := skills.LastRunKey(name, sc, i, "")
			last, err := s.lastRuns(ctx, k.Skill, k.Schedule)
			if err != nil {
				return nil, err
			}
			it.Last = last
			out = append(out, it)
		}
	}
	// The skills that are off: their schedules as the skill has them.
	for _, info := range skills.Available(a.cfg.Skills.Dirs, a.cfg.Skills.Enabled) {
		dir := skills.Find(a.cfg.Skills.Dirs, info.Name)
		f, err := skills.ReadFile(dir)
		if err != nil {
			// docs/spec/fallbacks.md, row 29.
			a.log.Warn("could not read a skill that is off; its schedules are missing from the page", "skill", info.Name, "error", err)
			continue
		}
		for _, sc := range f.ScheduleDecls() {
			it := scheduleItem(info.Name, sc)
			it.SkillOff, it.Editable = true, false
			out = append(out, it)
		}
	}
	for _, sj := range r.System {
		it := ui.ScheduleItem{Skill: skills.SystemSkill, Name: sj.Name, Description: sj.Description, Own: true,
			Cron: sj.Cron, Enabled: true, State: r.State(skills.SystemSkill, sj.Name)}
		it.Time, it.Days, it.Editable = timeOfCron(sj.Cron)
		if next := r.NextSystemRun(sj); !next.IsZero() {
			it.Next = next.Format("2006-01-02 15:04")
		}
		last, err := s.lastRuns(ctx, skills.SystemSkill, sj.Name)
		if err != nil {
			return nil, err
		}
		it.Last = last
		out = append(out, it)
	}
	return out, nil
}

func scheduleItem(skill string, sc skills.Schedule) ui.ScheduleItem {
	it := ui.ScheduleItem{Skill: skill, Name: sc.Name, Description: sc.Description, PerPerson: sc.PerPerson, Cron: sc.Cron}
	it.Time, it.Days, it.Editable = timeOfCron(sc.Cron)
	it.Editable = it.Editable && sc.Name != ""
	return it
}

// lastRuns are a schedule's last run, and per person its persons'.
func (s schedules) lastRuns(ctx context.Context, skill, schedule string) ([]ui.LastRun, error) {
	runs, err := s.a.store.LastRuns(ctx, skill, schedule)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(runs))
	for k := range runs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	persons := s.a.persons()
	var out []ui.LastRun
	for _, k := range keys {
		r := runs[k]
		l := ui.LastRun{Started: r.Started.In(time.Local).Format("2006-01-02 15:04"), Status: r.Status, Text: r.Text, Error: r.Error,
			Seconds: int(r.Ended.Sub(r.Started).Seconds())}
		if p := k; p != "" {
			l.Person = p
			if pp, ok := persons[p]; ok && pp.Name != "" {
				l.Person = pp.Name
			}
		}
		out = append(out, l)
	}
	return out, nil
}

func (s schedules) Change(ctx context.Context, skill, name string, c ui.ScheduleEdit) error {
	a := s.a
	if a.editor() == nil {
		return errors.New("mira.yaml cannot be written here")
	}
	var sc *skills.Schedule
	var sys *skills.SystemJob
	if skill == skills.SystemSkill {
		for _, sj := range a.runner.System {
			if sj.Name == name {
				sys = sj
			}
		}
		if sys == nil {
			return fmt.Errorf("Mira has no schedule %q", name)
		}
		if c.Enabled != nil && !*c.Enabled {
			return errors.New("Mira's own schedules cannot be switched off")
		}
	} else {
		for _, x := range a.runner.Schedules(skill) {
			if name != "" && x.Name == name {
				x := x
				sc = &x
			}
		}
		if sc == nil {
			return fmt.Errorf("the skill %s has no schedule %q", skill, name)
		}
	}
	current := ""
	if sys != nil {
		current = sys.Cron
	} else {
		current = sc.Cron
	}
	cron := ""
	if c.Time != "" {
		if _, _, ok := timeOfCron(current); !ok {
			return errors.New("this schedule's time can only be changed in mira.yaml")
		}
		var err error
		if cron, err = cronOfTime(c.Time, c.Days); err != nil {
			return err
		}
	}
	off := sc != nil && sc.Off
	if c.Enabled != nil {
		off = !*c.Enabled
	}
	err := a.edit.editFile(func(top *yaml.Node) error {
		if sys != nil {
			if cron != "" {
				config.MapSet(config.MapChild(top, "memory"), "profile_schedule", config.Scalar(cron))
			}
			return nil
		}
		entries := config.MapChild(top, "skills")
		entry := config.MapGet(entries, skill)
		if entry == nil || entry.Kind != yaml.MappingNode {
			entry = &yaml.Node{Kind: yaml.MappingNode}
		}
		list := config.MapGet(entry, config.SkillSchedules)
		if list == nil || list.Kind != yaml.MappingNode {
			list = &yaml.Node{Kind: yaml.MappingNode}
		}
		one := config.MapGet(list, name)
		if one == nil || one.Kind != yaml.MappingNode {
			one = &yaml.Node{Kind: yaml.MappingNode}
		}
		switch {
		case cron == "":
		case cron == sc.Default:
			config.MapDelete(one, "cron") // the skill's own time is no line
		default:
			config.MapSet(one, "cron", config.Scalar(cron))
		}
		if off {
			config.MapSet(one, config.SkillOn, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "false"})
		} else {
			config.MapDelete(one, config.SkillOn)
		}
		if len(one.Content) > 0 {
			config.MapSet(list, name, one)
		} else {
			config.MapDelete(list, name)
		}
		if len(list.Content) > 0 {
			config.MapSet(entry, config.SkillSchedules, list)
		} else {
			config.MapDelete(entry, config.SkillSchedules)
		}
		if len(entry.Content) > 0 {
			config.MapSet(entries, skill, entry)
		} else if config.MapGet(entries, skill) != nil {
			config.MapDelete(entries, skill)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return a.runner.Change(skill, name, skills.ScheduleChange{Cron: cron, Off: off})
}

func (s schedules) RunNow(skill, name string) error {
	err := s.a.runner.RunNow(skill, name)
	if errors.Is(err, skills.ErrRunning) {
		return ui.ErrScheduleRunning
	}
	return err
}

// dailyCron is a cron expression the fields can show: one minute, one
// hour, every day of the month, the weekdays as a list or ranges.
var dailyCron = regexp.MustCompile(`^(\d{1,2}) (\d{1,2}) \* \* (\*|[0-7](?:[-,][0-7])*)$`)

// timeOfCron is a cron expression as time of day ("06:45") and weekdays
// (0 Sunday … 6 Saturday); ok is false for one the fields cannot show.
func timeOfCron(cron string) (string, []int, bool) {
	m := dailyCron.FindStringSubmatch(strings.TrimSpace(cron))
	if m == nil {
		return "", nil, false
	}
	minute, _ := strconv.Atoi(m[1])
	hour, _ := strconv.Atoi(m[2])
	if minute > 59 || hour > 23 {
		return "", nil, false
	}
	days := map[int]bool{}
	if m[3] == "*" {
		for d := 0; d < 7; d++ {
			days[d] = true
		}
	} else {
		for _, part := range strings.Split(m[3], ",") {
			from, to, isRange := strings.Cut(part, "-")
			a, _ := strconv.Atoi(from)
			b := a
			if isRange {
				b, _ = strconv.Atoi(to)
			}
			if b < a {
				return "", nil, false
			}
			for d := a; d <= b; d++ {
				days[d%7] = true
			}
		}
	}
	var list []int
	for d := 0; d < 7; d++ {
		if days[d] {
			list = append(list, d)
		}
	}
	return fmt.Sprintf("%02d:%02d", hour, minute), list, true
}

// cronOfTime is a time of day and weekdays as a cron expression: every day
// is *, three or more days in a row a range ("1-5").
func cronOfTime(at string, days []int) (string, error) {
	t, err := time.Parse("15:04", at)
	if err != nil {
		return "", fmt.Errorf("the time %q is not hh:mm", at)
	}
	set := map[int]bool{}
	for _, d := range days {
		if d < 0 || d > 6 {
			return "", fmt.Errorf("weekday %d is not 0 (Sunday) to 6", d)
		}
		set[d] = true
	}
	if len(set) == 0 {
		return "", errors.New("pick at least one weekday")
	}
	field := "*"
	if len(set) < 7 {
		var sorted []int
		for d := range set {
			sorted = append(sorted, d)
		}
		slices.Sort(sorted)
		var parts []string
		for i := 0; i < len(sorted); {
			j := i
			for j+1 < len(sorted) && sorted[j+1] == sorted[j]+1 {
				j++
			}
			switch {
			case j-i >= 2:
				parts = append(parts, fmt.Sprintf("%d-%d", sorted[i], sorted[j]))
			default:
				for k := i; k <= j; k++ {
					parts = append(parts, strconv.Itoa(sorted[k]))
				}
			}
			i = j + 1
		}
		field = strings.Join(parts, ",")
	}
	return fmt.Sprintf("%d %d * * %s", t.Minute(), t.Hour(), field), nil
}

// editFile changes mira.yaml without asking for a restart: what it writes
// holds at once.
func (e *editor) editFile(fn func(top *yaml.Node) error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.file.Edit(fn); err != nil {
		return err
	}
	e.a.log.Info("config written on the page", "file", e.file.Path)
	return nil
}
