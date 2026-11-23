package conversation_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/conversation"
	"github.com/miraassist/mira/internal/hooks"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/store/storetest"
	"github.com/miraassist/mira/internal/testsupport/fakellm"
	"github.com/miraassist/mira/internal/tools"
)

// TV-14 The purge leaves a history a turn runs in
// (docs/spec/thread-history.md, decision 10): the history's time runs out
// while the turn waits for its model; the purge leaves it, the turn stores
// its answer, and the history goes once the time the turn gave it ran out.
func TestTV14_ThePurgeLeavesAHistoryATurnRunsIn(t *testing.T) {
	var mu sync.Mutex
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	clk := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	st, err := store.Open(context.Background(), store.Options{Path: storetest.Path(t), Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	taken, hold := make(chan struct{}), make(chan struct{})
	model := fakellm.New(fakellm.Step{Content: "Gut.", Taken: taken, Hold: hold})
	defer model.Close()
	cfg := config.Default()
	e := conversation.New(conversation.Options{StructurePrompt: func() string { return harnessHousePrompt },
		Config: cfg, LLM: llm.New(llm.Options{BaseURL: model.BaseURL(), Model: "local"}),
		Store: st, Tools: tools.NewRegistry(nil, tools.Internal(i18n.DE, st)...),
		Guard:     tools.NewGuard(i18n.DE, cfg.Security, st, nil, hooks.Load("", i18n.DE, nil)),
		Structure: houseSnapshot,
		Logger:    quietLogger(), Location: time.UTC,
	})
	answered := make(chan error, 1)
	go func() {
		_, err := e.Handle(context.Background(), conversation.Request{Channel: config.ChannelVoice, ConversationID: "c", Text: "Hallo"})
		answered <- err
	}()
	// Given a turn runs in a history whose time has run out
	<-taken
	inUse := e.SessionsInUse()
	if len(inUse) != 1 {
		t.Fatalf("sessions in use: %v", inUse)
	}
	key := inUse[0]
	mu.Lock()
	now = now.Add(365 * 24 * time.Hour)
	mu.Unlock()
	// When the expired histories are purged
	if n, err := st.PurgeExpiredSessions(context.Background(), e.SessionsInUse()...); err != nil || n != 0 {
		t.Fatalf("purged %d: %v", n, err)
	}
	// Then that history stays, and the turn stores its answer
	close(hold)
	if err := <-answered; err != nil {
		t.Fatalf("the turn: %v", err)
	}
	if msgs, err := st.ChatMessages(context.Background(), key); err != nil || len(msgs) == 0 {
		t.Fatalf("history %s: %d messages, %v", key, len(msgs), err)
	}
	// And the history goes once the time the turn gave it ran out too
	mu.Lock()
	now = now.Add(365 * 24 * time.Hour)
	mu.Unlock()
	if n, err := st.PurgeExpiredSessions(context.Background(), e.SessionsInUse()...); err != nil || n != 1 {
		t.Errorf("purged %d after the turn: %v", n, err)
	}
}
