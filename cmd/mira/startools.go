package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/startools"
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
	res := sk.Starlark.Call(ctx, tool, args, startools.Env{Requester: a.oauth, Writes: sk.WritesTool(tool)})
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
