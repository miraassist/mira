package main

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/agent"
	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/conversation"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/notifier"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/testsupport/fakeha"
	"github.com/miraassist/mira/internal/testsupport/fakellm"
	"github.com/miraassist/mira/internal/threads"
	"github.com/miraassist/mira/internal/tools"
	"gopkg.in/yaml.v3"
)

// quietHA is Home Assistant as the notifier needs it, doing nothing.
type quietHA struct{}

func (quietHA) CallService(context.Context, string, string, map[string]any, *haws.Target) error {
	return nil
}
func (quietHA) States(context.Context) ([]haws.State, error)    { return nil, nil }
func (quietHA) Entities(context.Context) ([]haws.Entity, error) { return nil, nil }

// deliveryApp is the part of the app that delivers runs: Home Assistant,
// the agents with notify.compose on a scripted model, and the notifier.
func deliveryApp(t *testing.T, script ...fakellm.Step) (*app, *fakeha.Server, *fakellm.Server) {
	t.Helper()
	model := fakellm.New(script...)
	t.Cleanup(model.Close)
	ha := fakeha.New()
	t.Cleanup(ha.Close)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	client, err := haws.New(haws.Options{URL: ha.URL, Token: ha.Token, Logger: quiet})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go client.Run(ctx)
	for i := 0; !client.Connected(); i++ {
		if i > 200 {
			t.Fatal("home assistant not connected")
		}
		time.Sleep(10 * time.Millisecond)
	}
	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "mira.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	cfg := config.Default()
	setMain(&cfg, func(m *config.LLM) { m.BaseURL = model.BaseURL() })
	cfg.Persons = map[string]config.Person{"anna": {Name: "Anna"}}
	a := &app{cfg: cfg, live: cfg, log: quiet, ha: client, store: st, lang: i18n.DE,
		llm: llm.New(llm.Options{BaseURL: model.BaseURL(), Model: "test"})}
	a.models = map[string]*llm.Client{config.MainModel: a.llm}
	registry := tools.NewRegistry(nil)
	guard := tools.NewGuard(i18n.DE, cfg.Security, st, nil)
	a.agents = a.newAgents(registry, guard)
	a.engine = conversation.New(conversation.Options{Config: cfg, LLM: a.llm, Store: st, Tools: registry,
		Guard: guard, Agents: a.agents, Logger: quiet})
	a.notifier = notifier.New(notifier.Options{Config: cfg, Store: st, HA: quietHA{}, Logger: quiet,
		Composer: a.notifyCompose})
	return a, ha, model
}

// AG-40 Delivery as a Home Assistant event
func TestAG40_DeliveryAsAHomeAssistantEvent(t *testing.T) {
	a, ha, _ := deliveryApp(t)
	// Given an asynchronous call with the delivery route "HA event"
	cc := agent.CallContext{Mode: agent.Mode{Async: true, Delivery: agent.Delivery{Route: agent.ToEvent}}}
	// When the run is done
	a.deliverRun(context.Background(), store.AgentRun{CallID: "c1", Agent: "rezept", Status: "done",
		Output: `{"steps":["Teig"]}`}, cc)
	// Then Home Assistant fires the event mira_agent_done
	// And it holds call ID, agent, status and output
	cmds := ha.Commands()
	if len(cmds) != 1 || cmds[0]["type"] != "mira/agent/done" || cmds[0]["call_id"] != "c1" ||
		cmds[0]["agent"] != "rezept" || cmds[0]["status"] != "done" || cmds[0]["output"] != `{"steps":["Teig"]}` {
		t.Errorf("commands %+v", cmds)
	}
}

// messages are the messages Mira holds.
func messages(t *testing.T, a *app) []store.Message {
	t.Helper()
	list, err := a.store.ListMessages(context.Background(), store.MessageFilter{IncludeExpired: true, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	return list
}

// AG-41 Delivery as a message
func TestAG41_DeliveryAsAMessage(t *testing.T) {
	a, _, _ := deliveryApp(t, fakellm.Step{Content: `{"text":"Anna, dein Rezept ist zerlegt: fünf Schritte."}`})
	// Given an asynchronous call with the delivery route "message to a person"
	cc := agent.CallContext{Mode: agent.Mode{Async: true, Delivery: agent.Delivery{Route: agent.ToMessage, Receiver: "anna"}}}
	// When the run is done
	a.deliverRun(context.Background(), store.AgentRun{CallID: "c1", Agent: "rezept", Status: "done",
		Output: `{"steps":["Teig","Backen"]}`}, cc)
	// Then the person gets a message that the conversation worded from the result
	list := messages(t, a)
	if len(list) != 1 || list[0].Receiver != "anna" || list[0].Spoken != "Anna, dein Rezept ist zerlegt: fünf Schritte." {
		t.Errorf("messages %+v", list)
	}
}

// AG-100 A result delivered as a message is worded by the announcement agent
func TestAG100_AResultAsAMessageIsWordedByNotifyCompose(t *testing.T) {
	a, _, model := deliveryApp(t, fakellm.Step{Content: `{"text":"Die Einkaufsliste ist fertig."}`})
	// Given an asynchronous call with the delivery route "message"
	cc := agent.CallContext{Mode: agent.Mode{Async: true, Delivery: agent.Delivery{Route: agent.ToMessage}}}
	// When the run is done
	a.deliverRun(context.Background(), store.AgentRun{CallID: "c1", Agent: "liste", Status: "done",
		Output: `{"items":["Milch"]}`}, cc)
	// Then the message's spoken text comes from notify.compose
	// And the run's output is in the message's details
	list := messages(t, a)
	if len(list) != 1 || list[0].Spoken != "Die Einkaufsliste ist fertig." || list[0].Details != `{"items":["Milch"]}` ||
		list[0].Receiver != store.ReceiverHouse {
		t.Fatalf("messages %+v", list)
	}
	reqs := model.Requests()
	if len(reqs) != 1 || !strings.Contains(reqs[0].Request.Messages[len(reqs[0].Request.Messages)-1].Content, `"items":["Milch"]`) ||
		reqs[0].Request.ResponseFormat == nil {
		t.Errorf("notify.compose was not asked as expected: %+v", reqs)
	}
}

// AG-49 An unknown agent in a target is a problem of the skill
func TestAG49_AnUnknownAgentInATargetIsAProblemOfTheSkill(t *testing.T) {
	a := &app{cfg: config.Default()}
	a.cfg.Agents.Defined = map[string]yaml.Node{"wetter": {}}
	known := a.agentNames()
	// Given a skill whose target names an agent that does not exist
	target := &skills.Target{Fields: []skills.TargetField{{Name: "wetter", Agent: "wetter"}, {Name: "x", Agent: "gibtsnicht"}}}
	// When Mira starts
	name := unknownAgent(target, known)
	// Then the skill is left out, and the problem names the agent
	text := skills.NewProblem("geburtstag", skills.KindUnknownAgent, name).Text(i18n.DE)
	if name != "gibtsnicht" || !strings.Contains(text, "„gibtsnicht“") {
		t.Errorf("unknown %q, problem %q", name, text)
	}
	if unknownAgent(&skills.Target{Plan: true, Agent: "delegate"}, known) != "" {
		t.Error("Mira's own agent is unknown")
	}
}

// An agent a thread starts runs in the runtime, and its result reaches the
// thread through the delivery: the whole way, with the app's wiring.
func TestThreadAgentRunsAndFillsTheField(t *testing.T) {
	a, _, _ := deliveryApp(t, fakellm.Step{Content: "Topfschlagen"})
	if p := a.agents.RegisterDefined([]agent.Defined{{Name: "spielideen",
		Def: config.AgentDef{Description: "Spielideen.", Instructions: "Nenne ein Spiel."}}}, func(string) bool { return true }); len(p) != 0 {
		t.Fatal(p)
	}
	target := skills.Skill{Name: "geburtstag", Description: "Feier", Body: "Plane.", Target: &skills.Target{
		Finish: skills.Finish{Name: "memory.save"}, Fields: []skills.TargetField{
			{Name: "fact", Description: "was", Agent: "spielideen"}}}}
	a.threads = threads.New(threads.Options{Lang: i18n.DE, Store: a.store, Targets: []skills.Skill{target},
		Finishers: map[string]threads.Finisher{"memory.save": threads.MemorySave(a.store, i18n.DE)},
		Dispatch:  a.dispatchForThread, Cancel: a.agents.Cancel})
	ctx := context.Background()
	inv := tools.Invocation{SessionKey: "telegram:1", Channel: config.ChannelTelegram}
	if _, err := a.threads.Start(ctx, inv, "geburtstag", "Plane Tims Geburtstag"); err != nil {
		t.Fatal(err)
	}
	for i := 0; ; i++ {
		facts, _ := a.store.AllFacts(ctx)
		if len(facts) == 1 && facts[0].Text == "Topfschlagen" {
			break
		}
		if i > 200 {
			t.Fatalf("the agent's result did not reach the thread: %+v", facts)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// setMain changes the model main of a config.
func setMain(cfg *config.Config, change func(*config.LLM)) {
	m := cfg.Main()
	change(&m)
	cfg.Models[config.MainModel] = m
}
