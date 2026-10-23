package conversation_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/agent"
	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/testsupport/fakellm"
	"github.com/miraassist/mira/internal/testsupport/fakemcp"
	"github.com/miraassist/mira/internal/tools"
)

// The skill paperless brings its MCP server (docs/spec/skill-mcp.md): a
// fake with four tools, two of them named by the skill, and a token.
type paperlessEnv struct {
	*harness
	server *fakemcp.Server
}

func newPaperless(t *testing.T, script ...fakellm.Step) *paperlessEnv {
	t.Helper()
	return newPaperlessWith(t, nil, script...)
}

// newPaperlessWith is newPaperless with a change to the skill's mcp section.
func newPaperlessWith(t *testing.T, change func(*config.MCPServer), script ...fakellm.Step) *paperlessEnv {
	t.Helper()
	p := &paperlessEnv{server: fakemcp.New("paperless")}
	t.Cleanup(p.server.Close)
	p.server.Token = "geheim"
	schema := `{"type":"object","properties":{"query":{"type":"string"},"id":{"type":"integer"}}}`
	p.server.AddTool("query_documents", "Volltextsuche", schema, false, func(map[string]any) (string, bool) {
		return `{"results":[{"id":7,"title":"Rechnung Schornsteinfeger"}]}`, false
	})
	p.server.AddTool("get_document_content", "Text eines Dokuments", schema, false, func(map[string]any) (string, bool) {
		return "Rechnungsbetrag: 86,40 Euro", false
	})
	p.server.AddTool("update_document", "Ändert ein Dokument", schema, false, func(map[string]any) (string, bool) { return "ok", false })
	p.server.AddTool("delete_tag", "Löscht einen Tag", schema, false, func(map[string]any) (string, bool) { return "ok", false })
	srv := config.MCPServer{Name: "paperless", Skill: "paperless", Transport: config.TransportHTTP,
		URL: p.server.Endpoint(), Token: "geheim", WebContent: true, ReadOnly: true,
		Tools: []string{"query_documents", "get_document_content"}}
	if change != nil {
		change(&srv)
	}
	set := &skills.Set{Lang: i18n.DE, Skills: []skills.Skill{{Name: "paperless", Description: "Dokumente durchsuchen",
		Body: "Such mit paperless__query_documents.", MCP: &srv}}}
	set.OnOpen = func(ctx context.Context, key, skill string) error { return p.store.AddSessionSkill(ctx, key, skill) }
	p.harness = newHarnessTools(t, func(c *config.Config) { c.MCPServers = append(c.MCPServers, srv) }, set.Tools(), script...)
	return p
}

// offered are the paperless tools a request offered.
func offered(req llm.ChatRequest) []string {
	var out []string
	for _, tl := range req.Tools {
		if strings.HasPrefix(tl.Function.Name, "paperless__") {
			out = append(out, tl.Function.Name)
		}
	}
	return out
}

func openSkill(id string) fakellm.Step {
	return fakellm.Step{ToolCalls: []llm.ToolCall{fakellm.ToolCall(id, "skill_open", `{"name":"paperless"}`)}}
}

// SM-01 A skill's server is connected with the skill's settings
func TestSM01_ASkillsServerIsConnectedWithItsSettings(t *testing.T) {
	// Given a skill with an mcp section whose url and token come from its settings, the token secret
	// When Mira starts
	p := newPaperless(t, openSkill("o1"), fakellm.Step{Content: "Offen."})
	// Then she connects to the server with that url and token
	if _, ok := p.engine.ToolsForTest().Lookup("paperless__query_documents"); !ok {
		t.Fatal("the skill's server is not connected (the fake requires the token)")
	}
	// And the token is in no prompt
	p.say(t, config.ChannelTelegram, "1", "Such in meinen Dokumenten.")
	for _, r := range p.llm.Requests() {
		if strings.Contains(string(r.RawBody), "geheim") {
			t.Fatal("the token reached a prompt")
		}
	}
}

// SM-02 A skill's tools are in no conversation before the skill is opened
func TestSM02_ASkillsToolsAreInNoConversationBeforeTheSkillIsOpened(t *testing.T) {
	// Given a skill with an mcp section
	p := newPaperless(t, fakellm.Step{Content: "Hallo."})
	// When a conversation starts
	p.say(t, config.ChannelTelegram, "1", "Hallo")
	// Then none of the server's tools is offered to the model
	if got := offered(p.llm.Last().Request); len(got) != 0 {
		t.Errorf("offered %v", got)
	}
}

// SM-03 Opening the skill brings its tools for the rest of the conversation
func TestSM03_OpeningTheSkillBringsItsTools(t *testing.T) {
	// Given a conversation in which the model opens the skill
	p := newPaperless(t, openSkill("o1"), fakellm.Step{Content: "Wonach soll ich suchen?"}, fakellm.Step{Content: "Gefunden."})
	p.say(t, config.ChannelTelegram, "1", "Such in meinen Dokumenten.")
	reqs := p.llm.Requests()
	// When the next turns of the same conversation run
	p.say(t, config.ChannelTelegram, "1", "Nach der Rechnung vom Schornsteinfeger.")
	// Then the skill's tools are offered in each of them
	if got := offered(reqs[1].Request); strings.Join(got, ",") != "paperless__get_document_content,paperless__query_documents" {
		t.Errorf("round after opening: %v", got)
	}
	if got := offered(p.llm.Last().Request); len(got) != 2 {
		t.Errorf("next turn: %v", got)
	}
	if got := offered(reqs[0].Request); len(got) != 0 {
		t.Errorf("before opening: %v", got)
	}
}

// SM-04 Only the tools the skill names
func TestSM04_OnlyTheToolsTheSkillNames(t *testing.T) {
	// Given a skill whose server has 44 tools and whose tools list names 2
	p := newPaperless(t, openSkill("o1"), fakellm.Step{Content: "Offen."})
	// When the skill is opened
	p.say(t, config.ChannelTelegram, "1", "Such in meinen Dokumenten.")
	// Then exactly those 2 are offered
	got := offered(p.llm.Last().Request)
	if len(got) != 2 || strings.Contains(strings.Join(got, ","), "update") || strings.Contains(strings.Join(got, ","), "delete") {
		t.Errorf("offered %v", got)
	}
}

// SM-05 Another conversation does not get them
func TestSM05_AnotherConversationDoesNotGetThem(t *testing.T) {
	// Given a conversation in the kitchen that opened the skill
	p := newPaperless(t, openSkill("o1"), fakellm.Step{Content: "Offen."}, fakellm.Step{Content: "Hallo."})
	p.say(t, config.ChannelTelegram, "1", "Such in meinen Dokumenten.")
	// When someone talks in the living room
	p.say(t, config.ChannelTelegram, "2", "Hallo")
	// Then the skill's tools are not offered there
	if got := offered(p.llm.Last().Request); len(got) != 0 {
		t.Errorf("offered %v", got)
	}
	// Nor can a model call one there.
	p.llm.Push(fakellm.Step{ToolCalls: []llm.ToolCall{fakellm.ToolCall("q", "paperless__query_documents", `{"query":"x"}`)}},
		fakellm.Step{Content: "?"})
	p.say(t, config.ChannelTelegram, "2", "Such trotzdem.")
	if len(p.server.Calls()) != 0 {
		t.Error("a conversation that did not open the skill called its tool")
	}
}

// SM-06 After the session expired the tools are gone
func TestSM06_AfterTheSessionExpiredTheToolsAreGone(t *testing.T) {
	// Given a conversation that opened the skill
	p := newPaperless(t, openSkill("o1"), fakellm.Step{Content: "Offen."}, fakellm.Step{Content: "Hallo."})
	p.say(t, config.ChannelVoice, "v1", "Such in meinen Dokumenten.")
	// When its session expires and a new one starts
	p.clock.Advance(time.Hour)
	p.say(t, config.ChannelVoice, "v1", "Hallo")
	// Then the skill's tools are not offered
	if got := offered(p.llm.Last().Request); len(got) != 0 {
		t.Errorf("offered %v", got)
	}
}

// SM-07 What the server returns is third-party text
func TestSM07_WhatTheServerReturnsIsThirdPartyText(t *testing.T) {
	// Given a skill whose mcp section has web_content true
	p := newPaperless(t, openSkill("o1"),
		// When the model calls one of its tools
		fakellm.Step{ToolCalls: []llm.ToolCall{fakellm.ToolCall("q", "paperless__get_document_content", `{"id":7}`)}},
		fakellm.Step{ToolCalls: []llm.ToolCall{fakellm.ToolCall("m", tools.ToolMemorySave, `{"text":"Der Betrag ist 86,40"}`)}},
		fakellm.Step{Content: "Soll ich das merken?"})
	p.say(t, config.ChannelTelegram, "1", "Lies mir die Rechnung vor.")
	// Then the turn counts as having read third-party text
	if facts, _ := p.store.AllFacts(context.Background()); len(facts) != 0 {
		t.Errorf("kept without asking: %+v", facts)
	}
	if len(p.server.Calls()) != 1 {
		t.Errorf("server calls %+v", p.server.Calls())
	}
}

// SM-08 The skill's schedules get its tools
func TestSM08_TheSkillsSchedulesGetItsTools(t *testing.T) {
	// Given a skill with an mcp section and a schedule
	p := newPaperless(t, fakellm.Step{Content: `{"importance":0,"spoken":""}`}, fakellm.Step{Content: `{"importance":0,"spoken":""}`})
	// When the schedule runs
	if _, err := p.engine.RunTask(context.Background(), "paperless", "Prüf die neuen Dokumente."); err != nil {
		t.Fatal(err)
	}
	// Then the skill's tools are offered to skill.task
	if got := offered(p.llm.Last().Request); len(got) != 2 {
		t.Errorf("offered %v", got)
	}
	// Another skill's schedule does not get them.
	p.engine.RunTask(context.Background(), "abfall", "Prüf den Kalender.")
	if got := offered(p.llm.Last().Request); len(got) != 0 {
		t.Errorf("another skill got %v", got)
	}
}

// recherche registers the skill's research agent, and one of mira.yaml that
// names the same tools.
func (p *paperlessEnv) recherche(t *testing.T) {
	t.Helper()
	main := func(string) (config.LLM, bool) { return config.Default().Main(), true }
	def := config.AgentDef{Description: "Durchsucht die Dokumente.", Instructions: "Such.", Context: "background",
		Rights: []string{"read_house", "web"},
		Tools:  config.AgentTools{Names: []string{"paperless__query_documents", "paperless__get_document_content"}}}
	if probs := p.engine.AgentsForTest().RegisterDefined([]agent.Defined{
		{Name: "paperless.recherche", Skill: "paperless", Def: def},
		{Name: "fremd", Def: def}}, main); len(probs) != 0 {
		t.Fatal(probs)
	}
}

// SM-12 A skill's agent works with the skill's tools
func TestSM12_ASkillsAgentWorksWithTheSkillsTools(t *testing.T) {
	// Given a skill with an mcp section and an agent whose tools list names two of its tools
	p := newPaperless(t,
		fakellm.Step{ToolCalls: []llm.ToolCall{fakellm.ToolCall("q", "paperless__query_documents", `{"query":"Wipfler"}`)}},
		fakellm.Step{Content: "Drei Rechnungen."}, fakellm.Step{Content: "Kenne ich nicht."})
	p.recherche(t)
	// When the agent runs
	res, err := p.engine.AgentsForTest().CallByName(context.Background(), "paperless.recherche",
		json.RawMessage(`{"input":"Rechnungen von Wipfler"}`), agent.CallContext{})
	if err != nil || res.Text != "Drei Rechnungen." {
		t.Fatalf("run: %q %v", res.Text, err)
	}
	// Then those tools are offered to it and it can call them
	reqs := p.llm.Requests()
	if got := offered(reqs[0].Request); len(got) != 2 || len(p.server.Calls()) != 1 {
		t.Errorf("offered %v, server calls %+v", got, p.server.Calls())
	}
	// And an agent of mira.yaml naming the same tools does not get them
	p.engine.AgentsForTest().CallByName(context.Background(), "fremd", json.RawMessage(`{"input":"x"}`), agent.CallContext{})
	if got := offered(p.llm.Last().Request); len(got) != 0 {
		t.Errorf("an agent of mira.yaml got %v", got)
	}
}

// SM-13 With conversation false only the skill's agents and schedules get the tools
func TestSM13_WithConversationFalseOnlyTheSkillsAgentsGetTheTools(t *testing.T) {
	// Given a skill whose mcp section has conversation false
	no := false
	p := newPaperlessWith(t, func(s *config.MCPServer) { s.Conversation = &no },
		openSkill("o1"), fakellm.Step{Content: "Offen."},
		fakellm.Step{ToolCalls: []llm.ToolCall{fakellm.ToolCall("q", "paperless__query_documents", `{"query":"x"}`)}},
		fakellm.Step{Content: "?"},
		fakellm.Step{ToolCalls: []llm.ToolCall{fakellm.ToolCall("q", "paperless__query_documents", `{"query":"x"}`)}},
		fakellm.Step{Content: "Gefunden."},
		fakellm.Step{Content: `{"importance":0,"spoken":""}`})
	// When a conversation opens the skill
	p.say(t, config.ChannelTelegram, "1", "Such in meinen Dokumenten.")
	// Then the server's tools are not offered to the conversation, nor can it call them
	if got := offered(p.llm.Last().Request); len(got) != 0 {
		t.Errorf("offered to the conversation: %v", got)
	}
	p.say(t, config.ChannelTelegram, "1", "Such trotzdem.")
	if len(p.server.Calls()) != 0 {
		t.Fatal("the conversation called a tool only for the skill's agents")
	}
	// But the skill's agent and its schedule get them
	p.recherche(t)
	if res, err := p.engine.AgentsForTest().CallByName(context.Background(), "paperless.recherche",
		json.RawMessage(`{"input":"x"}`), agent.CallContext{}); err != nil || len(p.server.Calls()) != 1 {
		t.Errorf("agent: %q %v, calls %+v", res.Text, err, p.server.Calls())
	}
	p.engine.RunTask(context.Background(), "paperless", "Prüf.")
	if got := offered(p.llm.Last().Request); len(got) != 2 {
		t.Errorf("schedule offered %v", got)
	}
}
