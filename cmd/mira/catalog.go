package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/agent"
	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/setup"
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
	c.Models = a.modelInfos(ctx, c.Agents)
	c.Hooks = []ui.HookInfo{}
	for _, h := range a.hooks.Infos() {
		c.Hooks = append(c.Hooks, ui.HookInfo{Level: h.Level, Name: h.Name, Description: h.Description,
			Source: h.Source, Replaced: h.Replaced, Problem: h.Problem})
	}
	return c
}

// modelInfos are the models, main first: their settings, who takes them, how
// their calls went and what the stored runs of the last hour used.
func (a *app) modelInfos(ctx context.Context, agents []ui.AgentInfo) []ui.ModelInfo {
	names := sortedNames(a.cfg.Models)
	if i := slices.Index(names, config.MainModel); i > 0 {
		names = append([]string{config.MainModel}, slices.Delete(names, i, i+1)...)
	}
	fromHA := false
	if s, err := setup.Load(ctx, a.store); err == nil && s.LLM != nil {
		fromHA = true
	}
	type usage struct{ runs, tokens int }
	used := map[string]usage{}
	if runs, err := a.store.RecentAgentRuns(ctx, time.Now().Add(-time.Hour)); err == nil {
		for _, r := range runs {
			u := used[r.Usage.Model]
			u.runs++
			u.tokens += r.Usage.PromptTokens + r.Usage.CompletionTokens
			used[r.Usage.Model] = u
		}
	}
	stamp := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.In(time.Local).Format("2006-01-02 15:04:05")
	}
	out := []ui.ModelInfo{}
	for _, name := range names {
		m, _ := a.cfg.Model(name)
		info := ui.ModelInfo{Name: name, Endpoint: m.BaseURL, Model: m.Model, TokenSet: m.APIKey != "", Privacy: m.Privacy,
			Parallel: m.Parallel, Fallback: m.Fallback, FromHA: fromHA && name == config.MainModel,
			ContextTokens: m.ContextTokens, ReasoningEffort: m.ReasoningEffort, Temperature: m.Temperature,
			MaxToolRounds: m.MaxToolRounds, CachePrompt: m.CachePrompt, Warmup: m.WarmupOnStart || m.WarmupOnPrefixChange,
			Runs: used[name].runs, Tokens: used[name].tokens}
		if m.Timeout > 0 {
			info.Timeout = m.Timeout.Duration().String()
		}
		for _, ag := range agents {
			if ag.Model == name {
				info.UsedBy = append(info.UsedBy, ag.Name)
			}
		}
		for _, other := range names {
			if o, _ := a.cfg.Model(other); o.Fallback == name {
				info.FallbackFor = append(info.FallbackFor, other)
			}
		}
		if c := a.models[name]; c != nil {
			last := c.Last()
			info.LastOK, info.LastFailed, info.Problem = stamp(last.OK), stamp(last.Failed), last.Problem
		}
		out = append(out, info)
	}
	return out
}

// checkModel sends a model a short request with its own settings (address,
// model, token), the way Mira calls it: a server that is up but does not know
// the model, or refuses the token, fails here, not only one that is down.
func (a *app) checkModel(ctx context.Context, name string) ui.CheckResult {
	c := a.models[name]
	m, ok := a.cfg.Model(name)
	if c == nil || !ok {
		return ui.CheckResult{Error: fmt.Sprintf("no model %q", name)}
	}
	began := time.Now()
	resp, err := c.Chat(ctx, llm.ChatRequest{MaxTokens: 64, ReasoningEffort: m.ReasoningEffort,
		Messages: []llm.Message{{Role: "user", Content: "Antworte nur mit OK."}}})
	res := ui.CheckResult{MS: time.Since(began).Milliseconds()}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		res.Error = fmt.Sprintf("no answer within %s", time.Since(began).Round(time.Second))
	case err != nil:
		res.Error = err.Error()
	default:
		res.OK = true
		if len(resp.Choices) > 0 {
			res.Answer = strings.TrimSpace(resp.Choices[0].Message.Content)
		}
	}
	return res
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
	// The phone book: that it is on, its books and the box, never a contact
	// (docs/spec/telefon.md).
	if sk.Name == phoneSkill && a.phonebook != nil {
		st := a.phonebook.Status()
		pb := &ui.PhonebookInfo{Link: st.Link, Books: st.Books, Error: st.Error}
		if !st.ReadAt.IsZero() {
			pb.ReadAt = st.ReadAt.In(time.Local).Format("2006-01-02 15:04")
		}
		info.Phonebook = pb
	}
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
	if sk.Name == phoneSkill {
		out = append(out, "phonebook")
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
