package conversation_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/agent"
	fakeclock "github.com/miraassist/mira/internal/clock"
	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/conversation"
	"github.com/miraassist/mira/internal/hooks"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/store/storetest"
	"github.com/miraassist/mira/internal/testsupport/fakellm"
	"github.com/miraassist/mira/internal/tools"

	"gopkg.in/yaml.v3"
)

// twoModels is an engine with a local main model (slots, cache_prompt) and
// a cloud model "smart" that serves Telegram and the model of delegate,
// which the conversation's agent tool "denker" takes.
func twoModels(t *testing.T) (*conversation.Engine, *fakellm.Server, *fakellm.Server) {
	t.Helper()
	return twoModelsRouted(t, "conversation.telegram: { model: smart }\ndelegate: { model: smart }")
}

// twoModelsRouted gives the agents their models as mira.yaml's agents
// section does.
func twoModelsRouted(t *testing.T, agents string) (*conversation.Engine, *fakellm.Server, *fakellm.Server) {
	t.Helper()
	return twoModelsDelivering(t, agents, nil)
}

// twoModelsDelivering hands the finished asynchronous runs to deliver.
func twoModelsDelivering(t *testing.T, agentsYAML string, deliver agent.Deliverer, change ...func(*config.Config)) (*conversation.Engine, *fakellm.Server, *fakellm.Server) {
	t.Helper()
	return twoModelsOn(t, nil, agentsYAML, deliver, change...)
}

// twoModelsOn is twoModelsDelivering with the agents' runtime on clk (nil
// for the real one).
func twoModelsOn(t *testing.T, clk fakeclock.Clock, agentsYAML string, deliver agent.Deliverer, change ...func(*config.Config)) (*conversation.Engine, *fakellm.Server, *fakellm.Server) {
	t.Helper()
	main, smart := fakellm.New(), fakellm.New()
	t.Cleanup(main.Close)
	t.Cleanup(smart.Close)

	cfg := config.Default()
	m := cfg.Main()
	m.PinSlots = map[config.Channel]int{config.ChannelVoice: 0, config.ChannelTelegram: 1, config.ChannelBackground: 2}
	m.CachePrompt, m.MaxToolRounds, m.WarmupOnStart = true, 3, true
	cfg.Models[config.MainModel] = m
	cfg.Models["smart"] = config.LLM{
		BaseURL: smart.BaseURL(), Model: "cloud", Privacy: config.PrivacyCloud, Parallel: 4, MaxToolRounds: 3,
		MaxTokens: 512, ContextTokens: 100000, CompactAtFraction: 0.8, ReasoningEffort: "low",
		// It asks for warmups; a cloud model never gets them.
		WarmupOnStart: true, WarmupOnPrefixChange: true,
	}
	if err := yaml.Unmarshal([]byte(agentsYAML), &cfg.Agents); err != nil {
		t.Fatal(err)
	}
	for _, c := range change {
		c(&cfg)
	}
	cfg.HA.URL, cfg.HA.Token = "http://ha", "t"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}

	st, err := store.Open(context.Background(), store.Options{Path: storetest.Path(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	var engine *conversation.Engine
	registry := tools.NewRegistry(nil, tools.Internal(i18n.DE, st)...)
	mainClient := llm.New(llm.Options{BaseURL: main.BaseURL(), Model: "local"})
	smartClient := llm.New(llm.Options{BaseURL: smart.BaseURL(), Model: "cloud"})
	guard := tools.NewGuard(i18n.DE, cfg.Security, st, nil, hooks.Load("", i18n.DE, nil))
	agents := agent.New(agent.Options{
		Config: func() config.Config { return cfg },
		Models: func(name string) (agent.Chatter, config.LLM, bool) {
			switch name {
			case config.MainModel:
				return mainClient, cfg.Main(), true
			case "smart":
				return smartClient, cfg.Models["smart"], true
			}
			return nil, config.LLM{}, false
		},
		Tools: registry, Guard: guard, Runs: st, Deliver: deliver,
		Background: func(ctx context.Context, cc agent.CallContext, instructions, input string) (agent.Built, error) {
			return engine.BackgroundContext(ctx, cc, instructions, input)
		},
		Logger: quietLogger(), Clock: clk,
	})
	engine = conversation.New(conversation.Options{StructurePrompt: func() string { return harnessHousePrompt },
		Config: cfg,
		LLM:    mainClient,
		Models: map[string]conversation.Chatter{"smart": smartClient},
		Store:  st, Tools: registry, Agents: agents,
		Structure: houseSnapshot,
		Guard:     guard,
		Logger:    quietLogger(), Location: time.UTC,
	})
	// An agent as a tool of the conversation, as the skill nachdenken's:
	// it takes the model of delegate (cmd/mira builds such tools from
	// YAML; docs/spec/agents.md, An agent as a tool in a conversation).
	agents.Register(agent.Spec{Name: "denker", Description: "Denkt nach.", ModelOf: "delegate",
		Profile: agent.Background, Instructions: "Beantworte die Frage.", Tools: agent.Tools{Kind: agent.ReadOnlyTools},
		Rights: agent.ReadHouse | agent.Web})
	registry.Add(tools.InternalTool{Name: "denker", Description: "Denkt nach.", ReadOnly: true,
		Schema: `{"type":"object","properties":{"input":{"type":"string"}},"required":["input"]}`,
		Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
			res, err := agents.CallByName(ctx, "denker", inv.Args, engine.AgentCall(ctx))
			return engine.AgentAnswer(ctx, res, err)
		}})
	return engine, main, smart
}

func handle(t *testing.T, e *conversation.Engine, ch config.Channel, text string) conversation.Response {
	t.Helper()
	resp, err := e.Handle(context.Background(), conversation.Request{Channel: ch, ConversationID: "c", Text: text})
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// MO-11 Mira's own agents take the model mira.yaml names
func TestMO11_MirasOwnAgentsTakeTheModelMiraYAMLNames(t *testing.T) {
	// Given agents.conversation.telegram.model is smart
	e, main, smart := twoModels(t)
	main.Push(fakellm.Step{Content: "Hallo aus der Küche."})
	smart.Push(fakellm.Step{Content: "Hallo aus der Cloud."})

	if r := handle(t, e, config.ChannelVoice, "Hallo"); r.Text != "Hallo aus der Küche." || r.Model != "main" {
		t.Errorf("voice: %+v", r)
	}
	// When someone writes on Telegram
	// Then the turn runs on smart
	if r := handle(t, e, config.ChannelTelegram, "Hallo"); r.Text != "Hallo aus der Cloud." || r.Model != "smart" {
		t.Errorf("telegram: %+v", r)
	}

	local := main.Last()
	if !local.HasSlot || !strings.Contains(string(local.RawBody), `"cache_prompt":true`) {
		t.Error("the local model lost its slot or cache_prompt")
	}
	// The cloud model gets only standard fields: an unknown field makes some
	// providers reject the request.
	cloud := string(smart.Last().RawBody)
	for _, field := range []string{"id_slot", "cache_prompt"} {
		if strings.Contains(cloud, field) {
			t.Errorf("cloud request carries %s: %s", field, cloud)
		}
	}
	if !strings.Contains(cloud, `"reasoning_effort":"low"`) || !strings.Contains(cloud, `"model":"cloud"`) {
		t.Errorf("cloud request lacks its own settings: %.300s", cloud)
	}
}

// MO-07 Only models that want it are warmed up
func TestMO07_OnlyModelsThatWantItAreWarmedUp(t *testing.T) {
	// Given main local with warmup_on_start and smart cloud
	e, main, smart := twoModelsRouted(t, "conversation.telegram: { model: smart }")
	// When Mira starts
	e.WarmupAll(context.Background(), conversation.WarmupStartup)
	// Then main is warmed for the conversation channels whose agent takes main, smart is not warmed
	if len(smart.Requests()) != 0 {
		t.Errorf("the cloud model was warmed %d times", len(smart.Requests()))
	}
	// Slots are pinned: voice and background stay local, Telegram is cloud.
	if n := len(main.Requests()); n != 2 {
		t.Errorf("main warmed %d times, want 2", n)
	}
	// A prefix change warms only models with warmup_on_prefix_change.
	e.WarmupAll(context.Background(), "structure_changed")
	if n := len(main.Requests()); n != 2 {
		t.Errorf("main warmed again without warmup_on_prefix_change: %d", n)
	}
}

// An agent the conversation calls as a tool with the model of delegate runs
// on that model, and the conversation's model hears its answer.
func TestAnAgentAsAToolTakesTheModelOfDelegate(t *testing.T) {
	e, main, smart := twoModels(t)
	main.Push(
		fakellm.Step{ToolCalls: []llm.ToolCall{fakellm.ToolCall("d1", "denker", `{"input":"Plane ein Menü für vier Personen."}`)}},
		fakellm.Step{Content: "Vorspeise Suppe, dann Risotto."},
	)
	smart.Push(fakellm.Step{Content: "Suppe, Risotto, Tiramisu."})
	r := handle(t, e, config.ChannelVoice, "Was koche ich heute für vier?")
	if r.Text != "Vorspeise Suppe, dann Risotto." {
		t.Fatalf("answer: %q", r.Text)
	}
	reqs := smart.Requests()
	if len(reqs) != 1 {
		t.Fatalf("the model of delegate got %d requests", len(reqs))
	}
	if last := lastContent(reqs[0].Request.Messages); !strings.Contains(last, "Plane ein Menü für vier Personen.") || !strings.Contains(last, "Beantworte die Frage.") {
		t.Errorf("the agent's turn: %q", last)
	}
	// The fast model reads the stronger model's answer as the tool result.
	back := main.Last().Request.Messages
	if got := back[len(back)-1]; got.Role != llm.RoleTool || got.Content != "Suppe, Risotto, Tiramisu." {
		t.Errorf("tool result: %+v", got)
	}
	// The conversation has no tool delegate any more (Decided 44).
	for _, tl := range main.Requests()[0].Request.Tools {
		if tl.Function.Name == "delegate" {
			t.Error("the conversation still has the tool delegate")
		}
	}
}

// Before the integration has sent what Assist may see, Mira answers that she
// waits and runs no tool: Home Assistant would act on what is exposed there,
// and the guard cannot know those names yet.
func TestTurnWaitsForTheExposureList(t *testing.T) {
	model := fakellm.New(fakellm.Step{Content: "must not be asked"})
	defer model.Close()
	st, err := store.Open(context.Background(), store.Options{Path: storetest.Path(t)})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg := config.Default()
	engine := conversation.New(conversation.Options{StructurePrompt: func() string { return harnessHousePrompt },
		Config: cfg, LLM: llm.New(llm.Options{BaseURL: model.BaseURL(), Model: "local"}),
		Store: st, Tools: tools.NewRegistry(nil, tools.Internal(i18n.DE, st)...),
		Guard:  tools.NewGuard(i18n.DE, cfg.Security, st, nil, hooks.Load("", i18n.DE, nil)),
		Logger: quietLogger(), Location: time.UTC,
		// no Structure: the list has not arrived
	})
	resp := handle(t, engine, config.ChannelTelegram, "Mach das Licht an")
	if !strings.Contains(resp.Text, "warte noch auf die Verbindung mit Home Assistant") {
		t.Errorf("answer = %q", resp.Text)
	}
	if n := len(model.Requests()); n != 0 {
		t.Errorf("the model was asked %d times", n)
	}
}

// AG-99 An agent as a tool at a satellite answers late
func TestAG99_AnAgentAsAToolAtASatelliteAnswersLate(t *testing.T) {
	defer conversation.SetAgentWait(100 * time.Millisecond)()
	type delivery struct {
		rec store.AgentRun
		cc  agent.CallContext
	}
	delivered := make(chan delivery, 1)
	// Given a voice conversation that calls an agent as a tool
	clk := fakeclock.NewFake(time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC))
	e, main, smart := twoModelsOn(t, clk, "conversation.telegram: { model: smart }\ndelegate: { model: smart }",
		func(_ context.Context, rec store.AgentRun, cc agent.CallContext) { delivered <- delivery{rec, cc} })
	main.Push(
		fakellm.Step{ToolCalls: []llm.ToolCall{fakellm.ToolCall("d1", "denker", `{"input":"Plane die Woche."}`)}},
		fakellm.Step{Content: "Ich melde mich, sobald der Plan steht."},
	)
	// When the agent is not done after 5 seconds
	hold := make(chan struct{})
	smart.Push(fakellm.Step{Content: "Montag Suppe, Dienstag Nudeln.", Hold: hold})
	answered := make(chan conversation.Response, 1)
	go func() { answered <- handle(t, e, config.ChannelVoice, "Plan mir die Woche.") }()
	<-clk.AwaitWaiters(2) // the run's time limit and the conversation's wait for it
	clk.Advance(100 * time.Millisecond)
	r := <-answered
	close(hold)
	// Then Mira tells the user she will come back
	back := main.Last().Request.Messages
	if got := back[len(back)-1]; got.Role != llm.RoleTool || !strings.Contains(got.Content, "Nachricht") || r.Text != "Ich melde mich, sobald der Plan steht." {
		t.Fatalf("tool result %+v, answer %q", got, r.Text)
	}
	// And the run goes on and its result comes as a message to the room or the person
	if d := <-delivered; d.rec.Status != "done" || d.rec.Output != "Montag Suppe, Dienstag Nudeln." || d.cc.Mode.Delivery.Route != agent.ToMessage ||
		d.cc.Mode.Delivery.Receiver != store.ReceiverHouse {
		t.Errorf("delivered %+v, %+v", d.rec, d.cc.Mode.Delivery)
	}
}

// MO-08 Slot pinning per model
func TestMO08_SlotPinningPerModel(t *testing.T) {
	// Given main with pin_slots for voice, telegram and background
	e, main, smart := twoModelsRouted(t, "conversation.telegram: { model: smart }")
	main.Push(fakellm.Step{Content: "Hallo."})
	smart.Push(fakellm.Step{Content: "Hallo."})
	// When a voice turn runs on main
	handle(t, e, config.ChannelVoice, "Hallo")
	// Then the request carries the voice slot; a model without pin_slots sends none
	if r := main.Last(); !r.HasSlot || r.Slot != 0 {
		t.Errorf("voice: slot %d (%v)", r.Slot, r.HasSlot)
	}
	handle(t, e, config.ChannelTelegram, "Hallo")
	if smart.Last().HasSlot {
		t.Error("smart has no pin_slots but sent id_slot")
	}
}

// A conversation takes the fallback of its failing model too, for the
// rest of the turn.
func TestConversationTakesTheFallback(t *testing.T) {
	e, main, smart := twoModelsDelivering(t, "conversation.telegram: { model: smart }", nil, func(c *config.Config) {
		s := c.Models["smart"]
		s.Fallback = config.MainModel
		c.Models["smart"] = s
	})
	smart.Push(fakellm.Step{Status: 503, Body: "overloaded"}, fakellm.Step{Status: 503, Body: "overloaded"},
		fakellm.Step{Status: 503, Body: "overloaded"})
	main.Push(fakellm.Step{Content: "Hallo von main."})
	r := handle(t, e, config.ChannelTelegram, "Hallo")
	if r.Text != "Hallo von main." || r.Model != "main" || r.FallbackFrom != "smart" {
		t.Errorf("answer %+v", r)
	}
}
