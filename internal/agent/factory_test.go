package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/skills"
)

// fromYAML registers the agents of a mira.yaml's agents section.
func (e *env) fromYAML(t *testing.T, text string) []Problem {
	t.Helper()
	var file struct {
		Agents config.Agents `yaml:"agents"`
	}
	if err := yaml.Unmarshal([]byte(text), &file); err != nil {
		t.Fatal(err)
	}
	return e.rt.RegisterDefined(FromConfig(file.Agents.Defined), func(m string) bool { return m == "main" || m == "smart" })
}

// AG-A1 A YAML agent in mira.yaml can be called
func TestAGA1_AYAMLAgentInMiraYAMLCanBeCalled(t *testing.T) {
	e := setup(t)
	// Given an agent described in mira.yaml under agents
	problems := e.fromYAML(t, `
agents:
  max_depth: 2
  rezept:
    description: Macht aus einer Rezeptseite eine Liste von Schritten.
    model: smart
    input:
      type: object
      properties: { url: { type: string } }
      required: [url]
    instructions: Du bekommst eine Rezeptseite und gibst die Schritte als Liste zurück.
    output:
      type: object
      properties: { steps: { type: array, items: { type: string } } }
      required: [steps]
`)
	if len(problems) != 0 {
		t.Fatalf("problems: %+v", problems)
	}
	e.smart.steps = []step{{text: `{"steps":["Teig rühren"]}`}}
	// When an automation calls mira.agent with its name and a fitting input
	res, err := e.rt.CallByName(context.Background(), "rezept", json.RawMessage(`{"url":"https://example.com/kuchen"}`),
		CallContext{Origin: FromAutomation})
	// Then the agent runs with the instructions, input schema and output schema from the YAML
	if err != nil || res.Text != `{"steps":["Teig rühren"]}` {
		t.Fatalf("%+v, %v", res, err)
	}
	req := e.smart.reqs[0]
	if req.Messages[0].Content != "Du bekommst eine Rezeptseite und gibst die Schritte als Liste zurück." ||
		!strings.Contains(lastUser(req), "example.com/kuchen") || req.ResponseFormat == nil {
		t.Errorf("request %+v", req)
	}
	if _, err := e.rt.CallByName(context.Background(), "rezept", json.RawMessage(`{"link":"x"}`), CallContext{}); err == nil {
		t.Error("an input against the schema was taken")
	}
}

// AG-A2 A YAML agent in a skill can be called
func TestAGA2_AYAMLAgentInASkillCanBeCalled(t *testing.T) {
	e := setup(t)
	// Given an agent described in a skill under metadata.mira.agents
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "garten"), 0o755); err != nil {
		t.Fatal(err)
	}
	skill := `---
name: garten
description: Hilft im Garten.
metadata:
  mira:
    settings:
      pflanze:
        description: Was im Garten wächst.
        required: true
    agents:
      giessplan:
        description: Plant das Gießen für {{pflanze}}.
        instructions: Mach einen Gießplan für {{pflanze}}.
---
Der Garten.
`
	if err := os.WriteFile(filepath.Join(dir, "garten", "SKILL.md"), []byte(skill), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, problems := skills.Load([]string{dir}, map[string]map[string]any{"garten": {"pflanze": "Tomaten"}})
	if len(problems) != 0 || len(loaded) != 1 {
		t.Fatalf("skills %+v, problems %+v", loaded, problems)
	}
	var defs []Defined
	for name, d := range loaded[0].Agents {
		defs = append(defs, Defined{Name: name, Skill: "garten", Def: d})
	}
	if p := e.rt.RegisterDefined(defs, func(string) bool { return true }); len(p) != 0 {
		t.Fatalf("problems %+v", p)
	}
	// When it is called
	res, err := e.rt.CallByName(context.Background(), "giessplan", json.RawMessage(`{"input":"diese Woche"}`), CallContext{})
	// Then it runs, with the skill's settings filled into its placeholders
	if err != nil || res.Status != Done || e.main.reqs[0].Messages[0].Content != "Mach einen Gießplan für Tomaten." {
		t.Errorf("%+v, %v, system %q", res, err, e.main.reqs[0].Messages[0].Content)
	}
}

// AG-A3 A YAML agent without a model takes main
func TestAGA3_AYAMLAgentWithoutAModelTakesMain(t *testing.T) {
	e := setup(t)
	// Given a YAML agent without model
	e.fromYAML(t, `
agents:
  kurz:
    description: Fasst kurz zusammen.
    instructions: Fasse zusammen.
`)
	// When it runs
	res, _ := e.rt.CallByName(context.Background(), "kurz", json.RawMessage(`{"input":"x"}`), CallContext{})
	// Then it takes the model main
	if res.Model != "main" || e.main.calls() != 1 || e.smart.calls() != 0 {
		t.Errorf("model %q, main %d, smart %d", res.Model, e.main.calls(), e.smart.calls())
	}
}

// AG-A4 A YAML agent without rights may only read the house
func TestAGA4_AYAMLAgentWithoutRightsMayOnlyReadTheHouse(t *testing.T) {
	e := setup(t)
	// Given a YAML agent without rights
	e.fromYAML(t, `
agents:
  dj:
    description: Macht Musik.
    context: background
    tools: [look, radio_play]
    instructions: Spiel etwas.
`)
	// When it runs
	e.main.steps = []step{{calls: []llm.ToolCall{call("look", "{}"), call("radio_play", "{}")}}, {text: "ok"}}
	e.rt.CallByName(context.Background(), "dj", json.RawMessage(`{"input":"x"}`), CallContext{})
	// Then its effective rights are at most "read the house"
	if e.count("look") != 1 || e.count("radio_play") != 0 || !e.refused(t, "dj", "radio_play", "act_on_house") {
		t.Errorf("ran %v", e.ran)
	}
}

// AG-A5 A YAML agent cannot use the conversation profile
func TestAGA5_AYAMLAgentCannotUseTheConversationProfile(t *testing.T) {
	e := setup(t)
	// Given a YAML agent with context conversation
	// When Mira loads it
	problems := e.fromYAML(t, `
agents:
  plauder:
    description: Plaudert.
    context: conversation
    instructions: Plaudere.
`)
	// Then it is not started, and the problem names the profile
	if len(problems) != 1 || problems[0].Agent != "plauder" || !strings.Contains(problems[0].Text(i18n.DE), "„conversation“") {
		t.Fatalf("problems %+v", problems)
	}
	if _, err := e.rt.CallByName(context.Background(), "plauder", json.RawMessage(`{"input":"x"}`), CallContext{}); err == nil {
		t.Error("the agent was started")
	}
}

// What does not fit is left out with a reason; the others start.
func TestYAMLAgentProblems(t *testing.T) {
	e := setup(t)
	problems := e.fromYAML(t, `
agents:
  delegate: { description: x, instructions: x }
  Gross: { description: x, instructions: x }
  ohne: { instructions: x }
  tippfehler: { description: x, instructions: x, intructions: y }
  modell: { description: x, instructions: x, model: gibtsnicht }
  rechte: { description: x, instructions: x, rights: [fly] }
  werkzeug: { description: x, instructions: x, tools: read_only }
  gut: { description: x, instructions: x }
`)
	got := map[string]string{}
	for _, p := range problems {
		got[p.Agent] = p.kind
	}
	want := map[string]string{"delegate": "ownfields", "Gross": "name", "ohne": "nodesc", "tippfehler": "yaml",
		"modell": "model", "rechte": "rights", "werkzeug": "isolatedtools"}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("%s: problem %q, want %q", name, got[name], kind)
		}
	}
	if _, ok := e.rt.lookup("gut"); !ok || len(problems) != len(want) {
		t.Errorf("problems %+v", problems)
	}
}

func TestProblemTextsComplete(t *testing.T) {
	for kind := range problemTexts[i18n.DE] {
		if problemTexts[i18n.EN][kind] == "" {
			t.Errorf("%s has no English text", kind)
		}
	}
	if len(problemTexts[i18n.DE]) != len(problemTexts[i18n.EN]) {
		t.Error("the languages differ")
	}
}

// An agent without an input schema can be called without any input.
func TestAgentWithoutInputNeedsNone(t *testing.T) {
	e := setup(t)
	e.fromYAML(t, `
agents:
  wetter:
    description: Sagt, wie warm es ist.
    instructions: Sag, wie warm es ist.
`)
	for _, in := range []string{``, `{}`} {
		if res, err := e.rt.CallByName(context.Background(), "wetter", json.RawMessage(in), CallContext{}); err != nil || res.Status != Done {
			t.Errorf("input %q: %+v, %v", in, res, err)
		}
	}
}

// MO-12 Mira's own agents take only model and privacy
func TestMO12_MirasOwnAgentsTakeOnlyModelAndPrivacy(t *testing.T) {
	e := setup(t)
	// Given an entry for delegate under agents with instructions
	// When Mira loads it
	problems := e.fromYAML(t, `
agents:
  delegate: { model: smart, instructions: Sei knapp. }
  tasks.plan: { model: smart }
`)
	// Then the entry is not taken, the problem names the field, and Mira starts
	if len(problems) != 1 || problems[0].Agent != "delegate" || !strings.Contains(problems[0].Text(i18n.DE), "instructions") {
		t.Errorf("problems %+v", problems)
	}
	if _, ok := e.rt.lookup("tasks.plan"); ok {
		t.Error("an entry for one of Mira's own agents became an agent of its own")
	}
}

// MO-13 An unknown model of one of Mira's own agents is a problem
func TestMO13_AnUnknownModelOfAnOwnAgentIsAProblem(t *testing.T) {
	e := setup(t)
	// Given agents.delegate.model names a model that does not exist
	text := "agents:\n  delegate: { model: gibtsnicht }\n"
	// When Mira loads it
	problems := e.fromYAML(t, text)
	// Then the problem names the model, and delegate takes main
	if len(problems) != 1 || !strings.Contains(problems[0].Text(i18n.DE), "„gibtsnicht“") {
		t.Errorf("problems %+v", problems)
	}
	var file struct {
		Agents config.Agents `yaml:"agents"`
	}
	yaml.Unmarshal([]byte(text), &file)
	e.cfg.Agents = file.Agents
	if res, _ := e.rt.Call(context.Background(), Spec{Name: "delegate", Profile: Isolated}, "x", CallContext{}); res.Model != "main" {
		t.Errorf("model %q", res.Model)
	}
}
