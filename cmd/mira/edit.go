package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/miraassist/mira/internal/agent"
	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/mcp"
	"github.com/miraassist/mira/internal/phone"
	"github.com/miraassist/mira/internal/setup"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/ui"
)

// editor is the editing on Mira's page (docs/spec/bearbeiten.md): it writes
// mira.yaml and the user's own SKILL.md files; what it saves holds after a
// restart.
type editor struct {
	a    *app
	file config.File
	// mu keeps one change at a time; changed says one was saved.
	mu      sync.Mutex
	changed bool
	// probe tries a model (setup.Probe; tests set their own).
	probe func(context.Context, setup.LLM) error
}

// editor is the page's editor, nil (no editing) without a config path.
func (a *app) editor() ui.Editor {
	if a.configPath == "" {
		return nil
	}
	if a.edit == nil {
		a.edit = &editor{a: a, file: config.File{Path: a.configPath, Secrets: config.DefaultSecrets()}, probe: setup.Probe}
	}
	return a.edit
}

var modelNamePattern = regexp.MustCompile(`^[a-z0-9]+([_-][a-z0-9]+)*$`)

// --- state ---------------------------------------------------------------

func (e *editor) State(ctx context.Context) ui.EditState {
	e.mu.Lock()
	changed := e.changed
	e.mu.Unlock()
	st := ui.EditState{File: e.file.Path, Encrypted: e.file.Secrets.Encrypted(), RestartNeeded: changed,
		Models: map[string]ui.ModelEdit{}, Agents: map[string]ui.AgentEdit{}, Skills: map[string]ui.SkillEdit{},
		Providers: llm.Providers}
	if err := e.file.Writable(); err != nil {
		st.ReadOnly = err.Error()
	}
	doc, err := e.file.Read()
	if err != nil {
		st.ReadOnly = err.Error()
		return st
	}
	top := doc.Content[0]
	e.modelsState(ctx, top, &st)
	e.agentsState(top, &st)
	e.skillsState(top, &st)
	return st
}

func (e *editor) fromHA(ctx context.Context) bool {
	s, err := setup.Load(ctx, e.a.store)
	return err == nil && s.LLM != nil
}

func (e *editor) modelsState(ctx context.Context, top *yaml.Node, st *ui.EditState) {
	models := config.MapGet(top, "models")
	names := config.Keys(models)
	for name := range e.a.cfg.Models {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	fromHA := e.fromHA(ctx)
	for _, name := range names {
		var m ui.ModelEdit
		if n := config.MapGet(models, name); n != nil {
			var raw map[string]any
			_ = n.Decode(&raw)
			m = ui.ModelEdit{Endpoint: str(raw["endpoint"]), Model: str(raw["model"]), Privacy: str(raw["privacy"]),
				Parallel: num(raw["parallel"]), Fallback: str(raw["fallback"]), ContextTokens: num(raw["context_tokens"]),
				ReasoningEffort: str(raw["reasoning_effort"]), MaxToolRounds: num(raw["max_tool_rounds"]),
				Timeout: str(raw["timeout"]), TokenSet: str(raw["token"]) != "", InFile: true}
			if t, ok := raw["temperature"].(float64); ok {
				m.Temperature = &t
			} else if t, ok := raw["temperature"].(int); ok {
				f := float64(t)
				m.Temperature = &f
			}
		} else {
			l := e.a.cfg.Models[name]
			m = ui.ModelEdit{Endpoint: l.BaseURL, Model: l.Model, Privacy: l.Privacy, Parallel: l.Parallel,
				Fallback: l.Fallback, TokenSet: l.APIKey != ""}
		}
		m.FromHA = fromHA && name == config.MainModel
		st.Models[name] = m
	}
}

func (e *editor) agentsState(top *yaml.Node, st *ui.EditState) {
	agents := config.MapGet(top, "agents")
	for _, name := range agent.OwnNames() {
		a := ui.AgentEdit{Own: true, UsedBy: []string{"Mira"}}
		var raw map[string]any
		if n := config.MapGet(agents, name); n != nil {
			_ = n.Decode(&raw)
		}
		a.Model, a.Privacy = str(raw["model"]), str(raw["privacy"])
		st.Agents[name] = a
	}
	for _, name := range config.Keys(agents) {
		if name == "max_depth" || agent.Own(name) {
			continue
		}
		n := config.MapGet(agents, name)
		var raw map[string]any
		_ = n.Decode(&raw)
		a := ui.AgentEdit{Description: str(raw["description"]), Model: str(raw["model"]), Privacy: str(raw["privacy"]),
			Context: str(raw["context"]), Conversation: raw["conversation"] == true, MaxRounds: num(raw["max_rounds"]),
			Instructions: str(raw["instructions"]), Input: yamlText(config.MapGet(n, "input")),
			Output: yamlText(config.MapGet(n, "output")), UsedBy: e.agentUsers(top, name)}
		if tools := config.MapGet(n, "tools"); tools != nil {
			if tools.Kind == yaml.SequenceNode {
				_ = tools.Decode(&a.Tools)
			} else {
				a.ToolsWord = tools.Value
			}
		}
		if rights := config.MapGet(n, "rights"); rights != nil {
			_ = rights.Decode(&a.Rights)
		}
		st.Agents[name] = a
	}
}

func (e *editor) skillsState(top *yaml.Node, st *ui.EditState) {
	entries := config.MapGet(top, "skills")
	secrets, _ := e.file.Secrets.Load()
	names := map[string]bool{}
	for _, info := range skills.Available(e.a.cfg.Skills.Dirs, nil) {
		names[info.Name] = true
	}
	for _, n := range config.Keys(entries) {
		if n != "dirs" {
			names[n] = true
		}
	}
	for name := range names {
		dir := skills.Find(e.a.cfg.Skills.Dirs, name)
		if dir == "" {
			continue
		}
		f, err := skills.ReadFile(dir)
		if err != nil {
			st.Skills[name] = ui.SkillEdit{ReadOnly: err.Error()}
			continue
		}
		values := config.MapGet(entries, name)
		s := ui.SkillEdit{Enabled: skillOn(values, name), Settings: map[string]string{}, Own: !bundled(dir)}
		for _, d := range f.SettingDecls() {
			def := ui.SettingDef{Name: d.Name, Description: d.Description, Required: d.Required, Secret: d.Secret}
			v := config.MapGet(values, d.Name)
			if d.Secret {
				def.SecretSet = v != nil && v.Value != "" && secretValue(v.Value, secrets) != ""
				if v != nil && !isRef(v.Value) && v.Value != "" {
					def.SecretSet = true
				}
			} else if v != nil {
				s.Settings[d.Name] = nodeText(v)
			}
			s.SettingDefs = append(s.SettingDefs, def)
		}
		if s.Own {
			if err := writableFile(filepath.Join(dir, "SKILL.md")); err != nil {
				s.ReadOnly = err.Error()
			}
			s.Description = str(scalarValue(config.MapGet(f.Front, "description")))
			s.Guide = f.Body
			mira := config.MapGet(config.MapGet(f.Front, "metadata"), "mira")
			s.Agents = yamlText(config.MapGet(mira, "agents"))
			s.Target = yamlText(config.MapGet(mira, "target"))
			s.MCP = yamlText(config.MapGet(mira, "mcp"))
		}
		st.Skills[name] = s
	}
}

// --- models --------------------------------------------------------------

func (e *editor) SaveModel(ctx context.Context, name string, m ui.ModelEdit) error {
	newName := strings.TrimSpace(m.Name)
	if newName == "" {
		newName = name
	}
	switch {
	case name == config.MainModel && newName != name:
		return errors.New("main kann nicht umbenannt werden: Es ist das Modell, das jeder Teil ohne eigenes Modell nimmt.")
	case name == config.MainModel && e.fromHA(ctx):
		return errors.New("main kommt aus der Einrichtung in Home Assistant: Ändern geht dort unter Einstellungen → Geräte & Dienste → Mira → Konfigurieren.")
	case !modelNamePattern.MatchString(newName):
		return fmt.Errorf("Der Name „%s“ geht nicht: kleine Buchstaben und Ziffern, verbunden mit - oder _.", newName)
	}
	// A new server, model or key is tried first: what does not answer is
	// not saved (docs/spec/bearbeiten.md).
	if old, ok := e.State(ctx).Models[name]; !ok || old.Endpoint != m.Endpoint || old.Model != m.Model || m.Token != "" {
		if res := e.check(ctx, ui.ModelRequest{Name: name, Endpoint: m.Endpoint, Model: m.Model, Token: m.Token, Privacy: m.Privacy}, false); !res.OK {
			return fmt.Errorf("%s (%s)", probeProblems[res.Reason], res.Error)
		}
	}
	var secret map[string]string
	tokenVar := config.SecretName("MODEL", newName, "TOKEN")
	if m.Token != "" {
		secret = map[string]string{tokenVar: m.Token}
	}
	return e.change(secret, func(top *yaml.Node) error {
		if newName != config.MainModel && config.MapGet(config.MapGet(top, "models"), config.MainModel) == nil && !e.fromHA(ctx) {
			return errors.New("main fehlt: Es steht weder in der mira.yaml noch kommt es aus der Einrichtung in Home Assistant. Richte zuerst main ein.")
		}
		models := config.MapChild(top, "models")
		if newName != name {
			if config.MapGet(models, newName) != nil {
				return fmt.Errorf("Ein Modell „%s“ gibt es schon.", newName)
			}
			if config.MapGet(models, name) != nil {
				config.MapRename(models, name, newName)
				renameModelRefs(top, name, newName)
				e.moveSecret(config.SecretName("MODEL", name, "TOKEN"), tokenVar, config.MapGet(models, newName))
			}
		}
		n := config.MapGet(models, newName)
		if n == nil || n.Kind != yaml.MappingNode {
			n = &yaml.Node{Kind: yaml.MappingNode}
			config.MapSet(models, newName, n)
		}
		setStr(n, "endpoint", m.Endpoint)
		setStr(n, "model", m.Model)
		setStr(n, "privacy", m.Privacy)
		setInt(n, "parallel", m.Parallel)
		setStr(n, "fallback", m.Fallback)
		setInt(n, "context_tokens", m.ContextTokens)
		setStr(n, "reasoning_effort", m.ReasoningEffort)
		setInt(n, "max_tool_rounds", m.MaxToolRounds)
		setStr(n, "timeout", m.Timeout)
		if m.Temperature != nil {
			config.MapSet(n, "temperature", config.Scalar(strconv.FormatFloat(*m.Temperature, 'f', -1, 64)))
		} else {
			config.MapDelete(n, "temperature")
		}
		switch {
		case m.Token != "":
			config.MapSet(n, "token", config.Scalar("${"+tokenVar+"}"))
		case m.TokenClear:
			config.MapDelete(n, "token")
		}
		return nil
	})
}

func (e *editor) DeleteModel(ctx context.Context, name string) error {
	if name == config.MainModel {
		return errors.New("main kann nicht gelöscht werden.")
	}
	return e.change(nil, func(top *yaml.Node) error {
		models := config.MapGet(top, "models")
		if config.MapGet(models, name) == nil {
			return fmt.Errorf("Das Modell „%s“ steht nicht in der mira.yaml.", name)
		}
		if users := e.modelUsers(top, name); len(users) > 0 {
			return fmt.Errorf("Das Modell „%s“ wird noch genutzt von: %s.", name, strings.Join(users, ", "))
		}
		config.MapDelete(models, name)
		return nil
	})
}

// modelUsers are the agents that take a model and the models that fall
// back to it.
func (e *editor) modelUsers(top *yaml.Node, name string) []string {
	var out []string
	agents := config.MapGet(top, "agents")
	for _, a := range config.Keys(agents) {
		if m := config.MapGet(config.MapGet(agents, a), "model"); m != nil && m.Value == name {
			out = append(out, a)
		}
	}
	models := config.MapGet(top, "models")
	for _, m := range config.Keys(models) {
		if f := config.MapGet(config.MapGet(models, m), "fallback"); f != nil && f.Value == name {
			out = append(out, "Fallback von "+m)
		}
	}
	for _, sk := range e.a.skillsWithAgents() {
		for a, def := range sk.Agents {
			if def.Model == name {
				out = append(out, a+" ("+sk.Name+")")
			}
		}
	}
	sort.Strings(out)
	return out
}

// renameModelRefs points the agents and fallbacks of mira.yaml that name a
// model at its new name.
func renameModelRefs(top *yaml.Node, from, to string) {
	agents := config.MapGet(top, "agents")
	for _, a := range config.Keys(agents) {
		if m := config.MapGet(config.MapGet(agents, a), "model"); m != nil && m.Value == from {
			m.Value = to
		}
	}
	models := config.MapGet(top, "models")
	for _, m := range config.Keys(models) {
		if f := config.MapGet(config.MapGet(models, m), "fallback"); f != nil && f.Value == from {
			f.Value = to
		}
	}
}

// moveSecret carries a renamed model's token to its new name, when the entry
// refers to the old one.
func (e *editor) moveSecret(from, to string, entry *yaml.Node) {
	tok := config.MapGet(entry, "token")
	if tok == nil || tok.Value != "${"+from+"}" {
		return
	}
	all, err := e.file.Secrets.Load()
	if err != nil || all[from] == "" {
		return
	}
	if e.file.Secrets.Set(map[string]string{to: all[from], from: ""}) == nil {
		tok.Value = "${" + to + "}"
	}
}

// --- agents --------------------------------------------------------------

func (e *editor) SaveAgent(ctx context.Context, name string, a ui.AgentEdit) error {
	if name == "max_depth" {
		return errors.New("max_depth ist kein Agent.")
	}
	return e.change(nil, func(top *yaml.Node) error {
		agents := config.MapChild(top, "agents")
		n := config.MapGet(agents, name)
		if n == nil || n.Kind != yaml.MappingNode {
			n = &yaml.Node{Kind: yaml.MappingNode}
		}
		setStr(n, "model", a.Model)
		setStr(n, "privacy", a.Privacy)
		if !agent.Own(name) {
			setStr(n, "description", a.Description)
			setStr(n, "context", a.Context)
			switch {
			case len(a.Tools) > 0:
				tools, _ := config.NodeOf(a.Tools)
				tools.Style = yaml.FlowStyle
				config.MapSet(n, "tools", tools)
			case a.ToolsWord != "":
				config.MapSet(n, "tools", config.Scalar(a.ToolsWord))
			default:
				config.MapDelete(n, "tools")
			}
			if len(a.Rights) > 0 {
				rights, _ := config.NodeOf(a.Rights)
				rights.Style = yaml.FlowStyle
				config.MapSet(n, "rights", rights)
			} else {
				config.MapDelete(n, "rights")
			}
			if a.Conversation {
				config.MapSet(n, "conversation", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
			} else {
				config.MapDelete(n, "conversation")
			}
			setInt(n, "max_rounds", a.MaxRounds)
			if text := strings.TrimSpace(a.Instructions); text != "" {
				config.MapSet(n, "instructions", &yaml.Node{Kind: yaml.ScalarNode, Value: text, Style: yaml.LiteralStyle})
			} else {
				config.MapDelete(n, "instructions")
			}
			for _, part := range []struct{ key, text string }{{"input", a.Input}, {"output", a.Output}} {
				if err := setYAML(n, part.key, part.text); err != nil {
					return err
				}
			}
		}
		if len(n.Content) == 0 {
			config.MapDelete(agents, name)
		} else {
			config.MapSet(agents, name, n)
		}
		if len(n.Content) > 0 {
			d, err := config.DecodeAgent(n)
			def := agent.Defined{Name: name, Def: d, Err: err}
			if p := agent.Check(def, e.modelsAfter(top)); p != nil {
				return errors.New(p.Text(e.a.lang))
			}
		}
		return nil
	})
}

func (e *editor) DeleteAgent(ctx context.Context, name string) error {
	if agent.Own(name) {
		return errors.New("Miras eigene Agenten lassen sich nicht löschen; hier gehen nur Modell und privacy.")
	}
	return e.change(nil, func(top *yaml.Node) error {
		agents := config.MapGet(top, "agents")
		if config.MapGet(agents, name) == nil {
			return fmt.Errorf("Der Agent „%s“ steht nicht in der mira.yaml; die Agenten eines Skills ändert man beim Skill.", name)
		}
		if users := e.agentUsers(top, name); len(users) > 0 {
			return fmt.Errorf("Der Agent „%s“ wird noch genutzt von: %s.", name, strings.Join(users, ", "))
		}
		config.MapDelete(agents, name)
		return nil
	})
}

// agentUsers are the agents whose tools name an agent and the skills whose
// thread names it.
func (e *editor) agentUsers(top *yaml.Node, name string) []string {
	var out []string
	agents := config.MapGet(top, "agents")
	for _, a := range config.Keys(agents) {
		var tools []string
		if t := config.MapGet(config.MapGet(agents, a), "tools"); t != nil && t.Kind == yaml.SequenceNode {
			_ = t.Decode(&tools)
		}
		if slices.Contains(tools, name) {
			out = append(out, a)
		}
	}
	for _, sk := range e.a.skillsWithAgents() {
		for a, def := range sk.Agents {
			if slices.Contains(def.Tools.Names, name) {
				out = append(out, a+" ("+sk.Name+")")
			}
		}
	}
	for _, sk := range e.a.targetSkills {
		if sk.Target == nil {
			continue
		}
		if sk.Target.Agent == name {
			out = append(out, "Vorgang "+sk.Name)
		}
		for _, f := range sk.Target.Fields {
			if f.Agent == name {
				out = append(out, "Vorgang "+sk.Name+"."+f.Name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// modelsAfter are the models as the changed file has them, for checking an
// agent before it is written.
func (e *editor) modelsAfter(top *yaml.Node) func(string) (config.LLM, bool) {
	models := config.MapGet(top, "models")
	return func(name string) (config.LLM, bool) {
		if name == "" {
			name = config.MainModel
		}
		if n := config.MapGet(models, name); n != nil {
			var raw map[string]any
			_ = n.Decode(&raw)
			return config.LLM{Model: str(raw["model"]), Privacy: str(raw["privacy"])}, true
		}
		m, ok := e.a.cfg.Models[name]
		return m, ok
	}
}

// --- skills --------------------------------------------------------------

func (e *editor) SaveSkill(ctx context.Context, name string, s ui.SkillEdit) error {
	dir := skills.Find(e.a.cfg.Skills.Dirs, name)
	if dir == "" {
		return fmt.Errorf("Den Skill „%s“ gibt es nicht.", name)
	}
	f, err := skills.ReadFile(dir)
	if err != nil {
		return err
	}
	decls := f.SettingDecls()
	secrets := map[string]string{}
	for _, d := range decls {
		if v := strings.TrimSpace(s.Settings[d.Name]); d.Secret && v != "" {
			secrets[config.SecretName("SKILL", name, d.Name)] = v
		}
	}
	// The SKILL.md's new parts, checked before anything is written.
	writeSkill := false
	if len(s.Parts) > 0 {
		if bundled(dir) {
			return errors.New("Ein mitgelieferter Skill lässt sich hier nur ein- und ausschalten und einstellen: Ein Update von Mira ersetzt seine Dateien.")
		}
		if err := writableFile(filepath.Join(dir, "SKILL.md")); err != nil {
			return err
		}
		if err := applySkillParts(&f, s); err != nil {
			return err
		}
		if err := f.Check(name, e.checkSettings(name, f, s), e.a.lang); err != nil {
			return err
		}
		writeSkill = true
	}
	err = e.change(secrets, func(top *yaml.Node) error {
		entries := config.MapChild(top, "skills")
		values := config.MapGet(entries, name)
		if values == nil || values.Kind != yaml.MappingNode {
			values = &yaml.Node{Kind: yaml.MappingNode}
		}
		// Off keeps the settings; on is the default, so it is no line.
		if s.Enabled {
			config.MapDelete(values, config.SkillOn)
		} else {
			config.MapSet(values, config.SkillOn, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "false"})
		}
		for _, d := range decls {
			if !s.Enabled && s.Settings == nil {
				break // switching off alone leaves the settings as they are
			}
			v := strings.TrimSpace(s.Settings[d.Name])
			switch {
			case d.Secret && v != "":
				config.MapSet(values, d.Name, config.Scalar("${"+config.SecretName("SKILL", name, d.Name)+"}"))
			case d.Secret:
				// Empty keeps the secret as it is.
			case v == "":
				config.MapDelete(values, d.Name)
			case isList(config.MapGet(values, d.Name)):
				list, _ := config.NodeOf(splitList(v))
				config.MapSet(values, d.Name, list)
			default:
				config.MapSet(values, d.Name, config.Scalar(v))
			}
		}
		switch {
		case len(values.Content) > 0:
			config.MapSet(entries, name, values)
		case slices.Contains(alwaysOn(), name):
			// Mira's own skills are on without a line.
			config.MapDelete(entries, name)
		default:
			config.MapSet(entries, name, &yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle})
		}
		return nil
	})
	if err != nil {
		return err
	}
	if writeSkill {
		return f.Write(dir)
	}
	return nil
}

// applySkillParts puts the form's changed parts into the SKILL.md.
func applySkillParts(f *skills.File, s ui.SkillEdit) error {
	for _, part := range s.Parts {
		switch part {
		case "description":
			if strings.TrimSpace(s.Description) == "" {
				return errors.New("Die Beschreibung darf nicht leer sein: Danach entscheidet Mira, wann sie den Skill öffnet.")
			}
			config.MapSet(f.Front, "description", config.Scalar(strings.TrimSpace(s.Description)))
		case "guide":
			f.Body = s.Guide
		case "agents", "target", "mcp":
			text := map[string]string{"agents": s.Agents, "target": s.Target, "mcp": s.MCP}[part]
			if err := setYAML(f.Mira(), part, text); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown part %q", part)
		}
	}
	return nil
}

// checkSettings are the values a skill is checked with: the form's, else
// what Mira loaded, else a stand-in, so a check is about the file, not about
// a setting still to be entered.
func (e *editor) checkSettings(name string, f skills.File, s ui.SkillEdit) map[string]any {
	loaded := e.a.cfg.Skills.Enabled[name]
	out := map[string]any{}
	for _, d := range f.SettingDecls() {
		switch {
		case strings.TrimSpace(s.Settings[d.Name]) != "":
			out[d.Name] = s.Settings[d.Name]
		case loaded[d.Name] != nil:
			out[d.Name] = loaded[d.Name]
		default:
			out[d.Name] = "-"
		}
	}
	return out
}

func (e *editor) CheckMCP(ctx context.Context, name string, c ui.MCPCheckRequest) ui.MCPCheckResult {
	dir := skills.Find(e.a.cfg.Skills.Dirs, name)
	if dir == "" {
		return ui.MCPCheckResult{Error: fmt.Sprintf("Den Skill „%s“ gibt es nicht.", name)}
	}
	f, err := skills.ReadFile(dir)
	if err != nil {
		return ui.MCPCheckResult{Error: err.Error()}
	}
	if name == phoneSkill {
		return e.checkPhonebook(ctx)
	}
	settings := e.checkSettings(name, f, ui.SkillEdit{Settings: c.Settings})
	loaded, problems := skills.Load([]string{filepath.Dir(dir)}, map[string]map[string]any{name: settings})
	if len(problems) > 0 {
		return ui.MCPCheckResult{Error: problems[0].Text(e.a.lang)}
	}
	if len(loaded) == 0 || loaded[0].MCP == nil {
		return ui.MCPCheckResult{Error: "Der Skill hat keinen MCP-Server."}
	}
	began := time.Now()
	host := mcp.New(mcp.Options{Servers: []config.MCPServer{*loaded[0].MCP}, Logger: e.a.log, Version: version,
		CallTimeout: 15 * time.Second})
	_ = host.Start(ctx)
	defer host.Close()
	res := ui.MCPCheckResult{MS: time.Since(began).Milliseconds()}
	for _, st := range host.Status() {
		res.OK, res.Tools, res.Missing, res.Error = st.Connected, st.Tools, st.Missing, st.Error
	}
	return res
}

// checkPhonebook reads the phone book from Mira FRITZ!Box: how many
// contacts, never which.
func (e *editor) checkPhonebook(ctx context.Context) ui.MCPCheckResult {
	began := time.Now()
	pb, err := e.a.readPhonebook(ctx)
	res := ui.MCPCheckResult{MS: time.Since(began).Milliseconds()}
	switch {
	case errors.Is(err, phone.ErrNoIntegration):
		res.Error = phone.MissingText(e.a.lang)
	case err != nil:
		res.Error = err.Error()
	default:
		res.OK, res.Contacts, res.Books = true, len(pb.Contacts), pb.Books
	}
	return res
}

// probeProblems say in a sentence why a model did not answer, by the
// setup's reason.
var probeProblems = map[string]string{
	"unreachable":  "Mira erreicht diese Adresse nicht.",
	"bad_key":      "Der Anbieter hat den Schlüssel abgelehnt.",
	"no_model":     "Der Server kennt dieses Modell nicht, oder es ist nicht geladen.",
	"no_tools":     "Das Modell antwortet, benutzt aber keine Werkzeuge; die braucht Mira.",
	"server_error": "Der Server hat mit einem Fehler geantwortet.",
}

// ModelCheck tries the model as the form has it and, when it answers, asks
// its server how much context it has (docs/spec/modelle.md, Context size).
func (e *editor) ModelCheck(ctx context.Context, c ui.ModelRequest) ui.ModelCheckResult {
	return e.check(ctx, c, true)
}

func (e *editor) check(ctx context.Context, c ui.ModelRequest, withContext bool) ui.ModelCheckResult {
	key := e.modelKey(c.Name, c.Token)
	kind := setup.KindCustom
	if c.Privacy == "cloud" {
		kind = setup.KindCloud
	}
	if err := e.probe(ctx, setup.LLM{Kind: kind, BaseURL: c.Endpoint, Model: c.Model, APIKey: key}); err != nil {
		var me *setup.ModelError
		if !errors.As(err, &me) {
			me = &setup.ModelError{Reason: "server_error", Detail: err.Error()}
		}
		return ui.ModelCheckResult{Reason: me.Reason, Error: me.Detail}
	}
	res := ui.ModelCheckResult{OK: true}
	if withContext {
		res.ContextTokens, _ = llm.ContextSize(ctx, nil, c.Endpoint, key, c.Model)
		if res.ContextTokens == 0 {
			res.ContextTokens = e.a.cfg.Models[config.MainModel].ContextTokens
		}
	}
	return res
}

// modelKey is the key a check uses: the form's, else the one the model has
// in the secrets file or got at start.
func (e *editor) modelKey(name, token string) string {
	if token != "" {
		return token
	}
	if all, err := e.file.Secrets.Load(); err == nil {
		if v := all[config.SecretName("MODEL", name, "TOKEN")]; v != "" {
			return v
		}
	}
	return e.a.cfg.Models[name].APIKey
}

// ModelList asks the model's server which models it offers, with the
// form's token or the one the model has.
func (e *editor) ModelList(ctx context.Context, c ui.ModelRequest) ui.ModelListResult {
	key := e.modelKey(c.Name, c.Token)
	models, err := llm.ListModels(ctx, nil, c.Endpoint, key)
	if err != nil {
		return ui.ModelListResult{Error: err.Error()}
	}
	return ui.ModelListResult{Models: models}
}

// --- restart and writing ---------------------------------------------------

// Restart starts Mira anew in her own process, a moment after the answer.
func (e *editor) Restart() {
	time.Sleep(300 * time.Millisecond)
	restart()
}

// change writes secrets (when given) and then edits mira.yaml; when the edit
// fails, the secrets go back as they were.
func (e *editor) change(secrets map[string]string, fn func(top *yaml.Node) error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	var before map[string]string
	if len(secrets) > 0 {
		all, err := e.file.Secrets.Load()
		if err != nil {
			return err
		}
		before = map[string]string{}
		for k := range secrets {
			before[k] = all[k]
		}
		if err := e.file.Secrets.Set(secrets); err != nil {
			return err
		}
	}
	if err := e.file.Edit(fn); err != nil {
		if before != nil {
			_ = e.file.Secrets.Set(before)
		}
		return err
	}
	e.changed = true
	e.a.log.Info("config written on the page", "file", e.file.Path)
	return nil
}

// writeSections puts settings sections (YAML by name, what differs from
// the defaults) into mira.yaml (docs/spec/bearbeiten.md, Settings sections). keep are the
// notifier's parts the file keeps as they are. They hold at once, so no
// restart is asked for.
func (e *editor) writeSections(sections map[string][]byte, keep []string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	err := e.file.Edit(func(top *yaml.Node) error {
		for name, raw := range sections {
			var doc yaml.Node
			if err := yaml.Unmarshal(raw, &doc); err != nil {
				return err
			}
			node := &yaml.Node{Kind: yaml.MappingNode}
			if len(doc.Content) > 0 && doc.Content[0].Tag != "!!null" {
				node = doc.Content[0]
			}
			if name == "notifier" {
				old := config.MapGet(top, name)
				for _, k := range keep {
					if v := config.MapGet(old, k); v != nil {
						config.MapSet(node, k, v)
					} else {
						config.MapDelete(node, k)
					}
				}
			}
			// The section's place: its parents are made when missing.
			path := config.SectionPath(name)
			parent := top
			for _, k := range path[:len(path)-1] {
				parent = config.MapChild(parent, k)
			}
			key := path[len(path)-1]
			// A section at its defaults needs no entry, nor do parents
			// left empty.
			if node.Kind == yaml.MappingNode && len(node.Content) == 0 {
				config.MapDelete(parent, key)
				if parent != top && len(parent.Content) == 0 {
					config.MapDelete(top, path[0])
				}
				continue
			}
			config.MapSet(parent, key, node)
		}
		return nil
	})
	if err == nil {
		e.a.log.Info("settings written on the page", "file", e.file.Path)
	}
	return err
}

// --- helpers ---------------------------------------------------------------

// alwaysOn are the skills that are on without an entry.
func alwaysOn() []string { return append(builtinTargets(), builtinMail()...) }

// skillOn says whether a skill is on, by its entry under skills: there and
// not enabled: false, or one of Mira's own without an entry.
func skillOn(entry *yaml.Node, name string) bool {
	if entry == nil {
		return slices.Contains(alwaysOn(), name)
	}
	on := config.MapGet(entry, config.SkillOn)
	return on == nil || on.Value != "false"
}

// bundled says whether a skill's folder is in Mira's own skills folder,
// which an update replaces.
func bundled(dir string) bool {
	own := config.Default().Skills.Dirs[0]
	return filepath.Clean(filepath.Dir(dir)) == filepath.Clean(own)
}

func writableFile(path string) error {
	return config.File{Path: path}.Writable()
}

func str(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	default:
		return fmt.Sprint(x)
	}
}

func num(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case float64:
		return int(x)
	}
	return 0
}

func scalarValue(n *yaml.Node) any {
	if n == nil {
		return nil
	}
	return n.Value
}

func setStr(n *yaml.Node, key, v string) {
	if v = strings.TrimSpace(v); v == "" {
		config.MapDelete(n, key)
		return
	}
	config.MapSet(n, key, config.Scalar(v))
}

func setInt(n *yaml.Node, key string, v int) {
	if v == 0 {
		config.MapDelete(n, key)
		return
	}
	config.MapSet(n, key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(v)})
}

// setYAML puts YAML text under key; empty text removes it.
func setYAML(n *yaml.Node, key, text string) error {
	if strings.TrimSpace(text) == "" {
		config.MapDelete(n, key)
		return nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return fmt.Errorf("%s ist kein gültiges YAML: %v", key, err)
	}
	if len(doc.Content) == 0 {
		config.MapDelete(n, key)
		return nil
	}
	config.MapSet(n, key, doc.Content[0])
	return nil
}

// yamlText is a node as YAML text, for a text field.
func yamlText(n *yaml.Node) string {
	if n == nil {
		return ""
	}
	out, err := yaml.Marshal(n)
	if err != nil {
		return ""
	}
	return string(out)
}

// nodeText is a setting's value for its field: a list as "a, b".
func nodeText(n *yaml.Node) string {
	if n.Kind == yaml.SequenceNode {
		var items []string
		for _, c := range n.Content {
			items = append(items, c.Value)
		}
		return strings.Join(items, ", ")
	}
	return n.Value
}

func isList(n *yaml.Node) bool { return n != nil && n.Kind == yaml.SequenceNode }

func splitList(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

var refPattern = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)(:-[^}]*)?\}$`)

func isRef(v string) bool { return refPattern.MatchString(v) }

// secretValue is what a setting's ${NAME} stands for in the secrets file or
// the environment.
func secretValue(v string, secrets map[string]string) string {
	m := refPattern.FindStringSubmatch(v)
	if m == nil {
		return v
	}
	if s := secrets[m[1]]; s != "" {
		return s
	}
	s, _ := config.OSLookup(m[1])
	return s
}
