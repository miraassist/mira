package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

// ToolRandomPick draws a skill's axes by chance: a model cannot draw, but
// fills a narrow request differently each time (docs/spec/random-pick.md).
const ToolRandomPick = "random_pick"

// maxPick is the most lines one call draws.
const maxPick = 30

// Axis is one of a skill's axes (metadata.mira.random): a list of values, or
// values with a list of sub-values each (Subs set).
type Axis struct {
	Name   string
	Values []string
	Subs   map[string][]string
}

// parseRandom reads a random block; file names the file for a problem. An
// absent block is no axes and no problem.
func parseRandom(node *yaml.Node, file string) ([]Axis, *Problem) {
	if node == nil || node.Kind == 0 {
		return nil, nil
	}
	if node.Kind != yaml.MappingNode || len(node.Content) == 0 {
		return nil, problem("randomnone", file)
	}
	var out []Axis
	for i := 0; i+1 < len(node.Content); i += 2 {
		name := strings.TrimSpace(node.Content[i].Value)
		val := node.Content[i+1]
		a := Axis{Name: name}
		switch val.Kind {
		case yaml.SequenceNode:
			vals, ok := scalars(val)
			if !ok {
				return nil, problem("randomaxis", file, name)
			}
			if len(vals) == 0 {
				return nil, problem("randomempty", file, name)
			}
			a.Values = vals
		case yaml.MappingNode:
			if len(val.Content) == 0 {
				return nil, problem("randomempty", file, name)
			}
			a.Subs = map[string][]string{}
			for j := 0; j+1 < len(val.Content); j += 2 {
				key := strings.TrimSpace(val.Content[j].Value)
				subs, ok := scalars(val.Content[j+1])
				if !ok || val.Content[j].Kind != yaml.ScalarNode || key == "" {
					return nil, problem("randomaxis", file, name)
				}
				if len(subs) == 0 {
					return nil, problem("randomempty", file, name)
				}
				a.Values = append(a.Values, key)
				a.Subs[key] = subs
			}
		default:
			return nil, problem("randomaxis", file, name)
		}
		if name == "" {
			return nil, problem("randomaxis", file, name)
		}
		out = append(out, a)
	}
	return out, nil
}

// scalars are a sequence's values; false when it is no sequence of short
// texts.
func scalars(n *yaml.Node) ([]string, bool) {
	if n.Kind != yaml.SequenceNode {
		return nil, false
	}
	out := make([]string, 0, len(n.Content))
	for _, c := range n.Content {
		v := strings.TrimSpace(c.Value)
		if c.Kind != yaml.ScalarNode || v == "" {
			return nil, false
		}
		out = append(out, v)
	}
	return out, true
}

// AxesIn are the skill's axes in l: its translation's, else its own.
func (s Skill) AxesIn(l i18n.Lang) []Axis {
	if t, ok := s.translations[l]; ok && len(t.Random) > 0 {
		return t.Random
	}
	return s.Random
}

// hasAxes says whether the skill has axes in any language.
func (s Skill) hasAxes() bool {
	if len(s.Random) > 0 {
		return true
	}
	for _, t := range s.translations {
		if len(t.Random) > 0 {
			return true
		}
	}
	return false
}

// bag deals values in a shuffled order and shuffles anew once all were
// dealt: a value comes again only after all others.
type bag struct {
	values []string
	next   []string
}

func (b *bag) deal() string {
	if len(b.next) == 0 {
		b.next = append([]string(nil), b.values...)
		rand.Shuffle(len(b.next), func(i, j int) { b.next[i], b.next[j] = b.next[j], b.next[i] })
	}
	v := b.next[0]
	b.next = b.next[1:]
	return v
}

// Pick draws count lines over axes; fixed gives an axis's value by its name
// (any case). It fails for an axis the skill does not have.
func Pick(axes []Axis, fixed map[string]string, count int) ([]string, error) {
	byName := map[string]string{}
	for k, v := range fixed {
		byName[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
	}
	cols := make([][]string, len(axes))
	for i, a := range axes {
		v, isFixed := byName[strings.ToLower(a.Name)]
		delete(byName, strings.ToLower(a.Name))
		cols[i] = drawAxis(a, v, isFixed, count)
	}
	if len(byName) > 0 {
		for k := range byName {
			return nil, unknownAxis{name: k, axes: axes}
		}
	}
	lines := make([]string, count)
	for n := range lines {
		parts := make([]string, len(axes))
		for i, a := range axes {
			parts[i] = a.Name + ": " + cols[i][n]
		}
		lines[n] = fmt.Sprintf("%d. %s", n+1, strings.Join(parts, " · "))
	}
	return lines, nil
}

// drawAxis is one axis's count values: fixed as given (a map value with its
// sub-values drawn, a sub-value with its value), else drawn.
func drawAxis(a Axis, v string, isFixed bool, count int) []string {
	out := make([]string, count)
	if isFixed && a.Subs == nil {
		for i := range out {
			out[i] = v
		}
		return out
	}
	if isFixed {
		key, sub := findValue(a, v)
		switch {
		case key != "" && sub == "":
			b := &bag{values: a.Subs[key]}
			for i := range out {
				out[i] = key + " / " + b.deal()
			}
		case key != "":
			for i := range out {
				out[i] = key + " / " + sub
			}
		default:
			for i := range out {
				out[i] = v
			}
		}
		return out
	}
	top := &bag{values: a.Values}
	subs := map[string]*bag{}
	for i := range out {
		val := top.deal()
		if a.Subs == nil {
			out[i] = val
			continue
		}
		if subs[val] == nil {
			subs[val] = &bag{values: a.Subs[val]}
		}
		out[i] = val + " / " + subs[val].deal()
	}
	return out
}

// findValue finds v among a map axis's values (key) or sub-values (key and
// sub), ignoring case.
func findValue(a Axis, v string) (key, sub string) {
	for _, k := range a.Values {
		if strings.EqualFold(k, v) {
			return k, ""
		}
	}
	for _, k := range a.Values {
		for _, s := range a.Subs[k] {
			if strings.EqualFold(s, v) {
				return k, s
			}
		}
	}
	return "", ""
}

type unknownAxis struct {
	name string
	axes []Axis
}

func (u unknownAxis) Error() string { return "unknown axis " + u.name }

// RandomTools returns random_pick over the skills that have axes; none when
// no skill has any.
func RandomTools(lang i18n.Lang, list []Skill) []tools.InternalTool {
	var with []Skill
	for _, sk := range list {
		if sk.hasAxes() {
			with = append(with, sk)
		}
	}
	if len(with) == 0 {
		return nil
	}
	t := i18n.Pick(catalog, lang)
	return []tools.InternalTool{{
		Name:        ToolRandomPick,
		Description: t.pickDesc,
		DescribeIn:  func(l i18n.Lang) string { return i18n.Pick(catalog, l).pickDesc },
		Schema: fmt.Sprintf(`{"type":"object","properties":{"skill":{"type":"string","description":%q},"fixed":{"type":"object","additionalProperties":{"type":"string"},"description":%q},"count":{"type":"integer","minimum":1,"maximum":%d,"description":%q}},"required":["skill"]}`,
			t.pickSkill, t.pickFixed, maxPick, t.pickCount),
		ReadOnly: true,
		Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
			l, ok := i18n.FromContext(ctx)
			if !ok {
				l = lang
			}
			t := i18n.Pick(catalog, l)
			var a struct {
				Skill string         `json:"skill"`
				Fixed map[string]any `json:"fixed"`
				Count *int           `json:"count"`
			}
			if len(inv.Args) > 0 {
				if err := json.Unmarshal(inv.Args, &a); err != nil {
					return "", err
				}
			}
			name := strings.TrimSpace(a.Skill)
			var sk *Skill
			for i := range list {
				if list[i].Name == name {
					sk = &list[i]
				}
			}
			if sk == nil {
				return "", fmt.Errorf(t.noSkill, name)
			}
			axes := sk.AxesIn(l)
			if len(axes) == 0 {
				return "", fmt.Errorf(t.noAxes, name)
			}
			count := 1
			if a.Count != nil {
				count = *a.Count
			}
			if count < 1 || count > maxPick {
				return "", fmt.Errorf(t.badCount, maxPick)
			}
			fixed := map[string]string{}
			for k, v := range a.Fixed {
				if v == nil {
					continue
				}
				fixed[k] = fmt.Sprint(v)
			}
			lines, err := Pick(axes, fixed, count)
			if u, ok := err.(unknownAxis); ok {
				names := make([]string, len(u.axes))
				for i, ax := range u.axes {
					names[i] = ax.Name
				}
				return "", fmt.Errorf(t.noAxis, name, u.name, strings.Join(names, ", "))
			}
			if err != nil {
				return "", err
			}
			return strings.Join(lines, "\n"), nil
		},
	}}
}
