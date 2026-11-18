package skillha

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/tools"
)

// ToolName is the name of a skill script's tool (docs/spec/skill-files.md,
// decision 27): flug__abfrage.
func ToolName(skill, id string) string { return skill + "__" + id }

// Tools is step 8 of the path: every script the skills declare as a tool
// of its skill, its parameters from Home Assistant's fields of the action
// script.<id> (one get_services). A script Home Assistant does not offer
// has no tool and is a problem of its skill, kept with the problems of the
// last Run; get_services failing is a problem of every skill with scripts.
// Call it after Run.
func (s *Sync) Tools(ctx context.Context, list []skills.Skill) []tools.InternalTool {
	var with []skills.Skill
	for _, sk := range list {
		if len(sk.Scripts) > 0 {
			with = append(with, sk)
		}
	}
	if len(with) == 0 {
		return nil
	}
	var problems []found
	defer func() {
		s.mu.Lock()
		s.problems = append(s.problems, problems...)
		s.mu.Unlock()
	}()
	actions, err := s.opts.HA.ServicesOf(ctx, domainScript)
	if err != nil {
		s.log.Error("could not read home assistant's actions; the skills' scripts have no tools until the next start", "error", err)
		for _, sk := range with {
			problems = append(problems, found{skill: sk.Name, kind: skills.KindScriptTools, args: []any{err}})
		}
		return nil
	}
	var out []tools.InternalTool
	for _, sk := range with {
		for _, a := range sk.Scripts {
			id := AutomationID(sk.Name, a.ID)
			action, ok := actions[id]
			if !ok {
				s.log.Warn("a skill script is not among home assistant's actions; it has no tool", "skill", sk.Name,
					"script", a.ID, "action", "script."+id)
				problems = append(problems, found{skill: sk.Name, kind: skills.KindScriptTool, args: []any{a.ID, "script." + id}})
				continue
			}
			out = append(out, s.tool(sk.Name, id, a, action.Fields))
		}
	}
	return out
}

// tool is one script's tool: offered with its skill, to its schedules and
// agents too (Background), never in the shared prefix.
func (s *Sync) tool(skill, id string, a skills.Automation, fields map[string]haws.ServiceField) tools.InternalTool {
	return tools.InternalTool{
		Name: ToolName(skill, a.ID), Description: a.Description, Schema: schemaOf(fields),
		Skill: skill, Background: true,
		Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
			var args map[string]any
			if len(inv.Args) > 0 {
				if err := json.Unmarshal(inv.Args, &args); err != nil {
					return "", fmt.Errorf("the arguments for script.%s are no object: %q", id, string(inv.Args))
				}
			}
			raw, err := s.opts.HA.CallServiceResponse(ctx, domainScript, id, args)
			if err != nil {
				// The caller logs a failed tool with its name; the error
				// names the action and Home Assistant's answer.
				return "", fmt.Errorf("script.%s: %w", id, err)
			}
			return string(raw), nil
		},
	}
}

// schemaOf is the tool's parameters from the action's fields: a select's
// options as an enum, number and boolean as such, any other selector as
// text (docs/spec/skill-files.md, the path, step 8).
func schemaOf(fields map[string]haws.ServiceField) string {
	props := map[string]any{}
	required := []string{}
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		f := fields[name]
		p := map[string]any{"type": "string"}
		if f.Description != "" {
			p["description"] = f.Description
		}
		switch {
		case f.Selector["number"] != nil:
			p["type"] = "number"
		case f.Selector["boolean"] != nil:
			p["type"] = "boolean"
		case f.Selector["select"] != nil:
			if sel, ok := f.Selector["select"].(map[string]any); ok {
				var enum []string
				opts, _ := sel["options"].([]any)
				for _, o := range opts {
					switch v := o.(type) {
					case string:
						enum = append(enum, v)
					case map[string]any:
						if val, ok := v["value"].(string); ok {
							enum = append(enum, val)
						}
					}
				}
				if len(enum) > 0 {
					p["enum"] = enum
				}
			}
		}
		props[name] = p
		if f.Required {
			required = append(required, name)
		}
	}
	raw, _ := json.Marshal(map[string]any{"type": "object", "properties": props, "required": required})
	return string(raw)
}
