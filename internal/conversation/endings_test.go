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

// EN-06 The stop waits for the running turns (docs/spec/endings.md):
// waiting for the turns ends only once the turn that runs ended.
func TestEN06_TheStopWaitsForTheRunningTurns(t *testing.T) {
	var mu sync.Mutex
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	st, err := store.Open(context.Background(), store.Options{Path: storetest.Path(t),
		Clock: func() time.Time { mu.Lock(); defer mu.Unlock(); return now }})
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
		Structure: houseSnapshot, Logger: quietLogger(), Location: time.UTC,
	})
	answered := make(chan error, 1)
	go func() {
		_, err := e.Handle(context.Background(), conversation.Request{Channel: config.ChannelVoice, ConversationID: "c", Text: "Hallo"})
		answered <- err
	}()
	// Given a turn runs
	<-taken
	gaveUp, stop := context.WithCancel(context.Background())
	stop()
	if err := e.WaitTurns(gaveUp); err == nil {
		t.Fatal("waiting for the turns ended while one ran")
	}
	close(hold)
	if err := <-answered; err != nil {
		t.Fatal(err)
	}
	if err := e.WaitTurns(context.Background()); err != nil {
		t.Errorf("waiting after the turn: %v", err)
	}
}
