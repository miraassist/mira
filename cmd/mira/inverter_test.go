package main

import (
	"context"
	"encoding/json"
	"github.com/miraassist/mira/internal/llm"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/clock"
	"github.com/miraassist/mira/internal/config"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/testsupport/fakeha"
	"github.com/miraassist/mira/internal/testsupport/fakellm"
	"github.com/miraassist/mira/internal/testsupport/fakemcp"
)

// TestInverterCaseEndToEnd is the M3 acceptance scenario through the real
// wiring: the inverter faults and a task (a Home Assistant automation)
// calls mira.assess, the model rates it, motion in the bathroom gets it
// announced on the satellite there, and "Was genau?" from that satellite is
// answered with the details and acknowledges the message.
func TestInverterCaseEndToEnd(t *testing.T) {
	verdict := fakellm.Step{Content: `{"importance":2,"spoken":"Der Wechselrichter meldet einen Fehler.","details":"Status fault, Fehlercode 42."}`}
	// The announcement is worded by the model (docs/spec/messages.md,
	// decision 29): it names the message's id from the request.
	announce := fakellm.Step{Answer: func(req llm.ChatRequest) string {
		ids := regexp.MustCompile(`Nachricht \[([^\]]+)\]`).FindAllStringSubmatch(req.Messages[len(req.Messages)-1].Content, -1)
		var list []string
		for _, m := range ids {
			list = append(list, `"`+m[1]+`"`)
		}
		return `{"text":"Der Wechselrichter meldet einen Fehler.","ids":[` + strings.Join(list, ",") + `]}`
	}}
	model := fakellm.New(verdict, verdict, verdict, announce, fakellm.Step{Content: "Er meldet Fehlercode zweiundvierzig."})
	t.Cleanup(model.Close)

	ha := fakeha.New()
	t.Cleanup(ha.Close)
	// Home Assistant's MCP server: Mira does not answer without its prompt.
	haMCP := fakemcp.HA("")
	haMCP.Token = ha.Token
	t.Cleanup(haMCP.Close)
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
		{EntityID: "person.ben", State: "home"},
		{EntityID: "sensor.inverter_status", State: "ok"},
		{EntityID: "binary_sensor.bad_motion", State: "off"},
	})

	cfg := config.Default()
	cfg.Server = config.Server{Listen: freeAddr(t), Token: "api-token"}
	setMain(&cfg, func(m *config.LLM) { m.BaseURL = model.BaseURL() }) // no warmups: keep the model script in order
	cfg.Store.Path = filepath.Join(t.TempDir(), "mira.db")
	seedExposed(t, cfg.Store.Path, "assist_satellite.bad", "sensor.inverter_status")
	cfg.HA = config.HA{URL: ha.URL, Token: ha.Token, StructureDebounce: config.Duration(time.Second)}
	cfg.MCPServers = []config.MCPServer{{Name: "ha", Transport: config.TransportHTTP, URL: haMCP.Endpoint(), Token: ha.Token}}
	cfg.Telemetry.LogLevel = "error"
	cfg.Satellites = map[string]config.Entities{"bad": {"assist_satellite.bad"}}
	cfg.Persons = map[string]config.Person{"ben": {Entity: "person.ben"}}
	cfg.Notifier.MorningTrigger = config.MorningTrigger{
		Areas: []string{"bad"}, Presence: map[string]config.Entities{"bad": {"binary_sensor.bad_motion"}},
	}
	cfg.Notifier.QuietHours = config.QuietHours{} // the test runs at any hour

	logs := startAgent(t, cfg)
	ctx := context.Background()
	// The notifier watches the motion sensor.
	<-ha.Await(func() bool { return ha.SubscriptionCount() >= 1 })

	push := func(entity, from, to string) {
		ha.SetState(entity, from, nil)
		ha.SetState(entity, to, nil)
	}

	// 1. The fault, flapping a few times: each time the automation calls
	// mira.assess with the same dedupe key.
	for i := 0; i < 3; i++ {
		req, _ := http.NewRequest("POST", "http://"+cfg.Server.Listen+"/v1/assess", strings.NewReader(
			`{"goal":"Sag Bescheid, wenn der Wechselrichter einen Fehler meldet.","observation":"sensor.inverter_status ist fault.","dedupe_key":"inverter:fault","task":"mira_inverter"}`))
		req.Header.Set("Authorization", "Bearer api-token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("assess: %d", resp.StatusCode)
		}
	}
	st, err := store.Open(ctx, store.Options{Path: cfg.Store.Path, Clock: clock.Real().Now})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	// assess answers once the message is kept.
	message, err := st.OpenMessageByDedupeKey(ctx, "inverter:fault")
	if err != nil {
		t.Fatalf("the message: %v", err)
	}

	// 2. Motion in the bathroom: exactly one announcement there.
	push("binary_sensor.bad_motion", "off", "on")
	<-ha.Await(func() bool { return len(ha.ServiceCalls()) > 0 })
	push("binary_sensor.bad_motion", "off", "on")
	<-logs.AwaitN(2, `msg="presence `) // the second motion was handled too
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
	if len(reqs) != 5 {
		t.Fatalf("model saw %d requests: three assessments, the announcement, one turn", len(reqs))
	}
	turn := reqs[4].Request.Messages
	if layer7 := turn[len(turn)-1].Content; !strings.Contains(layer7, "Fehlercode 42") {
		t.Fatalf("follow-up lacks the details:\n%s", layer7)
	}
	if got, _ := st.GetMessage(ctx, message.ID); got.State != store.MessageAcked {
		t.Errorf("message state = %s, want acked", got.State)
	}
}
