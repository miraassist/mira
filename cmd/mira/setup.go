package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/ui"
)

// setupCalls calls a skill's setup tools for an admin on the page
// (docs/spec/skill-programs.md): never for a model, and the hooks do not run
// (no model asks, an admin clicks). The log names who, the skill, the tool
// and the outcome, not the arguments (a redirect address carries a code).
type setupCalls struct{ a *app }

const setupTimeout = 60 * time.Second

func (s setupCalls) Call(ctx context.Context, who, skill, tool string, args json.RawMessage) (ui.SetupResult, error) {
	a := s.a
	sk, ok := a.skillOn(skill)
	if !ok || sk.MCP == nil || !sk.MCP.IsSetup(tool) || a.mcp == nil {
		return ui.SetupResult{}, fmt.Errorf("%w: %s %s", ui.ErrNoSetupTool, skill, tool)
	}
	// real time: a bound of a call or of the service's start or stop; no test waits for it
	ctx, cancel := context.WithTimeout(ctx, setupTimeout)
	defer cancel()
	res, err := a.mcp.CallSetup(ctx, sk.MCP.Name, tool, args)
	outcome := "ok"
	switch {
	case err != nil:
		outcome = "failed: " + err.Error()
	case res.IsError:
		outcome = "error answer"
	}
	a.log.Info("setup tool called", "who", who, "skill", skill, "tool", tool, "outcome", outcome)
	if err != nil {
		return ui.SetupResult{}, err
	}
	return ui.SetupResult{Text: res.Text, IsError: res.IsError}, nil
}

// skillOn is a switched-on skill by name.
func (a *app) skillOn(name string) (skills.Skill, bool) {
	for _, sk := range a.skillsOn {
		if sk.Name == name {
			return sk, true
		}
	}
	return skills.Skill{}, false
}

// setupInfo is a skill's setup, program and folder as the page shows them.
func (a *app) setupInfo(sk skills.Skill, info *ui.SkillInfo) {
	info.DataDir = sk.DataDir
	if p := sk.Program; p != nil {
		pi := &ui.ProgramInfo{Name: p.Name, Version: p.Version}
		if a.programs != nil {
			pi.Loading = a.programs.Loading(sk.Name)
		}
		info.Program = pi
	}
	if sk.MCP == nil {
		return
	}
	for _, st := range sk.MCP.Setup {
		si := ui.SetupInfo{Tool: st.Tool, Label: st.Label, Description: st.Description}
		if a.mcp != nil {
			if def, ok := a.mcp.SetupTool(sk.MCP.Name, st.Tool); ok {
				si.Schema = def.Schema
				if si.Description == "" {
					si.Description = def.Description
				}
			}
		}
		info.Setup = append(info.Setup, si)
	}
}
