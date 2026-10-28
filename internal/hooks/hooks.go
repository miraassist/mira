// Package hooks decides on the model's tool calls (docs/spec/hooks.md): small
// Starlark scripts on three levels, system, default and household, called
// household first so that the system has the last word. The strictest
// verdict wins: deny over confirm over allow. The guard (tools.Guard) is the
// mechanism around them: it builds the call, keeps the question for the
// user's yes and carries out what the hooks decided.
package hooks

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"

	"github.com/miraassist/mira/internal/ha/structure"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

// Levels of hooks.
const (
	System    = "system"
	Default   = "default"
	Household = "household"
)

// systemOrder is the fixed order of the system hooks: ask comes last, as it
// judges the questions the others asked for.
var systemOrder = []string{"rights.star", "background.star", "web.star", "ask.star"}

// maxSteps bounds a hook's work; more counts as a failure.
const maxSteps = 100_000

// criticalFile also says what a task may not switch (DOMAINS, critical()).
const criticalFile = "critical.star"

//go:embed system/*.star default/*.star
var builtin embed.FS

// hook is one loaded file.
type hook struct {
	level, name string
	source      string
	description string
	pre         starlark.Callable
	globals     starlark.StringDict
}

// id is how the log and the verdict name a hook: "system/web.star".
func (h *hook) id() string { return h.level + "/" + h.name }

// Info is a hook as Mira's page shows it.
type Info struct {
	Level       string `json:"level"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source"`
	// ReplacedBy: a default hook the household's file of the same name
	// replaces.
	Replaced bool `json:"replaced,omitempty"`
	// Problem: a household file that did not load and is not active.
	Problem string `json:"problem,omitempty"`
}

// Problem is a household file that did not load.
type Problem struct {
	File, Text string
}

// Engine holds the loaded hooks in the order they run.
type Engine struct {
	texts    texts
	log      *slog.Logger
	chain    []*hook
	critical *hook
	infos    []Info
	problems []Problem
}

// Load reads the built-in hooks and the household's files in dir (the folder
// hooks/ next to mira.yaml; empty or missing: none). A household file that
// does not load is reported and left out.
func Load(dir string, lang i18n.Lang, log *slog.Logger) *Engine {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	e := &Engine{texts: textsFor(lang), log: log}

	defaults := e.builtinLevel(Default, nil)
	system := e.builtinLevel(System, systemOrder)
	household, sources := e.household(dir)

	replaced := map[string]bool{}
	for _, h := range household {
		replaced[h.name] = true
	}
	e.chain = append(e.chain, household...)
	for _, h := range defaults {
		if !replaced[h.name] {
			e.chain = append(e.chain, h)
		}
	}
	e.chain = append(e.chain, system...)
	for _, h := range e.chain {
		if h.name == criticalFile && h.level != System {
			e.critical = h
		}
	}

	for _, h := range household {
		e.infos = append(e.infos, Info{Level: Household, Name: h.name, Description: h.description, Source: h.source})
	}
	for _, p := range e.problems {
		e.infos = append(e.infos, Info{Level: Household, Name: p.File, Source: sources[p.File], Problem: p.Text})
	}
	for _, h := range defaults {
		e.infos = append(e.infos, Info{Level: Default, Name: h.name, Description: h.description, Source: h.source, Replaced: replaced[h.name]})
	}
	for _, h := range system {
		e.infos = append(e.infos, Info{Level: System, Name: h.name, Description: h.description, Source: h.source})
	}

	var names []string
	for _, h := range e.chain {
		names = append(names, h.id())
	}
	var gone []string
	for name := range replaced {
		if slices.ContainsFunc(defaults, func(h *hook) bool { return h.name == name }) {
			gone = append(gone, Default+"/"+name)
		}
	}
	sort.Strings(gone)
	e.log.Info("hooks loaded", "chain", strings.Join(names, ","), "replaced", strings.Join(gone, ","), "problems", len(e.problems))
	return e
}

// builtinLevel loads the hooks in the binary; order, if given, is theirs.
// A built-in hook that does not load is a bug, caught by the tests; it
// panics so that it can never go unnoticed.
func (e *Engine) builtinLevel(level string, order []string) []*hook {
	entries, err := fs.ReadDir(builtin, level)
	if err != nil {
		panic(err)
	}
	var names []string
	for _, en := range entries {
		names = append(names, en.Name())
	}
	sort.Strings(names)
	if order != nil {
		if !slices.Equal(slices.Sorted(slices.Values(order)), names) {
			panic(fmt.Sprintf("hooks: %s hooks %v do not match their order %v", level, names, order))
		}
		names = order
	}
	var out []*hook
	for _, name := range names {
		src, err := builtin.ReadFile(level + "/" + name)
		if err != nil {
			panic(err)
		}
		h, err := e.compile(level, name, src)
		if err != nil {
			panic(fmt.Sprintf("hooks: %s/%s: %v", level, name, err))
		}
		out = append(out, h)
	}
	return out
}

// household loads the household's files, in the order of their names.
func (e *Engine) household(dir string) ([]*hook, map[string]string) {
	sources := map[string]string{}
	if dir == "" {
		return nil, sources
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			e.log.Warn("hooks folder not read", "dir", dir, "error", err)
		}
		return nil, sources
	}
	var out []*hook
	for _, en := range entries {
		if en.IsDir() || !strings.HasSuffix(en.Name(), ".star") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, en.Name()))
		if err == nil {
			sources[en.Name()] = string(src)
			var h *hook
			if h, err = e.compile(Household, en.Name(), src); err == nil {
				out = append(out, h)
				continue
			}
		}
		text := fmt.Sprintf(e.texts.notLoaded, err)
		e.problems = append(e.problems, Problem{File: en.Name(), Text: text})
		e.log.Warn("hook not loaded", "file", en.Name(), "problem", err)
	}
	return out, sources
}

// compile runs a file once and checks what it must have.
func (e *Engine) compile(level, name string, src []byte) (*hook, error) {
	h := &hook{level: level, name: name, source: string(src)}
	thread := e.thread(h)
	globals, err := starlark.ExecFileOptions(&syntax.FileOptions{}, thread, name, src, e.predeclared())
	if err != nil {
		return nil, describeErr(err)
	}
	h.globals = globals
	desc, ok := globals["DESCRIPTION"].(starlark.String)
	if !ok || strings.TrimSpace(string(desc)) == "" {
		return nil, errors.New("DESCRIPTION is missing")
	}
	h.description = string(desc)
	pre, ok := globals["pre_tool"].(starlark.Callable)
	if !ok {
		return nil, errors.New("pre_tool(call, verdict) is missing")
	}
	h.pre = pre
	if name == criticalFile && level != System {
		_, list := globals["DOMAINS"].(*starlark.List)
		_, fn := globals["critical"].(starlark.Callable)
		if !list || !fn {
			return nil, errors.New(e.texts.criticalMissing)
		}
	}
	return h, nil
}

func describeErr(err error) error {
	var ev *starlark.EvalError
	if errors.As(err, &ev) {
		return errors.New(strings.TrimSpace(ev.Backtrace()))
	}
	return err
}

// thread is a fresh interpreter thread for one hook's work.
func (e *Engine) thread(h *hook) *starlark.Thread {
	t := &starlark.Thread{Name: h.id(), Print: func(_ *starlark.Thread, msg string) {
		e.log.Info("hook log", "hook", h.id(), "message", msg)
	}}
	t.SetMaxExecutionSteps(maxSteps)
	t.SetLocal("hook", h)
	return t
}

// Decide runs the chain on a call (tools.Policy).
func (e *Engine) Decide(ctx context.Context, c tools.Call) tools.Decision {
	call := callValue(c)
	cur := &verdictValue{kind: kindAllow}
	for _, h := range e.chain {
		thread := e.thread(h)
		thread.SetLocal("call", c)
		res, err := starlark.Call(thread, h.pre, starlark.Tuple{call, cur}, nil)
		var v *verdictValue
		if err == nil {
			v, err = asVerdict(res)
		}
		if err != nil {
			e.log.Warn("hook failed", "hook", h.id(), "tool", c.Spec.QualifiedName(), "error", describeErr(err))
			return tools.Decision{Outcome: tools.Deny, By: h.id(), Reason: fmt.Sprintf(e.texts.failed, h.id())}
		}
		switch v.kind {
		case kindDeny:
			return tools.Decision{Outcome: tools.Deny, By: h.id(), Reason: v.reason}
		case kindConfirm:
			if cur.kind == kindAllow {
				cur = &verdictValue{kind: kindConfirm, reason: v.reason, by: h.id()}
			}
		}
	}
	if cur.kind == kindConfirm {
		return tools.Decision{Outcome: tools.NeedsConfirmation, By: cur.by, Reason: cur.reason}
	}
	return tools.Decision{Outcome: tools.Allow}
}

// Forbidden is what a task may not switch (docs/spec/hooks.md): the domains
// critical.star names and the entities of the house its critical() calls
// critical. An entity it fails on counts as critical.
func (e *Engine) Forbidden(snap structure.Snapshot) (domains, entities []string) {
	if e == nil || e.critical == nil {
		return nil, nil
	}
	h := e.critical
	if list, ok := h.globals["DOMAINS"].(*starlark.List); ok {
		for i := range list.Len() {
			if s, ok := list.Index(i).(starlark.String); ok {
				domains = append(domains, strings.ToLower(string(s)))
			}
		}
	}
	fn, _ := h.globals["critical"].(starlark.Callable)
	var ids []string
	for id := range snap.EntityNames {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if slices.Contains(domains, structure.Domain(id)) {
			continue
		}
		res, err := starlark.Call(e.thread(h), fn, starlark.Tuple{entityValue(tools.EntityOf(snap, id))}, nil)
		if err != nil {
			e.log.Warn("hook failed", "hook", h.id(), "function", "critical", "entity", id, "error", describeErr(err))
			entities = append(entities, id)
			continue
		}
		if res.Truth() {
			entities = append(entities, id)
		}
	}
	return domains, entities
}

// Infos are the hooks for Mira's page: household, default, system. A nil
// engine (an app built in a test without hooks) has none.
func (e *Engine) Infos() []Info {
	if e == nil {
		return nil
	}
	return e.infos
}

// Problems are the household files that did not load.
func (e *Engine) Problems() []Problem {
	if e == nil {
		return nil
	}
	return e.problems
}
