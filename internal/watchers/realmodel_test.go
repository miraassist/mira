package watchers

import (
	"context"
	"github.com/sker65/mira/internal/i18n"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sker65/mira/internal/config"
	"github.com/sker65/mira/internal/conversation"
	"github.com/sker65/mira/internal/llm"
	"github.com/sker65/mira/internal/store"
	"github.com/sker65/mira/internal/tools"
)

// TestRealModelAssessment runs the assessment against a real model. It is
// skipped unless MIRA_TEST_LLM_URL (and optionally MIRA_TEST_LLM_MODEL,
// MIRA_TEST_LLM_KEY) is set, e.g.
//
//	MIRA_TEST_LLM_URL=http://localhost:1234/v1 MIRA_TEST_LLM_MODEL=qwen/qwen3-30b-a3b-2507 go test -run RealModel -v ./internal/watchers
func TestRealModelAssessment(t *testing.T) {
	url := os.Getenv("MIRA_TEST_LLM_URL")
	if url == "" {
		t.Skip("MIRA_TEST_LLM_URL not set")
	}
	cfg := config.Default()
	cfg.LLM.SlotPinning, cfg.LLM.CachePrompt = false, false
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{Path: filepath.Join(t.TempDir(), "db")})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := conversation.New(conversation.Options{
		Config: cfg, Store: st, Logger: quiet,
		LLM:   llm.New(llm.Options{BaseURL: url, Model: os.Getenv("MIRA_TEST_LLM_MODEL"), APIKey: os.Getenv("MIRA_TEST_LLM_KEY"), Timeout: 2 * time.Minute}),
		Tools: tools.NewRegistry(nil, tools.Internal(i18n.DE, st, nil)...),
		Guard: tools.NewGuard(i18n.DE, cfg.Security, st, nil),
	})
	m := New(Options{Store: st, Assessor: engine, MinImportance: 0, Logger: quiet})

	cases := []struct {
		watcher config.Watcher
		event   Event
	}{
		{inverter, Event{EntityID: "sensor.inverter_status", FriendlyName: "Wechselrichter Status", From: "ok", To: "fault",
			Attributes: map[string]any{"error_code": 42, "message": "Isolation fault"}}},
		{config.Watcher{Name: "washer", Description: "Die Waschmaschine ist fertig."},
			Event{EntityID: "sensor.washer_state", FriendlyName: "Waschmaschine", From: "running", To: "done"}},
	}
	for _, c := range cases {
		raw, err := engine.Assess(ctx, m.instruction(c.watcher, c.event), assessmentSchema)
		if err != nil {
			t.Fatalf("%s: %v", c.watcher.Name, err)
		}
		a, err := m.assess(ctx, c.watcher, c.event)
		if err != nil {
			t.Fatalf("%s: model answer not usable: %v\n%s", c.watcher.Name, err, raw)
		}
		t.Logf("%s: importance %d\n  spoken:  %s\n  details: %s\n  expires: %dh", c.watcher.Name, a.Importance, a.Spoken, a.Details, a.ExpiresInHours)
	}
}

// TestRealModelFeed reads a real feed and lets a real model pick what is
// interesting. Besides MIRA_TEST_LLM_URL it needs MIRA_TEST_FEED_URL, e.g.
// https://zenmux.ai/rss/models.xml.
func TestRealModelFeed(t *testing.T) {
	url, feedURL := os.Getenv("MIRA_TEST_LLM_URL"), os.Getenv("MIRA_TEST_FEED_URL")
	if url == "" || feedURL == "" {
		t.Skip("MIRA_TEST_LLM_URL or MIRA_TEST_FEED_URL not set")
	}
	cfg := config.Default()
	cfg.LLM.SlotPinning, cfg.LLM.CachePrompt = false, false
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{Path: filepath.Join(t.TempDir(), "db")})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := conversation.New(conversation.Options{
		Config: cfg, Store: st, Logger: quiet,
		LLM:   llm.New(llm.Options{BaseURL: url, Model: os.Getenv("MIRA_TEST_LLM_MODEL"), APIKey: os.Getenv("MIRA_TEST_LLM_KEY"), Timeout: 3 * time.Minute}),
		Tools: tools.NewRegistry(nil, tools.Internal(i18n.DE, st, nil)...),
		Guard: tools.NewGuard(i18n.DE, cfg.Security, st, nil),
	})
	w := config.Watcher{Name: "modelle", Schedule: "0 8 * * *", Kind: "rss", URL: feedURL,
		Description: "Neue KI-Modelle. Interessant sind vor allem Sprachmodelle, die man lokal betreiben kann (offene Gewichte, etwa Qwen, Llama, Gemma, Mistral), und große Sprünge bei den bekannten Anbietern. Bild- und Videomodelle sind weniger wichtig."}
	m := New(Options{Store: st, Assessor: engine, MinImportance: 1, Logger: quiet, Watchers: []config.Watcher{w}})
	// The first run only looks at the last day; mark the feed as known with
	// nothing seen, so the whole current feed is judged.
	if err := st.SetKV(ctx, "feed_seen:modelle", "[]"); err != nil {
		t.Fatal(err)
	}
	queued, err := m.RunFeed(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	if !queued {
		t.Log("nothing judged relevant")
		return
	}
	n, err := st.OpenNoticeByDedupeKey(ctx, "watcher:modelle:"+time.Now().Format("2006-01-02"))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("importance %d\n  spoken:  %s\n  details:\n%s", n.Importance, n.Spoken, n.Details)
}
