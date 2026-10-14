package watchers

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sker65/lares/internal/config"
	"github.com/sker65/lares/internal/conversation"
	"github.com/sker65/lares/internal/llm"
	"github.com/sker65/lares/internal/store"
	"github.com/sker65/lares/internal/tools"
)

// TestRealModelAssessment runs the assessment against a real model. It is
// skipped unless LARES_TEST_LLM_URL (and optionally LARES_TEST_LLM_MODEL,
// LARES_TEST_LLM_KEY) is set, e.g.
//
//	LARES_TEST_LLM_URL=http://localhost:1234/v1 LARES_TEST_LLM_MODEL=qwen/qwen3-30b-a3b-2507 go test -run RealModel -v ./internal/watchers
func TestRealModelAssessment(t *testing.T) {
	url := os.Getenv("LARES_TEST_LLM_URL")
	if url == "" {
		t.Skip("LARES_TEST_LLM_URL not set")
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
		LLM:   llm.New(llm.Options{BaseURL: url, Model: os.Getenv("LARES_TEST_LLM_MODEL"), APIKey: os.Getenv("LARES_TEST_LLM_KEY"), Timeout: 2 * time.Minute}),
		Tools: tools.NewRegistry(nil, tools.Internal(st, nil)...),
		Guard: tools.NewGuard(cfg.Security, st, nil),
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
