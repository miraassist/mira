package hooks

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/tools"
)

// The kinds of a verdict.
const (
	kindAllow   = "allow"
	kindConfirm = "confirm"
	kindDeny    = "deny"
)

// verdictValue is what allow(), confirm() and deny() return, and the verdict
// so far a hook gets: kind, reason and the hook that made it so (by).
type verdictValue struct {
	kind, reason, by string
}

var _ starlark.HasAttrs = (*verdictValue)(nil)

func (v *verdictValue) String() string {
	return fmt.Sprintf("%s(%q)", v.kind, v.reason)
}
func (v *verdictValue) Type() string         { return "verdict" }
func (v *verdictValue) Freeze()              {}
func (v *verdictValue) Truth() starlark.Bool { return true }
func (v *verdictValue) Hash() (uint32, error) {
	return starlark.String(v.kind + "\x00" + v.reason).Hash()
}
func (v *verdictValue) AttrNames() []string { return []string{"by", "kind", "reason"} }
func (v *verdictValue) Attr(name string) (starlark.Value, error) {
	switch name {
	case "kind":
		return starlark.String(v.kind), nil
	case "reason":
		return starlark.String(v.reason), nil
	case "by":
		return starlark.String(v.by), nil
	}
	return nil, nil
}

// asVerdict reads what pre_tool returned: None is allow.
func asVerdict(v starlark.Value) (*verdictValue, error) {
	switch x := v.(type) {
	case starlark.NoneType:
		return &verdictValue{kind: kindAllow}, nil
	case *verdictValue:
		return x, nil
	}
	return nil, fmt.Errorf("pre_tool returned %s, not allow(), confirm() or deny()", v.Type())
}

// predeclared are the names every hook has: the verdicts, Mira's texts and
// the log.
func (e *Engine) predeclared() starlark.StringDict {
	return starlark.StringDict{
		"allow": starlark.NewBuiltin("allow", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			if err := starlark.UnpackArgs(b.Name(), args, kwargs); err != nil {
				return nil, err
			}
			return &verdictValue{kind: kindAllow}, nil
		}),
		"confirm": starlark.NewBuiltin("confirm", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			var reason string
			if err := starlark.UnpackArgs(b.Name(), args, kwargs, "reason?", &reason); err != nil {
				return nil, err
			}
			return &verdictValue{kind: kindConfirm, reason: reason}, nil
		}),
		"deny": starlark.NewBuiltin("deny", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			var reason string
			if err := starlark.UnpackArgs(b.Name(), args, kwargs, "reason", &reason); err != nil {
				return nil, err
			}
			return &verdictValue{kind: kindDeny, reason: reason}, nil
		}),
		"text": starlark.NewBuiltin("text", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			if len(kwargs) > 0 || len(args) == 0 {
				return nil, fmt.Errorf("%s: wants a key and its values", b.Name())
			}
			key, ok := starlark.AsString(args[0])
			if !ok {
				return nil, fmt.Errorf("%s: the key must be a string", b.Name())
			}
			t, ok := e.texts.keys[key]
			if !ok {
				return nil, fmt.Errorf("%s: no text %q", b.Name(), key)
			}
			var vals []any
			for _, a := range args[1:] {
				if s, ok := starlark.AsString(a); ok {
					vals = append(vals, s)
				} else {
					vals = append(vals, a.String())
				}
			}
			if len(vals) > 0 {
				t = fmt.Sprintf(t, vals...)
			}
			return starlark.String(t), nil
		}),
		"log": starlark.NewBuiltin("log", func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("%s: wants one message", b.Name())
			}
			msg, ok := starlark.AsString(args[0])
			if !ok {
				msg = args[0].String()
			}
			attrs := []any{"message", msg}
			if h, ok := thread.Local("hook").(*hook); ok {
				attrs = append([]any{"hook", h.id()}, attrs...)
			}
			if c, ok := thread.Local("call").(tools.Call); ok {
				attrs = append(attrs, "tool", c.Spec.QualifiedName())
			}
			for _, kv := range kwargs {
				name, _ := starlark.AsString(kv[0])
				val, ok := starlark.AsString(kv[1])
				if !ok {
					val = kv[1].String()
				}
				attrs = append(attrs, name, val)
			}
			e.log.Info("hook log", attrs...)
			return starlark.None, nil
		}),
	}
}

// callValue is the call as a hook sees it (docs/spec/hooks.md, What a hook
// sees); frozen, so hooks only read it.
func callValue(c tools.Call) starlark.Value {
	who := c.PersonBy
	if c.Person != "" && who == "" {
		who = "signed_in"
	}
	name := c.PersonName
	if name == "" {
		name = c.Person
	}
	kind := "conversation"
	if c.Agent {
		kind = "agent"
	}
	targets := make([]starlark.Value, 0, len(c.Targets))
	for _, e := range c.Targets {
		targets = append(targets, entityValue(e))
	}
	candidates := make([]starlark.Value, 0, len(c.Candidates))
	for _, e := range c.Candidates {
		candidates = append(candidates, entityValue(e))
	}
	server, short := c.Spec.Server, c.Spec.Name
	v := object(starlark.StringDict{
		"tool": object(starlark.StringDict{
			"name":               starlark.String(short),
			"server":             starlark.String(server),
			"qualified":          starlark.String(c.Spec.QualifiedName()),
			"read_only":          starlark.Bool(c.Spec.ReadOnly),
			"own":                starlark.Bool(c.Spec.Internal),
			"web_content":        starlark.Bool(c.Spec.WebContent),
			"keeps_or_passes_on": starlark.Bool(c.Spec.AskAfterWeb),
			"skill":              starlark.String(c.Spec.Skill),
			"right":              starlark.String(c.Right),
		}),
		"args":       toValue(c.Args),
		"domains":    strings_(c.Domains),
		"targets":    starlark.NewList(targets),
		"candidates": starlark.NewList(candidates),
		"summary":    starlark.String(c.Summary),
		"who": object(starlark.StringDict{
			"person": starlark.String(c.Person),
			"name":   starlark.String(name),
			"by":     starlark.String(who),
		}),
		"where": object(starlark.StringDict{
			"channel": starlark.String(string(c.Channel)),
			"area":    starlark.String(c.Area),
			"device":  starlark.String(c.DeviceID),
		}),
		"origin": object(starlark.StringDict{
			"kind":    starlark.String(kind),
			"rights":  strings_(c.Rights),
			"can_ask": starlark.Bool(c.Channel != config.ChannelBackground),
		}),
		"turn": object(starlark.StringDict{
			"web_read": starlark.Bool(c.WebRead),
			"foreign":  starlark.Bool(c.Foreign),
			"text":     starlark.String(c.Text),
		}),
		"time": object(starlark.StringDict{
			"hour":    starlark.MakeInt(c.Time.Hour()),
			"minute":  starlark.MakeInt(c.Time.Minute()),
			"weekday": starlark.MakeInt((int(c.Time.Weekday())+6)%7 + 1),
			"date":    starlark.String(c.Time.Format("2006-01-02")),
		}),
	})
	v.Freeze()
	return v
}

func entityValue(e tools.Entity) starlark.Value {
	v := object(starlark.StringDict{
		"id":        starlark.String(e.ID),
		"domain":    starlark.String(e.Domain),
		"name":      starlark.String(e.Name),
		"area":      starlark.String(e.Area),
		"area_name": starlark.String(e.AreaName),
		"labels":    strings_(e.Labels),
	})
	v.Freeze()
	return v
}

func object(d starlark.StringDict) *starlarkstruct.Struct {
	return starlarkstruct.FromStringDict(starlarkstruct.Default, d)
}

func strings_(list []string) *starlark.List {
	out := make([]starlark.Value, 0, len(list))
	for _, s := range list {
		out = append(out, starlark.String(s))
	}
	return starlark.NewList(out)
}

// toValue turns decoded JSON into Starlark values.
func toValue(v any) starlark.Value {
	switch x := v.(type) {
	case nil:
		return starlark.None
	case bool:
		return starlark.Bool(x)
	case string:
		return starlark.String(x)
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1<<53 {
			return starlark.MakeInt64(int64(x))
		}
		return starlark.Float(x)
	case []any:
		out := make([]starlark.Value, 0, len(x))
		for _, e := range x {
			out = append(out, toValue(e))
		}
		return starlark.NewList(out)
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		d := starlark.NewDict(len(x))
		for _, k := range keys {
			_ = d.SetKey(starlark.String(k), toValue(x[k]))
		}
		return d
	}
	return starlark.String(strings.TrimSpace(fmt.Sprint(v)))
}
