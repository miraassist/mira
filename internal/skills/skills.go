// Package skills loads skills: folders with a SKILL.md in the Agent Skills
// format (YAML frontmatter with name and description, Markdown body). Mira
// reads her own additions from metadata.mira: settings, schedules and a
// target (docs/spec/vorgaenge.md, docs/spec/skill-aktionen.md).
//
// A skill costs the prompt one line until it is used: the skill_open tool
// lists the enabled skills, and only opening one puts its instructions into
// the conversation. That keeps the shared prefix short and stable however
// many skills are installed.
//
// Skills never run code. What they can do comes from the core: the tools
// every conversation has (calendar_events among them), the building blocks
// of a target, and Home Assistant actions a target finishes with.
package skills

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/miraassist/mira/internal/schedule"
)

// Skill is one loaded and enabled skill.
type Skill struct {
	Name        string
	Description string
	// Body is the Markdown after the frontmatter, with {{setting}}
	// placeholders filled in (secret settings never are).
	Body      string
	Dir       string
	Settings  map[string]Setting
	Schedules []Schedule
	// Target, when set, makes the skill a goal a thread ("Vorgang") works
	// towards: fields to fill, how they are checked, how it ends.
	Target *Target
}

// Target is a goal definition (docs/spec/vorgaenge.md). Checks and Finish
// name building blocks of the core: a skill runs no code.
type Target struct {
	Fields []TargetField `yaml:"fields"`
	// Confirm asks the user for a yes before an action finish runs; the
	// building blocks decide that themselves.
	Confirm bool `yaml:"confirm"`
	// Finish does the work and ends the thread: a building block of the
	// core (tasks.automation, memory.save), or a Home Assistant action the
	// skill names (docs/spec/skill-aktionen.md).
	Finish Finish `yaml:"finish"`
	// Undo takes back what an action finish did, for a correction ("no,
	// half past six"); optional.
	Undo *Action `yaml:"undo"`
	// Timeout ends a thread nobody answers; default 15m.
	Timeout string `yaml:"timeout"`
	timeout time.Duration
}

// TimeoutDuration is Timeout parsed, 15 minutes by default.
func (t Target) TimeoutDuration() time.Duration {
	if t.timeout > 0 {
		return t.timeout
	}
	return 15 * time.Minute
}

type TargetField struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Check       string   `yaml:"check"`
	Enum        []string `yaml:"enum"`
	// Optional fields may stay empty; the thread does not wait for them.
	Optional bool `yaml:"optional"`
}

// FinishAction is the name of a finish that calls a Home Assistant action.
const FinishAction = "ha.action"

// Finish is a building block by name, or a Home Assistant action.
type Finish struct {
	Name   string
	Action *Action
}

// Action is a Home Assistant action with its data. Both may hold
// placeholders: {{ field }} for the thread's fields, and the place: area,
// area_name, satellite, satellite.device, person.
type Action struct {
	Action string         `yaml:"action"`
	Data   map[string]any `yaml:"data"`
	// OmitEmpty leaves out data that comes out empty, for actions that take
	// one of several fields (calendar.create_event: a time or a day). By
	// default empty data stays: ESPHome actions want every field.
	OmitEmpty bool `yaml:"omit_empty"`
}

func (f *Finish) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		f.Name = strings.TrimSpace(node.Value)
		return nil
	}
	var a Action
	if err := node.Decode(&a); err != nil {
		return err
	}
	f.Name, f.Action = FinishAction, &a
	return nil
}

func (f Finish) String() string {
	if f.Action != nil {
		return f.Action.Action
	}
	return f.Name
}

type Setting struct {
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
	// Secret settings (URLs with an address or a token) are never shown to
	// a model; the core uses them.
	Secret bool `yaml:"secret"`
}

// Schedule runs Task as a background job at Cron; the result may become a
// message.
type Schedule struct {
	Cron string `yaml:"cron"`
	Task string `yaml:"task"`
	// PerPerson runs the task once for each person a message can reach (a
	// channel of their own, or "alone at home is me"), with {{person}} and
	// {{person_calendar}} (their own calendar, one named like them; "-" if
	// none) filled in and what Mira knows about them added; the message goes
	// to that person, not to the house.
	PerPerson bool `yaml:"per_person"`
	cron      schedule.Cron
}

func (s Schedule) Parsed() schedule.Cron { return s.cron }

type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Metadata    struct {
		Mira struct {
			Settings  map[string]Setting `yaml:"settings"`
			Schedules []Schedule         `yaml:"schedules"`
			Target    *Target            `yaml:"target"`
		} `yaml:"mira"`
	} `yaml:"metadata"`
}

var namePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Load finds every enabled skill in dirs (the first folder of that name
// wins) and checks it against its settings. A skill that is enabled but
// missing or broken is left out and comes back as a Problem: the agent must
// start anyway, and the problem is shown to whoever enabled the skill.
func Load(dirs []string, enabled map[string]map[string]any) ([]Skill, []Problem) {
	var out []Skill
	var problems []Problem
	names := make([]string, 0, len(enabled))
	for n := range enabled {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		dir := find(dirs, name)
		if dir == "" {
			problems = append(problems, Problem{Skill: name, kind: "nofolder", args: []any{name, strings.Join(dirs, ", ")}})
			continue
		}
		s, p := load(dir, enabled[name])
		if p != nil {
			p.Skill = name
			problems = append(problems, *p)
			continue
		}
		out = append(out, s)
	}
	return out, problems
}

func find(dirs []string, name string) string {
	for _, d := range dirs {
		p := filepath.Join(d, name)
		if st, err := os.Stat(filepath.Join(p, "SKILL.md")); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func load(dir string, values map[string]any) (Skill, *Problem) {
	raw, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return Skill{}, problem("unreadable", err)
	}
	fm, body, p := split(raw)
	if p != nil {
		return Skill{}, p
	}
	var meta frontmatter
	if err := yaml.Unmarshal(fm, &meta); err != nil {
		return Skill{}, problem("yaml", err)
	}
	if meta.Name != filepath.Base(dir) || !namePattern.MatchString(meta.Name) {
		return Skill{}, problem("name", meta.Name)
	}
	if strings.TrimSpace(meta.Description) == "" {
		return Skill{}, problem("nodesc")
	}
	m := meta.Metadata.Mira
	s := Skill{Name: meta.Name, Description: strings.TrimSpace(meta.Description), Dir: dir,
		Settings: m.Settings}

	vals := map[string]string{}
	for k, v := range values {
		if _, ok := m.Settings[k]; !ok {
			if len(m.Settings) == 0 {
				return Skill{}, problem("nosettings", k)
			}
			known := make([]string, 0, len(m.Settings))
			for name := range m.Settings {
				known = append(known, name)
			}
			sort.Strings(known)
			return Skill{}, problem("unknownsetting", k, strings.Join(known, ", "))
		}
		vals[k] = render(v)
	}
	for k, decl := range m.Settings {
		if decl.Required && vals[k] == "" {
			return Skill{}, problem("required", k, decl.Description)
		}
	}
	for i, sc := range m.Schedules {
		c, err := schedule.Parse(sc.Cron)
		if err != nil {
			return Skill{}, problem("cron", i+1, err)
		}
		if strings.TrimSpace(sc.Task) == "" {
			return Skill{}, problem("notask", i+1)
		}
		sc.cron = c
		sc.Task = fill(sc.Task, vals, m.Settings)
		s.Schedules = append(s.Schedules, sc)
	}
	if t := m.Target; t != nil {
		if len(t.Fields) == 0 || t.Finish.Name == "" {
			return Skill{}, problem("targetneeds")
		}
		for _, a := range []*Action{t.Finish.Action, t.Undo} {
			if a != nil && !strings.Contains(a.Action, ".") {
				return Skill{}, problem("targetaction", a.Action)
			}
		}
		for i, f := range t.Fields {
			if !namePattern.MatchString(strings.ReplaceAll(f.Name, "_", "-")) || f.Description == "" {
				return Skill{}, problem("targetfield", i+1)
			}
		}
		if t.Timeout != "" {
			d, err := time.ParseDuration(t.Timeout)
			if err != nil || d <= 0 {
				return Skill{}, problem("targettimeout", t.Timeout)
			}
			t.timeout = d
		}
		s.Target = t
	}
	s.Body = fill(strings.TrimSpace(string(body)), vals, m.Settings)
	return s, nil
}

// split separates the YAML frontmatter from the Markdown body.
func split(raw []byte) ([]byte, []byte, *Problem) {
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	if !bytes.HasPrefix(raw, []byte("---")) {
		return nil, nil, problem("nofront")
	}
	rest := raw[3:]
	end := bytes.Index(rest, []byte("\n---"))
	if end < 0 {
		return nil, nil, problem("frontopen")
	}
	body := rest[end+4:]
	if i := bytes.IndexByte(body, '\n'); i >= 0 {
		body = body[i+1:]
	} else {
		body = nil
	}
	return rest[:end], body, nil
}

func render(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case []any:
		parts := make([]string, 0, len(x))
		for _, p := range x {
			parts = append(parts, render(p))
		}
		return strings.Join(parts, ", ")
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}

var placeholder = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_]+)\s*\}\}`)

// fill replaces {{setting}} with its value. Secret settings and unknown
// names stay unreplaced, so a secret can never reach the prompt by a typo
// in a skill.
func fill(body string, vals map[string]string, decl map[string]Setting) string {
	return placeholder.ReplaceAllStringFunc(body, func(m string) string {
		key := placeholder.FindStringSubmatch(m)[1]
		d, ok := decl[key]
		if !ok || d.Secret {
			return m
		}
		if vals[key] == "" {
			return "-"
		}
		return vals[key]
	})
}

// Exists reports whether a skill folder of that name is in dirs.
func Exists(dirs []string, name string) bool { return find(dirs, name) != "" }
