// Package skills loads skills: folders with a SKILL.md in the Agent Skills
// format (YAML frontmatter with name and description, Markdown body). Mira
// reads her own additions from metadata.mira: settings, schedules and a
// target (docs/spec/threads.md, docs/spec/skill-actions.md).
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
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/schedule"
)

// Skill is one loaded and enabled skill.
type Skill struct {
	Name        string
	Description string
	// Body is the Markdown after the frontmatter, with {{setting}}
	// placeholders filled in (secret settings never are).
	Body string
	// Lang is the language SKILL.md is written in (metadata.mira.language,
	// German if not said); translations are SKILL.<lang>.md beside it, with
	// only texts (docs/spec/skill-languages.md). DescriptionIn and BodyIn give
	// the texts for a turn's language.
	Lang         i18n.Lang
	translations map[i18n.Lang]translation
	// Dir is the skill's folder on disk; empty for a skill from a skill
	// source, which lives in memory only (docs/spec/skill-source.md).
	Dir string
	// Files are the skill's files: every file of its folder, in every
	// subfolder, as it is, rooted at the folder (SKILL.md, blueprints/…),
	// from a folder or from a source alike (docs/spec/skill-files.md).
	Files fs.FS
	// Source is the address of the skill source the skill came from; empty
	// for a skill from a folder.
	Source string
	// Automations are the skill automations it declares
	// (metadata.mira.automations), their inputs filled from the settings
	// (docs/spec/skill-files.md, decision 12).
	Automations []Automation
	// Scripts are the skill scripts it declares (metadata.mira.scripts),
	// made from its script blueprints (docs/spec/skill-files.md, decision
	// 18).
	Scripts   []Automation
	Settings  map[string]Setting
	Schedules []Schedule
	// Target, when set, makes the skill a goal a thread ("Vorgang") works
	// towards: fields to fill, how they are checked, how it ends.
	Target *Target
	// Mail, when set, makes the skill a mail application: mails of an IMAP
	// entry named like the skill go into a calendar of that name
	// (docs/spec/mail-calendar.md).
	Mail *Mail
	// Inbox, when set, is how Mira reads what lands in her inbox
	// (docs/spec/inbox.md): the reading's instructions.
	Inbox *Inbox
	// Takes is the kind of message whose entries lead to this skill's
	// threads (metadata.mira.takes; docs/spec/messages.md, decision 33);
	// empty for none.
	Takes string
	// Agents are the agents the skill describes (metadata.mira.agents), by
	// name, with the settings filled into description and instructions
	// (docs/spec/agents.md, Factory).
	Agents map[string]config.AgentDef
	// Privacy local makes every agent the skill uses run on a local model
	// (docs/spec/models.md, Privacy as a rule).
	Privacy string
	// MCP is the skill's MCP server, named after the skill, with its
	// settings filled in (docs/spec/skill-mcp.md); nil for none.
	MCP *config.MCPServer
	// OAuth is the skill's login at a service, with its settings filled in
	// (docs/spec/oauth.md); nil for none.
	OAuth *OAuth
	// Random are the skill's axes (metadata.mira.random), which random_pick
	// draws from (docs/spec/random-pick.md); nil for none.
	Random []Axis
	// Chapter is how the heading of the chapter in Mira's guide about this
	// skill begins (metadata.mira.chapter, "## Kochen und Backen"), in the
	// skill's language; Mira's page links it. Empty for none.
	Chapter string
}

// Mail is a skill's part in the mail calendar: how to read its mails. The
// schema and the checks are the core's.
type Mail struct {
	Extract string `yaml:"extract"`
	// Done is a short sign the core puts before the title of an entry
	// that is done ("●"); the calendar's short line shows it first.
	Done string `yaml:"done"`
}

// Inbox is a skill's part in Mira's inbox: how to read a document. The
// schema and the checks are the core's.
type Inbox struct {
	Extract string `yaml:"extract"`
}

// Target is a goal definition (docs/spec/threads.md). Checks and Finish
// name building blocks of the core: a skill runs no code.
type Target struct {
	// Plan makes a thread that carries a plan (a Markdown text with
	// checkboxes) for a long run step by step, such as cooking; it has no
	// fields and no finish (docs/spec/threads.md, Pläne). Agent and Input
	// let an agent write the plan when the thread starts.
	Plan   bool           `yaml:"plan"`
	Agent  string         `yaml:"agent"`
	Input  map[string]any `yaml:"input"`
	Fields []TargetField  `yaml:"fields"`
	// Confirm asks the user for a yes before an action finish runs; the
	// building blocks decide that themselves.
	Confirm bool `yaml:"confirm"`
	// Finish does the work and ends the thread: a building block of the
	// core (tasks.automation, memory.save), or a Home Assistant action the
	// skill names (docs/spec/skill-actions.md).
	Finish Finish `yaml:"finish"`
	// Undo takes back what an action finish did, for a correction ("no,
	// half past six"); optional.
	Undo *Action `yaml:"undo"`
	// Timeout ends a thread nobody answers; default 15m, 1h for a plan.
	// Every step of a plan starts it anew.
	Timeout string `yaml:"timeout"`
	timeout time.Duration
	// Bound is what a thread of the target is bound to: "person" (the
	// default), "room" (docs/spec/threads.md, Assignment) or "house"
	// (docs/spec/messages.md, decision 27).
	Bound string `yaml:"bound"`
	// DoneAfter keeps a plan whose steps are all done open that long, for
	// "wait, not done yet"; default 30m.
	DoneAfter string `yaml:"done_after"`
	doneAfter time.Duration
}

// Bound to the room, rather than the person.
const BoundRoom = "room"

// BoundHouse: the house's, wherever a thread is taken up
// (docs/spec/messages.md, decision 27).
const BoundHouse = "house"

// DoneAfterDuration is DoneAfter parsed, 30 minutes by default.
func (t Target) DoneAfterDuration() time.Duration {
	if t.doneAfter > 0 {
		return t.doneAfter
	}
	return 30 * time.Minute
}

// TimeoutDuration is Timeout parsed, 15 minutes by default (1 hour for a
// plan).
func (t Target) TimeoutDuration() time.Duration {
	switch {
	case t.timeout > 0:
		return t.timeout
	case t.Plan:
		return time.Hour
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
	// Agent fills the field instead of the user, with Input (placeholders:
	// the thread's fields, {{ request }}); it starts once every field its
	// input names is there (docs/spec/agents.md, Continuation).
	Agent string         `yaml:"agent"`
	Input map[string]any `yaml:"input"`
}

// AgentInput is an agent's input for a field or a plan: what the skill
// wrote, or else what the user asked for.
func AgentInput(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{"input": "{{ request }}"}
	}
	return input
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
	// Name is the schedule's key within the skill: mira.yaml changes it by
	// it (docs/spec/schedules.md). Without one it cannot be changed there.
	Name string `yaml:"name"`
	// Description is a few words for the page.
	Description string `yaml:"description"`
	Cron        string `yaml:"cron"`
	Task        string `yaml:"task"`
	// PerPerson runs the task once for each person a message can reach (a
	// channel of their own, or "alone at home is me"), with {{person}} and
	// {{person_calendar}} (their own calendar, one named like them; "-" if
	// none) and {{person_list}} (their own to-do list, the same way) filled in and what Mira knows about them added; the message goes
	// to that person, not to the house.
	PerPerson bool `yaml:"per_person"`
	// To names one person the message goes to, privately over their
	// channels and never spoken (docs/spec/schedule-receiver.md): a person's
	// key, name or login, filled with the skill's settings. Empty: the house.
	To string `yaml:"to"`
	// Urgent lets the job report importance 3, which goes out at once, in
	// quiet hours too (an official warning of extreme weather); without it
	// a job reports at most 2.
	Urgent bool `yaml:"urgent"`
	// Default is the skill's own time; Cron is mira.yaml's when it has one.
	Default string `yaml:"-"`
	// Off: mira.yaml switches it off (enabled: false); it keeps its time.
	Off  bool `yaml:"-"`
	cron schedule.Cron
}

// SetCron gives the schedule a new time.
func (s *Schedule) SetCron(expr string) error {
	c, err := schedule.Parse(expr)
	if err != nil {
		return err
	}
	s.Cron, s.cron = expr, c
	return nil
}

// ScheduleChange is a schedule's entry in mira.yaml, under the skill's
// schedules: a time of its own and whether it is off.
type ScheduleChange struct {
	Cron string
	Off  bool
}

// ScheduleChanges reads the entry schedules of a skill's settings in
// mira.yaml: name → change.
func ScheduleChanges(v any) (map[string]ScheduleChange, error) {
	if v == nil {
		return nil, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("schedules is a mapping of schedule names")
	}
	out := map[string]ScheduleChange{}
	for name, e := range m {
		fields, ok := e.(map[string]any)
		if !ok && e != nil {
			return nil, fmt.Errorf("schedules.%s takes cron and enabled", name)
		}
		var c ScheduleChange
		for k, f := range fields {
			switch k {
			case "cron":
				s, ok := f.(string)
				if !ok {
					return nil, fmt.Errorf("schedules.%s.cron must be a cron expression in quotes", name)
				}
				c.Cron = s
			case config.SkillOn:
				b, ok := f.(bool)
				if !ok {
					return nil, fmt.Errorf("schedules.%s.enabled must be true or false", name)
				}
				c.Off = !b
			default:
				return nil, fmt.Errorf("schedules.%s.%s does not exist; possible are cron and enabled", name, k)
			}
		}
		out[name] = c
	}
	return out, nil
}

func (s Schedule) Parsed() schedule.Cron { return s.cron }

type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Metadata    struct {
		Mira struct {
			Settings  map[string]Setting   `yaml:"settings"`
			Schedules []Schedule           `yaml:"schedules"`
			Target    *Target              `yaml:"target"`
			Mail      *Mail                `yaml:"mail"`
			Inbox     *Inbox               `yaml:"inbox"`
			Takes     string               `yaml:"takes"`
			Agents    map[string]yaml.Node `yaml:"agents"`
			Privacy   string               `yaml:"privacy"`
			MCP       *config.MCPServer    `yaml:"mcp"`
			OAuth     *OAuth               `yaml:"oauth"`
			Language  string               `yaml:"language"`
			Chapter   string               `yaml:"chapter"`
			Random    yaml.Node            `yaml:"random"`
			// Automations are kept as written: their inputs are filled
			// with the settings' values as mira.yaml has them.
			Automations []automationDecl `yaml:"automations"`
			Scripts     []automationDecl `yaml:"scripts"`
		} `yaml:"mira"`
	} `yaml:"metadata"`
}

var namePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Root is one place skills are found in: a skill directory on disk, or the
// skills of a skill source in memory (docs/spec/skill-source.md). Its file
// system holds one folder per skill.
type Root struct {
	// Name is the folder's path, or the source's address: what a problem
	// names.
	Name string
	FS   fs.FS
	// Source says the skills come from a skill source.
	Source bool
}

// Roots are the skill directories as roots, in order.
func Roots(dirs []string) []Root {
	out := make([]Root, 0, len(dirs))
	for _, d := range dirs {
		out = append(out, Root{Name: d, FS: os.DirFS(d)})
	}
	return out
}

// Load finds every enabled skill in dirs (the first folder of that name
// wins) and checks it against its settings. A skill that is enabled but
// missing or broken is left out and comes back as a Problem: the service must
// start anyway, and the problem is shown to whoever enabled the skill.
func Load(dirs []string, enabled map[string]map[string]any) ([]Skill, []Problem) {
	return LoadFrom(Roots(dirs), enabled)
}

// LoadFrom is Load over roots: the skill directories, then a source's skills
// (docs/spec/skill-source.md, B). A source's skill with the name of a skill
// in a folder before it is left out, with a problem naming both (decision
// 12); the folder's is the one loaded.
func LoadFrom(roots []Root, enabled map[string]map[string]any) ([]Skill, []Problem) {
	var out []Skill
	var problems []Problem
	for i, r := range roots {
		if !r.Source {
			continue
		}
		entries, err := fs.ReadDir(r.FS, ".")
		if err != nil {
			continue // a source's skills are in memory: nothing to fail
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if other := findIn(roots[:i], e.Name()); other != nil {
				problems = append(problems, Problem{Skill: e.Name(), kind: KindSourceTwice, args: []any{e.Name(), other.Name, r.Name}})
			}
		}
	}
	names := make([]string, 0, len(enabled))
	for n := range enabled {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		root := findIn(roots, name)
		if root == nil {
			where := make([]string, 0, len(roots))
			for _, r := range roots {
				where = append(where, r.Name)
			}
			problems = append(problems, Problem{Skill: name, kind: "nofolder", args: []any{name, strings.Join(where, ", ")}})
			continue
		}
		s, p := load(*root, name, enabled[name])
		if p != nil {
			p.Skill = name
			problems = append(problems, *p)
			continue
		}
		out = append(out, s)
	}
	return out, problems
}

// findIn is the first root with a skill folder of that name, nil if none.
func findIn(roots []Root, name string) *Root {
	if !fs.ValidPath(name) || strings.Contains(name, "/") {
		return nil
	}
	for i := range roots {
		if st, err := fs.Stat(roots[i].FS, path.Join(name, "SKILL.md")); err == nil && !st.IsDir() {
			return &roots[i]
		}
	}
	return nil
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

func load(root Root, name string, values map[string]any) (Skill, *Problem) {
	files, err := fs.Sub(root.FS, name)
	if err != nil {
		return Skill{}, problem("unreadable", err)
	}
	raw, err := fs.ReadFile(files, "SKILL.md")
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
	if meta.Name != name || !namePattern.MatchString(meta.Name) {
		return Skill{}, problem("name", meta.Name)
	}
	if strings.TrimSpace(meta.Description) == "" {
		return Skill{}, problem("nodesc")
	}
	m := meta.Metadata.Mira
	s := Skill{Name: meta.Name, Description: strings.TrimSpace(meta.Description), Files: files,
		Settings: m.Settings, Privacy: m.Privacy}
	if root.Source {
		s.Source = root.Name
	} else {
		s.Dir = filepath.Join(root.Name, name)
	}
	if s.Privacy != "" && s.Privacy != config.PrivacyLocal {
		return Skill{}, problem("privacyword", s.Privacy)
	}

	if _, ok := m.Settings[config.SkillSchedules]; ok {
		return Skill{}, problem("schedulessetting")
	}
	changes, err := ScheduleChanges(values[config.SkillSchedules])
	if err != nil {
		return Skill{}, problem("scheduleentry", err)
	}
	vals := map[string]string{}
	for k, v := range values {
		if k == config.SkillSchedules {
			continue
		}
		if m.OAuth != nil && (k == SettingClientID || k == SettingClientSecret) {
			vals[k] = render(v)
			continue
		}
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
			return Skill{}, problem("required", k, decl.Description, meta.Name)
		}
	}
	named := map[string]bool{}
	for i, sc := range m.Schedules {
		c, err := schedule.Parse(sc.Cron)
		if err != nil {
			return Skill{}, problem("cron", i+1, err)
		}
		if strings.TrimSpace(sc.Task) == "" {
			return Skill{}, problem("notask", i+1)
		}
		sc.Name = strings.TrimSpace(sc.Name)
		if sc.Name != "" && named[sc.Name] {
			return Skill{}, problem("scheduledup", sc.Name)
		}
		named[sc.Name] = sc.Name != ""
		sc.cron, sc.Default = c, sc.Cron
		if ch, ok := changes[sc.Name]; ok && sc.Name != "" {
			if ch.Cron != "" {
				if err := sc.SetCron(ch.Cron); err != nil {
					return Skill{}, problem("schedulecron", sc.Name, err)
				}
			}
			sc.Off = ch.Off
		}
		sc.Task = fill(sc.Task, vals, m.Settings)
		sc.To = strings.TrimSpace(fill(sc.To, vals, m.Settings))
		if sc.To != "" && sc.PerPerson {
			return Skill{}, problem("toperperson", i+1)
		}
		s.Schedules = append(s.Schedules, sc)
	}
	for name := range changes {
		if !named[name] {
			return Skill{}, problem("scheduleunknown", name)
		}
	}
	if t := m.Target; t != nil {
		if t.Plan && (len(t.Fields) > 0 || t.Finish.Name != "") {
			return Skill{}, problem("targetplan")
		}
		if !t.Plan && (len(t.Fields) == 0 || t.Finish.Name == "") {
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
		if t.Bound != "" && t.Bound != "person" && t.Bound != BoundRoom && t.Bound != BoundHouse {
			return Skill{}, problem("targetbound", t.Bound)
		}
		if t.DoneAfter != "" {
			d, err := time.ParseDuration(t.DoneAfter)
			if err != nil || d <= 0 {
				return Skill{}, problem("targettimeout", t.DoneAfter)
			}
			t.doneAfter = d
		}
		s.Target = t
	}
	if m.Mail != nil {
		if strings.TrimSpace(m.Mail.Extract) == "" {
			return Skill{}, problem("mailextract")
		}
		mail := *m.Mail
		mail.Extract = fill(strings.TrimSpace(mail.Extract), vals, m.Settings)
		mail.Done = strings.TrimSpace(mail.Done)
		s.Mail = &mail
	}
	if m.Inbox != nil {
		if strings.TrimSpace(m.Inbox.Extract) == "" {
			return Skill{}, problem("inboxextract")
		}
		inbox := *m.Inbox
		inbox.Extract = fill(strings.TrimSpace(inbox.Extract), vals, m.Settings)
		s.Inbox = &inbox
	}
	names := make([]string, 0, len(m.Agents))
	for name := range m.Agents {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		node := m.Agents[name]
		d, err := config.DecodeAgent(&node)
		if err != nil {
			return Skill{}, problem("agent", name, err)
		}
		d.Description = fill(strings.TrimSpace(d.Description), vals, m.Settings)
		d.Instructions = fill(strings.TrimSpace(d.Instructions), vals, m.Settings)
		if d.Remote != nil {
			if p := remoteAgent(name, &d, s.Privacy, vals, m.Settings); p != nil {
				return Skill{}, p
			}
		} else if d.Name != "" {
			return Skill{}, problem("agentname", name)
		}
		if s.Privacy == config.PrivacyLocal {
			d.Privacy = config.PrivacyLocal
		}
		if s.Agents == nil {
			s.Agents = map[string]config.AgentDef{}
		}
		s.Agents[name] = d
	}
	if m.OAuth != nil {
		o, p := oauthSection(meta.Name, *m.OAuth, vals)
		if p != nil {
			return Skill{}, p
		}
		s.OAuth = &o
	}
	if m.MCP != nil {
		oauth := strings.TrimSpace(m.MCP.Token) == TokenOAuth
		if oauth && s.OAuth == nil {
			return Skill{}, problem("oauthmcp")
		}
		srv, p := mcpServer(meta.Name, *m.MCP, vals)
		if p != nil {
			return Skill{}, p
		}
		if oauth {
			srv.Token, srv.OAuth = "", true
		}
		s.MCP = &srv
	}
	if s.Automations, p = automations("automation", files, m.Automations, values, vals, m.Settings); p != nil {
		return Skill{}, p
	}
	if s.Scripts, p = automations("script", files, m.Scripts, values, vals, m.Settings); p != nil {
		return Skill{}, p
	}
	s.Body = fill(strings.TrimSpace(string(body)), vals, m.Settings)
	s.Chapter = strings.TrimSpace(m.Chapter)
	s.Takes = strings.TrimSpace(m.Takes)
	if s.Random, p = parseRandom(&m.Random, "SKILL.md"); p != nil {
		return Skill{}, p
	}
	if s.Lang, err = i18n.Parse(m.Language); err != nil {
		return Skill{}, problem("language", m.Language)
	}
	for _, l := range i18n.All {
		if l == s.Lang {
			continue
		}
		t, ok, p := readTranslation(files, l, vals, m.Settings)
		if p != nil {
			return Skill{}, p
		}
		if ok {
			if s.translations == nil {
				s.translations = map[i18n.Lang]translation{}
			}
			s.translations[l] = t
		}
	}
	return s, nil
}

// translation is a skill's texts in another language than its own. A text it
// leaves out keeps the skill's own.
type translation struct {
	Description, Body, Chapter string
	// Random replaces the skill's axes as a whole: their names and values
	// are all text (docs/spec/random-pick.md).
	Random []Axis
}

// readTranslation reads SKILL.<lang>.md: frontmatter with the translated
// texts by key (the first step: the description), the translated guide as
// its body, filled like the skill's own.
func readTranslation(files fs.FS, l i18n.Lang, vals map[string]string, decl map[string]Setting) (translation, bool, *Problem) {
	name := "SKILL." + string(l) + ".md"
	raw, err := fs.ReadFile(files, name)
	if errors.Is(err, fs.ErrNotExist) {
		return translation{}, false, nil
	}
	if err != nil {
		return translation{}, false, problem("translation", name, err)
	}
	fm, body, p := split(raw)
	if p != nil {
		return translation{}, false, problem("translation", name, p.kind)
	}
	var meta struct {
		Description string    `yaml:"description"`
		Chapter     string    `yaml:"chapter"`
		Random      yaml.Node `yaml:"random"`
	}
	if err := yaml.Unmarshal(fm, &meta); err != nil {
		return translation{}, false, problem("translation", name, err)
	}
	random, p := parseRandom(&meta.Random, name)
	if p != nil {
		return translation{}, false, p
	}
	return translation{
		Description: strings.TrimSpace(meta.Description),
		Chapter:     strings.TrimSpace(meta.Chapter),
		Body:        fill(strings.TrimSpace(string(body)), vals, decl),
		Random:      random,
	}, true, nil
}

// DescriptionIn is the skill's description in l: its translation's, else
// its own.
func (s Skill) DescriptionIn(l i18n.Lang) string {
	if t, ok := s.translations[l]; ok && t.Description != "" {
		return t.Description
	}
	return s.Description
}

// Chapters are the skill's chapter of Mira's guide by language: its own
// language's and its translations'; nil for none.
func (s Skill) Chapters() map[string]string {
	if s.Chapter == "" {
		return nil
	}
	out := map[string]string{string(s.Lang): s.Chapter}
	for l, t := range s.translations {
		if t.Chapter != "" {
			out[string(l)] = t.Chapter
		}
	}
	return out
}

// BodyIn is the skill's guide in l: its translation's, else its own.
func (s Skill) BodyIn(l i18n.Lang) string {
	if t, ok := s.translations[l]; ok && t.Body != "" {
		return t.Body
	}
	return s.Body
}

// SectionAnnounce is the heading of a skill's section that says how a
// message of the kind it takes up is announced (docs/spec/messages.md,
// decision 30).
const SectionAnnounce = "Ansage"

// Section is the text under the heading "## <heading>" of the body in a
// language, up to the next heading of the same level; "" when there is none.
func (s Skill) Section(l i18n.Lang, heading string) string {
	var out []string
	in := false
	for _, line := range strings.Split(s.BodyIn(l), "\n") {
		if strings.HasPrefix(line, "## ") {
			if in {
				break
			}
			in = strings.TrimSpace(strings.TrimPrefix(line, "## ")) == heading
			continue
		}
		if in {
			out = append(out, line)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
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

// mcpServer is a skill's mcp section as a server of its own: named after the
// skill, with every setting filled in, secret ones too (they go to the
// connection, never into a prompt).
func mcpServer(name string, srv config.MCPServer, vals map[string]string) (config.MCPServer, *Problem) {
	all := func(s string) string {
		return placeholder.ReplaceAllStringFunc(s, func(m string) string {
			return vals[placeholder.FindStringSubmatch(m)[1]]
		})
	}
	srv.Name, srv.Skill = name, name
	srv.URL, srv.Token, srv.Command = all(srv.URL), all(srv.Token), all(srv.Command)
	for i := range srv.Args {
		srv.Args[i] = all(srv.Args[i])
	}
	for i := range srv.Env {
		srv.Env[i] = all(srv.Env[i])
	}
	if srv.Transport == "" {
		srv.Transport = config.TransportHTTP
		if srv.Command != "" {
			srv.Transport = config.TransportStdio
		}
	}
	switch {
	case srv.Transport == config.TransportHTTP && strings.TrimSpace(srv.URL) == "":
		return srv, problem("mcpurl")
	case srv.Transport == config.TransportStdio && strings.TrimSpace(srv.Command) == "":
		return srv, problem("mcpcommand")
	case srv.Transport != config.TransportHTTP && srv.Transport != config.TransportStdio:
		return srv, problem("mcptransport", srv.Transport)
	}
	return srv, nil
}

// remoteAgent checks a remote agent of a skill (docs/spec/remote-agents.md,
// What a skill writes) and fills the skill's settings into it, secret ones
// too (they go to the remote agent, never into a prompt). The fields of a
// model agent do not go with remote: the remote agent runs its own model and
// tools.
func remoteAgent(name string, d *config.AgentDef, privacy string, vals map[string]string, decl map[string]Setting) *Problem {
	for _, f := range []struct {
		field string
		set   bool
	}{
		{"model", d.Model != ""}, {"privacy", d.Privacy != ""}, {"context", d.Context != ""},
		{"tools", d.Tools.Word != "" || len(d.Tools.Names) > 0}, {"rights", len(d.Rights) > 0},
		{"max_rounds", d.MaxRounds != 0}, {"instructions", d.Instructions != ""},
		{"conversation", d.Conversation}, {"input", len(d.Input) > 0}, {"output", len(d.Output) > 0},
	} {
		if f.set {
			return problem("remotefields", name, f.field)
		}
	}
	if privacy == config.PrivacyLocal {
		return problem("remoteprivacy", name)
	}
	all := func(s string) string {
		return strings.TrimSpace(placeholder.ReplaceAllStringFunc(s, func(m string) string {
			return vals[placeholder.FindStringSubmatch(m)[1]]
		}))
	}
	r := *d.Remote
	r.URL, r.Token, r.A2ASkill = strings.TrimRight(all(r.URL), "/"), all(r.Token), all(r.A2ASkill)
	if u, err := url.Parse(r.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return problem("remoteurl", name, r.URL)
	}
	d.Remote = &r
	d.Name = fill(strings.TrimSpace(d.Name), vals, decl)
	return nil
}

// Exists reports whether a skill folder of that name is in dirs.
func Exists(dirs []string, name string) bool { return find(dirs, name) != "" }

// ExistsIn reports whether a skill of that name is in roots.
func ExistsIn(roots []Root, name string) bool { return findIn(roots, name) != nil }

// Info is a skill folder's name, description and chapter of Mira's guide
// by language (as Skill.Chapters), read without loading it.
type Info struct {
	Name        string
	Description string
	Chapters    map[string]string
}

// Available are the skills in dirs that are not enabled, by name: what a
// user could switch on (the first folder of a name wins, as for Load).
func Available(dirs []string, enabled map[string]map[string]any) []Info {
	return AvailableIn(Roots(dirs), enabled)
}

// AvailableIn is Available over roots, a source's skills among them.
func AvailableIn(roots []Root, enabled map[string]map[string]any) []Info {
	seen := map[string]bool{}
	var out []Info
	for _, r := range roots {
		entries, err := fs.ReadDir(r.FS, ".")
		if err != nil {
			// docs/spec/fallbacks.md, row 29; a folder not made yet is no error.
			if !errors.Is(err, fs.ErrNotExist) {
				slog.Warn("could not read a skills folder; its skills are missing from the list", "dir", r.Name, "error", err)
			}
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if !e.IsDir() || seen[name] {
				continue
			}
			raw, err := fs.ReadFile(r.FS, path.Join(name, "SKILL.md"))
			if err != nil {
				// A folder without SKILL.md is no skill.
				if !errors.Is(err, fs.ErrNotExist) {
					slog.Warn("could not read a skill; it is missing from the list", "skill", name, "dir", r.Name, "error", err)
				}
				continue
			}
			seen[name] = true
			if _, on := enabled[name]; on {
				continue
			}
			info := Info{Name: name}
			if front, _, p := split(raw); p == nil {
				var fm frontmatter
				if yaml.Unmarshal(front, &fm) == nil {
					info.Description = strings.TrimSpace(fm.Description)
					if sub, err := fs.Sub(r.FS, name); err == nil {
						info.Chapters = availableChapters(sub, fm.Metadata.Mira.Language, fm.Metadata.Mira.Chapter)
					}
				}
			}
			out = append(out, info)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// availableChapters are the chapters of a skill not switched on: its own
// and those of its translations, as Skill.Chapters.
func availableChapters(files fs.FS, language, chapter string) map[string]string {
	own, err := i18n.Parse(language)
	if err != nil || strings.TrimSpace(chapter) == "" {
		return nil
	}
	out := map[string]string{string(own): strings.TrimSpace(chapter)}
	for _, l := range i18n.All {
		if l == own {
			continue
		}
		if t, ok, _ := readTranslation(files, l, nil, nil); ok && t.Chapter != "" {
			out[string(l)] = t.Chapter
		}
	}
	return out
}
