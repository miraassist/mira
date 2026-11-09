package zigbee

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

// Adding a Zigbee device by voice: docs/spec/device-onboarding.md, step 3.
// fakeHA is Home Assistant with a Zigbee2MQTT bridge that answers like the
// real one (and the fake in testenv/z2m_fake).

const (
	bridgeID = "bridge-device"
	ieee     = "0x00158d0001a2b3c4"
	deviceID = "new-device"
	thread   = "th1"
)

type published struct {
	Topic   string
	Payload map[string]any
}

type fakeHA struct {
	mu        sync.Mutex
	mqtt      map[string][]func(haws.MQTTMessage)
	events    map[string][]haws.EventHandler
	published []published
	updates   []map[string]any
	exposed   []string
	entities  map[string]entry // entity registry
	related   []string         // the new device's entities
	refuse    map[string]error // by command type
	renameErr string
}

func newFakeHA() *fakeHA {
	return &fakeHA{mqtt: map[string][]func(haws.MQTTMessage){}, events: map[string][]haws.EventHandler{},
		entities: map[string]entry{
			"sensor.x_temperature": {DeviceID: deviceID, UniqueID: ieee + "_temperature_zigbee2mqtt"},
			"sensor.x_humidity":    {DeviceID: deviceID, UniqueID: ieee + "_humidity_zigbee2mqtt"},
			"sensor.x_battery":     {DeviceID: deviceID, UniqueID: ieee + "_battery_zigbee2mqtt", Category: "diagnostic"},
		},
		related: []string{"sensor.x_temperature", "sensor.x_humidity", "sensor.x_battery"},
		refuse:  map[string]error{}}
}

func (f *fakeHA) Call(_ context.Context, cmd map[string]any) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	typ := cmd["type"].(string)
	if err := f.refuse[typ]; err != nil {
		return nil, err
	}
	switch typ {
	case "mqtt/device/debug_info":
		if cmd["device_id"] == bridgeID {
			return json.RawMessage(`{"entities":[{"entity_id":"binary_sensor.z","discovery_data":{"payload":{"state_topic":"zigbee2mqtt/bridge/state"}}}]}`), nil
		}
		return json.RawMessage(`{"entities":[]}`), nil
	case "config/entity_registry/get_entries":
		out := map[string]any{}
		for _, id := range cmd["entity_ids"].([]string) {
			if e, ok := f.entities[id]; ok {
				out[id] = e
			} else {
				out[id] = nil
			}
		}
		raw, _ := json.Marshal(out)
		return raw, nil
	case "config/device_registry/update":
		f.updates = append(f.updates, cmd)
		return json.RawMessage(`{}`), nil
	case "search/related":
		raw, _ := json.Marshal(map[string]any{"entity": f.related})
		return raw, nil
	case "homeassistant/expose_entity":
		f.exposed = append(f.exposed, cmd["entity_ids"].([]string)...)
		return json.RawMessage(`null`), nil
	}
	return nil, fmt.Errorf("unknown command %s", typ)
}

func (f *fakeHA) CallService(_ context.Context, domain, service string, data map[string]any, _ *haws.Target) error {
	if domain != "mqtt" || service != "publish" {
		return fmt.Errorf("unexpected %s.%s", domain, service)
	}
	var payload map[string]any
	_ = json.Unmarshal([]byte(data["payload"].(string)), &payload)
	topic := data["topic"].(string)
	f.mu.Lock()
	f.published = append(f.published, published{topic, payload})
	renameErr := f.renameErr
	f.mu.Unlock()
	// The bridge answers.
	endpoint := strings.TrimPrefix(topic, "zigbee2mqtt/bridge/request/")
	answer := map[string]any{"status": "ok", "data": map[string]any{}, "transaction": payload["transaction"]}
	if endpoint == "device/rename" && renameErr != "" {
		answer = map[string]any{"status": "error", "error": renameErr, "data": map[string]any{}, "transaction": payload["transaction"]}
	}
	f.send("zigbee2mqtt/bridge/response/"+endpoint, answer)
	if endpoint == "permit_join" {
		f.send("zigbee2mqtt/bridge/info", map[string]any{"permit_join": payload["time"].(float64) > 0})
	}
	return nil
}

func (f *fakeHA) Subscribe(_ context.Context, eventType string, h haws.EventHandler) (func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events[eventType] = append(f.events[eventType], h)
	n := len(f.events[eventType]) - 1
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.events[eventType][n] = nil
	}, nil
}

func (f *fakeHA) SubscribeMQTT(_ context.Context, topic string, h func(haws.MQTTMessage)) (func(), error) {
	f.mu.Lock()
	f.mqtt[topic] = append(f.mqtt[topic], h)
	n := len(f.mqtt[topic]) - 1
	f.mu.Unlock()
	if topic == "zigbee2mqtt/bridge/info" { // retained: pairing closed
		h(haws.MQTTMessage{Topic: topic, Payload: `{"permit_join":false}`, Retain: true})
	}
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.mqtt[topic][n] = nil
	}, nil
}

func (f *fakeHA) send(topic string, payload any) {
	raw, _ := json.Marshal(payload)
	f.mu.Lock()
	var hs []func(haws.MQTTMessage)
	for sub, list := range f.mqtt {
		if sub == topic || (strings.HasSuffix(sub, "/#") && strings.HasPrefix(topic, strings.TrimSuffix(sub, "#"))) {
			hs = append(hs, list...)
		}
	}
	f.mu.Unlock()
	for _, h := range hs {
		if h != nil {
			h(haws.MQTTMessage{Topic: topic, Payload: string(raw)})
		}
	}
}

func (f *fakeHA) bridgeEvent(typ string, data map[string]any) {
	f.send("zigbee2mqtt/bridge/event", map[string]any{"type": typ, "data": data})
}

func (f *fakeHA) entityCreated(id string) {
	f.mu.Lock()
	hs := append([]haws.EventHandler{}, f.events["entity_registry_updated"]...)
	f.mu.Unlock()
	data, _ := json.Marshal(map[string]any{"action": "create", "entity_id": id})
	for _, h := range hs {
		if h != nil {
			h(haws.Event{EventType: "entity_registry_updated", Data: data})
		}
	}
}

// permits are the permit_join times published, in order.
func (f *fakeHA) permits() []float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []float64
	for _, p := range f.published {
		if strings.HasSuffix(p.Topic, "/permit_join") {
			out = append(out, p.Payload["time"].(float64))
		}
	}
	return out
}

type env struct {
	ha      *fakeHA
	p       *Pairing
	asked   chan string
	notes   []string
	ticked  []string
	ended   chan string
	logs    *bytes.Buffer
	devices map[string]haws.Device
	mu      sync.Mutex
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{ha: newFakeHA(), asked: make(chan string, 8), ended: make(chan string, 4), logs: &bytes.Buffer{},
		devices: map[string]haws.Device{bridgeID: {ID: bridgeID, Manufacturer: "Zigbee2MQTT", Model: "Bridge", Name: "Zigbee2MQTT Bridge"}}}
	var logMu sync.Mutex
	logger := slog.New(slog.NewTextHandler(writerFunc(func(b []byte) (int, error) {
		logMu.Lock()
		defer logMu.Unlock()
		return e.logs.Write(b)
	}), nil))
	e.p = New(Options{Lang: i18n.DE, Logger: logger, HA: e.ha, Wait: time.Second, Interview: time.Second,
		Devices:    func() map[string]haws.Device { return e.devices },
		DeviceArea: func() map[string]string { return map[string]string{"old": "kueche"} },
		AreaKeys:   func() map[string]string { return map[string]string{"wohnzimmer": "wohnzimmer", "küche": "kueche"} },
		AreaName: func(id string) string {
			return map[string]string{"wohnzimmer": "Wohnzimmer", "kueche": "Küche"}[id]
		},
		Start:  func(context.Context, tools.Invocation, string) (string, error) { return thread, nil },
		Thread: func(context.Context, tools.Invocation) string { return thread },
		Tick: func(_ context.Context, _, match string) error {
			e.mu.Lock()
			defer e.mu.Unlock()
			e.ticked = append(e.ticked, match)
			return nil
		},
		Note: func(_ context.Context, _, text string) error {
			e.mu.Lock()
			defer e.mu.Unlock()
			e.notes = append(e.notes, text)
			return nil
		},
		Ask:      func(_ context.Context, _, text string) error { e.asked <- text; return nil },
		End:      func(_ context.Context, th string) { e.ended <- th },
		Question: func(_ context.Context, facts string) (string, error) { return "Ich hab's: " + facts, nil },
	})
	return e
}

type writerFunc func([]byte) (int, error)

func (w writerFunc) Write(b []byte) (int, error) { return w(b) }

var voice = tools.Invocation{Channel: config.ChannelVoice, DeviceID: "show", Area: "schlafzimmer"}

func (e *env) run(t *testing.T, name string, args string) string {
	t.Helper()
	for _, tool := range e.p.Tools() {
		if tool.Name == name {
			inv := voice
			inv.Args = json.RawMessage(args)
			out, err := tool.Run(context.Background(), inv)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			return out
		}
	}
	t.Fatalf("no tool %s", name)
	return ""
}

func (e *env) said(t *testing.T) string {
	t.Helper()
	select {
	case s := <-e.asked:
		return s
	case <-time.After(3 * time.Second):
		t.Fatal("Mira said nothing at the satellite")
	}
	return ""
}

// pairNew opens pairing and lets the temperature sensor join, its interview
// succeed and Home Assistant create its entities; it returns what Mira asks.
func (e *env) pairNew(t *testing.T) string {
	t.Helper()
	if out := e.run(t, ToolStart, `{}`); out != e.p.t.pairingOpen {
		t.Fatalf("start = %q", out)
	}
	e.ha.bridgeEvent("device_joined", map[string]any{"friendly_name": ieee, "ieee_address": ieee})
	e.ha.bridgeEvent("device_announce", map[string]any{"friendly_name": ieee, "ieee_address": ieee})
	e.ha.bridgeEvent("device_interview", map[string]any{"friendly_name": ieee, "status": "started", "ieee_address": ieee})
	e.ha.entityCreated("sensor.x_temperature")
	e.ha.bridgeEvent("device_interview", map[string]any{"friendly_name": ieee, "status": "successful", "ieee_address": ieee,
		"supported": true, "definition": map[string]any{"vendor": "Aqara", "model": "WSDCGQ11LM",
			"description": "Temperature, humidity and pressure sensor",
			"exposes":     []map[string]any{{"name": "temperature"}, {"name": "battery", "category": "diagnostic"}}}})
	if s := e.said(t); s != e.p.t.joining {
		t.Fatalf("first said %q, want that the device is being learned", s)
	}
	return e.said(t)
}

func TestTextsComplete(t *testing.T) {
	for lang, x := range catalog {
		v := reflect.ValueOf(x)
		for i := range v.NumField() {
			if v.Field(i).String() == "" {
				t.Errorf("%s: %s is empty", lang, v.Type().Field(i).Name)
			}
		}
	}
}

func TestDV01_PairingOpensThroughZigbee2MQTT(t *testing.T) {
	e := newEnv(t)
	if out := e.run(t, ToolStart, `{}`); out != e.p.t.pairingOpen {
		t.Fatalf("start = %q", out)
	}
	if got := e.ha.permits(); len(got) != 1 || got[0] != 180 {
		t.Fatalf("permit_join published = %v, want [180]", got)
	}
	e.p.ThreadEnded(context.Background(), thread)
}

func TestDV02_NoBridgeNoPairing(t *testing.T) {
	e := newEnv(t)
	e.devices = map[string]haws.Device{}
	if out := e.run(t, ToolStart, `{}`); out != e.p.t.noBridge {
		t.Fatalf("start = %q", out)
	}
	if len(e.ha.published) != 0 {
		t.Fatalf("published: %+v", e.ha.published)
	}
	if !strings.Contains(e.logs.String(), "zigbee bridge not found") {
		t.Errorf("log: %s", e.logs)
	}
}

func TestDV03_TheNewDeviceIsAnnouncedAtTheThreadsSatellite(t *testing.T) {
	e := newEnv(t)
	q := e.pairNew(t)
	if !strings.HasPrefix(q, "Ich hab's: ") || !strings.Contains(q, "Aqara") || !strings.Contains(q, "Temperature, humidity") {
		t.Fatalf("question = %q", q)
	}
	if got := e.ha.permits(); len(got) != 2 || got[1] != 0 {
		t.Fatalf("permit_join published = %v, want pairing closed", got)
	}
	if len(e.notes) != 1 || !strings.Contains(e.notes[0], "WSDCGQ11LM") {
		t.Errorf("notes = %v", e.notes)
	}
	if len(e.ticked) != 1 || e.ticked[0] != "Gerät anmelden" {
		t.Errorf("ticked = %v", e.ticked)
	}
}

func TestDV03_TheQuestionWithoutTheModel(t *testing.T) {
	e := newEnv(t)
	e.p.o.Question = func(context.Context, string) (string, error) { return "", errors.New("model down") }
	if q := e.pairNew(t); q != fmt.Sprintf(e.p.t.found, "Aqara", "WSDCGQ11LM") {
		t.Fatalf("question = %q", q)
	}
	if !strings.Contains(e.logs.String(), "could not word the question") {
		t.Errorf("log: %s", e.logs)
	}
}

func TestDV04_TheAnswerNamesTheRoom(t *testing.T) {
	e := newEnv(t)
	e.pairNew(t)
	out := e.run(t, ToolAssign, `{"room":"Wohnzimmer"}`)
	if !strings.Contains(out, "Wohnzimmer") {
		t.Fatalf("assign = %q", out)
	}
	if len(e.ha.updates) != 1 || e.ha.updates[0]["device_id"] != deviceID || e.ha.updates[0]["area_id"] != "wohnzimmer" {
		t.Fatalf("updates = %+v", e.ha.updates)
	}
	if out := e.run(t, ToolAssign, `{"room":"Dachboden"}`); !strings.Contains(out, "Dachboden") || !strings.Contains(out, "Küche, Wohnzimmer") {
		t.Errorf("unknown room = %q", out)
	}
}

func TestDV05_FinishingRenamesAndExposes(t *testing.T) {
	e := newEnv(t)
	e.pairNew(t)
	e.run(t, ToolAssign, `{"room":"Wohnzimmer"}`)
	out := e.run(t, ToolFinish, `{"name":"Temperatursensor Wohnzimmer"}`)
	if !strings.Contains(out, "Temperatursensor Wohnzimmer") || !strings.Contains(out, "2 Werte") {
		t.Fatalf("finish = %q", out)
	}
	if strings.Join(e.ha.exposed, ",") != "sensor.x_humidity,sensor.x_temperature" {
		t.Errorf("exposed = %v", e.ha.exposed)
	}
	last := e.ha.published[len(e.ha.published)-1]
	if last.Topic != "zigbee2mqtt/bridge/request/device/rename" || last.Payload["from"] != ieee ||
		last.Payload["to"] != "Temperatursensor Wohnzimmer" || last.Payload["homeassistant_rename"] != true {
		t.Errorf("rename = %+v", last)
	}
	select {
	case th := <-e.ended:
		if th != thread {
			t.Errorf("ended %s", th)
		}
	default:
		t.Error("the thread is not done")
	}
}

func TestDV05_ARefusedRenameKeepsTheRoom(t *testing.T) {
	e := newEnv(t)
	e.pairNew(t)
	e.ha.renameErr = "Friendly name cannot end with a number"
	out := e.run(t, ToolFinish, `{"name":"Sensor 1"}`)
	if !strings.Contains(out, "Friendly name cannot end with a number") || !strings.Contains(out, "2 Werte") {
		t.Fatalf("finish = %q", out)
	}
	if !strings.Contains(e.logs.String(), "could not rename the zigbee device") {
		t.Errorf("log: %s", e.logs)
	}
}

func TestDV06_NothingJoins(t *testing.T) {
	e := newEnv(t)
	e.run(t, ToolStart, `{}`)
	// Zigbee2MQTT ends pairing when its time is over.
	e.ha.send("zigbee2mqtt/bridge/info", map[string]any{"permit_join": false})
	if s := e.said(t); s != e.p.t.nothingJoined {
		t.Fatalf("said %q", s)
	}
}

func TestDV07_TheInterviewFails(t *testing.T) {
	e := newEnv(t)
	e.run(t, ToolStart, `{}`)
	e.ha.bridgeEvent("device_joined", map[string]any{"friendly_name": ieee, "ieee_address": ieee})
	e.ha.bridgeEvent("device_interview", map[string]any{"friendly_name": ieee, "status": "failed", "ieee_address": ieee})
	if s := e.said(t); s != e.p.t.joining {
		t.Fatalf("said %q", s)
	}
	if s := e.said(t); s != e.p.t.interviewFailed {
		t.Fatalf("said %q", s)
	}
	if got := e.ha.permits(); len(got) != 1 {
		t.Fatalf("permit_join published = %v, want pairing still open", got)
	}
	if !strings.Contains(e.logs.String(), "zigbee interview failed") {
		t.Errorf("log: %s", e.logs)
	}
	e.p.ThreadEnded(context.Background(), thread)
}

func TestDV08_AKnownDevicePairedAgain(t *testing.T) {
	e := newEnv(t)
	e.devices["old"] = haws.Device{ID: "old", Name: "Steckdose Küche", Manufacturer: "IKEA"}
	e.run(t, ToolStart, `{}`)
	e.ha.bridgeEvent("device_announce", map[string]any{"friendly_name": "Steckdose Küche", "ieee_address": "0x01"})
	if s := e.said(t); s != fmt.Sprintf(e.p.t.known, "Steckdose Küche", "Küche") {
		t.Fatalf("said %q", s)
	}
	if got := e.ha.permits(); len(got) != 2 || got[1] != 0 {
		t.Fatalf("permit_join published = %v, want pairing closed", got)
	}
	select {
	case <-e.ended:
	case <-time.After(time.Second):
		t.Fatal("the thread is not done")
	}
}

func TestDV11_AJoiningDeviceIsSaidOnce(t *testing.T) {
	e := newEnv(t)
	e.run(t, ToolStart, `{}`)
	e.ha.bridgeEvent("device_joined", map[string]any{"friendly_name": ieee, "ieee_address": ieee})
	if s := e.said(t); s != e.p.t.joining {
		t.Fatalf("said %q", s)
	}
	// The device leaves and joins again (reset once more): nothing new is said.
	e.ha.bridgeEvent("device_leave", map[string]any{"friendly_name": ieee, "ieee_address": ieee})
	e.ha.bridgeEvent("device_joined", map[string]any{"friendly_name": ieee, "ieee_address": ieee})
	select {
	case s := <-e.asked:
		t.Fatalf("said again %q", s)
	case <-time.After(300 * time.Millisecond):
	}
	e.p.ThreadEnded(context.Background(), thread)
}

func TestDV09_CancellingClosesPairing(t *testing.T) {
	e := newEnv(t)
	e.run(t, ToolStart, `{}`)
	e.p.ThreadEnded(context.Background(), thread)
	if got := e.ha.permits(); len(got) != 2 || got[1] != 0 {
		t.Fatalf("permit_join published = %v, want pairing closed", got)
	}
}

func TestDV10_ARefusedChangeIsSaidNotSwallowed(t *testing.T) {
	e := newEnv(t)
	e.pairNew(t)
	e.ha.refuse["config/device_registry/update"] = errors.New("Unauthorized")
	out := e.run(t, ToolAssign, `{"room":"Wohnzimmer"}`)
	if !strings.Contains(out, "Unauthorized") {
		t.Fatalf("assign = %q", out)
	}
	if !strings.Contains(e.logs.String(), "could not put the zigbee device into its room") {
		t.Errorf("log: %s", e.logs)
	}
}

func TestPairingOnlyAtASatellite(t *testing.T) {
	e := newEnv(t)
	for _, tool := range e.p.Tools() {
		if tool.Name == ToolStart {
			out, _ := tool.Run(context.Background(), tools.Invocation{Channel: config.ChannelTelegram, Args: json.RawMessage(`{}`)})
			if out != e.p.t.notVoice || len(e.ha.published) != 0 {
				t.Fatalf("start on Telegram = %q, published %v", out, e.ha.published)
			}
		}
	}
}
