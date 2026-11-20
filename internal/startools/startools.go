// Package startools runs a skill's tools written in Starlark
// (docs/spec/api-tools.md): the functions of the skill's tools.star become
// tools of the skill, described by their docstrings, and reach the skill's
// service only through the module api with the connection's token.
package startools

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"sort"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// FileName is the file of a skill's tools (decision 2).
const FileName = "tools.star"

// maxSteps is a tool's limit of Starlark steps, as for a hook.
const maxSteps = 100_000

// types are the parameter types a docstring may name (decision 5) and their
// JSON schema.
var types = map[string]map[string]any{
	"str":   {"type": "string"},
	"int":   {"type": "integer"},
	"float": {"type": "number"},
	"bool":  {"type": "boolean"},
	"list":  {"type": "array", "items": map[string]any{"type": "string"}},
}

var toolName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// File is a skill's tools.star, loaded.
type File struct {
	Skill string
	Tools []*Tool
	// settings are the skill's settings that are not secret, frozen.
	settings *starlark.Dict
	log      *slog.Logger
}

// Tool is one function of the file that the model may call.
type Tool struct {
	Name        string
	Description string
	Params      []Param
	// Schema is the JSON schema of the arguments.
	Schema string
	// Line is where the function is defined.
	Line int
	fn   *starlark.Function
}

// Param is a tool's parameter.
type Param struct {
	Name, Type, Description string
	Required                bool
	// Default is the parameter's default, as JSON; nil for a required one
	// or a default of None.
	Default any
}

// Tool is the file's tool of that name.
func (f *File) Tool(name string) (*Tool, bool) {
	for _, t := range f.Tools {
		if t.Name == name {
			return t, true
		}
	}
	return nil, false
}

// Names are the file's tools by name.
func (f *File) Names() []string {
	out := make([]string, len(f.Tools))
	for i, t := range f.Tools {
		out[i] = t.Name
	}
	return out
}

// Load runs the file's top level once and reads its tools (docs/spec/api-tools.md,
// Loading). Its error names the file, the line and what is wrong, as the
// skill's problem shows it.
func Load(skill string, src []byte, settings map[string]string, log *slog.Logger) (*File, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	f := &File{Skill: skill, settings: settingsDict(settings), log: log}
	thread := &starlark.Thread{Name: skill + "/" + FileName, Print: func(_ *starlark.Thread, msg string) {
		log.Info("starlark tool print", "skill", skill, "file", FileName, "message", msg)
	}}
	thread.SetMaxExecutionSteps(maxSteps)
	globals, err := starlark.ExecFileOptions(&syntax.FileOptions{}, thread, FileName, src, f.predeclared())
	if err != nil {
		return nil, describe(err)
	}
	names := make([]string, 0, len(globals))
	for n := range globals {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fn, ok := globals[n].(*starlark.Function)
		if !ok || strings.HasPrefix(n, "_") || fn.Name() != n {
			continue
		}
		t, err := readTool(fn)
		if err != nil {
			return nil, err
		}
		f.Tools = append(f.Tools, t)
	}
	if len(f.Tools) == 0 {
		return nil, fmt.Errorf("%s has no tool: a tool is a function whose name does not begin with _", FileName)
	}
	return f, nil
}

// readTool reads a function's parameters from its signature and its
// docstring and checks that both agree.
func readTool(fn *starlark.Function) (*Tool, error) {
	pos := fn.Position()
	at := func(format string, args ...any) error {
		return fmt.Errorf("%s:%d: %s: %s", FileName, pos.Line, fn.Name(), fmt.Sprintf(format, args...))
	}
	if !toolName.MatchString(fn.Name()) {
		return nil, at("a tool's name is small letters, digits and _, beginning with a letter")
	}
	if fn.HasVarargs() || fn.HasKwargs() {
		return nil, at("a tool takes no *args or **kwargs")
	}
	if strings.TrimSpace(fn.Doc()) == "" {
		return nil, at("the tool has no docstring: its description and its parameters with their types (Args:)")
	}
	d, err := parseDoc(fn.Doc())
	if err != nil {
		return nil, at("%v", err)
	}
	if d.Text == "" {
		return nil, at("the docstring has no description before Args:")
	}
	byName := map[string]docArg{}
	for _, a := range d.Args {
		if _, twice := byName[a.Name]; twice {
			return nil, at("the docstring names the parameter %s twice", a.Name)
		}
		byName[a.Name] = a
	}
	t := &Tool{Name: fn.Name(), Description: d.Text, Line: int(pos.Line), fn: fn}
	props := map[string]any{}
	required := []string{}
	seen := map[string]bool{}
	for i := 0; i < fn.NumParams(); i++ {
		name, _ := fn.Param(i)
		a, ok := byName[name]
		if !ok {
			return nil, at("the parameter %s has no entry under Args: in the docstring", name)
		}
		seen[name] = true
		schema, ok := types[a.Type]
		if !ok {
			return nil, at("the parameter %s has the type %q; the types are str, int, float, bool, list", name, a.Type)
		}
		p := Param{Name: name, Type: a.Type, Description: a.Text}
		prop := map[string]any{}
		for k, v := range schema {
			prop[k] = v
		}
		if a.Text != "" {
			prop["description"] = a.Text
		}
		if def := fn.ParamDefault(i); def == nil {
			p.Required = true
			required = append(required, name)
		} else if def != starlark.None {
			if err := fits(def, a.Type); err != nil {
				return nil, at("the default of %s: %v", name, err)
			}
			p.Default = toJSON(def)
			prop["default"] = p.Default
		}
		props[name] = prop
		t.Params = append(t.Params, p)
	}
	for _, a := range d.Args {
		if !seen[a.Name] {
			return nil, at("the docstring names the parameter %s, which the function does not have", a.Name)
		}
	}
	raw, _ := json.Marshal(map[string]any{"type": "object", "properties": props, "required": required,
		"additionalProperties": false})
	t.Schema = string(raw)
	return t, nil
}

// fits checks a Starlark value against a parameter's type.
func fits(v starlark.Value, typ string) error {
	ok := false
	switch typ {
	case "str":
		_, ok = v.(starlark.String)
	case "int":
		_, ok = v.(starlark.Int)
	case "float":
		switch v.(type) {
		case starlark.Float, starlark.Int:
			ok = true
		}
	case "bool":
		_, ok = v.(starlark.Bool)
	case "list":
		var it starlark.Iterable
		switch x := v.(type) {
		case *starlark.List:
			it = x
		case starlark.Tuple:
			it = x
		}
		if it != nil {
			ok = true
			iter := it.Iterate()
			defer iter.Done()
			var e starlark.Value
			for iter.Next(&e) {
				if _, s := e.(starlark.String); !s {
					ok = false
				}
			}
		}
	}
	if !ok {
		return fmt.Errorf("%s is no %s", v.String(), typ)
	}
	return nil
}

// toJSON is a default value as JSON.
func toJSON(v starlark.Value) any {
	switch x := v.(type) {
	case starlark.String:
		return string(x)
	case starlark.Bool:
		return bool(x)
	case starlark.Int:
		if n, ok := x.Int64(); ok {
			return n
		}
		return x.String()
	case starlark.Float:
		return float64(x)
	case *starlark.List:
		out := []any{}
		for i := 0; i < x.Len(); i++ {
			out = append(out, toJSON(x.Index(i)))
		}
		return out
	case starlark.Tuple:
		out := []any{}
		for _, e := range x {
			out = append(out, toJSON(e))
		}
		return out
	}
	return v.String()
}

func settingsDict(settings map[string]string) *starlark.Dict {
	d := starlark.NewDict(len(settings))
	keys := make([]string, 0, len(settings))
	for k := range settings {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		_ = d.SetKey(starlark.String(k), starlark.String(settings[k]))
	}
	d.Freeze()
	return d
}

// describe is a Starlark error with its backtrace (file and line).
func describe(err error) error {
	var ev *starlark.EvalError
	if errors.As(err, &ev) {
		return errors.New(strings.TrimSpace(ev.Backtrace()))
	}
	return err
}

// SetLogger gives the file the log its calls write to (requests, prints,
// failures).
func (f *File) SetLogger(l *slog.Logger) {
	if l != nil {
		f.log = l
	}
}
