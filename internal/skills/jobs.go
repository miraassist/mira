package skills

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/schedule"
)

// JobTemplate is what a skill offers to schedule (docs/spec/scheduled-jobs.md):
// a task with placeholders for its parameters, and the first jobs made from
// it. A skill of before (schedules in SKILL.md) has one per schedule, with no
// parameters of its own (Legacy).
type JobTemplate struct {
	Key         string             `yaml:"-"`
	Description string             `yaml:"description"`
	Parameters  map[string]Setting `yaml:"parameters"`
	Task        string             `yaml:"task"`
	PerPerson   bool               `yaml:"per_person"`
	To          string             `yaml:"to"`
	Urgent      bool               `yaml:"urgent"`
	// Window is how far back {{since}} reaches at a job's first run.
	Window string     `yaml:"window"`
	First  []FirstJob `yaml:"first"`
	// Legacy: made from a schedule of before.
	Legacy bool `yaml:"-"`
	window time.Duration
}

// FirstJob is a job a skill brings, made when the skill is switched on.
type FirstJob struct {
	Name       string         `yaml:"name"`
	Cron       string         `yaml:"cron"`
	Parameters map[string]any `yaml:"parameters"`
}

// ParamNames are the template's parameters, sorted.
func (t JobTemplate) ParamNames() []string {
	out := make([]string, 0, len(t.Parameters))
	for k := range t.Parameters {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Template is the skill's job template of that key.
func (s Skill) Template(key string) (JobTemplate, bool) {
	for _, t := range s.Templates {
		if t.Key == key {
			return t, true
		}
	}
	return JobTemplate{}, false
}

// JobEntry is a scheduled job as mira.yaml has it under skills.<skill>.jobs.
type JobEntry struct {
	Name     string
	Template string
	Cron     string
	Off      bool
	Params   map[string]string
}

// Job keys that are no parameter.
const (
	JobTemplateKey = "template"
	JobCronKey     = "cron"
)

var jobName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

const defaultWindow = 24 * time.Hour

// JobEntries reads a skill's jobs in mira.yaml (skills.<skill>.jobs): name →
// template, cron, enabled and the parameters.
func JobEntries(v any) ([]JobEntry, error) {
	m, ok := v.(map[string]any)
	if !ok && v != nil {
		return nil, errors.New("jobs is a mapping of job names")
	}
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []JobEntry
	for _, n := range names {
		fields, ok := m[n].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("jobs.%s takes template, cron, enabled and the template's parameters", n)
		}
		e := JobEntry{Name: n, Params: map[string]string{}}
		for k, f := range fields {
			switch k {
			case JobTemplateKey:
				e.Template = strings.TrimSpace(render(f))
			case JobCronKey:
				s, ok := f.(string)
				if !ok {
					return nil, fmt.Errorf("jobs.%s.cron must be a cron expression in quotes", n)
				}
				e.Cron = strings.TrimSpace(s)
			case config.SkillOn:
				b, ok := f.(bool)
				if !ok {
					return nil, fmt.Errorf("jobs.%s.enabled must be true or false", n)
				}
				e.Off = !b
			default:
				e.Params[k] = render(f)
			}
		}
		out = append(out, e)
	}
	return out, nil
}

// templates reads a skill's job templates: its jobs, or, from a skill of
// before, one per schedule (docs/spec/scheduled-jobs.md, Compatibility).
func templates(jobs map[string]JobTemplate, scheds []Schedule) ([]JobTemplate, *Problem) {
	if len(jobs) > 0 && len(scheds) > 0 {
		return nil, problem("jobsandschedules")
	}
	var out []JobTemplate
	named := map[string]bool{}
	if len(scheds) > 0 {
		for i, sc := range scheds {
			if _, err := schedule.Parse(sc.Cron); err != nil {
				return nil, problem("cron", i+1, err)
			}
			if strings.TrimSpace(sc.Task) == "" {
				return nil, problem("notask", i+1)
			}
			name := strings.TrimSpace(sc.Name)
			if name != "" && named[name] {
				return nil, problem("scheduledup", name)
			}
			named[name] = name != ""
			if strings.TrimSpace(sc.To) != "" && sc.PerPerson {
				return nil, problem("toperperson", i+1)
			}
			out = append(out, JobTemplate{Key: name, Description: sc.Description, Task: sc.Task, PerPerson: sc.PerPerson,
				To: sc.To, Urgent: sc.Urgent, Legacy: true, window: defaultWindow,
				First: []FirstJob{{Name: name, Cron: sc.Cron}}})
		}
		return out, nil
	}
	keys := make([]string, 0, len(jobs))
	for k := range jobs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t := jobs[k]
		t.Key = k
		if !jobName.MatchString(k) {
			return nil, problem("templatename", k)
		}
		if strings.TrimSpace(t.Task) == "" {
			return nil, problem("templatetask", k)
		}
		if strings.TrimSpace(t.To) != "" && t.PerPerson {
			return nil, problem("templateto", k)
		}
		t.window = defaultWindow
		if w := strings.TrimSpace(t.Window); w != "" {
			d, err := time.ParseDuration(w)
			if err != nil || d <= 0 {
				return nil, problem("templatewindow", k, w)
			}
			t.window = d
		}
		for _, f := range t.First {
			if !jobName.MatchString(f.Name) {
				return nil, problem("jobname", f.Name)
			}
			if named[f.Name] {
				return nil, problem("scheduledup", f.Name)
			}
			named[f.Name] = true
			if _, err := schedule.Parse(f.Cron); err != nil {
				return nil, problem("jobcron", f.Name, err)
			}
			for p := range f.Parameters {
				if _, ok := t.Parameters[p]; !ok {
					return nil, problem("jobparamunknown", f.Name, p)
				}
			}
		}
		out = append(out, t)
	}
	return out, nil
}

// firstJobs are the jobs the skill brings, while mira.yaml has none under
// it; a first job without a value for a required parameter is not made
// (decision 9).
func firstJobs(tpls []JobTemplate) []JobEntry {
	var out []JobEntry
	for _, t := range tpls {
	first:
		for _, f := range t.First {
			e := JobEntry{Name: f.Name, Template: t.Key, Cron: f.Cron, Params: map[string]string{}}
			for k, v := range f.Parameters {
				e.Params[k] = render(v)
			}
			for k, decl := range t.Parameters {
				if decl.Required && strings.TrimSpace(e.Params[k]) == "" {
					continue first
				}
			}
			out = append(out, e)
		}
	}
	return out
}

// jobSchedules makes the skill's schedules from its jobs: the jobs in
// mira.yaml when it has them, else the skill's first jobs; schedules of
// before in mira.yaml change the job of their name. A job that is wrong
// gets an Issue and does not run; the skill does.
func jobSchedules(tpls []JobTemplate, values map[string]any, changes map[string]ScheduleChange,
	vals map[string]string, settings map[string]Setting) ([]Schedule, *Problem) {
	var entries []JobEntry
	raw, written := values[config.SkillJobs]
	if written {
		var err error
		if entries, err = JobEntries(raw); err != nil {
			return nil, problem("jobsentry", err)
		}
	} else {
		entries = firstJobs(tpls)
	}
	// The first job's time of the same name: a time equal to it is the
	// skill's own (a schedule of before).
	defaults := map[string]string{}
	for _, t := range tpls {
		for _, f := range t.First {
			defaults[f.Name] = f.Cron
		}
	}
	known := map[string]bool{}
	var out []Schedule
	for _, e := range entries {
		known[e.Name] = e.Name != ""
		sc := Schedule{Name: e.Name, Default: defaults[e.Name], Template: e.Template, Params: e.Params, Off: e.Off}
		if ch, ok := changes[e.Name]; ok && e.Name != "" {
			if ch.Cron != "" {
				// As before: a wrong time under schedules stops the skill.
				if _, err := schedule.Parse(ch.Cron); err != nil {
					return nil, problem("schedulecron", e.Name, err)
				}
				e.Cron = ch.Cron
			}
			sc.Off = sc.Off || ch.Off
		}
		c, err := schedule.Parse(e.Cron)
		if err != nil {
			sc.Cron = e.Cron
			sc.Issue = problem("jobcron", e.Name, err)
			out = append(out, sc)
			continue
		}
		sc.Cron, sc.cron = e.Cron, c
		var t JobTemplate
		found := false
		for _, x := range tpls {
			if x.Key == e.Template {
				t, found = x, true
			}
		}
		if !found {
			sc.Issue = problem("jobtemplate", e.Name, e.Template)
			out = append(out, sc)
			continue
		}
		sc.Description, sc.PerPerson, sc.Urgent, sc.Window = t.Description, t.PerPerson, t.Urgent, t.window
		if issue := checkParams(e, t); issue != nil {
			sc.Issue = issue
			out = append(out, sc)
			continue
		}
		task := fill(t.Task, e.Params, t.Parameters)
		sc.Task = fill(task, vals, settings)
		// An empty value leaves to empty: the house (docs/spec/schedule-receiver.md).
		sc.To = strings.TrimSpace(fillOrEmpty(fillOrEmpty(t.To, e.Params, t.Parameters), vals, settings))
		out = append(out, sc)
	}
	for name := range changes {
		if !known[name] {
			return nil, problem("scheduleunknown", name)
		}
	}
	return out, nil
}

// checkParams finds a parameter the template does not have, or a required
// one without a value.
func checkParams(e JobEntry, t JobTemplate) *Problem {
	names := make([]string, 0, len(e.Params))
	for k := range e.Params {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if _, ok := t.Parameters[k]; !ok {
			return problem("jobparamunknown", e.Name, k)
		}
	}
	for _, k := range t.ParamNames() {
		if t.Parameters[k].Required && strings.TrimSpace(e.Params[k]) == "" {
			return problem("jobparammissing", e.Name, k, t.Parameters[k].Description)
		}
	}
	return nil
}
