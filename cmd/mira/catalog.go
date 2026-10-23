package main

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/agent"
	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/ui"
)

// catalog is every skill and agent as Mira loaded them, for the view on her
// page (docs/spec/uebersicht.md). It reads; it changes nothing.
func (a *app) catalog(ctx context.Context) ui.Catalog {
	c := ui.Catalog{Skills: []ui.SkillInfo{}, Agents: []ui.AgentInfo{}}
	problems := map[string]string{}
	agentProblems := map[string]string{}
	for _, p := range a.problems {
		switch p.Source {
		case "skill":
			problems[p.Name] = p.Text
		case "agent":
			agentProblems[p.Name] = p.Text
		}
	}
	active := a.skillsWithAgents()
	started := map[string]bool{}
	for _, sk := range active {
		started[sk.Name] = true
		c.Skills = append(c.Skills, a.skillInfo(sk))
	}
	for _, name := range sortedNames(a.cfg.Skills.Enabled) {
		if !started[name] {
			if text, ok := problems[name]; ok {
				c.Skills = append(c.Skills, ui.SkillInfo{Name: name, State: "problem", Problem: text})
			}
		}
	}
	for _, info := range skills.Available(a.cfg.Skills.Dirs, a.cfg.Skills.Enabled) {
		if _, problem := problems[info.Name]; !problem && !started[info.Name] {
			c.Skills = append(c.Skills, ui.SkillInfo{Name: info.Name, Description: info.Description, State: "available"})
		}
	}
	sort.SliceStable(c.Skills, func(i, j int) bool {
		if c.Skills[i].State != c.Skills[j].State {
			return stateOrder(c.Skills[i].State) < stateOrder(c.Skills[j].State)
		}
		return c.Skills[i].Name < c.Skills[j].Name
	})

	// Mira's own agents, then those of mira.yaml, then the skills'.
	for _, name := range agent.OwnNames() {
		info := ui.AgentInfo{Name: name, Origin: "own", Privacy: a.cfg.PrivacyFor(name)}
		a.modelInfo(&info, a.cfg.ModelFor(name))
		c.Agents = append(c.Agents, info)
	}
	defs := agent.FromConfig(a.cfg.Agents.Defined)
	for _, sk := range active {
		for _, name := range sortedNames(sk.Agents) {
			defs = append(defs, agent.Defined{Name: name, Skill: sk.Name, Def: sk.Agents[name]})
		}
	}
	for _, d := range defs {
		if d.Skill == "" && agent.Own(d.Name) {
			continue
		}
		c.Agents = append(c.Agents, a.agentInfo(d, defs, active, agentProblems[d.Name]))
	}
	return c
}

func stateOrder(s string) int {
	return map[string]int{"active": 0, "problem": 1, "available": 2}[s]
}

// skillInfo is what the view shows of a started skill.
func (a *app) skillInfo(sk skills.Skill) ui.SkillInfo {
	info := ui.SkillInfo{Name: sk.Name, Description: sk.Description, State: "active", Guide: strings.TrimSpace(sk.Body)}
	values := a.cfg.Skills.Enabled[sk.Name]
	for _, name := range sortedNames(sk.Settings) {
		decl := sk.Settings[name]
		v, set := values[name]
		s := ui.SettingInfo{Name: name, Description: decl.Description, Secret: decl.Secret,
			Set: set && strings.TrimSpace(fmt.Sprint(v)) != ""}
		if set && !decl.Secret {
			s.Value = fmt.Sprint(v)
		}
		info.Settings = append(info.Settings, s)
	}
	now := time.Now()
	for _, s := range sk.Schedules {
		si := ui.ScheduleInfo{Cron: s.Cron, PerPerson: s.PerPerson}
		if next := s.Parsed().Next(now); !next.IsZero() {
			si.Next = next.Format("2006-01-02 15:04")
		}
		info.Schedules = append(info.Schedules, si)
	}
	if t := sk.Target; t != nil {
		ti := &ui.TargetInfo{Plan: t.Plan, Agent: t.Agent, Confirm: t.Confirm, Timeout: t.TimeoutDuration().String()}
		switch {
		case t.Finish.Action != nil:
			ti.Finish = t.Finish.Action.Action
		default:
			ti.Finish = t.Finish.Name
		}
		for _, f := range t.Fields {
			ti.Fields = append(ti.Fields, ui.FieldInfo{Name: f.Name, Description: f.Description, Optional: f.Optional,
				Check: f.Check, Agent: f.Agent})
		}
		info.Target = ti
	}
	if m := sk.MCP; m != nil {
		mi := &ui.MCPInfo{Transport: string(m.Transport), Address: m.URL, Tools: m.Tools, Conversation: m.ForConversation(),
			WebContent: m.WebContent}
		if m.Transport == config.TransportStdio {
			mi.Address = m.Command
		}
		if a.mcp != nil {
			for _, st := range a.mcp.Status() {
				if st.Skill == sk.Name {
					mi.Connected, mi.Error, mi.Missing = st.Connected, st.Error, st.Missing
				}
			}
		}
		info.MCP = mi
	}
	info.Agents = sortedNames(sk.Agents)
	info.Kinds = skillKinds(sk)
	return info
}

// skillKinds names what a skill brings, for the view's badges.
func skillKinds(sk skills.Skill) []string {
	var out []string
	switch {
	case sk.Target != nil && sk.Target.Plan:
		out = append(out, "plan")
	case sk.Target != nil:
		out = append(out, "thread")
	}
	if len(sk.Schedules) > 0 {
		out = append(out, "schedules")
	}
	if sk.Mail != nil {
		out = append(out, "mail")
	}
	if sk.MCP != nil {
		out = append(out, "mcp")
	}
	if len(sk.Agents) > 0 {
		out = append(out, "agents")
	}
	if len(out) == 0 {
		out = append(out, "guide")
	}
	return out
}

// modelInfo fills in a model's name, privacy and fallback.
func (a *app) modelInfo(info *ui.AgentInfo, name string) {
	info.Model = name
	if m, ok := a.cfg.Model(name); ok {
		info.ModelPrivacy, info.Fallback = m.Privacy, m.Fallback
		if info.MaxRounds == 0 {
			info.MaxRounds = m.MaxToolRounds
		}
	}
}

// agentInfo is what the view shows of a YAML agent: from its registered
// description, or its problem if it did not start.
func (a *app) agentInfo(d agent.Defined, defs []agent.Defined, active []skills.Skill, problem string) ui.AgentInfo {
	info := ui.AgentInfo{Name: d.Name, Description: strings.TrimSpace(d.Def.Description), Origin: "config", Skill: d.Skill,
		Conversation: d.Def.Conversation, Privacy: d.Def.Privacy, Problem: problem}
	if d.Skill != "" {
		info.Origin = "skill"
	}
	if spec, ok := a.agents.Lookup(d.Name); ok && problem == "" {
		info.Description = spec.Description
		info.Context = map[agent.Profile]string{agent.Isolated: "isolated", agent.Background: "background"}[spec.Profile]
		switch spec.Tools.Kind {
		case agent.ListedTools:
			info.Tools, info.ToolNames = "list", spec.Tools.Names
		case agent.ReadOnlyTools:
			info.Tools = "read_only"
		default:
			info.Tools = "none"
		}
		info.Rights = strings.Split(spec.Rights.String(), ",")
		info.MaxRounds = spec.Limits.MaxRounds
		a.modelInfo(&info, spec.Model)
	}
	for _, sk := range active {
		t := sk.Target
		if t == nil {
			continue
		}
		if t.Agent == d.Name {
			info.Threads = append(info.Threads, sk.Name)
		}
		for _, f := range t.Fields {
			if f.Agent == d.Name {
				info.Threads = append(info.Threads, sk.Name+"."+f.Name)
			}
		}
	}
	for _, other := range defs {
		if other.Name != d.Name && slices.Contains(other.Def.Tools.Names, d.Name) {
			info.CalledBy = append(info.CalledBy, other.Name)
		}
	}
	return info
}
