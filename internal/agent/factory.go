package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
)

// The factory (docs/spec/agenten.md, Factory): agents described in YAML, in
// mira.yaml under agents and in skills under metadata.mira.agents. Each is
// checked on its own; one that does not fit is left out with a problem that
// says why, and Mira starts all the same.

// Defined is a YAML agent as it was found.
type Defined struct {
	Name string
	// Skill is the skill it comes from; empty for mira.yaml.
	Skill string
	Def   config.AgentDef
	// Err is why its YAML could not be read, if it could not.
	Err error
}

// FromConfig are the agents in mira.yaml, read one by one.
func FromConfig(nodes map[string]yaml.Node) []Defined {
	var out []Defined
	for _, name := range sortedKeys(nodes) {
		node := nodes[name]
		d, err := config.DecodeAgent(&node)
		out = append(out, Defined{Name: name, Def: d, Err: err})
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Problem is why a YAML agent did not start.
type Problem struct {
	Agent string
	// Skill is where it is described; empty for mira.yaml.
	Skill string
	kind  string
	args  []any
}

// Text is the problem in a language, without the agent's name.
func (p Problem) Text(l i18n.Lang) string {
	format, ok := i18n.Pick(problemTexts, l)[p.kind]
	if !ok {
		return p.kind
	}
	return fmt.Sprintf(format, p.args...)
}

// Callable are Mira's own agents with a fixed description, callable by
// name (Decided 12); cmd/mira registers exactly these.
var Callable = []string{"delegate", "skill.task", "notify.compose"}

// codeAgents are the agents of the code; a YAML agent may not take their
// names.
var codeAgents = []string{"conversation.voice", "conversation.telegram", "delegate", "skill.task", "notify.compose",
	"mail.read", "mail.match", "tasks.assess", "tasks.decide", "tasks.plan", "profile.compact"}

// OwnNames are Mira's own agents, in a fixed order.
func OwnNames() []string { return slices.Clone(codeAgents) }

// Own reports whether a name is one of Mira's own agents: its entry under
// agents names only its model and privacy.
func Own(name string) bool { return slices.Contains(codeAgents, name) }

var agentName = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*$`)

// RegisterDefined checks the YAML agents and registers those that fit.
// models gives a model's settings by name.
func (r *Runtime) RegisterDefined(defs []Defined, models func(name string) (config.LLM, bool)) []Problem {
	var problems []Problem
	seen := map[string]bool{}
	for _, d := range defs {
		_, registered := r.lookup(d.Name)
		if !Own(d.Name) || d.Skill != "" {
			if seen[d.Name] || registered {
				problems = append(problems, Problem{Agent: d.Name, Skill: d.Skill, kind: "taken", args: []any{d.Name}})
				continue
			}
		}
		spec, p := check(d, models)
		if p != nil {
			problems = append(problems, *p)
			continue
		}
		if spec == nil {
			continue
		}
		seen[d.Name] = true
		r.Register(*spec)
	}
	return problems
}

// Check is what Mira says about a YAML agent when she starts, without
// registering it: nil when it fits (Mira's page checks before it writes).
func Check(d Defined, models func(name string) (config.LLM, bool)) *Problem {
	_, p := check(d, models)
	return p
}

// check reads one YAML agent: its spec, nil for an entry of one of Mira's
// own agents, or what does not fit.
func check(d Defined, models func(name string) (config.LLM, bool)) (*Spec, *Problem) {
	fail := func(kind string, args ...any) (*Spec, *Problem) {
		return nil, &Problem{Agent: d.Name, Skill: d.Skill, kind: kind, args: args}
	}
	switch {
	case d.Err != nil:
		return fail("yaml", d.Err)
	case slices.Contains(codeAgents, d.Name) && d.Skill == "":
		// One of Mira's own agents: mira.yaml names only its model and
		// privacy (docs/spec/modelle.md).
		model, known := models(d.Def.Model)
		switch {
		case ownExtra(d.Def) != "":
			return fail("ownfields", ownExtra(d.Def))
		case d.Def.Model != "" && !known:
			return fail("ownmodel", d.Def.Model)
		case d.Def.Privacy != "" && d.Def.Privacy != config.PrivacyLocal:
			return fail("privacyword", d.Def.Privacy)
		case d.Def.Privacy == config.PrivacyLocal && model.Privacy != config.PrivacyLocal:
			return fail("ownprivacy", modelName(d.Def.Model), model.Privacy)
		}
		return nil, nil
	case !agentName.MatchString(d.Name):
		return fail("name", d.Name)
	case slices.Contains(codeAgents, d.Name):
		return fail("taken", d.Name)
	}
	spec, kind, args := specOf(d, models)
	if kind != "" {
		return fail(kind, args...)
	}
	return &spec, nil
}

// modelName is a model's name as an agent's entry gives it; empty is main.
func modelName(name string) string {
	if name == "" {
		return config.MainModel
	}
	return name
}

// ownExtra names a field an entry for one of Mira's own agents may not set.
func ownExtra(d config.AgentDef) string {
	switch {
	case d.Description != "":
		return "description"
	case d.Instructions != "":
		return "instructions"
	case d.Context != "":
		return "context"
	case d.Tools.Word != "" || len(d.Tools.Names) > 0:
		return "tools"
	case len(d.Rights) > 0:
		return "rights"
	case len(d.Input) > 0:
		return "input"
	case len(d.Output) > 0:
		return "output"
	}
	return ""
}

// specOf turns a YAML description into a Spec, or says what does not fit.
// maxRounds is the most tool rounds a YAML agent may ask for.
const maxRounds = 50

func specOf(d Defined, models func(string) (config.LLM, bool)) (Spec, string, []any) {
	def := d.Def
	spec := Spec{Name: d.Name, Description: strings.TrimSpace(def.Description),
		Instructions: strings.TrimSpace(def.Instructions), Model: def.Model, Skill: d.Skill}
	if spec.Description == "" {
		return spec, "nodesc", nil
	}
	if spec.Instructions == "" {
		return spec, "noinstructions", nil
	}
	switch def.Context {
	case "", "isolated":
		spec.Profile = Isolated
	case "background":
		spec.Profile = Background
	default:
		return spec, "context", []any{def.Context}
	}
	if slices.Contains(codeAgents, spec.Model) {
		// The model of one of Mira's own agents (model: delegate): whatever
		// mira.yaml names for it when the agent runs, without a setting of
		// its own.
		spec.ModelOf, spec.Model = spec.Model, ""
		switch def.Privacy {
		case "":
		case config.PrivacyLocal:
			spec.Privacy = config.PrivacyLocal
		default:
			return spec, "privacyword", []any{def.Privacy}
		}
	} else {
		if spec.Model == "" {
			spec.Model = config.MainModel
		}
		model, known := models(spec.Model)
		if !known {
			return spec, "model", []any{spec.Model}
		}
		switch def.Privacy {
		case "":
		case config.PrivacyLocal:
			if model.Privacy != config.PrivacyLocal {
				return spec, "privacy", []any{spec.Model, model.Privacy}
			}
			spec.Privacy = config.PrivacyLocal
		default:
			return spec, "privacyword", []any{def.Privacy}
		}
	}
	switch {
	case len(def.Tools.Names) > 0:
		spec.Tools = Tools{Kind: ListedTools, Names: def.Tools.Names}
	case def.Tools.Word == "" || def.Tools.Word == "none":
		spec.Tools = Tools{Kind: NoTools}
	case def.Tools.Word == "read_only":
		spec.Tools = Tools{Kind: ReadOnlyTools}
	default:
		return spec, "tools", []any{def.Tools.Word}
	}
	if spec.Profile == Isolated && spec.Tools.Kind != NoTools {
		return spec, "isolatedtools", nil
	}
	if def.MaxRounds < 0 || def.MaxRounds > maxRounds {
		return spec, "maxrounds", []any{def.MaxRounds, maxRounds}
	}
	spec.Limits.MaxRounds = def.MaxRounds
	spec.Rights = ReadHouse
	if len(def.Rights) > 0 {
		rights, unknown := ParseRights(def.Rights)
		if len(unknown) > 0 {
			return spec, "rights", []any{strings.Join(unknown, ", ")}
		}
		spec.Rights = rights
	}
	for _, part := range []struct {
		schema map[string]any
		into   *json.RawMessage
	}{{def.Input, &spec.Input}, {def.Output, &spec.Output}} {
		if len(part.schema) == 0 {
			continue
		}
		raw, err := json.Marshal(part.schema)
		if err != nil {
			return spec, "schema", []any{err}
		}
		*part.into = raw
	}
	return spec, "", nil
}

type problemTable map[string]string

var problemTexts = map[i18n.Lang]problemTable{
	i18n.DE: {
		"yaml":           "Die Beschreibung ist so nicht lesbar (%s). Meist ist ein Feldname falsch geschrieben oder eine Einrückung stimmt nicht.",
		"name":           "Der Name „%s“ geht nicht: kleine Wörter, verbunden mit Punkt, Bindestrich oder Unterstrich.",
		"taken":          "Den Namen „%s“ hat schon ein anderer Agent.",
		"nodesc":         "Es fehlt die Beschreibung (description): wann man diesen Agenten ruft.",
		"noinstructions": "Es fehlt die Anleitung (instructions): was der Agent tun soll.",
		"context":        "Der Kontext „%s“ geht hier nicht. Ein Agent aus YAML arbeitet isolated oder background; conversation bleibt bei den beiden Gesprächs-Agenten.",
		"model":          "Das Modell „%s“ gibt es nicht. Trag es unter models ein, oder lass model weg, dann nimmt der Agent main.",
		"tools":          "tools ist „%s“; möglich sind none, read_only oder eine Liste von Werkzeugen und Agenten.",
		"isolatedtools":  "Ein Agent mit context isolated hat keine Werkzeuge. Lass tools weg, oder nimm context background.",
		"maxrounds":      "max_rounds %d geht nicht: erlaubt sind 1 bis %d.",
		"rights":         "Diese Rechte gibt es nicht: %s. Möglich sind read_house, act_on_house, remember, messages, web.",
		"schema":         "input oder output ist kein gültiges Schema (%s).",
		"ownfields":      "Für Miras eigene Agenten gehen hier nur model und privacy, nicht %s.",
		"ownmodel":       "Das Modell „%s“ gibt es nicht; der Agent nimmt main. Trag es unter models ein.",
		"privacy":        "Der Agent muss lokal laufen (privacy: local), aber sein Modell „%s“ ist %s. Nimm ein Modell mit privacy: local.",
		"ownprivacy":     "Der Agent muss lokal laufen (privacy: local), aber sein Modell „%s“ ist %s; jeder Aufruf scheitert. Nimm ein Modell mit privacy: local.",
		"privacyword":    "privacy ist „%s“; hier geht nur local.",
	},
	i18n.EN: {
		"yaml":           "The description cannot be read like this (%s). Usually a field name is misspelt or an indent is wrong.",
		"name":           "The name \"%s\" does not work: lower-case words joined by dot, hyphen or underscore.",
		"taken":          "Another agent already has the name \"%s\".",
		"nodesc":         "The description is missing: when to call this agent.",
		"noinstructions": "The instructions are missing: what the agent is to do.",
		"context":        "The context \"%s\" does not work here. An agent from YAML works isolated or background; conversation stays with the two conversation agents.",
		"model":          "There is no model \"%s\". Add it under models, or leave model out and the agent takes main.",
		"tools":          "tools is \"%s\"; possible are none, read_only or a list of tools and agents.",
		"isolatedtools":  "An agent with context isolated has no tools. Leave tools out, or take context background.",
		"maxrounds":      "max_rounds %d does not work: 1 to %d are allowed.",
		"rights":         "These rights do not exist: %s. Possible are read_house, act_on_house, remember, messages, web.",
		"schema":         "input or output is not a valid schema (%s).",
		"ownfields":      "For Mira's own agents only model and privacy go here, not %s.",
		"ownmodel":       "There is no model \"%s\"; the agent takes main. Add it under models.",
		"privacy":        "The agent must run locally (privacy: local), but its model \"%s\" is %s. Take a model with privacy: local.",
		"ownprivacy":     "The agent must run locally (privacy: local), but its model \"%s\" is %s; every call fails. Take a model with privacy: local.",
		"privacyword":    "privacy is \"%s\"; only local goes here.",
	},
}
