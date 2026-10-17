// Package skills loads skills: folders with a SKILL.md in the Agent Skills
// format (YAML frontmatter with name and description, Markdown body). Mira
// reads her own additions from metadata.mira: settings, calendars and
// schedules.
//
// A skill costs the prompt one line until it is used: the skill_open tool
// lists the enabled skills, and only opening one puts its instructions into
// the conversation. That keeps the shared prefix short and stable however
// many skills are installed.
//
// Skills never run code. What they can do comes from the core: the tools
// every conversation has, and safe building blocks such as calendar_events,
// which reads a calendar by name — the URL stays in the config and never
// reaches a model. A skill that needs more brings an MCP server (later).
package skills

import (
	"bytes"
	"errors"
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
	Calendars map[string]string // calendar name -> URL (from a setting)
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
	// a model; the core uses them, for example as a calendar's URL.
	Secret bool `yaml:"secret"`
}

// Schedule runs Task as a background job at Cron; the result may become a
// message.
type Schedule struct {
	Cron string `yaml:"cron"`
	Task string `yaml:"task"`
	cron schedule.Cron
}

func (s Schedule) Parsed() schedule.Cron { return s.cron }

type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Metadata    struct {
		Mira struct {
			Settings  map[string]Setting `yaml:"settings"`
			Calendars map[string]string  `yaml:"calendars"`
			Schedules []Schedule         `yaml:"schedules"`
			Target    *Target            `yaml:"target"`
		} `yaml:"mira"`
	} `yaml:"metadata"`
}

var namePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Load finds every enabled skill in dirs (the first folder of that name
// wins) and checks it against its settings. A skill that is enabled but
// missing or broken is an error: the user asked for it.
func Load(dirs []string, enabled map[string]map[string]any) ([]Skill, error) {
	var out []Skill
	var errs []error
	names := make([]string, 0, len(enabled))
	for n := range enabled {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		dir := find(dirs, name)
		if dir == "" {
			errs = append(errs, fmt.Errorf("skill %s: no folder %s/SKILL.md in %s", name, name, strings.Join(dirs, ", ")))
			continue
		}
		s, err := load(dir, enabled[name])
		if err != nil {
			errs = append(errs, fmt.Errorf("skill %s: %w", name, err))
			continue
		}
		out = append(out, s)
	}
	return out, errors.Join(errs...)
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

func load(dir string, values map[string]any) (Skill, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return Skill{}, err
	}
	fm, body, err := split(raw)
	if err != nil {
		return Skill{}, err
	}
	var meta frontmatter
	if err := yaml.Unmarshal(fm, &meta); err != nil {
		return Skill{}, fmt.Errorf("frontmatter: %w", err)
	}
	if meta.Name != filepath.Base(dir) || !namePattern.MatchString(meta.Name) {
		return Skill{}, fmt.Errorf("name %q must match its folder and be lower-case words with hyphens", meta.Name)
	}
	if strings.TrimSpace(meta.Description) == "" {
		return Skill{}, errors.New("description is missing")
	}
	m := meta.Metadata.Mira
	s := Skill{Name: meta.Name, Description: strings.TrimSpace(meta.Description), Dir: dir,
		Settings: m.Settings, Calendars: map[string]string{}}

	vals := map[string]string{}
	for k, v := range values {
		if _, ok := m.Settings[k]; !ok {
			return Skill{}, fmt.Errorf("unknown setting %s", k)
		}
		vals[k] = render(v)
	}
	for k, decl := range m.Settings {
		if decl.Required && vals[k] == "" {
			return Skill{}, fmt.Errorf("setting %s is required: %s", k, decl.Description)
		}
	}
	for cal, key := range m.Calendars {
		if _, ok := m.Settings[key]; !ok {
			return Skill{}, fmt.Errorf("calendar %s refers to unknown setting %s", cal, key)
		}
		if vals[key] != "" {
			s.Calendars[cal] = vals[key]
		}
	}
	for i, sc := range m.Schedules {
		c, err := schedule.Parse(sc.Cron)
		if err != nil {
			return Skill{}, fmt.Errorf("schedule %d: %w", i+1, err)
		}
		if strings.TrimSpace(sc.Task) == "" {
			return Skill{}, fmt.Errorf("schedule %d has no task", i+1)
		}
		sc.cron = c
		s.Schedules = append(s.Schedules, sc)
	}
	if t := m.Target; t != nil {
		if len(t.Fields) == 0 || t.Finish.Name == "" {
			return Skill{}, errors.New("target needs fields and finish")
		}
		for _, a := range []*Action{t.Finish.Action, t.Undo} {
			if a != nil && !strings.Contains(a.Action, ".") {
				return Skill{}, fmt.Errorf("target action %q needs a domain, like esphome.show_alarm_set", a.Action)
			}
		}
		for i, f := range t.Fields {
			if !namePattern.MatchString(strings.ReplaceAll(f.Name, "_", "-")) || f.Description == "" {
				return Skill{}, fmt.Errorf("target field %d needs a name and a description", i+1)
			}
		}
		if t.Timeout != "" {
			d, err := time.ParseDuration(t.Timeout)
			if err != nil || d <= 0 {
				return Skill{}, fmt.Errorf("target timeout %q is not a duration like 15m", t.Timeout)
			}
			t.timeout = d
		}
		s.Target = t
	}
	s.Body = fill(strings.TrimSpace(string(body)), vals, m.Settings)
	return s, nil
}

// split separates the YAML frontmatter from the Markdown body.
func split(raw []byte) ([]byte, []byte, error) {
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	if !bytes.HasPrefix(raw, []byte("---")) {
		return nil, nil, errors.New("SKILL.md must start with --- frontmatter")
	}
	rest := raw[3:]
	end := bytes.Index(rest, []byte("\n---"))
	if end < 0 {
		return nil, nil, errors.New("frontmatter is not closed with ---")
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
