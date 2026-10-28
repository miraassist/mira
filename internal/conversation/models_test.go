package conversation_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/agent"
	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/conversation"
	"github.com/miraassist/mira/internal/hooks"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/testsupport/fakellm"
	"github.com/miraassist/mira/internal/tools"

	"gopkg.in/yaml.v3"
)

// twoModels is an engine with a local main model (slots, cache_prompt) and
// a cloud model "smart" that serves Telegram and delegated tasks.
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

	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "mira.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	var engine *conversation.Engine
	delegate := func(ctx context.Context, task string) (string, error) { return engine.Delegate(ctx, task) }
	registry := tools.NewRegistry(nil, tools.Internal(i18n.DE, st, delegate)...)
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
		Logger: quietLogger(),
	})
	engine = conversation.New(conversation.Options{
		Config: cfg,
		LLM:    mainClient,
		Models: map[string]conversation.Chatter{"smart": smartClient},
		Store:  st, Tools: registry, Agents: agents,
		Structure: houseSnapshot,
		Guard:     guard,
		Logger:    quietLogger(), Location: time.UTC,
	})
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

func TestDelegateHandsATaskToTheStrongerModel(t *testing.T) {
	e, main, smart := twoModels(t)
	main.Push(
		fakellm.Step{ToolCalls: []llm.ToolCall{fakellm.ToolCall("d1", "delegate", `{"task":"Plane ein Menü für vier Personen."}`)}},
		fakellm.Step{Content: "Vorspeise Suppe, dann Risotto."},
	)
	// The delegated model tries to delegate again; that is refused.
	smart.Push(
		fakellm.Step{ToolCalls: []llm.ToolCall{fakellm.ToolCall("d2", "delegate", `{"task":"noch einmal"}`)}},
		fakellm.Step{Content: "Suppe, Risotto, Tiramisu."},
	)
	r := handle(t, e, config.ChannelVoice, "Was koche ich heute für vier?")
	if r.Text != "Vorspeise Suppe, dann Risotto." {
		t.Fatalf("answer: %q", r.Text)
	}

	reqs := smart.Requests()
	if len(reqs) != 2 {
		t.Fatalf("delegate model got %d requests", len(reqs))
	}
	first := reqs[0].Request.Messages
	if last := first[len(first)-1].Content; !strings.Contains(last, "Plane ein Menü für vier Personen.") || !strings.Contains(last, "Abgegebene Aufgabe") {
		t.Errorf("delegated turn: %q", last)
	}
	refused := reqs[1].Request.Messages[len(reqs[1].Request.Messages)-1]
	if refused.Role != llm.RoleTool || !strings.Contains(refused.Content, "cannot be called from an agent's run") {
		t.Errorf("nested delegate not refused: %+v", refused)
	}
	// The fast model reads the stronger model's answer as the tool result.
	back := main.Last().Request.Messages
	if got := back[len(back)-1]; got.Role != llm.RoleTool || got.Content != "Suppe, Risotto, Tiramisu." {
		t.Errorf("tool result: %+v", got)
	}
}

// AG-72 The delegate agent exists without an entry too
func TestAG72_TheDelegateAgentExistsWithoutAnEntry(t *testing.T) {
	// Given routing has nothing for delegate
	e, main, _ := twoModelsRouted(t, "")
	main.Push(
		fakellm.Step{ToolCalls: []llm.ToolCall{fakellm.ToolCall("d1", "delegate", `{"task":"Erkläre Ebbe und Flut."}`)}},
		fakellm.Step{Content: "Der Mond zieht am Wasser."},
		fakellm.Step{Content: "Der Mond macht das."},
	)
	// When the conversation is called
	r := handle(t, e, config.ChannelVoice, "Warum gibt es Ebbe?")
	// Then it has the tool delegate
	offered := false
	for _, tl := range main.Requests()[0].Request.Tools {
		offered = offered || tl.Function.Name == tools.ToolDelegate
	}
	if !offered {
		t.Fatal("no delegate tool")
	}
	// And the delegate agent takes the model main
	reqs := main.Requests()
	if len(reqs) != 3 || !strings.Contains(lastContent(reqs[1].Request.Messages), "Erkläre Ebbe und Flut.") || r.Text != "Der Mond macht das." {
		t.Errorf("%d requests to main, answer %q", len(reqs), r.Text)
	}
}

// Before the integration has sent what Assist may see, Mira answers that she
// waits and runs no tool: Home Assistant would act on what is exposed there,
// and the guard cannot know those names yet.
func TestTurnWaitsForTheExposureList(t *testing.T) {
	model := fakellm.New(fakellm.Step{Content: "must not be asked"})
	defer model.Close()
	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "mira.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg := config.Default()
	engine := conversation.New(conversation.Options{
		Config: cfg, LLM: llm.New(llm.Options{BaseURL: model.BaseURL(), Model: "local"}),
		Store: st, Tools: tools.NewRegistry(nil, tools.Internal(i18n.DE, st, nil)...),
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
	defer conversation.SetDelegateWait(100 * time.Millisecond)()
	type delivery struct {
		rec store.AgentRun
		cc  agent.CallContext
	}
	delivered := make(chan delivery, 1)
	// Given a voice conversation that calls an agent as a tool
	e, main, smart := twoModelsDelivering(t, "conversation.telegram: { model: smart }\ndelegate: { model: smart }",
		func(_ context.Context, rec store.AgentRun, cc agent.CallContext) { delivered <- delivery{rec, cc} })
	main.Push(
		fakellm.Step{ToolCalls: []llm.ToolCall{fakellm.ToolCall("d1", "delegate", `{"task":"Plane die Woche."}`)}},
		fakellm.Step{Content: "Ich melde mich, sobald der Plan steht."},
	)
	// When the agent is not done after 5 seconds
	smart.Push(fakellm.Step{Content: "Montag Suppe, Dienstag Nudeln.", Delay: 400 * time.Millisecond})
	r := handle(t, e, config.ChannelVoice, "Plan mir die Woche.")
	// Then Mira tells the user she will come back
	back := main.Last().Request.Messages
	if got := back[len(back)-1]; got.Role != llm.RoleTool || !strings.Contains(got.Content, "Nachricht") || r.Text != "Ich melde mich, sobald der Plan steht." {
		t.Fatalf("tool result %+v, answer %q", got, r.Text)
	}
	// And the run goes on and its result comes as a message to the room or the person
	select {
	case d := <-delivered:
		if d.rec.Status != "done" || d.rec.Output != "Montag Suppe, Dienstag Nudeln." || d.cc.Mode.Delivery.Route != agent.ToMessage ||
			d.cc.Mode.Delivery.Receiver != store.ReceiverHouse {
			t.Errorf("delivered %+v, %+v", d.rec, d.cc.Mode.Delivery)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the run was not delivered")
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
