package devices

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/tools"
)

// Devices Home Assistant found: docs/spec/found-devices.md, steps 3 and 4.
// fakeHA answers as the test HA did with testenv/found_fake (2026-11-10).

type fakeDevice struct {
	registry
	measuring bool
	platform  string // the integration of its entities; "" for hue
}

type fakeHA struct {
	mu        sync.Mutex
	flowSubs  []func([]haws.FlowEvent)
	devSubs   []haws.EventHandler
	devices   map[string]*fakeDevice
	labels    map[string]string // id -> name
	steps     map[string]haws.FlowStep
	submitted map[string]map[string]any
	ignored   map[string]string
	updates   []map[string]any
	exposed   []string
	renamed   []string // old->new entity ids
	refuse    map[string]error
	// onSubmit creates the entry's devices, as Home Assistant does before it
	// answers.
	onSubmit map[string]func() haws.FlowStep
}

func newFakeHA() *fakeHA {
	return &fakeHA{devices: map[string]*fakeDevice{}, labels: map[string]string{}, steps: map[string]haws.FlowStep{},
		submitted: map[string]map[string]any{}, ignored: map[string]string{}, refuse: map[string]error{},
		onSubmit: map[string]func() haws.FlowStep{}}
}

func (f *fakeHA) Call(_ context.Context, cmd map[string]any) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	typ := cmd["type"].(string)
	if err := f.refuse[typ]; err != nil {
		return nil, err
	}
	switch typ {
	case "config/device_registry/list":
		var list []registry
		for _, d := range f.devices {
			list = append(list, d.registry)
		}
		return json.Marshal(list)
	case "config/entity_registry/list_for_display":
		var ents []map[string]any
		for id, d := range f.devices {
			pl := d.platform
			if pl == "" {
				pl = "hue"
			}
			ents = append(ents, map[string]any{"ei": "sensor." + id + "_signal", "di": id, "pl": pl, "ec": 1})
			if d.measuring {
				ents = append(ents, map[string]any{"ei": "sensor." + id + "_temperature", "di": id, "pl": pl})
			}
		}
		return json.Marshal(map[string]any{"entities": ents})
	case "config/label_registry/list":
		var list []map[string]string
		for id, name := range f.labels {
			list = append(list, map[string]string{"label_id": id, "name": name})
		}
		return json.Marshal(list)
	case "config/label_registry/create":
		f.labels["mira_ignoriert"] = cmd["name"].(string)
		return json.Marshal(map[string]string{"label_id": "mira_ignoriert", "name": cmd["name"].(string)})
	case "config/device_registry/update":
		f.updates = append(f.updates, cmd)
		if d := f.devices[cmd["device_id"].(string)]; d != nil {
			if area, ok := cmd["area_id"].(string); ok {
				d.AreaID = area
			}
			if labels, ok := cmd["labels"].([]string); ok {
				d.Labels = labels
			}
			if n, ok := cmd["name_by_user"].(string); ok {
				d.NameByUser = n
			}
		}
		return json.RawMessage(`{}`), nil
	case "config_entries/ignore_flow":
		f.ignored[cmd["flow_id"].(string)] = cmd["title"].(string)
		return json.RawMessage(`null`), nil
	case "manifest/get":
		return json.RawMessage(`{"name":"Philips Hue"}`), nil
	case "frontend/get_translations":
		return json.RawMessage(`{"resources":{"component.hue.config.step.link.title":"Hub verbinden",
			"component.hue.config.step.link.description":"Drück den Knopf auf der Bridge.",
			"component.hue.config.error.register_failed":"Der Knopf wurde nicht gedrückt."}}`), nil
	case "search/related":
		id := cmd["item_id"].(string)
		return json.Marshal(map[string]any{"entity": []string{"sensor." + id + "_temperature", "sensor." + id + "_signal"}})
	case "config/entity_registry/get_entries":
		out := map[string]any{}
		for _, id := range cmd["entity_ids"].([]string) {
			e := map[string]any{"entity_id": id}
			if strings.HasSuffix(id, "_signal") {
				e["entity_category"] = "diagnostic"
			}
			out[id] = e
		}
		return json.Marshal(out)
	case "config/entity_registry/update":
		f.renamed = append(f.renamed, cmd["entity_id"].(string)+"->"+cmd["new_entity_id"].(string))
		return json.RawMessage(`{}`), nil
	case "homeassistant/expose_entity":
		f.exposed = append(f.exposed, cmd["entity_ids"].([]string)...)
		return json.RawMessage(`null`), nil
	}
	return nil, fmt.Errorf("unknown command %s", typ)
}

func (f *fakeHA) Subscribe(_ context.Context, eventType string, h haws.EventHandler) (func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if eventType == "device_registry_updated" {
		f.devSubs = append(f.devSubs, h)
	}
	return func() {}, nil
}

func (f *fakeHA) SubscribeFlows(_ context.Context, h func([]haws.FlowEvent)) (func(), error) {
	f.mu.Lock()
	f.flowSubs = append(f.flowSubs, h)
	var initial []haws.FlowEvent
	for id, s := range f.steps {
		fl := haws.Flow{FlowID: id, Handler: s.Handler, StepID: s.StepID,
			Context: haws.FlowContext{Source: "zeroconf", UniqueID: "u-" + id, TitlePlaceholders: map[string]any{"name": s.Title}}}
		initial = append(initial, haws.FlowEvent{FlowID: id, Flow: &fl})
	}
	f.mu.Unlock()
	h(initial)
	return func() {}, nil
}

func (f *fakeHA) GetFlow(_ context.Context, id string) (haws.FlowStep, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.steps[id]
	if !ok {
		return haws.FlowStep{Type: "abort", Reason: "not found"}, nil
	}
	return s, nil
}

func (f *fakeHA) SubmitFlow(_ context.Context, id string, data map[string]any) (haws.FlowStep, error) {
	f.mu.Lock()
	if err := f.refuse["submit"]; err != nil {
		f.mu.Unlock()
		return haws.FlowStep{}, err
	}
	f.submitted[id] = data
	next := f.onSubmit[id]
	f.mu.Unlock()
	return next(), nil
}

// locked runs fn with the fake's lock held, for a test to look.
func (f *fakeHA) locked(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn()
}

// discover makes Home Assistant discover a flow (row 2).
func (f *fakeHA) discover(id, handler, name, stepID string, fields ...string) {
	step := haws.FlowStep{Type: "form", FlowID: id, Handler: handler, StepID: stepID, Title: name}
	for _, n := range fields {
		step.DataSchema = append(step.DataSchema, haws.FlowField{Name: n, Type: "string", Required: true})
	}
	f.mu.Lock()
	f.steps[id] = step
	subs := slices.Clone(f.flowSubs)
	f.mu.Unlock()
	fl := haws.Flow{FlowID: id, Handler: handler, StepID: stepID,
		Context: haws.FlowContext{Source: "zeroconf", UniqueID: "u-" + id, TitlePlaceholders: map[string]any{"name": name}}}
	for _, h := range subs {
		h([]haws.FlowEvent{{Type: "added", FlowID: id, Flow: &fl}})
	}
}

// addDevice creates a device in the registry and fires its create event.
func (f *fakeHA) addDevice(id, name, entry string, measuring bool) {
	f.addDeviceOf("", id, name, entry, measuring)
}

// addDeviceOf adds a device whose entities come from the integration platform.
func (f *fakeHA) addDeviceOf(platform, id, name, entry string, measuring bool) {
	f.mu.Lock()
	f.devices[id] = &fakeDevice{registry: registry{ID: id, Name: name, Manufacturer: "Signify", Model: "LCA001",
		ConfigEntries: []string{entry}}, measuring: measuring, platform: platform}
	subs := slices.Clone(f.devSubs)
	f.mu.Unlock()
	data, _ := json.Marshal(map[string]string{"action": "create", "device_id": id})
	for _, h := range subs {
		h(haws.Event{Data: data})
	}
}

type memKV struct {
	mu   sync.Mutex
	m    map[string]string
	said map[string]string
}

func (k *memKV) FoundSaid(context.Context) (map[string]string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return maps.Clone(k.said), nil
}

func (k *memKV) SetFoundSaid(_ context.Context, said map[string]string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.said = maps.Clone(said)
	return nil
}

// saidKeys are the said keys, sorted and joined.
func (k *memKV) saidKeys() string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return strings.Join(slices.Sorted(maps.Keys(k.said)), " ")
}

func (k *memKV) GetKV(_ context.Context, key string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.m[key]
	if !ok {
		return "", store.ErrNotFound
	}
	return v, nil
}

func (k *memKV) get(key string) string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.m[key]
}

func (k *memKV) SetKV(_ context.Context, key, value string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.m[key] = value
	return nil
}

const foundThread = "th-found"

type foundEnv struct {
	ha     *fakeHA
	kv     *memKV
	f      *Found
	core   *Core
	posts  chan [2]string
	ended  chan string
	ticked []string
	logs   *bytes.Buffer
	wifi   [2]string
	mu     sync.Mutex
	cancel context.CancelFunc
	// The threads as the real ones behave (docs/spec/messages.md, decisions
	// 27, 28): each found item's own thread "house:<item>", open until it
	// ends; current is the turn's thread; started counts threads of the room
	// (the way for an item found before the update).
	open    map[string]bool
	current string
	started int
}

func newFoundEnv(t *testing.T, kv *memKV, ha *fakeHA) *foundEnv {
	t.Helper()
	if kv == nil {
		kv = &memKV{m: map[string]string{keyStarted: "2026-11-10T00:00:00Z"}}
	}
	if ha == nil {
		ha = newFakeHA()
	}
	e := &foundEnv{ha: ha, kv: kv, posts: make(chan [2]string, 8), ended: make(chan string, 8), logs: &bytes.Buffer{}}
	var logMu sync.Mutex
	logger := slog.New(slog.NewTextHandler(writer(func(b []byte) (int, error) {
		logMu.Lock()
		defer logMu.Unlock()
		return e.logs.Write(b)
	}), nil))
	tick := func(_ context.Context, _, match string) error {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.ticked = append(e.ticked, match)
		return nil
	}
	e.open = map[string]bool{}
	end := func(_ context.Context, th string) {
		e.mu.Lock()
		delete(e.open, strings.TrimPrefix(th, "house:"))
		e.mu.Unlock()
		e.ended <- th
	}
	thread := func(context.Context, tools.Invocation) string {
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.current == "" {
			return foundThread
		}
		return e.current
	}
	e.core = NewCore(CoreOptions{Lang: i18n.DE, Logger: logger, HA: ha,
		AreaKeys: func() map[string]string { return map[string]string{"küche": "kueche"} },
		AreaName: func(string) string { return "Küche" }, Thread: thread, Tick: tick, End: end,
		DeviceName: func(id string) string {
			ha.mu.Lock()
			defer ha.mu.Unlock()
			if d := ha.devices[id]; d != nil {
				return d.Name
			}
			return ""
		}})
	e.f = NewFound(FoundOptions{Lang: i18n.DE, Logger: logger, HA: ha, Core: e.core, KV: kv,
		Post: func(_ context.Context, thread, spoken, details string) error {
			if !strings.HasPrefix(thread, "house:") {
				t.Errorf("an entry without its thread: %q", thread)
			}
			e.posts <- [2]string{spoken, details}
			return nil
		},
		StartHouse: func(_ context.Context, _, plan, item string) (string, error) {
			if !strings.Contains(plan, "Einbinden") {
				t.Errorf("plan = %q", plan)
			}
			e.mu.Lock()
			e.open[item] = true
			e.mu.Unlock()
			return "house:" + item, nil
		},
		TakeItem: func(_ context.Context, _ tools.Invocation, item string) (string, bool, error) {
			e.mu.Lock()
			defer e.mu.Unlock()
			if !e.open[item] {
				return "", false, nil
			}
			e.current = "house:" + item
			return e.current, true, nil
		},
		ItemThread: func(_ context.Context, item string) string {
			e.mu.Lock()
			defer e.mu.Unlock()
			if e.open[item] {
				return "house:" + item
			}
			return ""
		},
		Wifi: func() (string, string) { return e.wifi[0], e.wifi[1] },
		// A Zigbee dialog waits for the device "zb".
		Claims: []func(string) bool{func(id string) bool { return id == "zb" }},
		Start: func(context.Context, tools.Invocation, string) (string, error) {
			e.mu.Lock()
			defer e.mu.Unlock()
			e.started++
			e.current = foundThread
			return foundThread, nil
		},
		Thread: thread, Tick: tick, End: end,
		Note:   func(context.Context, string, string) error { return nil },
		Gather: 30 * time.Millisecond, EntryWait: 200 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	t.Cleanup(cancel)
	go func() { _ = e.f.Run(ctx) }()
	waitFor(t, func() bool {
		ha.mu.Lock()
		defer ha.mu.Unlock()
		return len(ha.flowSubs) > 0 && len(ha.devSubs) > 0
	})
	return e
}

type writer func([]byte) (int, error)

func (w writer) Write(b []byte) (int, error) { return w(b) }

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out")
}

func (e *foundEnv) post(t *testing.T) [2]string {
	t.Helper()
	select {
	case p := <-e.posts:
		return p
	case <-time.After(2 * time.Second):
		t.Fatal("no message to the house")
	}
	return [2]string{}
}

func (e *foundEnv) noPost(t *testing.T) {
	t.Helper()
	select {
	case p := <-e.posts:
		t.Fatalf("a message was left: %q", p[0])
	case <-time.After(150 * time.Millisecond):
	}
}

func (e *foundEnv) run(t *testing.T, name, args string) string {
	t.Helper()
	for _, tool := range append(e.f.Tools(), e.core.Tools()...) {
		if tool.Name == name {
			out, err := tool.Run(context.Background(), tools.Invocation{Args: json.RawMessage(args)})
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			return out
		}
	}
	t.Fatalf("no tool %s", name)
	return ""
}

func TestFD01_ADiscoveredDeviceIsSaidOnceAsAMessageToTheHouse(t *testing.T) {
	e := newFoundEnv(t, nil, nil)
	e.ha.discover("f1", "hue", "Hue Bridge", "link")
	p := e.post(t)
	if p[0] != "Neues Gerät gefunden: Hue Bridge" {
		t.Fatalf("spoken = %q", p[0])
	}
	if !strings.Contains(p[1], "flow:f1: Hue Bridge") || !strings.Contains(p[1], "geraet-einbinden") {
		t.Errorf("details = %q", p[1])
	}
	waitFor(t, func() bool { return strings.Contains(e.kv.saidKeys(), "flow:hue:u-f1") })
}

// Since 2026-11-12 (docs/spec/messages.md, decision 28) each device is an
// entry with its own thread; the store gathers the entries of a burst into
// one unheard device message (MS-32).
func TestFD02_ABurstOfNewDevicesIsOneMessage(t *testing.T) {
	e := newFoundEnv(t, nil, nil)
	for i := 1; i <= 5; i++ {
		e.ha.addDevice(fmt.Sprintf("d%d", i), fmt.Sprintf("Lampe %d", i), "e1", true)
	}
	for i := 1; i <= 5; i++ {
		if p := e.post(t); p[0] != fmt.Sprintf("Neues Gerät gefunden: Lampe %d", i) {
			t.Fatalf("entry %d = %q", i, p[0])
		}
	}
	e.noPost(t)
}

func TestFD03_SaidOnceNeverAgain(t *testing.T) {
	ha := newFakeHA()
	e := newFoundEnv(t, nil, ha)
	ha.addDevice("d1", "Hue Lampe", "e1", true)
	e.post(t)
	// Mira restarts: the subscriptions start again with what is open.
	e.cancel()
	ha.mu.Lock()
	ha.flowSubs, ha.devSubs = nil, nil
	ha.mu.Unlock()
	e2 := newFoundEnv(t, e.kv, ha)
	ha.addDevice("d1", "Hue Lampe", "e1", true) // the same device's event once more
	e2.noPost(t)
	if out := e2.run(t, ToolFound, `{}`); !strings.Contains(out, "device:d1: Hue Lampe") {
		t.Fatalf("list = %q", out)
	}
}

func TestFD04_WhatDoesNotCountIsNotSaid(t *testing.T) {
	e := newFoundEnv(t, nil, nil)
	// Without an entity that would be exposed.
	e.ha.addDevice("d1", "Bridge", "e1", false)
	// With an area by now.
	e.ha.addDevice("d2", "Lampe", "e1", true)
	e.ha.locked(func() { e.ha.devices["d2"].AreaID = "kueche" })
	// One a Zigbee dialog waits for.
	e.ha.addDevice("zb", "Sensor", "e1", true)
	// Mira's own integration.
	e.ha.discover("f9", "mira", "Mira", "token", "token")
	e.noPost(t)
}

// As the iBeacon integration made them in the live house (2026-11-12): a
// device tracker and an estimated distance, no area.
func TestFD14_ABeaconPassingByIsNotSaid(t *testing.T) {
	e := newFoundEnv(t, nil, nil)
	e.ha.addDeviceOf("ibeacon", "b1", "Ooono F646", "ib", true)
	e.ha.addDevice("d1", "Hue Lampe", "e1", true)
	p := e.post(t)
	if p[0] != "Neues Gerät gefunden: Hue Lampe" {
		t.Fatalf("spoken = %q", p[0])
	}
	if strings.Contains(p[1], "Ooono") {
		t.Errorf("the beacon is in the details: %q", p[1])
	}
	e.noPost(t)
	if out := e.run(t, ToolFound, `{}`); strings.Contains(out, "Ooono") {
		t.Errorf("the beacon is listed: %q", out)
	}
}

func TestFD05_TheBacklogIsNotReadOut(t *testing.T) {
	ha := newFakeHA()
	ha.discover("f1", "hue", "Hue Bridge", "link")
	ha.discover("f2", "shelly", "Shelly Plus 1", "confirm")
	e := newFoundEnv(t, &memKV{m: map[string]string{}}, ha)
	e.noPost(t)
	if !strings.Contains(e.kv.saidKeys(), "flow:hue:u-f1") || !strings.Contains(e.kv.saidKeys(), "flow:shelly:u-f2") ||
		e.kv.get(keyStarted) == "" {
		t.Fatalf("said = %s", e.kv.saidKeys())
	}
	out := e.run(t, ToolFound, `{}`)
	if !strings.Contains(out, "Hue Bridge") || !strings.Contains(out, "Shelly Plus 1") {
		t.Fatalf("list = %q", out)
	}
}

func TestFD06_ANewDeviceIsTakenInRoomNameExposed(t *testing.T) {
	e := newFoundEnv(t, nil, nil)
	e.ha.addDevice("d1", "Hue color lamp 1", "e1", true)
	e.post(t)
	if out := e.run(t, ToolAdopt, `{"item":"device:d1"}`); !strings.Contains(out, "Hue color lamp 1") || !strings.Contains(out, "Raum") {
		t.Fatalf("adopt = %q", out)
	}
	if out := e.run(t, ToolAssign, `{"room":"Küche"}`); !strings.Contains(out, "Küche") {
		t.Fatalf("assign = %q", out)
	}
	out := e.run(t, ToolFinish, `{"name":"Lampe Küche"}`)
	if !strings.Contains(out, "Lampe Küche") || !strings.Contains(out, "1 Werte") {
		t.Fatalf("finish = %q", out)
	}
	e.ha.locked(func() {
		d := e.ha.devices["d1"]
		if d.AreaID != "kueche" || d.NameByUser != "Lampe Küche" || strings.Join(e.ha.exposed, ",") != "sensor.d1_temperature" {
			t.Errorf("device = %+v, exposed %v", d.registry, e.ha.exposed)
		}
	})
	if strings.Join(e.ticked, ",") != "Einbinden,Raum,Name" {
		t.Errorf("ticked = %v", e.ticked)
	}
	e.tookItsThread(t, "device:d1")
	if th := <-e.ended; th != "house:device:d1" {
		t.Errorf("ended %s", th)
	}
}

// created: the submit makes the entry, and Home Assistant creates its device
// before it answers (seen on the test HA 2026-11-10).
func (e *foundEnv) created(flow, device string) {
	e.ha.mu.Lock()
	defer e.ha.mu.Unlock()
	e.ha.onSubmit[flow] = func() haws.FlowStep {
		go e.ha.addDevice(device, "Shelly Plus 1", "entry-"+flow, true)
		time.Sleep(20 * time.Millisecond)
		step := haws.FlowStep{Type: "create_entry", FlowID: flow}
		step.Result.EntryID = "entry-" + flow
		return step
	}
}

func TestFD06_ADeviceWithARoomIsNotTakenInAgain(t *testing.T) {
	e := newFoundEnv(t, nil, nil)
	e.ha.addDevice("d1", "Hue Lampe", "e1", true)
	e.post(t)
	// Taken in since; an older message still names it.
	e.ha.locked(func() { e.ha.devices["d1"].AreaID = "flur" })
	if out := e.run(t, ToolAdopt, `{"item":"device:d1"}`); out != e.f.t.notOpen {
		t.Fatalf("adopt = %q", out)
	}
	if e.core.Holds("d1") {
		t.Error("the thread holds the device")
	}
}

func TestFD07_ADiscoveredDeviceWithAConfirmationOnly(t *testing.T) {
	e := newFoundEnv(t, nil, nil)
	e.ha.discover("f1", "shelly", "Shelly Plus 1", "confirm")
	e.post(t)
	e.created("f1", "d1")
	if out := e.run(t, ToolAdopt, `{"item":"flow:f1"}`); !strings.Contains(out, "device_confirm gleich") {
		t.Fatalf("adopt = %q", out)
	}
	e.tookItsThread(t, "flow:f1")
	out := e.run(t, ToolConfirm, `{}`)
	if !strings.Contains(out, "Shelly Plus 1") || !strings.Contains(out, "Raum") {
		t.Fatalf("confirm = %q", out)
	}
	e.ha.locked(func() {
		if len(e.ha.submitted["f1"]) != 0 {
			t.Errorf("submitted %v", e.ha.submitted["f1"])
		}
	})
	// The device it brought is the thread's, not said again.
	e.noPost(t)
	if !e.core.Holds("d1") {
		t.Error("the thread does not hold the device")
	}
}

func TestFD08_AStepThatNeedsTheButtonPressed(t *testing.T) {
	e := newFoundEnv(t, nil, nil)
	e.ha.discover("f1", "hue", "Hue Bridge", "link")
	e.post(t)
	out := e.run(t, ToolAdopt, `{"item":"flow:f1"}`)
	if !strings.Contains(out, "Drück den Knopf auf der Bridge") {
		t.Fatalf("adopt = %q", out)
	}
	e.tookItsThread(t, "flow:f1")
	// Not pressed: Home Assistant answers with the form and its error.
	e.ha.locked(func() {
		e.ha.onSubmit["f1"] = func() haws.FlowStep {
			e.ha.mu.Lock()
			defer e.ha.mu.Unlock()
			s := e.ha.steps["f1"]
			s.Errors = map[string]string{"base": "register_failed"}
			return s
		}
	})
	if out := e.run(t, ToolConfirm, `{}`); !strings.Contains(out, "Der Knopf wurde nicht gedrückt") {
		t.Fatalf("confirm before pressing = %q", out)
	}
	e.created("f1", "d1")
	if out := e.run(t, ToolConfirm, `{}`); !strings.Contains(out, "Raum") {
		t.Fatalf("confirm after pressing = %q", out)
	}
}

func TestFD09_AStepWithFieldsIsLeftToHomeAssistant(t *testing.T) {
	e := newFoundEnv(t, nil, nil)
	e.ha.discover("f1", "esphome", "Küchensensor", "encryption_key", "noise_psk")
	e.post(t)
	out := e.run(t, ToolAdopt, `{"item":"flow:f1"}`)
	if !strings.Contains(out, "nur in Home Assistant") {
		t.Fatalf("adopt = %q", out)
	}
	e.ha.locked(func() {
		if len(e.ha.submitted) != 0 {
			t.Errorf("submitted %v", e.ha.submitted)
		}
	})
	e.tookItsThread(t, "flow:f1")
	if th := <-e.ended; th != "house:flow:f1" {
		t.Errorf("ended %s", th)
	}
}

func TestFD09_AWifiStepIsFilledFromTheHomeWifi(t *testing.T) {
	e := newFoundEnv(t, nil, nil)
	e.ha.discover("f1", "improv_ble", "Improv device", "provision", "ssid", "password")
	e.post(t)
	if out := e.run(t, ToolAdopt, `{"item":"flow:f1"}`); !strings.Contains(out, "Miras Seite") {
		t.Fatalf("adopt without a home Wi-Fi = %q", out)
	}
	e.wifi = [2]string{"Heimnetz", "geheim-123"}
	out := e.run(t, ToolAdopt, `{"item":"flow:f1"}`)
	if !strings.Contains(out, "Heimnetz") || strings.Contains(out, "geheim-123") {
		t.Fatalf("adopt = %q", out)
	}
	e.created("f1", "d1")
	e.run(t, ToolConfirm, `{}`)
	e.ha.locked(func() {
		if got := e.ha.submitted["f1"]; got["ssid"] != "Heimnetz" || got["password"] != "geheim-123" {
			t.Errorf("submitted %v", got)
		}
	})
	if strings.Contains(e.logs.String(), "geheim-123") {
		t.Error("the password is in the log")
	}
}

func TestFD10_IgnoringADiscoveredDeviceUsesHomeAssistantsIgnore(t *testing.T) {
	e := newFoundEnv(t, nil, nil)
	e.ha.discover("f1", "hue", "Hue Bridge", "link")
	e.post(t)
	if out := e.run(t, ToolIgnore, `{"item":"flow:f1"}`); !strings.Contains(out, "ignoriert") {
		t.Fatalf("ignore = %q", out)
	}
	var subs []func([]haws.FlowEvent)
	e.ha.locked(func() {
		if e.ha.ignored["f1"] != "Hue Bridge" {
			t.Errorf("ignored = %v", e.ha.ignored)
		}
		subs = slices.Clone(e.ha.flowSubs)
	})
	// Home Assistant removes the flow (row 3).
	for _, h := range subs {
		h([]haws.FlowEvent{{Type: "removed", FlowID: "f1"}})
	}
	waitFor(t, func() bool { return !strings.Contains(e.run(t, ToolFound, `{}`), "Hue Bridge") })
}

func TestFD11_IgnoringANewDeviceLabelsIt(t *testing.T) {
	e := newFoundEnv(t, nil, nil)
	e.ha.addDevice("d1", "Nachbars Sensor", "e1", true)
	e.post(t)
	if out := e.run(t, ToolIgnore, `{"item":"device:d1"}`); !strings.Contains(out, "Nachbars Sensor") {
		t.Fatalf("ignore = %q", out)
	}
	e.ha.locked(func() {
		if e.ha.labels["mira_ignoriert"] != IgnoreLabel || !slices.Contains(e.ha.devices["d1"].Labels, "mira_ignoriert") {
			t.Errorf("labels = %v, device %v", e.ha.labels, e.ha.devices["d1"].Labels)
		}
	})
	if out := e.run(t, ToolFound, `{}`); strings.Contains(out, "Nachbars Sensor") {
		t.Fatalf("still listed: %q", out)
	}
}

func TestFD12_ARefusedStepIsSaidNotSwallowed(t *testing.T) {
	e := newFoundEnv(t, nil, nil)
	e.ha.discover("f1", "shelly", "Shelly Plus 1", "confirm")
	e.post(t)
	e.run(t, ToolAdopt, `{"item":"flow:f1"}`)
	e.ha.locked(func() { e.ha.refuse["submit"] = errors.New("home assistant refused POST: 400 Bad Request") })
	out := e.run(t, ToolConfirm, `{}`)
	if !strings.Contains(out, "400 Bad Request") {
		t.Fatalf("confirm = %q", out)
	}
	if !strings.Contains(e.logs.String(), "could not confirm the discovered device") {
		t.Errorf("log: %s", e.logs)
	}
}

func TestFoundTextsComplete(t *testing.T) {
	for lang, x := range foundCatalog {
		if x.entryLine == "" || x.ignoreFailed == "" || x.plan == "" {
			t.Errorf("%s: texts missing", lang)
		}
	}
}

func TestFD13_TheEntityIdsFollowTheNewName(t *testing.T) {
	e := newFoundEnv(t, nil, nil)
	e.ha.addDevice("tasmota", "Tasmota", "e1", true)
	e.post(t)
	e.run(t, ToolAdopt, `{"item":"device:tasmota"}`)
	e.run(t, ToolAssign, `{"room":"Küche"}`)
	if out := e.run(t, ToolFinish, `{"name":"Lampe Küche"}`); !strings.Contains(out, "Lampe Küche") {
		t.Fatalf("finish = %q", out)
	}
	e.ha.locked(func() {
		slices.Sort(e.ha.renamed)
		want := "sensor.tasmota_signal->sensor.lampe_kuche_signal,sensor.tasmota_temperature->sensor.lampe_kuche_temperature"
		if got := strings.Join(e.ha.renamed, ","); got != want {
			t.Errorf("renamed = %s, want %s", got, want)
		}
	})
}

// tookItsThread: the adoption took the item's own thread from the house
// (docs/spec/messages.md, decision 28), it started none of the room.
func (e *foundEnv) tookItsThread(t *testing.T, item string) {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.started != 0 || e.current != "house:"+item {
		t.Errorf("adopted with %q (threads of the room started: %d), want the house's thread of %s", e.current, e.started, item)
	}
}

// docs/spec/kv-cleanup.md, step 2: a said device or flow Home Assistant no
// longer has is forgotten when the noticing starts; the others stay said.
func TestKV05_AGoneDeviceIsForgottenTheOthersStaySaid(t *testing.T) {
	ha := newFakeHA()
	ha.addDevice("d1", "Hue Lampe", "e1", true)
	ha.discover("f1", "hue", "Hue Bridge", "link")
	kv := &memKV{m: map[string]string{keyStarted: "2026-11-10T00:00:00Z"}, said: map[string]string{
		"device:d1": "Hue Lampe", "device:gone": "Alte Lampe", "flow:hue:u-f1": "Hue Bridge", "flow:hue:u-old": "Alte Bridge",
	}}
	e := newFoundEnv(t, kv, ha)
	waitFor(t, func() bool { return e.kv.saidKeys() == "device:d1 flow:hue:u-f1" })
	e.noPost(t)
}
