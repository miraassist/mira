package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"

	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/startools"
	"github.com/miraassist/mira/internal/tools"
	"github.com/miraassist/mira/internal/ui"
)

// starlarkTry runs a skill's Starlark tool for an admin on the page
// ("Ausprobieren", docs/spec/api-tools.md, decision 11): the answer, every
// request and every print, and an error with its backtrace. No model, no
// hooks; a writing tool was asked for on the page before.
type starlarkTry struct{ a *app }

func (s starlarkTry) Try(ctx context.Context, who, skill, tool string, args json.RawMessage) (ui.TryResult, error) {
	a := s.a
	sk, ok := a.skillOn(skill)
	if !ok || sk.Starlark == nil {
		return ui.TryResult{}, fmt.Errorf("%w: %s", ui.ErrNoTool, skill)
	}
	if _, ok := sk.Starlark.Tool(tool); !ok {
		return ui.TryResult{}, fmt.Errorf("%w: %s %s", ui.ErrNoTool, skill, tool)
	}
	if a.oauth == nil || !sk.StarlarkUsable(a.oauth) {
		return ui.TryResult{Error: fmt.Sprintf("%s is not connected: connect it on the page Connections", skill)}, nil
	}
	res := sk.Starlark.Call(ctx, tool, args, startools.Env{Requester: a.oauth, Writes: sk.WritesTool(tool), Reader: &a.reader, Reads: sk.Reads})
	out := ui.TryResult{Text: res.Text, Events: res.Events, Backtrace: res.Backtrace}
	outcome := "ok"
	if res.Err != nil {
		out.Error = res.Err.Error()
		outcome = "failed: " + res.Err.Error()
	}
	a.log.Info("starlark tool tried on the page", "who", who, "skill", skill, "tool", tool, "requests", requests(res.Events),
		"outcome", outcome)
	return out, nil
}

func requests(evs []startools.Event) int {
	n := 0
	for _, e := range evs {
		if e.Kind == "request" {
			n++
		}
	}
	return n
}

// starlarkInfo is a skill's Starlark tools as the page shows them.
func starlarkInfo(sk skills.Skill, info *ui.SkillInfo) {
	if sk.Starlark == nil {
		return
	}
	for _, t := range sk.Starlark.Tools {
		info.Tools = append(info.Tools, ui.ToolInfo{Name: t.Name, Description: t.Description,
			Schema: json.RawMessage(t.Schema), Writes: sk.WritesTool(t.Name)})
	}
}

// starlarkReader is how a skill's Starlark tools call Mira's read-only tools
// with mira.tool (docs/spec/api-tools.md, decision 19): the registry, set
// once it is built; a tool that may change something, or that returns
// third-party text, is refused.
type starlarkReader struct {
	reg atomic.Pointer[tools.Registry]
}

func (s *starlarkReader) ReadTool(ctx context.Context, name string, args json.RawMessage) (string, bool, error) {
	reg := s.reg.Load()
	if reg == nil {
		return "", true, fmt.Errorf("%w: %s (Mira's tools are not ready yet)", startools.ErrNoTool, name)
	}
	spec, ok := reg.Lookup(name)
	if !ok {
		return "", true, fmt.Errorf("%w: %s", startools.ErrNoTool, name)
	}
	if !spec.ReadOnly {
		return "", true, fmt.Errorf("%w: %s", startools.ErrNotReadOnly, name)
	}
	if spec.WebContent {
		return "", true, fmt.Errorf("%w: %s", startools.ErrWebContent, name)
	}
	return reg.Execute(ctx, tools.Invocation{Name: name, Args: args})
}
