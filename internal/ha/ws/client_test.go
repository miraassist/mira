package ws_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/clock"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/testsupport/fakeha"
	"github.com/miraassist/mira/internal/testsupport/logcap"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func startClient(t *testing.T, srv *fakeha.Server, token string) (*haws.Client, context.Context) {
	t.Helper()
	c, ctx, _ := startOnClock(t, srv, haws.Options{Token: token})
	return c, ctx
}

// startOnClock runs a client on a fake clock until it is connected; opts
// gives the token and what else the test needs.
func startOnClock(t *testing.T, srv *fakeha.Server, opts haws.Options) (*haws.Client, context.Context, *clock.Fake) {
	t.Helper()
	clk := clock.NewFake(time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC))
	opts.URL, opts.Clock, opts.CallTimeout = srv.URL, clk, 5*time.Second
	if opts.Logger == nil {
		opts.Logger = quietLogger()
	}
	c, err := haws.New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go c.Run(ctx)
	if err := c.WaitReady(ctx); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	return c, ctx, clk
}

// reconnect drops the connection and lets the client's wait before the
// next attempt run out.
func reconnect(srv *fakeha.Server, clk *clock.Fake) {
	srv.DropConnections()
	<-clk.AwaitWaiters(1) // the client noticed and waits before dialling again
	clk.AdvanceToNext()
}

// subscriptions is closed once the fake has n live subscriptions.
func subscriptions(srv *fakeha.Server, n int) <-chan struct{} {
	return srv.Await(func() bool { return srv.SubscriptionCount() == n })
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

// HU-05 An entry of a registry that Mira cannot read is left out, the rest is
// built (docs/spec/context.md, 1.3, decision 5).
func TestHU05_AnEntryOfARegistryThatMiraCannotReadIsLeftOutTheRestIsBuilt(t *testing.T) {
	srv := fakeha.New()
	defer srv.Close()
	// Given an entry of the device registry has a field of another type than
	// Mira reads (a name as a number)
	srv.SetRawList(haws.CmdDeviceRegistryList, `[
		{"id": "dev-good", "name": "Satellit", "identifiers": [["techo5", "show"]]},
		{"id": "dev-bad", "name": 12, "identifiers": [["some_integration", 4711]]},
		{"id": "dev-also-good", "name": "Lampe", "identifiers": [["zha", 4711]]}
	]`)
	// An identifier and a unique_id as numbers are kept (Home Assistant
	// stores them as an integration gave them).
	srv.SetRawList(haws.CmdEntityRegistryList, `[
		{"entity_id": "sensor.power", "unique_id": 4712, "platform": "some_integration"},
		{"entity_id": "sensor.odd", "unique_id": {"a": 1}},
		{"entity_id": "light.lamp", "unique_id": "lamp-1"}
	]`)
	log, lines := logcap.Logger()
	c, ctx, _ := startOnClock(t, srv, haws.Options{Token: "test-token", Logger: log})

	// When the list is read, it is built from the other entries
	devices, err := c.Devices(ctx)
	if err != nil {
		t.Fatalf("Devices: %v", err)
	}
	if len(devices) != 2 || devices[0].ID != "dev-good" || devices[1].ID != "dev-also-good" {
		t.Fatalf("Devices = %+v", devices)
	}
	// And the log names the list, the entry and why it was left out
	line := lines.Line("registry entry left out")
	for _, want := range []string{haws.CmdDeviceRegistryList, "id=dev-bad", "cannot unmarshal number", `\"name\":12`} {
		if !strings.Contains(line, want) {
			t.Errorf("the log line lacks %q: %s", want, line)
		}
	}

	if devices[1].Identifiers[0][1] != "4711" {
		t.Errorf("the identifier as a number = %q", devices[1].Identifiers[0][1])
	}
	entities, err := c.Entities(ctx)
	if err != nil || len(entities) != 2 || entities[0].UniqueID != "4712" || entities[1].UniqueID != "lamp-1" {
		t.Fatalf("Entities = %+v (%v)", entities, err)
	}
	if line := lines.Line("registry entry left out", "id=sensor.odd"); !strings.Contains(line, "where text is expected") {
		t.Errorf("the entity left out is not named with why: %s", lines.String())
	}

	// A list that is not a list fails, and the error names its answer.
	srv.SetRawList(haws.CmdAreaRegistryList, `{"areas": "none"}`)
	if _, err := c.Areas(ctx); err == nil || !strings.Contains(err.Error(), haws.CmdAreaRegistryList) ||
		!strings.Contains(err.Error(), `areas`) {
		t.Errorf("Areas error = %v", err)
	}
}

func TestAuthFailureStopsRunInsteadOfLoopingForever(t *testing.T) {
	srv := fakeha.New()
	defer srv.Close()

	// On a fake clock nobody moves, a retry would wait forever: Run
	// returns only by giving up (else go test's timeout names the test).
	c, err := haws.New(haws.Options{URL: srv.URL, Token: "wrong", Logger: quietLogger(),
		Clock: clock.NewFake(time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC))})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Run(context.Background()); err == nil {
		t.Fatal("Run returned nil for a bad token")
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
	if e := <-events; e.EventType != "state_changed" {
		t.Errorf("event type = %q", e.EventType)
	}

	// An event of a different type must not reach this subscription: the
	// next one of its type comes first, as one connection keeps the order.
	if n := srv.PushEvent("call_service", map[string]any{}); n != 0 {
		t.Errorf("call_service reached %d subscriptions", n)
	}
	srv.PushEvent("state_changed", map[string]any{"entity_id": "sensor.next"})
	if e := <-events; e.EventType != "state_changed" {
		t.Errorf("unexpected event %q", e.EventType)
	}

	cancel() // returns once Home Assistant confirmed the unsubscribe
	if n := srv.PushEvent("state_changed", map[string]any{}); n != 0 {
		t.Errorf("events still reach %d subscriptions after cancelling", n)
	}
}

// The open config flows come as a list, not a bus event
// (docs/spec/found-devices.md, state table row 1).
func TestSubscribeFlowsTakesAList(t *testing.T) {
	srv := fakeha.New()
	defer srv.Close()
	c, ctx := startClient(t, srv, "test-token")
	got := make(chan []haws.FlowEvent, 2)
	if _, err := c.SubscribeFlows(ctx, func(list []haws.FlowEvent) { got <- list }); err != nil {
		t.Fatalf("SubscribeFlows: %v", err)
	}
	srv.PushFlows([]map[string]any{{"type": "added", "flow_id": "f1", "flow": map[string]any{
		"flow_id": "f1", "handler": "hue", "step_id": "link",
		"context": map[string]any{"source": "zeroconf", "unique_id": "001788", "title_placeholders": map[string]any{"name": "Hue Bridge"}}}}})
	if list := <-got; len(list) != 1 || list[0].Type != "added" || list[0].Flow == nil || list[0].Flow.Name() != "Hue Bridge" {
		t.Fatalf("got %+v", list)
	}
}

func TestSubscriptionsSurviveReconnect(t *testing.T) {
	srv := fakeha.New()
	defer srv.Close()

	reconnected := make(chan struct{}, 4)
	c, ctx, clk := startOnClock(t, srv, haws.Options{Token: "test-token",
		OnConnect: func(context.Context) { reconnected <- struct{}{} }})
	<-reconnected // first connect

	events := make(chan haws.Event, 4)
	if _, err := c.Subscribe(ctx, "state_changed", func(e haws.Event) { events <- e }); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	reconnect(srv, clk)
	<-reconnected // after the subscriptions were restored
	if n := srv.SubscriptionCount(); n != 1 {
		t.Fatalf("%d subscriptions on the new connection, want 1", n)
	}

	srv.PushEvent("state_changed", map[string]any{"entity_id": "sensor.inverter_status"})
	<-events
	if srv.ConnectCount() < 2 {
		t.Errorf("connect count = %d, want at least 2", srv.ConnectCount())
	}
}

func TestCallFailsFastWhileDisconnected(t *testing.T) {
	srv := fakeha.New()
	clk := clock.NewFake(time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC))
	c, err := haws.New(haws.Options{URL: srv.URL, Token: "test-token", Logger: quietLogger(), Clock: clk})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv.Close() // never comes up

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)
	<-clk.AwaitWaiters(1) // the dial failed; the client waits before the next
	callErr := make(chan error, 1)
	go func() { _, err := c.Areas(ctx); callErr <- err }()
	cancel() // the caller gives up
	if err := <-callErr; !errors.Is(err, haws.ErrNotConnected) {
		t.Errorf("a call on a dead connection: %v", err)
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
	c, ctx, clk := startOnClock(t, srv, haws.Options{Token: "test-token",
		OnConnect: func(context.Context) { reconnected <- struct{}{} }})
	<-reconnected

	events := make(chan haws.Event, 4)
	unsubscribe, err := c.Subscribe(ctx, "state_changed", func(e haws.Event) { events <- e })
	if err != nil {
		t.Fatal(err)
	}
	reconnect(srv, clk)
	<-reconnected // after the subscriptions were restored
	if n := srv.SubscriptionCount(); n != 1 {
		t.Fatalf("%d subscriptions after the reconnect, want 1", n)
	}

	// The subscription now lives under a new id; cancelling must still work.
	unsubscribe() // returns once Home Assistant confirmed it
	if n := srv.SubscriptionCount(); n != 0 {
		t.Fatalf("server still has %d subscriptions after cancel", n)
	}

	// And it must not come back on the next reconnect.
	reconnect(srv, clk)
	<-reconnected // after the subscriptions were restored
	if n := srv.SubscriptionCount(); n != 0 {
		t.Fatalf("cancelled subscription was restored after reconnect (%d)", n)
	}
}

func TestCancelWhileDisconnectedDoesNotBlock(t *testing.T) {
	srv := fakeha.New()
	c, ctx, clk := startOnClock(t, srv, haws.Options{Token: "test-token"})
	unsubscribe, err := c.Subscribe(ctx, "state_changed", func(haws.Event) {})
	if err != nil {
		t.Fatal(err)
	}
	srv.Close() // no reconnect possible
	srv.DropConnections()
	<-clk.AwaitWaiters(1) // the client noticed and waits before dialling again
	if c.Connected() {
		t.Fatal("client did not notice the dropped connection")
	}
	unsubscribe() // does not block while disconnected (else go test's timeout names the test)
}

func TestSubscribeTriggerDeliversVariables(t *testing.T) {
	srv := fakeha.New()
	defer srv.Close()
	srv.SetStates([]haws.State{{EntityID: "sensor.inverter_status", State: "ok"}})
	c, ctx := startClient(t, srv, "test-token")
	got := make(chan haws.Event, 4)
	if _, err := c.SubscribeTrigger(ctx, map[string]any{"trigger": "state", "entity_id": "sensor.inverter_status", "to": []string{"fault"}},
		func(e haws.Event) { got <- e }); err != nil {
		t.Fatal(err)
	}
	srv.SetState("sensor.inverter_status", "ok", map[string]any{"power": 3}) // attribute change only
	srv.SetState("sensor.inverter_status", "fault", nil)
	srv.SetState("sensor.inverter_status", "ok", nil) // a change the trigger does not want
	srv.SetState("sensor.inverter_status", "fault", nil)
	{
		e := <-got
		var v haws.TriggerVariables
		if err := json.Unmarshal(e.Variables, &v); err != nil {
			t.Fatal(err)
		}
		if v.Trigger.EntityID != "sensor.inverter_status" || v.Trigger.ToState.State != "fault" || v.Trigger.FromState.State != "ok" {
			t.Fatalf("variables = %+v", v)
		}
		if v.Trigger.ToState.Attributes["power"] != 3.0 {
			t.Errorf("attributes not carried over: %+v", v.Trigger.ToState.Attributes)
		}
	}
	// The next event is the second fault: nothing came between.
	var v haws.TriggerVariables
	if err := json.Unmarshal((<-got).Variables, &v); err != nil || v.Trigger.FromState.State != "ok" || v.Trigger.ToState.State != "fault" {
		t.Errorf("second event: %+v %v", v, err)
	}
}

func TestStateChangeDuringReconnectFires(t *testing.T) {
	srv := fakeha.New()
	defer srv.Close()
	srv.SetStates([]haws.State{{EntityID: "binary_sensor.motion", State: "off"}})
	c, ctx, clk := startOnClock(t, srv, haws.Options{Token: "test-token"})
	got := make(chan haws.Event, 4)
	if _, err := c.SubscribeTrigger(ctx, map[string]any{"trigger": "state", "entity_id": "binary_sensor.motion", "not_to": "unavailable"},
		func(e haws.Event) { got <- e }); err != nil {
		t.Fatal(err)
	}
	// A first change proves the client knows the current state.
	<-subscriptions(srv, 1)
	srv.SetState("binary_sensor.motion", "on", nil)
	<-got
	srv.DropConnections()
	srv.SetState("binary_sensor.motion", "off", nil) // nobody connected
	<-clk.AwaitWaiters(1)
	clk.AdvanceToNext()
	<-got // the change during the outage is not lost
}

func TestSubscribeTriggerRejectsOtherTriggers(t *testing.T) {
	srv := fakeha.New()
	defer srv.Close()
	c, ctx := startClient(t, srv, "test-token")
	if _, err := c.SubscribeTrigger(ctx, map[string]any{"trigger": "time", "at": "07:00"}, func(haws.Event) {}); err == nil {
		t.Fatal("time trigger accepted")
	}
}

// Home Assistant refuses a command id lower than one it has seen. Calls
// running at the same time must still send in the order of their ids; live,
// a subscription at start-up failed with id_reuse.
func TestConcurrentCallsKeepIDsIncreasing(t *testing.T) {
	srv := fakeha.New()
	defer srv.Close()
	c, ctx := startClient(t, srv, "test-token")
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Areas(ctx); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

// WaitState watches one entity: it sees the state it has now, a later
// change, and gives up after its limit; it never asks for all states.
func TestWaitStateWatchesOneEntity(t *testing.T) {
	srv := fakeha.New()
	defer srv.Close()
	srv.SetStates([]haws.State{{EntityID: "assist_satellite.show", State: "responding"}, {EntityID: "light.other", State: "on"}})
	c, ctx, clk := startOnClock(t, srv, haws.Options{Token: "test-token"})
	if !c.WaitState(ctx, "assist_satellite.show", time.Second, func(s string) bool { return s == "responding" }) {
		t.Fatal("the state it has now was not seen")
	}
	done := make(chan bool, 1)
	go func() {
		done <- c.WaitState(ctx, "assist_satellite.show", 3*time.Second, func(s string) bool { return s == "idle" })
	}()
	<-subscriptions(srv, 1)
	srv.SetState("light.other", "off", nil)
	srv.SetState("assist_satellite.show", "idle", nil)
	if !<-done {
		t.Fatal("the change was not seen")
	}
	var reached bool
	clk.Drive(func() { // its limit runs out
		reached = c.WaitState(ctx, "assist_satellite.show", 50*time.Millisecond, func(s string) bool { return s == "listening" })
	})
	if reached {
		t.Error("a state it never had")
	}
	if !c.WaitState(ctx, "assist_satellite.gone", time.Second, func(s string) bool { return s == "" }) {
		t.Error("an unknown entity is not \"\"")
	}
}

// GetStates reads only what Home Assistant filtered for it: the entities
// and domains asked for, nothing else; an empty filter asks nothing.
func TestGetStatesReadsOnlyTheFilter(t *testing.T) {
	srv := fakeha.New()
	defer srv.Close()
	srv.SetStates([]haws.State{{EntityID: "person.anna", State: "home"}, {EntityID: "person.ben", State: "not_home"},
		{EntityID: "light.kitchen", State: "on"}, {EntityID: "sensor.calls", State: "2"}})
	c, ctx := startClient(t, srv, "test-token")
	got, err := c.GetStates(ctx, haws.StateFilter{EntityIDs: []string{"sensor.calls"}, Domains: []string{"person"}})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, s := range got {
		ids[s.EntityID] = s.State
	}
	if len(ids) != 3 || ids["person.anna"] != "home" || ids["sensor.calls"] != "2" {
		t.Errorf("got %v", ids)
	}
	if got, _ := c.GetStates(ctx, haws.StateFilter{}); got != nil {
		t.Errorf("empty filter read %v", got)
	}
	<-subscriptions(srv, 0)
}

// WatchStates keeps a domain's states current: changes and new entities
// come in, other domains do not.
func TestWatchStatesKeepsADomain(t *testing.T) {
	srv := fakeha.New()
	defer srv.Close()
	srv.SetStates([]haws.State{{EntityID: "person.anna", State: "home"}, {EntityID: "light.kitchen", State: "on"}})
	c, ctx := startClient(t, srv, "test-token")
	changed := make(chan struct{}, 8)
	w, err := c.WatchStates(ctx, haws.StateFilter{Domains: []string{"person"}}, func() { changed <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	<-changed
	if !w.Ready() || len(w.States()) != 1 {
		t.Fatalf("ready %v, states %v", w.Ready(), w.States())
	}
	srv.SetState("person.anna", "not_home", nil)
	srv.SetState("person.ben", "home", map[string]any{"friendly_name": "Ben"})
	srv.SetState("light.kitchen", "off", nil)
	for {
		a, _ := w.State("person.anna")
		b, _ := w.State("person.ben")
		if a.State == "not_home" && b.State == "home" {
			break
		}
		<-changed
	}
	if _, ok := w.State("light.kitchen"); ok {
		t.Error("another domain came in")
	}
}
