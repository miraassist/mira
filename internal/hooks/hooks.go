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
	"sync"
	"sync/atomic"
	"time"

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
var systemOrder = []string{"rights.star", "background.star", "web.star", "self.star", "ask.star"}

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
	// active is false for a hook with ACTIVE = False: loaded, not called.
	active bool
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
	// Off: a household hook with ACTIVE = False, loaded but not called.
	Off bool `json:"off,omitempty"`
}

// Problem is a household file that did not load.
type Problem struct {
	File, Text string
}

// Engine holds the loaded hooks. The household's files can change while
// Mira runs (docs/spec/hooks-schreiben.md, Reloading): each load builds a new
// state, which replaces the old one in one step.
type Engine struct {
	texts    texts
	log      *slog.Logger
	dir      string
	defaults []*hook
	system   []*hook
	mu       sync.Mutex // one load at a time
	cur      atomic.Pointer[state]
}

// state is one load: the chain in the order it runs, what the page shows.
type state struct {
	chain    []*hook
	critical *hook
	infos    []Info
	problems []Problem
	// sig is the folder as it was read, to notice a change.
	sig string
}

// Load reads the built-in hooks and the household's files in dir (the folder
// hooks/ next to mira.yaml; empty or missing: none). A household file that
// does not load is reported and left out.
func Load(dir string, lang i18n.Lang, log *slog.Logger) *Engine {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	e := &Engine{texts: textsFor(lang), log: log, dir: dir}
	e.defaults = e.builtinLevel(Default, nil)
	e.system = e.builtinLevel(System, systemOrder)
	e.Reload()
	return e
}

// Reload reads the household's files anew and puts them in place.
func (e *Engine) Reload() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cur.Store(e.load())
}

// load builds a state from the folder as it is now.
func (e *Engine) load() *state {
	st := &state{sig: folderSig(e.dir)}
	household, sources, problems, off := e.household(e.dir)
	st.problems = problems

	replaced := map[string]bool{}
	for _, h := range household {
		replaced[h.name] = true
	}
	st.chain = append(st.chain, household...)
	for _, h := range e.defaults {
		if !replaced[h.name] {
			st.chain = append(st.chain, h)
		}
	}
	st.chain = append(st.chain, e.system...)
	for _, h := range st.chain {
		if h.name == criticalFile && h.level != System {
			st.critical = h
		}
	}

	for _, h := range household {
		st.infos = append(st.infos, Info{Level: Household, Name: h.name, Description: h.description, Source: h.source})
	}
	for _, h := range off {
		st.infos = append(st.infos, Info{Level: Household, Name: h.name, Description: h.description, Source: h.source, Off: true})
	}
	for _, p := range problems {
		st.infos = append(st.infos, Info{Level: Household, Name: p.File, Source: sources[p.File], Problem: p.Text})
	}
	sort.SliceStable(st.infos, func(i, j int) bool { return st.infos[i].Name < st.infos[j].Name })
	for _, h := range e.defaults {
		st.infos = append(st.infos, Info{Level: Default, Name: h.name, Description: h.description, Source: h.source, Replaced: replaced[h.name]})
	}
	for _, h := range e.system {
		st.infos = append(st.infos, Info{Level: System, Name: h.name, Description: h.description, Source: h.source})
	}

	var names []string
	for _, h := range st.chain {
		names = append(names, h.id())
	}
	var gone []string
	for name := range replaced {
		if slices.ContainsFunc(e.defaults, func(h *hook) bool { return h.name == name }) {
			gone = append(gone, Default+"/"+name)
		}
	}
	sort.Strings(gone)
	e.log.Info("hooks loaded", "chain", strings.Join(names, ","), "replaced", strings.Join(gone, ","),
		"off", len(off), "problems", len(problems))
	return st
}

// Watch reloads the household's hooks whenever the folder changes, until ctx
// ends. It looks every interval; a change counts once it has held for one
// more look, so that an editor's several writes load once.
func (e *Engine) Watch(ctx context.Context, interval time.Duration) {
	if e.dir == "" {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	pending := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		sig := folderSig(e.dir)
		switch {
		case sig == e.cur.Load().sig:
			pending = ""
		case sig != pending:
			pending = sig
		default:
			e.log.Info("hooks folder changed, loading anew", "dir", e.dir)
			e.Reload()
			pending = ""
		}
	}
}

// folderSig is what the folder holds: names, sizes and times of its files.
func folderSig(dir string) string {
	if dir == "" {
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, en := range entries {
		if en.IsDir() || !strings.HasSuffix(en.Name(), ".star") {
			continue
		}
		info, err := en.Info()
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "%s|%d|%d\n", en.Name(), info.Size(), info.ModTime().UnixNano())
	}
	return b.String()
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

// household loads the household's files, in the order of their names: the
// active ones, those switched off (ACTIVE = False), and the problems of those
// that did not load.
func (e *Engine) household(dir string) (active []*hook, sources map[string]string, problems []Problem, off []*hook) {
	sources = map[string]string{}
	if dir == "" {
		return nil, sources, nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			e.log.Warn("hooks folder not read", "dir", dir, "error", err)
		}
		return nil, sources, nil, nil
	}
	for _, en := range entries {
		if en.IsDir() || !strings.HasSuffix(en.Name(), ".star") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, en.Name()))
		if err == nil {
			sources[en.Name()] = string(src)
			var h *hook
			if h, err = e.compile(Household, en.Name(), src); err == nil {
				if h.active {
					active = append(active, h)
				} else {
					off = append(off, h)
				}
				continue
			}
		}
		problems = append(problems, Problem{File: en.Name(), Text: fmt.Sprintf(e.texts.notLoaded, err)})
		e.log.Warn("hook not loaded", "file", en.Name(), "problem", err)
	}
	return active, sources, problems, off
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
	h.active = true
	if v, ok := globals["ACTIVE"]; ok {
		b, ok := v.(starlark.Bool)
		if !ok {
			return nil, errors.New("ACTIVE must be True or False")
		}
		h.active = bool(b)
	}
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
	for _, h := range e.cur.Load().chain {
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
	if e == nil || e.cur.Load().critical == nil {
		return nil, nil
	}
	h := e.cur.Load().critical
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
	return e.cur.Load().infos
}

// Problems are the household files that did not load.
func (e *Engine) Problems() []Problem {
	if e == nil {
		return nil
	}
	return e.cur.Load().problems
}
