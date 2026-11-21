// Package skillha brings a skill's part into Home Assistant at Mira's start
// (docs/spec/skill-files.md): its skill blueprints, saved under
// mira_skills/<skill>/ when missing or of another version and only when
// their interface stays compatible, its skill automations, made from them
// with inputs from the skill's settings, and its skill scripts, made the
// same way, each a tool of its skill (step 5). It deletes nothing
// (decision 1). It reuses Mira's own way with blueprints (internal/watch: the
// version, the retries) and with her automations (internal/ha/automation).
package skillha

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"path"
	"slices"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/miraassist/mira/internal/excerpt"
	"github.com/miraassist/mira/internal/ha/automation"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/retry"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/watch"
)

// Folder is where skill blueprints live among Home Assistant's automation
// blueprints: outside Mira's own mira/ (decision 7).
const Folder = "mira_skills/"

// HA is what the sync needs of Home Assistant: its blueprint commands, the
// automation and script config APIs, the steps of saving one of Mira's
// automations, and one entity's state.
type HA interface {
	automation.HA
	AutomationConfig(ctx context.Context, id string) (map[string]any, error)
	ScriptConfig(ctx context.Context, id string) (map[string]any, error)
	SaveScriptConfig(ctx context.Context, id string, config map[string]any) error
	ServicesOf(ctx context.Context, domain string) (map[string]haws.Service, error)
	CallServiceResponse(ctx context.Context, domain, service string, data map[string]any) (json.RawMessage, error)
}

type Options struct {
	HA     HA
	Own    automation.Own
	Lang   i18n.Lang
	Logger *slog.Logger
	// Policy replaces watch.Policy (tests: no waits).
	Policy *retry.Policy
}

// Sync brings the switched-on skills' blueprints and automations into Home
// Assistant and keeps the problems it found for the repairs.
type Sync struct {
	opts   Options
	log    *slog.Logger
	policy retry.Policy

	mu       sync.Mutex
	problems []found
}

// found is a problem whose text is made in the language asked for.
type found struct {
	skill, kind string
	args        []any
	reasons     []reason
}

func New(o Options) *Sync {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	p := watch.Policy
	if o.Policy != nil {
		p = *o.Policy
	}
	return &Sync{opts: o, log: o.Logger.With("component", "skillha"), policy: p}
}

// AutomationID is the id of a skill automation in Home Assistant; a skill
// script has the same form (decision 20).
func AutomationID(skill, id string) string { return "mira_skill_" + skill + "_" + id }

// The blueprint domains a skill blueprint may have (decision 6).
const (
	domainAutomation = "automation"
	domainScript     = "script"
)

// Problems are the problems of the last Run, in l.
func (s *Sync) Problems(l i18n.Lang) []skills.Problem {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]skills.Problem, 0, len(s.problems))
	for _, f := range s.problems {
		args := f.args
		if f.reasons != nil {
			args = append(slices.Clone(args), reasonsText(f.reasons, l))
		}
		out = append(out, skills.NewProblem(f.skill, f.kind, args...))
	}
	return out
}

// blueprint is a skill blueprint as read from the skill's files.
type blueprint struct {
	domain  string // automation or script
	file    string // "zu_ruhig.yaml"
	path    string // "mira_skills/zu-ruhig/zu_ruhig.yaml", as Home Assistant lists it
	name    string // its blueprint name
	version int
	inputs  map[string]input
	yaml    string
}

// Run is steps 5 to 7 of the path: once, at the start, after Home
// Assistant is connected. Every failure is logged here and kept as a
// problem of its skill; the others go on. Tools (step 8) follows it.
func (s *Sync) Run(ctx context.Context, list []skills.Skill) {
	var problems []found
	add := func(f found) { problems = append(problems, f) }
	defer func() {
		s.mu.Lock()
		s.problems = problems
		s.mu.Unlock()
	}()

	type own struct {
		sk  skills.Skill
		bps []blueprint
	}
	var all []own
	for _, sk := range list {
		if sk.Files == nil {
			continue
		}
		files, err := fs.Glob(sk.Files, skills.BlueprintsFolder+"/*.yaml")
		if err != nil || len(files) == 0 {
			continue
		}
		o := own{sk: sk}
		for _, f := range files {
			b, err := readBlueprint(sk, f)
			if err != nil {
				s.log.Warn("skill blueprint not usable; not saved", "skill", sk.Name, "file", f, "error", err)
				add(found{skill: sk.Name, kind: skills.KindBlueprintUnusable, args: []any{f, err}})
				continue
			}
			o.bps = append(o.bps, b)
		}
		all = append(all, o)
	}
	if len(all) == 0 {
		return
	}

	// One blueprint/list per domain a skill blueprint has.
	have := map[string]map[string]listed{}
	failed := map[string]error{}
	for _, o := range all {
		for _, b := range o.bps {
			if _, done := have[b.domain]; done || failed[b.domain] != nil {
				continue
			}
			list, err := s.list(ctx, b.domain)
			if err != nil {
				s.log.Error("could not list home assistant's blueprints; no skill blueprint of this domain, and nothing made from one, is saved until the next start",
					"domain", b.domain, "error", err)
				failed[b.domain] = err
				continue
			}
			have[b.domain] = list
		}
	}
	for _, o := range all {
		listFailed := false
		ready := map[string]blueprint{}
		for _, b := range o.bps {
			if err := failed[b.domain]; err != nil {
				if !listFailed {
					add(found{skill: o.sk.Name, kind: skills.KindBlueprintList, args: []any{err}})
					listFailed = true
				}
				continue
			}
			if f, ok := s.blueprint(ctx, o.sk.Name, b, have[b.domain]); !ok {
				if f != nil {
					add(*f)
				}
				continue
			}
			ready[b.file] = b
		}
		for _, a := range o.sk.Automations {
			if f := s.automation(ctx, o.sk.Name, a, ready, o.bps); f != nil {
				add(*f)
			}
		}
		for _, a := range o.sk.Scripts {
			if f := s.script(ctx, o.sk.Name, a, ready, o.bps); f != nil {
				add(*f)
			}
		}
	}
}

// otherKind is decision 24: the declaration names a blueprint of the skill
// whose domain is not want; nil when it is not so.
func (s *Sync) otherKind(skill, want, id, file string, bps []blueprint) *found {
	for _, b := range bps {
		if b.file == file && b.domain != want {
			s.log.Warn("skill declaration names a blueprint of the other kind; not made", "skill", skill, "declared", want, "id", id,
				"blueprint", b.path, "domain", b.domain)
			list := "automations"
			if want == domainScript {
				list = "scripts"
			}
			return &found{skill: skill, kind: skills.KindBlueprintKind, args: []any{id, list, b.path, b.domain}}
		}
	}
	return nil
}

// listed is an entry of blueprint/list.
type listed struct {
	Metadata struct {
		Description string         `json:"description"`
		Input       map[string]any `json:"input"`
	} `json:"metadata"`
	Error string `json:"error"`
}

func (s *Sync) list(ctx context.Context, domain string) (map[string]listed, error) {
	var have map[string]listed
	err := retry.Do(ctx, s.policy, func(ctx context.Context) error {
		raw, err := s.opts.HA.Call(ctx, map[string]any{"type": "blueprint/list", "domain": domain})
		if err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &have); err != nil {
			return retry.Fatal(fmt.Errorf("blueprint/list answered %q: %w", excerpt.Bytes(raw), err))
		}
		return nil
	}, retry.Warn(s.log, "listing the blueprints", s.policy))
	return have, err
}

// blueprint makes sure Home Assistant has b in its version; false when it
// has not (with the problem, nil when there is none to add).
func (s *Sync) blueprint(ctx context.Context, skill string, b blueprint, have map[string]listed) (*found, bool) {
	entry, exists := have[b.path]
	haveVersion := watch.VersionOf(entry.Metadata.Description)
	if exists && entry.Error == "" {
		if haveVersion == b.version {
			return nil, true
		}
		if reasons := compare(inputsOf(entry.Metadata.Input), b.inputs); len(reasons) > 0 {
			s.log.Warn("skill blueprint refused: its new version does not fit the automations using it; the old one stays",
				"skill", skill, "blueprint", b.path, "have", haveVersion, "want", b.version, "inputs", reasonsText(reasons, i18n.EN))
			return &found{skill: skill, kind: skills.KindBlueprintRefused, args: []any{b.path}, reasons: reasons}, false
		}
	}
	cmd := map[string]any{"type": "blueprint/save", "domain": b.domain, "path": strings.TrimSuffix(b.path, ".yaml"), "yaml": b.yaml}
	if exists {
		cmd["allow_override"] = true
	}
	err := retry.Do(ctx, s.policy, func(ctx context.Context) error {
		_, err := s.opts.HA.Call(ctx, cmd)
		return err
	}, retry.Warn(s.log, "saving a skill blueprint", s.policy, "blueprint", b.path))
	if err != nil {
		s.log.Error("could not save a skill blueprint; its automations are not made or changed", "skill", skill, "blueprint", b.path,
			"replacing", exists, "error", err)
		return &found{skill: skill, kind: skills.KindBlueprintSave, args: []any{b.path, err}}, false
	}
	if exists {
		s.log.Info("skill blueprint updated; home assistant reloads the automations using it", "skill", skill, "blueprint", b.path,
			"from", haveVersion, "to", b.version)
	} else {
		s.log.Info("skill blueprint saved", "skill", skill, "blueprint", b.path, "version", b.version)
	}
	return nil, true
}

// automation makes a skill automation, or makes it anew when its
// use_blueprint differs (decision 13); nil when it is as the skill wants it.
func (s *Sync) automation(ctx context.Context, skill string, a skills.Automation, ready map[string]blueprint, bps []blueprint) *found {
	if f := s.otherKind(skill, domainAutomation, a.ID, a.Blueprint, bps); f != nil {
		return f
	}
	b, ok := ready[a.Blueprint]
	if !ok {
		// Its blueprint's problem is already kept.
		s.log.Info("skill automation left as it is: its blueprint is not in home assistant in the skill's version",
			"skill", skill, "automation", a.ID, "blueprint", a.Blueprint)
		return nil
	}
	for _, name := range slices.Sorted(maps.Keys(a.Inputs)) {
		if _, ok := b.inputs[name]; !ok {
			s.log.Warn("skill automation not made: the blueprint has no such input", "skill", skill, "automation", a.ID, "blueprint", b.path, "input", name)
			return &found{skill: skill, kind: skills.KindAutomationInput, args: []any{a.ID, name, b.path}}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(b.inputs)) {
		if _, given := a.Inputs[name]; !given && !b.inputs[name].hasDefault {
			s.log.Warn("skill automation not made: a required input has no value", "skill", skill, "automation", a.ID, "blueprint", b.path, "input", name)
			return &found{skill: skill, kind: skills.KindAutomationNeeds, args: []any{a.ID, name, b.path}}
		}
	}
	id := AutomationID(skill, a.ID)
	want := map[string]any{"path": b.path, "input": a.Inputs}
	var current map[string]any
	err := retry.Do(ctx, s.policy, func(ctx context.Context) error {
		var err error
		current, err = s.opts.HA.AutomationConfig(ctx, id)
		if errors.Is(err, haws.ErrNoAutomation) || errors.Is(err, haws.ErrNotAdmin) {
			return retry.Fatal(err)
		}
		return err
	}, retry.Warn(s.log, "reading a skill automation", s.policy, "automation", id))
	switch {
	case errors.Is(err, haws.ErrNoAutomation):
		current = nil
	case err != nil:
		s.log.Error("could not read a skill automation; it is not made or changed until the next start", "skill", skill, "automation", id, "error", err)
		return &found{skill: skill, kind: skills.KindAutomationRead, args: []any{a.ID, err}}
	case same(current["use_blueprint"], want):
		return nil
	}
	config := map[string]any{}
	if current != nil {
		// Only use_blueprint is replaced: the alias and the description
		// the user may have changed stay (decision 13).
		maps.Copy(config, current)
		delete(config, "id")
	} else {
		config["alias"] = cmp(a.Alias, b.name, a.ID)
		config["description"] = cmp(a.Description, fmt.Sprintf(i18n.Pick(descriptions, s.opts.Lang), skill))
	}
	config["use_blueprint"] = want
	if err := automation.Save(ctx, s.opts.HA, s.opts.Own, s.log, id, config, automation.LabelTask); err != nil {
		// Logged by automation.Save with what failed.
		return &found{skill: skill, kind: skills.KindAutomationSave, args: []any{a.ID, err}}
	}
	if current == nil {
		s.log.Info("skill automation made", "skill", skill, "automation", id, "blueprint", b.path)
	} else {
		s.log.Info("skill automation made anew: its inputs differed", "skill", skill, "automation", id, "blueprint", b.path)
	}
	return nil
}

// script makes a skill script, or makes it anew when its use_blueprint
// differs, as an automation (decision 18), and loads it (decision 23); it
// is not exposed (decision 19). nil when it is as the skill wants it or
// made without a problem.
func (s *Sync) script(ctx context.Context, skill string, a skills.Automation, ready map[string]blueprint, bps []blueprint) *found {
	if f := s.otherKind(skill, domainScript, a.ID, a.Blueprint, bps); f != nil {
		return f
	}
	b, ok := ready[a.Blueprint]
	if !ok {
		s.log.Info("skill script left as it is: its blueprint is not in home assistant in the skill's version",
			"skill", skill, "script", a.ID, "blueprint", a.Blueprint)
		return nil
	}
	for _, name := range slices.Sorted(maps.Keys(a.Inputs)) {
		if _, ok := b.inputs[name]; !ok {
			s.log.Warn("skill script not made: the blueprint has no such input", "skill", skill, "script", a.ID, "blueprint", b.path, "input", name)
			return &found{skill: skill, kind: skills.KindScriptInput, args: []any{a.ID, name, b.path}}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(b.inputs)) {
		if _, given := a.Inputs[name]; !given && !b.inputs[name].hasDefault {
			s.log.Warn("skill script not made: a required input has no value", "skill", skill, "script", a.ID, "blueprint", b.path, "input", name)
			return &found{skill: skill, kind: skills.KindScriptNeeds, args: []any{a.ID, name, b.path}}
		}
	}
	id := AutomationID(skill, a.ID)
	want := map[string]any{"path": b.path, "input": a.Inputs}
	var current map[string]any
	err := retry.Do(ctx, s.policy, func(ctx context.Context) error {
		var err error
		current, err = s.opts.HA.ScriptConfig(ctx, id)
		if errors.Is(err, haws.ErrNoScript) || errors.Is(err, haws.ErrNotAdmin) {
			return retry.Fatal(err)
		}
		return err
	}, retry.Warn(s.log, "reading a skill script", s.policy, "script", id))
	switch {
	case errors.Is(err, haws.ErrNoScript):
		current = nil
	case err != nil:
		s.log.Error("could not read a skill script; it is not made or changed until the next start", "skill", skill, "script", id, "error", err)
		return &found{skill: skill, kind: skills.KindScriptRead, args: []any{a.ID, err}}
	case same(current["use_blueprint"], want):
		return nil
	}
	config := map[string]any{}
	if current != nil {
		// Only use_blueprint is replaced: the alias and the description
		// the user may have changed stay (decision 13).
		maps.Copy(config, current)
		delete(config, "id")
	} else {
		config["alias"] = cmp(a.Alias, b.name, a.ID)
		config["description"] = cmp(a.Description, fmt.Sprintf(i18n.Pick(descriptions, s.opts.Lang), skill))
	}
	config["use_blueprint"] = want
	if err := s.opts.HA.SaveScriptConfig(ctx, id, config); err != nil {
		s.log.Error("could not save a skill script; it is not made or changed until the next start", "skill", skill, "script", id, "error", err)
		return &found{skill: skill, kind: skills.KindScriptSave, args: []any{a.ID, err}}
	}
	// Saving answers before the script is loaded; the reload returns once
	// it is (decision 23, checked rows 5 and 6).
	if err := s.opts.HA.CallService(ctx, "script", "reload", map[string]any{}, nil); err != nil {
		s.log.Error("a skill script was saved but home assistant did not reload the scripts; it may be missing until its next reload", "skill", skill, "script", id, "error", err)
		return &found{skill: skill, kind: skills.KindScriptSave, args: []any{a.ID, err}}
	}
	if current == nil {
		s.log.Info("skill script made", "skill", skill, "script", id, "blueprint", b.path)
	} else {
		s.log.Info("skill script made anew: its inputs differed", "skill", skill, "script", id, "blueprint", b.path)
	}
	return nil
}

var descriptions = map[i18n.Lang]string{
	i18n.DE: "Von Mira angelegt für den Skill %s; die Eingaben kommen aus seinen Einstellungen.",
	i18n.EN: "Made by Mira for the skill %s; its inputs come from the skill's settings.",
}

func cmp(s ...string) string {
	for _, v := range s {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// same compares two use_blueprint values as JSON: a number from YAML and
// one from Home Assistant's JSON are the same.
func same(a, b any) bool {
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && string(x) == string(y)
}

// readBlueprint reads a skill blueprint: an automation blueprint with a
// mira-version and its inputs.
func readBlueprint(sk skills.Skill, file string) (blueprint, error) {
	raw, err := fs.ReadFile(sk.Files, file)
	if err != nil {
		return blueprint{}, err
	}
	var doc struct {
		Blueprint struct {
			Name        string    `yaml:"name"`
			Description string    `yaml:"description"`
			Domain      string    `yaml:"domain"`
			Input       yaml.Node `yaml:"input"`
		} `yaml:"blueprint"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return blueprint{}, fmt.Errorf("not YAML: %w", err)
	}
	if err := onlyInputTags(raw); err != nil {
		return blueprint{}, err
	}
	if doc.Blueprint.Domain != domainAutomation && doc.Blueprint.Domain != domainScript {
		return blueprint{}, fmt.Errorf("domain %q, neither automation nor script", doc.Blueprint.Domain)
	}
	b := blueprint{domain: doc.Blueprint.Domain, file: path.Base(file), name: doc.Blueprint.Name, version: watch.VersionOf(doc.Blueprint.Description), yaml: string(raw)}
	b.path = Folder + sk.Name + "/" + b.file
	if b.version == 0 {
		return blueprint{}, errors.New("no mira-version in its description")
	}
	var inputs map[string]any
	if doc.Blueprint.Input.Kind != 0 {
		if err := doc.Blueprint.Input.Decode(&inputs); err != nil {
			return blueprint{}, fmt.Errorf("its inputs: %w", err)
		}
	}
	b.inputs = inputsOf(inputs)
	return b, nil
}

// onlyInputTags refuses a blueprint with a YAML tag of Home Assistant's
// other than !input (docs/spec/skill-files.md, decision 28): Home Assistant
// resolves !include, !include_dir_*, !secret and !env_var while it saves a
// blueprint and writes what they read into the file, so a skill could read
// secrets.yaml, .storage or an environment variable such as the
// Supervisor's token, which no automation's template reaches.
func onlyInputTags(raw []byte) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("not YAML: %w", err)
	}
	var walk func(n *yaml.Node) error
	walk = func(n *yaml.Node) error {
		if strings.HasPrefix(n.Tag, "!") && !strings.HasPrefix(n.Tag, "!!") && n.Tag != "!input" {
			return fmt.Errorf("the YAML tag %s (line %d) is not allowed in a skill blueprint, only !input", n.Tag, n.Line)
		}
		for _, c := range n.Content {
			if err := walk(c); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(&doc)
}

// input is what the interface keeps of an input (docs/spec/skill-files.md,
// The interface).
type input struct {
	selector   string
	domains    []string // an entity selector's; none for any
	hasDefault bool
}

// inputsOf reads a blueprint's inputs, from its file or from Home
// Assistant's list alike; the inputs of a section count as inputs.
func inputsOf(m map[string]any) map[string]input {
	out := map[string]input{}
	for name, v := range m {
		spec, _ := v.(map[string]any)
		if sub, ok := spec["input"].(map[string]any); ok && spec["selector"] == nil {
			maps.Copy(out, inputsOf(sub))
			continue
		}
		in := input{}
		_, in.hasDefault = spec["default"]
		sel, _ := spec["selector"].(map[string]any)
		// A selector has one type, its only key.
		if keys := slices.Sorted(maps.Keys(sel)); len(keys) > 0 {
			in.selector = keys[0]
			if in.selector == "entity" {
				in.domains = entityDomains(sel["entity"])
			}
		}
		out[name] = in
	}
	return out
}

// entityDomains are the domains an entity selector allows: its domain and
// those of its filters; none for any.
func entityDomains(v any) []string {
	var out []string
	addAll := func(d any) {
		switch x := d.(type) {
		case string:
			out = append(out, x)
		case []any:
			for _, e := range x {
				if s, ok := e.(string); ok {
					out = append(out, s)
				}
			}
		}
	}
	sel, _ := v.(map[string]any)
	addAll(sel["domain"])
	switch f := sel["filter"].(type) {
	case map[string]any:
		addAll(f["domain"])
	case []any:
		for _, e := range f {
			if m, ok := e.(map[string]any); ok {
				addAll(m["domain"])
			}
		}
	}
	sort.Strings(out)
	return slices.Compact(out)
}

// reason is one incompatibility of an update.
type reason struct {
	kind, input, was, is string
}

// compare is decision 10: why the update from old to new is incompatible;
// none when it is compatible.
func compare(old, new map[string]input) []reason {
	var out []reason
	for _, name := range slices.Sorted(maps.Keys(old)) {
		o := old[name]
		n, ok := new[name]
		switch {
		case !ok:
			out = append(out, reason{kind: "removed", input: name})
			continue
		case n.selector != o.selector:
			out = append(out, reason{kind: "selector", input: name, was: o.selector, is: n.selector})
		case n.selector == "entity" && narrowed(o.domains, n.domains):
			out = append(out, reason{kind: "domains", input: name, was: strings.Join(o.domains, ", "), is: strings.Join(n.domains, ", ")})
		}
		if o.hasDefault && !n.hasDefault {
			out = append(out, reason{kind: "nodefault", input: name})
		}
	}
	for _, name := range slices.Sorted(maps.Keys(new)) {
		if _, ok := old[name]; !ok && !new[name].hasDefault {
			out = append(out, reason{kind: "newrequired", input: name})
		}
	}
	return out
}

// narrowed: the new domains do not allow every one the old allow (none
// meaning any).
func narrowed(old, new []string) bool {
	if len(new) == 0 {
		return false
	}
	if len(old) == 0 {
		return true
	}
	for _, d := range old {
		if !slices.Contains(new, d) {
			return true
		}
	}
	return false
}

var reasonTexts = map[i18n.Lang]map[string]string{
	i18n.DE: {
		"removed":     "„%[1]s“ fehlt",
		"selector":    "„%[1]s“ ist jetzt %[3]s statt %[2]s",
		"domains":     "„%[1]s“ nimmt nur noch %[3]s statt %[2]s",
		"nodefault":   "„%[1]s“ hat keinen Vorgabewert mehr",
		"newrequired": "„%[1]s“ ist neu und hat keinen Vorgabewert",
	},
	i18n.EN: {
		"removed":     "\"%[1]s\" removed",
		"selector":    "\"%[1]s\" is %[3]s instead of %[2]s",
		"domains":     "\"%[1]s\" takes only %[3]s instead of %[2]s",
		"nodefault":   "\"%[1]s\" has no default any more",
		"newrequired": "\"%[1]s\" is new and has no default",
	},
}

func reasonsText(rs []reason, l i18n.Lang) string {
	t := i18n.Pick(reasonTexts, l)
	parts := make([]string, 0, len(rs))
	for _, r := range rs {
		was, is := r.was, r.is
		if was == "" {
			was = "-"
		}
		if is == "" {
			is = "-"
		}
		parts = append(parts, fmt.Sprintf(t[r.kind], r.input, was, is))
	}
	return strings.Join(parts, "; ")
}
