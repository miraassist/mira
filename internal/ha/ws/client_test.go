package ws_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	haws "github.com/sker65/lares/internal/ha/ws"
	"github.com/sker65/lares/internal/testsupport/fakeha"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func startClient(t *testing.T, srv *fakeha.Server, token string) (*haws.Client, context.Context) {
	t.Helper()
	c, err := haws.New(haws.Options{
		URL: srv.URL, Token: token, Logger: quietLogger(),
		CallTimeout: 5 * time.Second, ReconnectMin: 20 * time.Millisecond, ReconnectMax: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go c.Run(ctx)

	ready, done := context.WithTimeout(ctx, 5*time.Second)
	defer done()
	if err := c.WaitReady(ready); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	return c, ctx
}

func TestRegistryReads(t *testing.T) {
	srv := fakeha.New()
	defer srv.Close()
	srv.SetRegistry(
		[]haws.Area{{AreaID: "kueche", Name: "Küche"}},
		[]haws.Device{{ID: "dev1", AreaID: "kueche", Name: "Satellit"}},
		[]haws.Entity{{EntityID: "light.kueche", AreaID: "kueche", Name: "Deckenlicht"}},
	)
	c, ctx := startClient(t, srv, "test-token")

	areas, err := c.Areas(ctx)
	if err != nil || len(areas) != 1 || areas[0].Name != "Küche" {
		t.Fatalf("Areas = %+v (%v)", areas, err)
	}
	devices, err := c.Devices(ctx)
	if err != nil || len(devices) != 1 {
		t.Fatalf("Devices = %+v (%v)", devices, err)
	}
	entities, err := c.Entities(ctx)
	if err != nil || len(entities) != 1 {
		t.Fatalf("Entities = %+v (%v)", entities, err)
	}
}

func TestAuthFailureStopsRunInsteadOfLoopingForever(t *testing.T) {
	srv := fakeha.New()
	defer srv.Close()

	c, err := haws.New(haws.Options{URL: srv.URL, Token: "wrong", Logger: quietLogger(),
		ReconnectMin: 10 * time.Millisecond})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run returned nil for a bad token")
		}
	case <-ctx.Done():
		t.Fatal("Run kept retrying a bad token instead of giving up")
	}
}

func TestCallServiceIsRecorded(t *testing.T) {
	srv := fakeha.New()
	defer srv.Close()
	c, ctx := startClient(t, srv, "test-token")

	err := c.CallService(ctx, "assist_satellite", "start_conversation",
		map[string]any{"start_message": "Der Wechselrichter meldet einen Fehler."},
		&haws.Target{EntityID: []string{"assist_satellite.bad"}})
	if err != nil {
		t.Fatalf("CallService: %v", err)
	}

	calls := srv.ServiceCalls()
	if len(calls) != 1 {
		t.Fatalf("got %d service calls, want 1", len(calls))
	}
	if calls[0].Domain != "assist_satellite" || calls[0].Service != "start_conversation" {
		t.Errorf("wrong service: %+v", calls[0])
	}
	if got := calls[0].Data["start_message"]; got != "Der Wechselrichter meldet einen Fehler." {
		t.Errorf("start_message = %v", got)
	}
	target, _ := calls[0].Target["entity_id"].([]any)
	if len(target) != 1 || target[0] != "assist_satellite.bad" {
		t.Errorf("target = %v", calls[0].Target)
	}
}

func TestSubscribeReceivesEvents(t *testing.T) {
	srv := fakeha.New()
	defer srv.Close()
	c, ctx := startClient(t, srv, "test-token")

	events := make(chan haws.Event, 4)
	cancel, err := c.Subscribe(ctx, "state_changed", func(e haws.Event) { events <- e })
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if n := srv.PushEvent("state_changed", map[string]any{"entity_id": "sensor.inverter_status"}); n != 1 {
		t.Fatalf("event reached %d subscriptions, want 1", n)
	}
	select {
	case e := <-events:
		if e.EventType != "state_changed" {
			t.Errorf("event type = %q", e.EventType)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no event arrived")
	}

	// An event of a different type must not reach this subscription.
	srv.PushEvent("call_service", map[string]any{})
	select {
	case e := <-events:
		t.Errorf("unexpected event %q", e.EventType)
	case <-time.After(200 * time.Millisecond):
	}

	cancel()
	// Give the unsubscribe a moment, then check nothing arrives any more.
	time.Sleep(200 * time.Millisecond)
	srv.PushEvent("state_changed", map[string]any{})
	select {
	case <-events:
		t.Error("events still arrive after cancelling the subscription")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestSubscriptionsSurviveReconnect(t *testing.T) {
	srv := fakeha.New()
	defer srv.Close()

	reconnected := make(chan struct{}, 4)
	c, err := haws.New(haws.Options{
		URL: srv.URL, Token: "test-token", Logger: quietLogger(),
		CallTimeout: 5 * time.Second, ReconnectMin: 20 * time.Millisecond, ReconnectMax: 50 * time.Millisecond,
		OnConnect: func(context.Context) { reconnected <- struct{}{} },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)
	if err := c.WaitReady(ctx); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	<-reconnected // first connect

	events := make(chan haws.Event, 4)
	if _, err := c.Subscribe(ctx, "state_changed", func(e haws.Event) { events <- e }); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	srv.DropConnections()

	select {
	case <-reconnected:
	case <-time.After(5 * time.Second):
		t.Fatal("client did not reconnect")
	}
	if err := c.WaitReady(ctx); err != nil {
		t.Fatalf("WaitReady after reconnect: %v", err)
	}

	// The subscription must have been re-issued on the new connection.
	deadline := time.Now().Add(3 * time.Second)
	for srv.SubscriptionCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if srv.SubscriptionCount() == 0 {
		t.Fatal("no subscription on the new connection")
	}

	srv.PushEvent("state_changed", map[string]any{"entity_id": "sensor.inverter_status"})
	select {
	case <-events:
	case <-time.After(3 * time.Second):
		t.Fatal("no event after reconnect; the subscription was lost")
	}
	if srv.ConnectCount() < 2 {
		t.Errorf("connect count = %d, want at least 2", srv.ConnectCount())
	}
}

func TestExposedEntities(t *testing.T) {
	srv := fakeha.New()
	defer srv.Close()
	srv.SetExposed("light.kueche", "sensor.inverter_status")
	c, ctx := startClient(t, srv, "test-token")

	exposed, err := c.ExposedEntities(ctx)
	if err != nil {
		t.Fatalf("ExposedEntities: %v", err)
	}
	if !exposed["light.kueche"] || !exposed["sensor.inverter_status"] {
		t.Errorf("exposed = %v", exposed)
	}
	if exposed["light.bad"] {
		t.Error("an entity that was not exposed came back as exposed")
	}

	// A Home Assistant version without the command must surface an error, so
	// the caller can fall back rather than believe the house is empty.
	srv.FailExposedEntities(true)
	if _, err := c.ExposedEntities(ctx); err == nil {
		t.Error("an unsupported command should be an error, not an empty map")
	}
}

func TestCallFailsFastWhileDisconnected(t *testing.T) {
	srv := fakeha.New()
	c, err := haws.New(haws.Options{URL: srv.URL, Token: "test-token", Logger: quietLogger(),
		CallTimeout: time.Second, ReconnectMin: time.Hour})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv.Close() // never comes up

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	go c.Run(ctx)
	if _, err := c.Areas(ctx); err == nil {
		t.Error("a call on a dead connection should fail")
	}
}

func TestWebsocketURLForms(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"http://ha.local:8123", "ws://ha.local:8123/api/websocket"},
		{"https://ha.example.com/", "wss://ha.example.com/api/websocket"},
		{"ws://ha.local:8123/api/websocket", "ws://ha.local:8123/api/websocket"},
	} {
		c, err := haws.New(haws.Options{URL: tc.in, Token: "x"})
		if err != nil {
			t.Fatalf("New(%q): %v", tc.in, err)
		}
		if got := c.Endpoint(); got != tc.want {
			t.Errorf("New(%q) endpoint = %q, want %q", tc.in, got, tc.want)
		}
	}
	if _, err := haws.New(haws.Options{URL: "ha.local:8123", Token: "x"}); err == nil {
		t.Error("a url without a scheme should be rejected")
	}
}

func TestCancelAfterReconnectStopsTheSubscription(t *testing.T) {
	srv := fakeha.New()
	defer srv.Close()
	reconnected := make(chan struct{}, 4)
	c, err := haws.New(haws.Options{
		URL: srv.URL, Token: "test-token", Logger: quietLogger(),
		CallTimeout: 5 * time.Second, ReconnectMin: 20 * time.Millisecond, ReconnectMax: 50 * time.Millisecond,
		OnConnect: func(context.Context) { reconnected <- struct{}{} },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)
	<-reconnected

	events := make(chan haws.Event, 4)
	unsubscribe, err := c.Subscribe(ctx, "state_changed", func(e haws.Event) { events <- e })
	if err != nil {
		t.Fatal(err)
	}
	srv.DropConnections()
	<-reconnected
	deadline := time.Now().Add(3 * time.Second)
	for srv.SubscriptionCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	// The subscription now lives under a new id; cancelling must still work.
	unsubscribe()
	deadline = time.Now().Add(3 * time.Second)
	for srv.SubscriptionCount() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := srv.SubscriptionCount(); n != 0 {
		t.Fatalf("server still has %d subscriptions after cancel", n)
	}

	// And it must not come back on the next reconnect.
	srv.DropConnections()
	<-reconnected
	time.Sleep(100 * time.Millisecond)
	if n := srv.SubscriptionCount(); n != 0 {
		t.Fatalf("cancelled subscription was restored after reconnect (%d)", n)
	}
}

func TestCancelWhileDisconnectedDoesNotBlock(t *testing.T) {
	srv := fakeha.New()
	c, ctx := startClient(t, srv, "test-token")
	unsubscribe, err := c.Subscribe(ctx, "state_changed", func(haws.Event) {})
	if err != nil {
		t.Fatal(err)
	}
	srv.Close() // no reconnect possible
	srv.DropConnections()
	deadline := time.Now().Add(3 * time.Second)
	for c.Connected() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if c.Connected() {
		t.Fatal("client did not notice the dropped connection")
	}

	done := make(chan struct{})
	go func() { unsubscribe(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("unsubscribe blocked while disconnected")
	}
}
