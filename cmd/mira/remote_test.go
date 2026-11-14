package main

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/miraassist/mira/internal/a2a"
	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/skills"
	a2afake "github.com/miraassist/mira/testenv/a2a_fake"
)

// A skill's remote agent starts no model agent and is no problem of the
// factory; what its card asks that Mira cannot do is a problem of the skill
// (docs/spec/remote-agents.md, decision 12), the page shows the card's
// description when it fits.
func TestRemoteAgentInTheApp(t *testing.T) {
	a, _, _ := deliveryApp(t)
	a.lang = i18n.DE
	srv := httptest.NewServer(a2afake.New(a2afake.Options{}))
	t.Cleanup(srv.Close)
	remote := func(card string) skills.Skill {
		return skills.Skill{Name: "recherche", Agents: map[string]config.AgentDef{
			"recherche": {Name: "Hermes", Remote: &config.RemoteDef{URL: srv.URL + card, Token: a2afake.DefaultToken}}}}
	}
	a.skills = &skills.Set{Skills: []skills.Skill{remote("/" + a2afake.CardNeither)}}
	a.problems = nil
	a.registerAgents(nil)
	if len(a.problems) != 0 {
		t.Fatalf("factory problems: %+v", a.problems)
	}
	if _, ok := a.agents.Lookup("recherche"); ok {
		t.Error("a remote agent is registered as a model agent")
	}
	a.remote = a2a.New(a2a.Options{Skills: a.skills.Skills, Logger: a.log})
	a.remote.Start(context.Background())
	var found bool
	for _, p := range a.allProblems() {
		if p.Source == "skill" && p.Name == "recherche" && strings.HasPrefix(p.Text, "Hermes ") {
			found = true
		}
	}
	if !found {
		t.Errorf("problems %+v", a.allProblems())
	}

	a.skills = &skills.Set{Skills: []skills.Skill{remote("")}}
	a.remote = a2a.New(a2a.Options{Skills: a.skills.Skills, Logger: a.log})
	a.remote.Start(context.Background())
	for _, p := range a.allProblems() {
		if p.Name == "recherche" {
			t.Errorf("a ready remote agent is a problem: %+v", p)
		}
	}
	c := a.catalog(context.Background())
	for _, info := range c.Agents {
		if info.Name == "recherche" && info.Description != a2afake.Description {
			t.Errorf("page: %+v", info)
		}
	}
}

// A remote agent in mira.yaml is refused: it belongs to a skill.
func TestRemoteAgentOnlyInASkill(t *testing.T) {
	a, _, _ := deliveryApp(t)
	a.lang = i18n.EN
	a.problems = nil
	a.skills = &skills.Set{}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("remote: { url: http://agent.example }"), &doc); err != nil {
		t.Fatal(err)
	}
	a.cfg.Agents.Defined = map[string]yaml.Node{"recherche": *doc.Content[0]}
	a.registerAgents(nil)
	if len(a.problems) != 1 || !strings.Contains(a.problems[0].Text, "only for the agent of a skill") {
		t.Errorf("problems %+v", a.problems)
	}
}
