package main

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/agent"
	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/testsupport/fakellm"
	"github.com/miraassist/mira/internal/tools"
	"gopkg.in/yaml.v3"
)

// nachdenkenApp is an app with the shipped skill nachdenken loaded.
func nachdenkenApp(t *testing.T, script ...fakellm.Step) (*app, *fakellm.Server) {
	t.Helper()
	a, _, model := deliveryApp(t, script...)
	loaded, problems := skills.Load([]string{"../../skills"}, map[string]map[string]any{"nachdenken": nil})
	if len(problems) > 0 || len(loaded) != 1 {
		t.Fatalf("nachdenken did not load: %v", problems)
	}
	a.skills = &skills.Set{Skills: loaded}
	return a, model
}

// AG-118 The conversation hands a question on through the skill nachdenken
func TestAG118_TheConversationHandsAQuestionOnThroughTheSkillNachdenken(t *testing.T) {
	// Given mira.yaml has no entry for the skill nachdenken
	// When Mira starts
	// Then the skill is on, as Mira's own skills are
	if !slices.Contains(alwaysOn(), "nachdenken") || !skills.Exists([]string{"../../skills"}, "nachdenken") {
		t.Fatal("nachdenken is not one of Mira's own skills")
	}
	// And the conversation has no tool delegate
	for _, it := range tools.Internal(i18n.DE, nil) {
		if it.Name == "delegate" {
			t.Error("the conversation still has the tool delegate")
		}
	}
	a, _ := nachdenkenApp(t)
	registry := tools.NewRegistry(nil)
	a.registerAgents(registry)
	for _, s := range registry.Specs(false) {
		if s.Name == "nachdenken" || s.Name == "delegate" {
			t.Errorf("%s is in every conversation", s.Name)
		}
	}
	// And once the conversation opened the skill, its agent nachdenken is a tool
	var names []string
	for _, d := range registry.ConversationSkillDefs("nachdenken") {
		names = append(names, d.Name)
	}
	if !slices.Contains(names, "nachdenken") {
		t.Errorf("after opening: %v", names)
	}
	// that reads the house and the web and changes nothing
	spec, ok := a.agents.Lookup("nachdenken")
	if !ok {
		t.Fatal("the agent nachdenken is not registered")
	}
	if spec.Profile != agent.Background || spec.Tools.Kind != agent.ReadOnlyTools || spec.Rights != agent.ReadHouse|agent.Web ||
		spec.ModelOf != "delegate" || spec.Skill != "nachdenken" {
		t.Errorf("spec %+v", spec)
	}
	if tool, _ := registry.Lookup("nachdenken"); !tool.ReadOnly || !tool.WebContent {
		t.Errorf("tool %+v", tool)
	}
}

// AG-119 The agent of nachdenken takes the model mira.yaml names for delegate
func TestAG119_TheAgentOfNachdenkenTakesTheModelOfDelegate(t *testing.T) {
	for _, tc := range []struct {
		name, agents, want string
	}{
		{"with agents.delegate", "delegate: { model: smart }", "smart"},
		{"without an entry", "", config.MainModel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, main := nachdenkenApp(t, fakellm.Step{Content: "Vom Hauptmodell."})
			// Given agents.delegate.model is smart (or no entry)
			smart := fakellm.New(fakellm.Step{Content: "Vom starken Modell."})
			t.Cleanup(smart.Close)
			cfg := a.cfg
			cfg.Models = map[string]config.LLM{}
			for k, v := range a.cfg.Models {
				cfg.Models[k] = v
			}
			cfg.Models["smart"] = config.LLM{BaseURL: smart.BaseURL(), Model: "cloud", Privacy: config.PrivacyCloud,
				Parallel: 1, MaxToolRounds: 3, MaxTokens: 512, ContextTokens: 100000, CompactAtFraction: 0.8}
			if err := yaml.Unmarshal([]byte(tc.agents), &cfg.Agents); err != nil {
				t.Fatal(err)
			}
			a.cfg, a.live = cfg, cfg
			a.models["smart"] = llm.New(llm.Options{BaseURL: smart.BaseURL(), Model: "cloud"})
			registry := tools.NewRegistry(nil)
			a.registerAgents(registry)
			// When the conversation calls nachdenken with a question
			out, isErr, err := registry.Execute(context.Background(), tools.Invocation{Name: "nachdenken",
				Args: json.RawMessage(`{"input":"Wie funktioniert eine Wärmepumpe?"}`)})
			if err != nil || isErr {
				t.Fatalf("result %q %v %v", out, isErr, err)
			}
			// Then the agent runs on that model (main without an entry) and its answer is the tool's result
			used, other := smart, main
			wantOut := "Vom starken Modell."
			if tc.want == config.MainModel {
				used, other, wantOut = main, smart, "Vom Hauptmodell."
			}
			if out != wantOut || len(used.Requests()) != 1 || len(other.Requests()) != 0 {
				t.Fatalf("answer %q, %d requests to the model, %d to the other", out, len(used.Requests()), len(other.Requests()))
			}
			// with the skill's instructions and the question
			last := used.Last().Request.Messages
			if c := last[len(last)-1].Content; !strings.Contains(c, "Wie funktioniert eine Wärmepumpe?") ||
				!strings.Contains(c, "gibt deine Antwort an den Nutzer weiter") {
				t.Errorf("the agent's turn: %q", c)
			}
		})
	}
}

// AG-120 The agent delegate stays callable from Home Assistant
func TestAG120_TheAgentDelegateStaysCallableFromHomeAssistant(t *testing.T) {
	a, _, _ := deliveryApp(t, fakellm.Step{Content: "Suppe am Samstag."})
	a.registerAgents(tools.NewRegistry(nil))
	// Given an automation calls mira.agent with the agent delegate
	res, err := a.agents.CallByName(context.Background(), "delegate", json.RawMessage(`{"input":"Plane ein Menü für Samstag."}`),
		agent.CallContext{Origin: agent.FromAutomation, Rights: agent.ReadHouse | agent.Web})
	// Then it answers as before
	if err != nil || res.Text != "Suppe am Samstag." {
		t.Fatalf("result %+v %v", res, err)
	}
	if !slices.Contains(agent.Callable, "delegate") {
		t.Error("delegate is not callable by name")
	}
}
