package conversation_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/conversation"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/testsupport/fakellm"
	"github.com/miraassist/mira/internal/tools"
)

// twoModels is an engine with a local main model (slots, cache_prompt) and
// a cloud model "smart" that serves Telegram and delegated tasks.
func twoModels(t *testing.T) (*conversation.Engine, *fakellm.Server, *fakellm.Server) {
	t.Helper()
	main, smart := fakellm.New(), fakellm.New()
	t.Cleanup(main.Close)
	t.Cleanup(smart.Close)

	cfg := config.Default()
	cfg.LLM.SlotPinning, cfg.LLM.CachePrompt, cfg.LLM.MaxToolRounds = true, true, 3
	cfg.Models = map[string]config.LLM{"smart": {
		BaseURL: smart.BaseURL(), Model: "cloud", MaxToolRounds: 3, MaxTokens: 512,
		ContextTokens: 100000, CompactAtFraction: 0.8, ReasoningEffort: "low",
	}}
	cfg.Routing = config.Routing{Telegram: "smart", Delegate: "smart"}
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
	engine = conversation.New(conversation.Options{
		Config: cfg,
		LLM:    llm.New(llm.Options{BaseURL: main.BaseURL(), Model: "local"}),
		Models: map[string]conversation.Chatter{"smart": llm.New(llm.Options{BaseURL: smart.BaseURL(), Model: "cloud"})},
		Store:  st, Tools: registry,
		Guard:  tools.NewGuard(i18n.DE, cfg.Security, st, nil),
		Logger: quietLogger(), Location: time.UTC,
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

func TestChannelsUseTheirModels(t *testing.T) {
	e, main, smart := twoModels(t)
	main.Push(fakellm.Step{Content: "Hallo aus der Küche."})
	smart.Push(fakellm.Step{Content: "Hallo aus der Cloud."})

	if r := handle(t, e, config.ChannelVoice, "Hallo"); r.Text != "Hallo aus der Küche." || r.Model != "main" {
		t.Errorf("voice: %+v", r)
	}
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

func TestOnlyTheLocalModelIsWarmed(t *testing.T) {
	e, main, smart := twoModels(t)
	e.WarmupAll(context.Background(), "test")
	if len(smart.Requests()) != 0 {
		t.Errorf("the cloud model was warmed %d times", len(smart.Requests()))
	}
	// Slots are pinned: voice and background stay local, Telegram is cloud.
	if n := len(main.Requests()); n != 2 {
		t.Errorf("main warmed %d times, want 2", n)
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
	if last := first[len(first)-1].Content; !strings.Contains(last, "Plane ein Menü für vier Personen.") || !strings.Contains(last, "abgegebene Aufgabe") {
		t.Errorf("delegated turn: %q", last)
	}
	refused := reqs[1].Request.Messages[len(reqs[1].Request.Messages)-1]
	if refused.Role != llm.RoleTool || !strings.Contains(refused.Content, "cannot be called from a delegated task") {
		t.Errorf("nested delegate not refused: %+v", refused)
	}
	// The fast model reads the stronger model's answer as the tool result.
	back := main.Last().Request.Messages
	if got := back[len(back)-1]; got.Role != llm.RoleTool || got.Content != "Suppe, Risotto, Tiramisu." {
		t.Errorf("tool result: %+v", got)
	}
}

func TestNoDelegateToolWithoutDelegateModel(t *testing.T) {
	for _, it := range tools.Internal(i18n.DE, nil, nil) {
		if it.Name == tools.ToolDelegate {
			t.Fatal("delegate offered without a delegate model")
		}
	}
}
