package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sker65/lares/internal/config"
	haws "github.com/sker65/lares/internal/ha/ws"
	"github.com/sker65/lares/internal/store"
	"github.com/sker65/lares/internal/testsupport/fakeha"
	"github.com/sker65/lares/internal/testsupport/fakellm"
)

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestInverterCaseEndToEnd is the M3 acceptance scenario through the real
// wiring: the inverter faults, the model rates it, motion in the bathroom
// gets it announced on the satellite there, and "Was genau?" from that
// satellite is answered with the details and acknowledges the notice.
func TestInverterCaseEndToEnd(t *testing.T) {
	model := fakellm.New(
		fakellm.Step{Content: `{"importance":2,"spoken":"Der Wechselrichter meldet einen Fehler.","details":"Status fault, Fehlercode 42.","expires_in_hours":24}`},
		fakellm.Step{Content: "Er meldet Fehlercode zweiundvierzig."},
	)
	defer model.Close()

	ha := fakeha.New()
	defer ha.Close()
	ha.SetRegistry(
		[]haws.Area{{AreaID: "bad", Name: "Bad"}},
		[]haws.Device{{ID: "dev-sat-bad", AreaID: "bad", Name: "Echo Bad"}},
		[]haws.Entity{
			{EntityID: "assist_satellite.bad", DeviceID: "dev-sat-bad", AreaID: "bad"},
			{EntityID: "sensor.inverter_status", Name: "Wechselrichter"},
		},
	)
	ha.SetStates([]haws.State{
		{EntityID: "assist_satellite.bad", State: "idle", Attributes: map[string]any{"supported_features": 3}},
		{EntityID: "person.stefan", State: "home"},
	})

	cfg := config.Default()
	cfg.Server = config.Server{Listen: freeAddr(t), Token: "api-token"}
	cfg.LLM.BaseURL = model.BaseURL()
	cfg.LLM.WarmupOnStart, cfg.LLM.WarmupOnPrefixChange = false, false // keep the model script in order
	cfg.Store.Path = filepath.Join(t.TempDir(), "lares.db")
	cfg.HA = config.HA{Enabled: true, URL: ha.URL, Token: ha.Token, StructureDebounce: config.Duration(time.Second)}
	cfg.Telegram.Enabled = false
	cfg.Telemetry.LogLevel = "error"
	cfg.Satellites = map[string]string{"bad": "assist_satellite.bad"}
	cfg.Persons = map[string]config.Person{"stefan": {Entity: "person.stefan"}}
	cfg.Watchers = []config.Watcher{{
		Name: "inverter_fault", DedupeKey: "inverter:fault",
		Trigger:     &config.StateTrigger{EntityID: []string{"sensor.inverter_status"}, To: []string{"fault"}},
		Description: "Der Wechselrichter meldet einen Fehler.",
	}}
	cfg.Notifier.MorningTrigger = config.MorningTrigger{
		Areas: []string{"bad"}, Presence: map[string]string{"bad": "binary_sensor.bad_motion"},
	}
	cfg.Notifier.QuietHours = config.QuietHours{} // the test runs at any hour

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runAgent(ctx, cfg) }()
	defer func() {
		http.DefaultClient.CloseIdleConnections()
		cancel()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Error("agent did not shut down")
		}
	}()
	// Watcher and notifier both subscribe a trigger.
	waitUntil(t, "trigger subscriptions", func() bool { return ha.SubscriptionCount() >= 2 })

	push := func(entity, from, to string) {
		ha.PushTrigger(map[string]any{"trigger": map[string]any{
			"platform": "state", "entity_id": entity,
			"from_state": map[string]any{"entity_id": entity, "state": from},
			"to_state":   map[string]any{"entity_id": entity, "state": to},
		}})
	}

	// 1. The fault, flapping a few times.
	for i := 0; i < 3; i++ {
		push("sensor.inverter_status", "ok", "fault")
	}
	st, err := store.Open(ctx, store.Options{Path: cfg.Store.Path})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var notice store.Notice
	waitUntil(t, "the notice", func() bool {
		notice, err = st.OpenNoticeByDedupeKey(ctx, "inverter:fault")
		return err == nil
	})

	// 2. Motion in the bathroom: exactly one announcement there.
	push("binary_sensor.bad_motion", "off", "on")
	waitUntil(t, "the announcement", func() bool { return len(ha.ServiceCalls()) > 0 })
	push("binary_sensor.bad_motion", "off", "on")
	time.Sleep(200 * time.Millisecond)
	calls := ha.ServiceCalls()
	if len(calls) != 1 || calls[0].Domain != "assist_satellite" || calls[0].Service != "start_conversation" {
		t.Fatalf("service calls = %+v", calls)
	}
	if msg, _ := calls[0].Data["start_message"].(string); !strings.Contains(msg, "Der Wechselrichter meldet einen Fehler.") {
		t.Fatalf("announced %q", msg)
	}

	// 3. "Was genau?" from that satellite.
	req, _ := http.NewRequest("POST", "http://"+cfg.Server.Listen+"/v1/conversation",
		strings.NewReader(`{"text":"Was genau?","device_id":"dev-sat-bad","language":"de"}`))
	req.Header.Set("Authorization", "Bearer api-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var out map[string]any
	json.Unmarshal(raw, &out)
	if out["text"] != "Er meldet Fehlercode zweiundvierzig." {
		t.Fatalf("answer = %s", raw)
	}
	reqs := model.Requests()
	if len(reqs) != 2 {
		t.Fatalf("model saw %d requests: one assessment, one turn (flapping must not add assessments)", len(reqs))
	}
	turn := reqs[1].Request.Messages
	if layer7 := turn[len(turn)-1].Content; !strings.Contains(layer7, "Fehlercode 42") {
		t.Fatalf("follow-up lacks the details:\n%s", layer7)
	}
	if got, _ := st.GetNotice(ctx, notice.ID); got.State != store.NoticeAcked {
		t.Errorf("notice state = %s, want acked", got.State)
	}
}
