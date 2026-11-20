package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/ui"
)

// jobs writes the skills' scheduled jobs in mira.yaml (docs/spec/scheduled-jobs.md):
// made, changed and deleted on the skill's entry; they hold after a restart.
type jobs struct{ a *app }

var jobNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// skillFile is a skill's SKILL.md and its templates.
func (a *app) skillFile(name string) (skills.File, []skills.JobTemplate, error) {
	dir := skills.Find(a.cfg.Skills.Dirs, name)
	if dir == "" {
		return skills.File{}, nil, fmt.Errorf("Den Skill „%s“ gibt es nicht.", name)
	}
	f, err := skills.ReadFile(dir)
	if err != nil {
		return skills.File{}, nil, err
	}
	tpls, err := f.Templates()
	if err != nil {
		return skills.File{}, nil, err
	}
	return f, tpls, nil
}

func (j jobs) SaveJob(ctx context.Context, skill, name string, e ui.JobEdit) error {
	a := j.a
	if a.editor() == nil {
		return errors.New("mira.yaml cannot be written here")
	}
	if !jobNamePattern.MatchString(name) {
		return fmt.Errorf("Der Name „%s“ geht nicht: nur kleine Buchstaben, Ziffern und Bindestriche.", name)
	}
	_, tpls, err := a.skillFile(skill)
	if err != nil {
		return err
	}
	var tpl *skills.JobTemplate
	for i := range tpls {
		if tpls[i].Key == e.Template {
			tpl = &tpls[i]
		}
	}
	if tpl == nil {
		return fmt.Errorf("Der Skill %s hat keine Auftragsvorlage „%s“.", skill, e.Template)
	}
	cron, err := cronOfTime(e.Time, e.Days)
	if err != nil {
		return err
	}
	params := map[string]string{}
	for k, v := range e.Params {
		if _, ok := tpl.Parameters[k]; !ok {
			return fmt.Errorf("Die Vorlage „%s“ hat keinen Wert „%s“.", tpl.Key, k)
		}
		if v = strings.TrimSpace(v); v != "" {
			params[k] = v
		}
	}
	for _, k := range tpl.ParamNames() {
		if d := tpl.Parameters[k]; d.Required && params[k] == "" {
			return fmt.Errorf("„%s“ fehlt: %s", k, d.Description)
		}
	}
	return a.edit.change(nil, func(top *yaml.Node) error {
		entry, list, err := a.jobsNode(top, skill, tpls)
		if err != nil {
			return err
		}
		config.MapSet(list, name, jobNode(skills.JobEntry{Name: name, Template: tpl.Key, Cron: cron, Off: !e.Enabled, Params: params}))
		config.MapSet(entry, config.SkillJobs, list)
		return nil
	})
}

func (j jobs) DeleteJob(ctx context.Context, skill, name string) error {
	a := j.a
	if a.editor() == nil {
		return errors.New("mira.yaml cannot be written here")
	}
	_, tpls, err := a.skillFile(skill)
	if err != nil {
		return err
	}
	return a.edit.change(nil, func(top *yaml.Node) error {
		entry, list, err := a.jobsNode(top, skill, tpls)
		if err != nil {
			return err
		}
		if config.MapGet(list, name) == nil {
			return fmt.Errorf("%w: %s %s", ui.ErrNoJob, skill, name)
		}
		config.MapDelete(list, name)
		if len(list.Content) == 0 {
			list.Style = yaml.FlowStyle // jobs: {}
		}
		config.MapSet(entry, config.SkillJobs, list)
		return nil
	})
}

// jobsNode is the skill's entry in mira.yaml with its jobs, made when it had
// none: the jobs that run now (or the skill's first ones), with a
// schedules entry of before moved into them (docs/spec/scheduled-jobs.md,
// Compatibility).
func (a *app) jobsNode(top *yaml.Node, skill string, tpls []skills.JobTemplate) (*yaml.Node, *yaml.Node, error) {
	entries := config.MapChild(top, "skills")
	entry := config.MapGet(entries, skill)
	if entry == nil || entry.Kind != yaml.MappingNode {
		entry = &yaml.Node{Kind: yaml.MappingNode}
		config.MapSet(entries, skill, entry)
	}
	if err := a.moveSchedules(entry, skill, tpls); err != nil {
		return nil, nil, err
	}
	list := config.MapGet(entry, config.SkillJobs)
	if list == nil || list.Kind != yaml.MappingNode {
		list = &yaml.Node{Kind: yaml.MappingNode}
	}
	list.Style = 0
	return entry, list, nil
}

// moveSchedules writes a skill's jobs into its entry when it has none yet,
// and moves an entry schedules of before into them; the log says so once.
// A schedule without a name cannot be a job: then nothing is moved.
func (a *app) moveSchedules(entry *yaml.Node, skill string, tpls []skills.JobTemplate) error {
	old := config.MapGet(entry, config.SkillSchedules)
	if config.MapGet(entry, config.SkillJobs) != nil && old == nil {
		return nil
	}
	var changes map[string]skills.ScheduleChange
	if old != nil {
		var raw any
		if err := old.Decode(&raw); err != nil {
			return err
		}
		var err error
		if changes, err = skills.ScheduleChanges(raw); err != nil {
			return err
		}
	}
	var list []skills.JobEntry
	if existing := config.MapGet(entry, config.SkillJobs); existing != nil {
		var raw any
		if err := existing.Decode(&raw); err != nil {
			return err
		}
		var err error
		if list, err = skills.JobEntries(raw); err != nil {
			return err
		}
	} else {
		// The jobs as they run now, else the skill's first ones.
		if sk, ok := a.skillOn(skill); ok {
			for _, sc := range sk.Schedules {
				list = append(list, skills.JobEntry{Name: sc.Name, Template: sc.Template, Cron: sc.Cron, Off: sc.Off,
					Params: sc.Params})
			}
		} else {
			list = skills.FirstJobs(tpls)
		}
	}
	for _, e := range list {
		if e.Name == "" {
			return errors.New("Ein Zeitplan dieses Skills hat keinen Namen (name in SKILL.md); seine Aufträge lassen sich hier erst ändern, wenn er einen hat.")
		}
	}
	var dropped []string
	for name, ch := range changes {
		i := slices.IndexFunc(list, func(e skills.JobEntry) bool { return e.Name == name })
		if i < 0 {
			dropped = append(dropped, name)
			continue
		}
		if ch.Cron != "" {
			list[i].Cron = ch.Cron
		}
		list[i].Off = list[i].Off || ch.Off
	}
	node := &yaml.Node{Kind: yaml.MappingNode}
	if len(list) == 0 {
		node.Style = yaml.FlowStyle
	}
	for _, e := range list {
		config.MapSet(node, e.Name, jobNode(e))
	}
	config.MapSet(entry, config.SkillJobs, node)
	if old != nil {
		config.MapDelete(entry, config.SkillSchedules)
		sort.Strings(dropped)
		a.log.Info("schedules moved to jobs", "skill", skill, "jobs", len(list), "dropped", dropped)
	}
	return nil
}

// firstJobsNode writes the skill's first jobs when it is switched on and
// has none in mira.yaml (decision 8); one whose required parameter has no
// value is left out (decision 9).
func firstJobsNode(entry *yaml.Node, tpls []skills.JobTemplate) {
	if config.MapGet(entry, config.SkillJobs) != nil || config.MapGet(entry, config.SkillSchedules) != nil {
		return
	}
	list := skills.FirstJobs(tpls)
	if len(list) == 0 && len(tpls) == 0 {
		return
	}
	node := &yaml.Node{Kind: yaml.MappingNode}
	if len(list) == 0 {
		node.Style = yaml.FlowStyle
	}
	for _, e := range list {
		if e.Name == "" {
			return // a schedule of before without a name stays as it is
		}
		config.MapSet(node, e.Name, jobNode(e))
	}
	config.MapSet(entry, config.SkillJobs, node)
}

// jobNode is a job's entry: template, cron, enabled: false when off, the
// parameters.
func jobNode(e skills.JobEntry) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode}
	config.MapSet(n, skills.JobTemplateKey, config.Scalar(e.Template))
	config.MapSet(n, skills.JobCronKey, &yaml.Node{Kind: yaml.ScalarNode, Value: e.Cron, Style: yaml.DoubleQuotedStyle})
	if e.Off {
		config.MapSet(n, config.SkillOn, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "false"})
	}
	keys := make([]string, 0, len(e.Params))
	for k := range e.Params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		config.MapSet(n, k, config.Scalar(e.Params[k]))
	}
	return n
}

// jobsInfo is a skill's templates and jobs as the page shows them.
func (a *app) jobsInfo(sk skills.Skill, scheds []skills.Schedule, info *ui.SkillInfo) {
	for _, t := range sk.Templates {
		if t.Legacy {
			continue // a schedule of before: shown as its job only
		}
		ti := ui.TemplateInfo{Key: t.Key, Description: t.Description, PerPerson: t.PerPerson}
		for _, k := range t.ParamNames() {
			d := t.Parameters[k]
			ti.Parameters = append(ti.Parameters, ui.SettingInfo{Name: k, Description: d.Description, Secret: d.Secret,
				Required: d.Required})
		}
		info.Templates = append(info.Templates, ti)
	}
	now := time.Now()
	for _, s := range scheds {
		if s.Name == "" {
			continue
		}
		ji := ui.JobInfo{Name: s.Name, Template: s.Template, Cron: s.Cron, Enabled: !s.Off, Params: s.Params}
		ji.Time, ji.Days, _ = timeOfCron(s.Cron)
		if s.Issue != nil {
			ji.Problem = s.Issue.Text(a.lang)
		} else if next := s.Parsed().Next(now); !next.IsZero() && !s.Off {
			ji.Next = next.Format("2006-01-02 15:04")
		}
		info.Jobs = append(info.Jobs, ji)
	}
}
