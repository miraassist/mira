package devices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/miraassist/mira/internal/excerpt"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

// Devices Home Assistant found, noticed by Mira and taken in by voice
// (docs/spec/found-devices.md): a config flow Home Assistant discovered and
// that waits for a yes, and a new device without a room.

// Tool names.
const (
	ToolFound   = "devices_found"
	ToolAdopt   = "device_adopt"
	ToolConfirm = "device_confirm"
	ToolIgnore  = "device_ignore"
)

// IgnoreLabel is the label of a device Mira was told to ignore (decision 2).
const IgnoreLabel = "Mira: ignoriert"

// Keys in Mira's database: what was said (each found item once, decision 1)
// and whether the noticing ever ran (the backlog is not read out).
const (
	keySaid    = "found.said"
	keyStarted = "found.started"
)

// passingBy are integrations that make a device for whatever passes by:
// their entities count as diagnostic ones, so such a device is not counted
// (docs/spec/found-devices.md, decision 9).
var passingBy = map[string]bool{"ibeacon": true}

// notDiscovered are the sources of flows nobody discovered: a user started
// them, or Home Assistant itself for its own reasons.
var notDiscovered = map[string]bool{"user": true, "import": true, "reauth": true, "reconfigure": true,
	"system": true, "ignore": true, "unignore": true, "onboarding": true}

// FoundHA is what the found devices need of Home Assistant.
type FoundHA interface {
	HA
	Subscribe(ctx context.Context, eventType string, handler haws.EventHandler) (func(), error)
	SubscribeFlows(ctx context.Context, handler func([]haws.FlowEvent)) (func(), error)
	GetFlow(ctx context.Context, flowID string) (haws.FlowStep, error)
	SubmitFlow(ctx context.Context, flowID string, data map[string]any) (haws.FlowStep, error)
}

// KV is where the said marks are kept (Mira's database).
type KV interface {
	GetKV(ctx context.Context, key string) (string, error)
	SetKV(ctx context.Context, key, value string) error
}

type FoundOptions struct {
	Lang   i18n.Lang
	Logger *slog.Logger
	HA     FoundHA
	Core   *Core
	KV     KV
	// Post puts a found device's entry into the house's device message,
	// leading to its thread (docs/spec/messages.md, decision 28).
	Post func(ctx context.Context, thread, spoken, details string) error
	// StartHouse starts a found device's own thread at the house with its
	// plan; item is its reference (flow:<id>, device:<id>). TakeItem takes
	// that thread up for a turn (it moves into the turn's room); false
	// without one. ItemThread finds it; "" without one.
	StartHouse func(ctx context.Context, request, plan, item string) (string, error)
	TakeItem   func(ctx context.Context, inv tools.Invocation, item string) (string, bool, error)
	ItemThread func(ctx context.Context, item string) string
	// Claims: a dialog that brings a device into its network (a Zigbee
	// pairing) has it; it is not counted.
	Claims []func(deviceID string) bool
	// Wifi is the home Wi-Fi Mira keeps ("" when not set): a step that asks
	// only for it is filled (decision 8).
	Wifi func() (ssid, password string)
	// The thread of the skill: Start starts the turn's (or takes the open
	// one) with its plan, Thread finds it, Tick, Note, End as for Zigbee.
	Start  func(ctx context.Context, inv tools.Invocation, plan string) (string, error)
	Thread func(ctx context.Context, inv tools.Invocation) string
	Tick   func(ctx context.Context, thread, match string) error
	Note   func(ctx context.Context, thread, text string) error
	End    func(ctx context.Context, thread string)

	// Gather: how long after the last find a burst is said (60 s; no signal
	// says a burst is over). EntryWait: how long after a flow made its entry
	// Mira waits for its devices (10 s, a safety net; the signal is the
	// registry's create).
	Gather    time.Duration
	EntryWait time.Duration
}

// Found notices what Home Assistant found and has the tools to take it in.
type Found struct {
	o   FoundOptions
	t   foundTexts
	log *slog.Logger

	in   chan foundInput
	wake chan struct{}

	mu      sync.Mutex
	said    map[string]string // key -> name, said once
	started bool              // the noticing ran before
	flows   map[string]haws.Flow
	pending map[string]bool // keys waiting for the gathering: flow:<id>, device:<id>
	flowOf  map[string]string
	names   map[string]string // integration names by domain
	trans   map[string]map[string]string
	// changed is closed and made anew at each change, for Await; recent are
	// the devices created lately, in order.
	changed chan struct{}
	recent  []string
}

type foundInput struct {
	flows   []haws.FlowEvent
	created string
}

func NewFound(o FoundOptions) *Found {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Gather == 0 {
		o.Gather = time.Minute
	}
	if o.EntryWait == 0 {
		o.EntryWait = 10 * time.Second
	}
	if o.Wifi == nil {
		o.Wifi = func() (string, string) { return "", "" }
	}
	return &Found{o: o, t: i18n.Pick(foundCatalog, o.Lang), log: o.Logger.With("component", "found-devices"),
		in: make(chan foundInput, 256), wake: make(chan struct{}, 1),
		said: map[string]string{}, flows: map[string]haws.Flow{}, pending: map[string]bool{},
		flowOf: map[string]string{}, names: map[string]string{}, trans: map[string]map[string]string{},
		changed: make(chan struct{})}
}

func flowKey(f haws.Flow) string {
	if f.Context.UniqueID != "" {
		return "flow:" + f.Handler + ":" + f.Context.UniqueID
	}
	return "flow:" + f.Handler + ":" + string(f.Context.DiscoveryKey)
}

func deviceKey(id string) string { return "device:" + id }

// counted: a flow Home Assistant discovered, not Mira's own integration's.
func counted(f haws.Flow) bool {
	return !notDiscovered[f.Context.Source] && f.Handler != "mira"
}

// Run watches the open flows and the device registry until ctx ends.
func (f *Found) Run(ctx context.Context) error {
	if raw, err := f.o.KV.GetKV(ctx, keySaid); err == nil {
		if err := json.Unmarshal([]byte(raw), &f.said); err != nil {
			f.log.Warn("could not read which found devices were said; they may be said once more", "error", err)
		}
	}
	if v, err := f.o.KV.GetKV(ctx, keyStarted); err == nil && v != "" {
		f.started = true
	}
	stopFlows, err := f.o.HA.SubscribeFlows(ctx, func(list []haws.FlowEvent) {
		select { // on the connection's read loop: only pass it on
		case f.in <- foundInput{flows: list}:
		default:
			f.log.Warn("a change of the discovered devices dropped, too many at once")
		}
	})
	if err != nil {
		f.log.Error("could not watch discovered devices; new devices are still noticed", "error", err)
	}
	stopDevices, err := f.o.HA.Subscribe(ctx, "device_registry_updated", func(ev haws.Event) {
		var d struct {
			Action   string `json:"action"`
			DeviceID string `json:"device_id"`
		}
		if json.Unmarshal(ev.Data, &d) != nil || d.Action != "create" {
			return
		}
		select {
		case f.in <- foundInput{created: d.DeviceID}:
		default:
			f.log.Warn("a new device dropped, too many at once", "device", d.DeviceID)
		}
	})
	if err != nil {
		f.log.Error("could not watch new devices", "error", err)
	}
	defer func() {
		if stopFlows != nil {
			stopFlows()
		}
		if stopDevices != nil {
			stopDevices()
		}
	}()
	var gather <-chan time.Time
	var timer *time.Timer
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-gather:
			gather = nil
			f.announce(ctx)
		case in := <-f.in:
			if f.take(ctx, in) {
				// The burst waits Gather after its last find.
				if timer != nil {
					timer.Stop()
				}
				timer = time.NewTimer(f.o.Gather)
				gather = timer.C
			}
		}
	}
}

// take keeps a change; true when something new waits to be said.
func (f *Found) take(ctx context.Context, in foundInput) bool {
	defer func() {
		f.mu.Lock()
		close(f.changed)
		f.changed = make(chan struct{})
		if in.created != "" {
			f.recent = append(f.recent, in.created)
			if len(f.recent) > 256 {
				f.recent = f.recent[len(f.recent)-256:]
			}
		}
		f.mu.Unlock()
	}()
	if in.created != "" {
		select {
		case f.wake <- struct{}{}:
		default:
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if _, said := f.said[deviceKey(in.created)]; said {
			return false
		}
		f.pending[deviceKey(in.created)] = true
		return true
	}
	newOnes := false
	f.mu.Lock()
	initial := false
	for _, ev := range in.flows {
		switch ev.Type {
		case "removed":
			delete(f.flows, ev.FlowID)
			delete(f.pending, "flow:"+ev.FlowID)
		default:
			if ev.Flow == nil {
				continue
			}
			f.flows[ev.FlowID] = *ev.Flow
			if !counted(*ev.Flow) {
				continue
			}
			if _, said := f.said[flowKey(*ev.Flow)]; said {
				continue
			}
			if ev.Type == "" && !f.started {
				// Home Assistant's backlog when the noticing runs for the
				// first time: marked said, not read out (FD-05).
				name := ev.Flow.Name()
				if name == "" {
					name = ev.Flow.Handler
				}
				f.said[flowKey(*ev.Flow)] = name
				initial = true
				continue
			}
			f.pending["flow:"+ev.FlowID] = true
			newOnes = true
		}
	}
	first := !f.started
	f.started = true
	f.mu.Unlock()
	if first {
		if err := f.o.KV.SetKV(ctx, keyStarted, time.Now().UTC().Format(time.RFC3339)); err != nil {
			f.log.Warn("could not remember that the noticing ran; the backlog may be said", "error", err)
		}
	}
	if initial {
		f.saveSaid(ctx)
	}
	return newOnes
}

// registry is one device of Home Assistant's device registry.
type registry struct {
	ID            string   `json:"id"`
	AreaID        string   `json:"area_id"`
	Name          string   `json:"name"`
	NameByUser    string   `json:"name_by_user"`
	Manufacturer  string   `json:"manufacturer"`
	Model         string   `json:"model"`
	DisabledBy    string   `json:"disabled_by"`
	EntryType     string   `json:"entry_type"`
	Labels        []string `json:"labels"`
	ConfigEntries []string `json:"config_entries"`
}

func (d registry) name() string {
	if d.NameByUser != "" {
		return d.NameByUser
	}
	if d.Name != "" {
		return d.Name
	}
	return strings.TrimSpace(d.Manufacturer + " " + d.Model)
}

// house is what is read of Home Assistant to count new devices: the device
// registry, which devices have an entity that would be exposed (not one of an
// integration in passingBy), and the id
// of the label "Mira: ignoriert" ("" before it exists). Three calls.
type house struct {
	devices   map[string]registry
	measuring map[string]bool
	ignore    string
}

func (f *Found) readHouse(ctx context.Context) (house, error) {
	h := house{devices: map[string]registry{}, measuring: map[string]bool{}}
	raw, err := f.o.HA.Call(ctx, map[string]any{"type": "config/device_registry/list"})
	if err != nil {
		return h, err
	}
	var list []registry
	if err := json.Unmarshal(raw, &list); err != nil {
		return h, fmt.Errorf("%q not understood: %w", excerpt.Bytes(raw), err)
	}
	for _, d := range list {
		h.devices[d.ID] = d
	}
	raw, err = f.o.HA.Call(ctx, map[string]any{"type": "config/entity_registry/list_for_display"})
	if err != nil {
		return h, err
	}
	var ents struct {
		Entities []struct {
			DeviceID string `json:"di"`
			Platform string `json:"pl"`
			Category *int   `json:"ec"`
			Hidden   bool   `json:"hb"`
		} `json:"entities"`
	}
	if err := json.Unmarshal(raw, &ents); err != nil {
		return h, fmt.Errorf("%q not understood: %w", excerpt.Bytes(raw), err)
	}
	for _, e := range ents.Entities {
		if e.DeviceID != "" && e.Category == nil && !e.Hidden && !passingBy[e.Platform] {
			h.measuring[e.DeviceID] = true
		}
	}
	h.ignore, err = f.ignoreLabel(ctx, false)
	return h, err
}

// ignoreLabel is the id of the label "Mira: ignoriert"; with create it is
// created when it does not exist.
func (f *Found) ignoreLabel(ctx context.Context, create bool) (string, error) {
	raw, err := f.o.HA.Call(ctx, map[string]any{"type": "config/label_registry/list"})
	if err != nil {
		return "", err
	}
	var labels []struct {
		ID   string `json:"label_id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &labels); err != nil {
		return "", fmt.Errorf("%q not understood: %w", excerpt.Bytes(raw), err)
	}
	for _, l := range labels {
		if l.Name == IgnoreLabel {
			return l.ID, nil
		}
	}
	if !create {
		return "", nil
	}
	raw, err = f.o.HA.Call(ctx, map[string]any{"type": "config/label_registry/create", "name": IgnoreLabel, "icon": "mdi:eye-off"})
	if err != nil {
		return "", err
	}
	var l struct {
		ID string `json:"label_id"`
	}
	if err := json.Unmarshal(raw, &l); err != nil {
		return "", fmt.Errorf("%q not understood: %w", excerpt.Bytes(raw), err)
	}
	return l.ID, nil
}

// countsDevice: a new device without a room that Mira says and lists
// (docs/spec/found-devices.md, what is not counted).
func (f *Found) countsDevice(h house, id string) bool {
	d, ok := h.devices[id]
	if !ok || d.DisabledBy != "" || d.EntryType == "service" || d.AreaID != "" || !h.measuring[id] {
		return false
	}
	if h.ignore != "" && slices.Contains(d.Labels, h.ignore) {
		return false
	}
	if f.o.Core != nil && f.o.Core.Holds(id) {
		return false
	}
	for _, claims := range f.o.Claims {
		if claims(id) {
			return false
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	// Said already: a device a confirmed flow brought, taken by its thread or
	// put into the list.
	_, said := f.said[deviceKey(id)]
	return !said
}

// item is something found, as the message and the list name it.
type item struct {
	ref, key, name, facts string
}

// announce says the gathered finds as one message to the house.
func (f *Found) announce(ctx context.Context) {
	f.mu.Lock()
	keys := make([]string, 0, len(f.pending))
	for k := range f.pending {
		keys = append(keys, k)
	}
	f.pending = map[string]bool{}
	f.mu.Unlock()
	sort.Strings(keys)
	var items []item
	var h house
	read := false
	for _, k := range keys {
		kind, id, _ := strings.Cut(k, ":")
		if kind == "flow" {
			f.mu.Lock()
			fl, open := f.flows[id]
			_, said := f.said[flowKey(fl)]
			f.mu.Unlock()
			if !open || said {
				continue
			}
			items = append(items, item{ref: "flow:" + id, key: flowKey(fl), name: f.flowName(ctx, fl), facts: fl.Handler})
			continue
		}
		if !read {
			var err error
			if h, err = f.readHouse(ctx); err != nil {
				f.log.Error("could not read the new devices; they are not said", "error", err)
				return
			}
			read = true
		}
		if !f.countsDevice(h, id) {
			continue
		}
		d := h.devices[id]
		items = append(items, item{ref: k, key: k, name: d.name(), facts: strings.TrimSpace(d.Manufacturer + " " + d.Model)})
	}
	if len(items) == 0 {
		return
	}
	// Each its own thread at the house and its entry (decision 28): an entry
	// without its thread would lead nowhere.
	said := 0
	for _, it := range items {
		thread, err := f.o.StartHouse(ctx, it.name, f.t.plan, it.ref)
		if err != nil {
			f.log.Error("could not start a found device's thread; it is not said", "item", it.ref, "name", it.name, "error", err)
			continue
		}
		spoken, details := f.entry(it)
		if err := f.o.Post(ctx, thread, spoken, details); err != nil {
			f.log.Error("could not leave the entry of a found device; its thread waits without it", "item", it.ref,
				"thread", thread, "error", err)
		}
		f.mu.Lock()
		f.said[it.key] = it.name
		f.mu.Unlock()
		said++
	}
	f.saveSaid(ctx)
	f.log.Info("found devices said", "count", said)
}

// entry is a found device's entry: its line and the details for the tools.
func (f *Found) entry(it item) (string, string) {
	t := f.t
	details := fmt.Sprintf("%s\n- %s: %s", t.details, it.ref, it.name)
	if it.facts != "" {
		details += " (" + it.facts + ")"
	}
	return fmt.Sprintf(t.entryLine, it.name), details
}

func (f *Found) saveSaid(ctx context.Context) {
	f.mu.Lock()
	raw, _ := json.Marshal(f.said)
	f.mu.Unlock()
	if err := f.o.KV.SetKV(ctx, keySaid, string(raw)); err != nil {
		f.log.Warn("could not remember a found device as said; it may be said once more after a restart", "error", err)
	}
}

func (f *Found) markSaid(ctx context.Context, key, name string) {
	f.mu.Lock()
	f.said[key] = name
	f.mu.Unlock()
	f.saveSaid(ctx)
}

// flowName names a discovered flow: its own name, else its integration's.
func (f *Found) flowName(ctx context.Context, fl haws.Flow) string {
	if n := fl.Name(); n != "" {
		return n
	}
	return f.integrationName(ctx, fl.Handler)
}

func (f *Found) integrationName(ctx context.Context, domain string) string {
	f.mu.Lock()
	n, ok := f.names[domain]
	f.mu.Unlock()
	if ok {
		return n
	}
	name := domain
	raw, err := f.o.HA.Call(ctx, map[string]any{"type": "manifest/get", "integration": domain})
	if err == nil {
		var m struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &m) == nil && m.Name != "" {
			name = m.Name
		}
	} else {
		f.log.Warn("could not read an integration's name; it is named by its domain", "domain", domain, "error", err)
	}
	f.mu.Lock()
	f.names[domain] = name
	f.mu.Unlock()
	return name
}

// Tools are devices_found, device_adopt, device_confirm, device_ignore.
func (f *Found) Tools() []tools.InternalTool {
	t := f.t
	itemArg := fmt.Sprintf(`{"type":"object","properties":{"item":{"type":"string","description":%q}},"required":["item"]}`, t.argItem)
	return []tools.InternalTool{
		{Name: ToolFound, Description: t.foundDesc, Skill: Skill, ReadOnly: true,
			Schema: `{"type":"object","properties":{},"required":[]}`, Run: f.list},
		{Name: ToolAdopt, Description: t.adoptDesc, Skill: Skill, Schema: itemArg, Run: f.adopt},
		{Name: ToolConfirm, Description: t.confirmDesc, Skill: Skill,
			Schema: `{"type":"object","properties":{},"required":[]}`, Run: f.confirm},
		{Name: ToolIgnore, Description: t.ignoreDesc, Skill: Skill, Schema: itemArg, Run: f.ignore},
	}
}

// open lists what is open: counted flows and the said devices still
// without a room.
func (f *Found) open(ctx context.Context) ([]item, error) {
	var items []item
	f.mu.Lock()
	flows := make([]haws.Flow, 0, len(f.flows))
	for _, fl := range f.flows {
		if counted(fl) {
			flows = append(flows, fl)
		}
	}
	var deviceIDs []string
	for k := range f.said {
		if id, ok := strings.CutPrefix(k, "device:"); ok {
			deviceIDs = append(deviceIDs, id)
		}
	}
	f.mu.Unlock()
	slices.SortFunc(flows, func(a, b haws.Flow) int { return strings.Compare(a.FlowID, b.FlowID) })
	for _, fl := range flows {
		items = append(items, item{ref: "flow:" + fl.FlowID, name: f.flowName(ctx, fl), facts: f.t.waitsForYes})
	}
	if len(deviceIDs) > 0 {
		h, err := f.readHouse(ctx)
		if err != nil {
			return nil, err
		}
		sort.Strings(deviceIDs)
		for _, id := range deviceIDs {
			d, ok := h.devices[id]
			// What is listed: still without a room, not ignored, not
			// disabled; one a dialog has now is not.
			if !ok || d.AreaID != "" || d.DisabledBy != "" || (h.ignore != "" && slices.Contains(d.Labels, h.ignore)) ||
				(f.o.Core != nil && f.o.Core.Holds(id)) {
				continue
			}
			items = append(items, item{ref: "device:" + id, name: d.name(),
				facts: strings.TrimSpace(d.Manufacturer + " " + d.Model + ", " + f.t.noRoom)})
		}
	}
	return items, nil
}

func (f *Found) list(ctx context.Context, _ tools.Invocation) (string, error) {
	items, err := f.open(ctx)
	if err != nil {
		f.log.Error("could not list the found devices", "error", err)
		return fmt.Sprintf(f.t.listFailed, err), nil
	}
	if len(items) == 0 {
		return f.t.nothingOpen, nil
	}
	var b strings.Builder
	b.WriteString(f.t.openList)
	for _, it := range items {
		fmt.Fprintf(&b, "\n- %s: %s (%s)", it.ref, it.name, it.facts)
	}
	return b.String(), nil
}

func parseItem(inv tools.Invocation) (string, string, error) {
	var args struct{ Item string }
	if err := json.Unmarshal(inv.Args, &args); err != nil || strings.TrimSpace(args.Item) == "" {
		return "", "", errors.New("item is required")
	}
	kind, id, ok := strings.Cut(strings.TrimSpace(args.Item), ":")
	if !ok || (kind != "flow" && kind != "device") || id == "" {
		return "", "", fmt.Errorf("item %q is not flow:<id> or device:<id>", args.Item)
	}
	return kind, id, nil
}

// plan steps of a found device's thread (matched to tick them).
var adoptStep = map[i18n.Lang]string{i18n.DE: "Einbinden", i18n.EN: "Add"}

func (f *Found) adoptStep() string {
	if s, ok := adoptStep[f.o.Lang]; ok {
		return s
	}
	return adoptStep[i18n.DE]
}

func (f *Found) tick(ctx context.Context, thread string) {
	if err := f.o.Tick(ctx, thread, f.adoptStep()); err != nil {
		f.log.Warn("could not tick the plan of the found device's thread", "thread", thread, "error", err)
	}
}

func (f *Found) note(ctx context.Context, thread, text string) {
	if err := f.o.Note(ctx, thread, text); err != nil {
		f.log.Warn("could not note the found device in its thread", "thread", thread, "error", err)
	}
}

// threadFor is the thread a found item is taken in with: its own, started
// at the house when it was found, taken up and moved into the turn's room
// (decisions 27, 28); one found before that has none, and gets a thread of
// the room as before.
func (f *Found) threadFor(ctx context.Context, inv tools.Invocation, item string) (string, error) {
	if f.o.TakeItem != nil {
		thread, ok, err := f.o.TakeItem(ctx, inv, item)
		if err != nil {
			return "", err
		}
		if ok {
			return thread, nil
		}
	}
	return f.o.Start(ctx, inv, f.t.plan)
}

// endItem ends a found item's own thread, unless it is the one just ended.
func (f *Found) endItem(ctx context.Context, item, ended string) {
	if f.o.ItemThread == nil {
		return
	}
	if thread := f.o.ItemThread(ctx, item); thread != "" && thread != ended {
		f.mu.Lock()
		delete(f.flowOf, thread)
		f.mu.Unlock()
		f.o.End(ctx, thread)
	}
}

func (f *Found) adopt(ctx context.Context, inv tools.Invocation) (string, error) {
	kind, id, err := parseItem(inv)
	if err != nil {
		return "", err
	}
	t := f.t
	if kind == "device" {
		h, err := f.readHouse(ctx)
		if err != nil {
			f.log.Error("could not read the found device", "device", id, "error", err)
			return fmt.Sprintf(t.listFailed, err), nil
		}
		d, ok := h.devices[id]
		// Taken in or ignored meanwhile (an older message names it still):
		// not again.
		if !ok || d.AreaID != "" || (h.ignore != "" && slices.Contains(d.Labels, h.ignore)) {
			return t.notOpen, nil
		}
		thread, err := f.threadFor(ctx, inv, deviceKey(id))
		if err != nil {
			return "", err
		}
		f.o.Core.Hold(thread, Device{ID: id, RenameEntities: true})
		facts := fmt.Sprintf(t.facts, d.name(), d.Manufacturer, d.Model)
		f.note(ctx, thread, facts)
		f.tick(ctx, thread)
		f.markSaid(ctx, deviceKey(id), d.name())
		return fmt.Sprintf(t.askRoom, facts), nil
	}
	f.mu.Lock()
	fl, open := f.flows[id]
	f.mu.Unlock()
	if !open {
		return t.notOpen, nil
	}
	thread, err := f.threadFor(ctx, inv, "flow:"+id)
	if err != nil {
		return "", err
	}
	f.mu.Lock()
	f.flowOf[thread] = id
	f.mu.Unlock()
	f.markSaid(ctx, flowKey(fl), f.flowName(ctx, fl))
	f.note(ctx, thread, fmt.Sprintf(t.flowFacts, f.flowName(ctx, fl), f.integrationName(ctx, fl.Handler)))
	step, err := f.o.HA.GetFlow(ctx, id)
	if err != nil {
		f.log.Error("could not confirm the discovered device: its step cannot be read", "flow", id, "error", err)
		return fmt.Sprintf(t.confirmFailed, err), nil
	}
	return f.describe(ctx, thread, step), nil
}

// wifiFields: a step whose only fields are a Wi-Fi's name and password
// (decision 8); the names of each, or false.
func wifiFields(step haws.FlowStep) (ssidField, passField string, ok bool) {
	for _, fld := range step.DataSchema {
		switch strings.ToLower(fld.Name) {
		case "ssid", "wifi_ssid", "network":
			ssidField = fld.Name
		case "password", "psk", "wifi_password", "passphrase":
			passField = fld.Name
		default:
			return "", "", false
		}
	}
	return ssidField, passField, ssidField != "" && passField != ""
}

// describe says what a flow's step needs, for the model; a step Mira cannot
// do ends the thread.
func (f *Found) describe(ctx context.Context, thread string, step haws.FlowStep) string {
	t := f.t
	switch step.Type {
	case "abort":
		f.finishFlow(ctx, thread)
		return t.notOpen
	case "form":
	default:
		f.finishFlow(ctx, thread)
		return t.onlyInHA
	}
	text := f.stepText(ctx, step)
	if len(step.DataSchema) == 0 {
		return fmt.Sprintf(t.stepNoFields, text)
	}
	if _, _, ok := wifiFields(step); ok {
		ssid, _ := f.o.Wifi()
		if ssid == "" {
			f.finishFlow(ctx, thread)
			return t.wifiMissing
		}
		return fmt.Sprintf(t.stepWifi, ssid)
	}
	f.finishFlow(ctx, thread)
	return t.onlyInHA
}

// stepText is the step's title, description and error in the household's
// language, with the step's placeholders filled.
func (f *Found) stepText(ctx context.Context, step haws.FlowStep) string {
	res := f.translations(ctx, step.Handler)
	fill := func(s string) string {
		for k, v := range step.DescriptionPlaceholders {
			s = strings.ReplaceAll(s, "{"+k+"}", fmt.Sprint(v))
		}
		return strings.TrimSpace(s)
	}
	prefix := "component." + step.Handler + ".config."
	var parts []string
	for _, k := range []string{"step." + step.StepID + ".title", "step." + step.StepID + ".description"} {
		if s := fill(res[prefix+k]); s != "" {
			parts = append(parts, s)
		}
	}
	for _, key := range step.Errors {
		if s := fill(res[prefix+"error."+key]); s != "" {
			parts = append(parts, f.t.stepError+s)
		} else {
			parts = append(parts, f.t.stepError+key)
		}
	}
	if len(parts) == 0 {
		return step.StepID
	}
	return strings.Join(parts, " ")
}

func (f *Found) translations(ctx context.Context, domain string) map[string]string {
	f.mu.Lock()
	res, ok := f.trans[domain]
	f.mu.Unlock()
	if ok {
		return res
	}
	raw, err := f.o.HA.Call(ctx, map[string]any{"type": "frontend/get_translations", "language": string(f.o.Lang),
		"category": "config", "integration": []string{domain}})
	res = map[string]string{}
	if err == nil {
		var out struct {
			Resources map[string]string `json:"resources"`
		}
		if json.Unmarshal(raw, &out) == nil {
			res = out.Resources
		}
	} else {
		f.log.Warn("could not read the texts of a discovered device's step; its step is named by its id", "domain", domain, "error", err)
	}
	f.mu.Lock()
	f.trans[domain] = res
	f.mu.Unlock()
	return res
}

// finishFlow ends a flow's thread: what is left is Home Assistant's page.
func (f *Found) finishFlow(ctx context.Context, thread string) {
	f.mu.Lock()
	delete(f.flowOf, thread)
	f.mu.Unlock()
	f.o.End(ctx, thread)
}

func (f *Found) confirm(ctx context.Context, inv tools.Invocation) (string, error) {
	t := f.t
	thread := f.o.Thread(ctx, inv)
	f.mu.Lock()
	id := f.flowOf[thread]
	f.mu.Unlock()
	if id == "" {
		return t.noFlow, nil
	}
	step, err := f.o.HA.GetFlow(ctx, id)
	if err != nil {
		f.log.Error("could not confirm the discovered device: its step cannot be read", "flow", id, "error", err)
		return fmt.Sprintf(t.confirmFailed, err), nil
	}
	data := map[string]any{}
	switch {
	case step.Type != "form":
		return f.describe(ctx, thread, step), nil
	case len(step.DataSchema) == 0:
	default:
		ssidField, passField, ok := wifiFields(step)
		ssid, pass := f.o.Wifi()
		if !ok || ssid == "" {
			return f.describe(ctx, thread, step), nil
		}
		data[ssidField], data[passField] = ssid, pass
	}
	next, err := f.o.HA.SubmitFlow(ctx, id, data)
	if err != nil {
		f.log.Error("could not confirm the discovered device", "flow", id, "error", err)
		return fmt.Sprintf(t.confirmFailed, err), nil
	}
	if next.Type != "create_entry" {
		return f.describe(ctx, thread, next), nil
	}
	f.mu.Lock()
	delete(f.flowOf, thread)
	f.mu.Unlock()
	list, err := f.entryDevices(ctx, next.Result.EntryID)
	if err != nil {
		f.log.Error("the discovered device is set up, but its devices cannot be read", "entry", next.Result.EntryID, "error", err)
		f.o.End(ctx, thread)
		return t.setUpNoDevice, nil
	}
	switch len(list) {
	case 0:
		f.log.Info("discovered device set up, no device yet", "entry", next.Result.EntryID)
		f.o.End(ctx, thread)
		return t.setUpNoDevice, nil
	case 1:
		d := list[0]
		f.o.Core.Hold(thread, Device{ID: d.ID, RenameEntities: true})
		facts := fmt.Sprintf(t.facts, d.name(), d.Manufacturer, d.Model)
		f.note(ctx, thread, facts)
		f.tick(ctx, thread)
		f.markSaid(ctx, deviceKey(d.ID), d.name())
		return fmt.Sprintf(t.askRoom, facts), nil
	}
	// Several: in the list, one by one (they were not said, now they are
	// open for "Gibt's neue Geräte?").
	for _, d := range list {
		f.markSaid(ctx, deviceKey(d.ID), d.name())
	}
	f.o.End(ctx, thread)
	return fmt.Sprintf(t.setUpSeveral, len(list)), nil
}

// entryDevices are the devices of a config entry. Home Assistant creates them
// while the entry is set up, mostly before the answer to the submit (seen on
// the test HA 2026-11-10); else Mira waits for the registry's create, up to
// EntryWait.
func (f *Found) entryDevices(ctx context.Context, entryID string) ([]registry, error) {
	deadline := time.NewTimer(f.o.EntryWait)
	defer deadline.Stop()
	for {
		raw, err := f.o.HA.Call(ctx, map[string]any{"type": "config/device_registry/list"})
		if err != nil {
			return nil, err
		}
		var all []registry
		if err := json.Unmarshal(raw, &all); err != nil {
			return nil, fmt.Errorf("%q not understood: %w", excerpt.Bytes(raw), err)
		}
		var list []registry
		for _, d := range all {
			if slices.Contains(d.ConfigEntries, entryID) && d.DisabledBy == "" && d.EntryType != "service" {
				list = append(list, d)
			}
		}
		if len(list) > 0 {
			return list, nil
		}
		select {
		case <-f.wake:
		case <-deadline.C:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (f *Found) ignore(ctx context.Context, inv tools.Invocation) (string, error) {
	kind, id, err := parseItem(inv)
	if err != nil {
		return "", err
	}
	t := f.t
	thread := f.o.Thread(ctx, inv)
	if kind == "flow" {
		f.mu.Lock()
		fl, open := f.flows[id]
		f.mu.Unlock()
		if !open {
			return t.notOpen, nil
		}
		name := f.flowName(ctx, fl)
		if _, err := f.o.HA.Call(ctx, map[string]any{"type": "config_entries/ignore_flow", "flow_id": id, "title": name}); err != nil {
			f.log.Error("could not ignore the found device", "flow", id, "error", err)
			return fmt.Sprintf(t.ignoreFailed, err), nil
		}
		f.markSaid(ctx, flowKey(fl), name)
		f.mu.Lock()
		mine := f.flowOf[thread] == id
		f.mu.Unlock()
		ended := ""
		if mine {
			f.finishFlow(ctx, thread)
			ended = thread
		}
		f.endItem(ctx, "flow:"+id, ended)
		f.log.Info("found device ignored", "flow", id, "name", name)
		return fmt.Sprintf(t.ignored, name), nil
	}
	label, err := f.ignoreLabel(ctx, true)
	if err == nil {
		var h house
		if h, err = f.readHouse(ctx); err == nil {
			d, ok := h.devices[id]
			if !ok {
				return t.notOpen, nil
			}
			labels := d.Labels
			if !slices.Contains(labels, label) {
				labels = append(slices.Clone(labels), label)
			}
			_, err = f.o.HA.Call(ctx, map[string]any{"type": "config/device_registry/update", "device_id": id, "labels": labels})
			if err == nil {
				f.markSaid(ctx, deviceKey(id), d.name())
				ended := ""
				if f.o.Core.Holds(id) {
					f.o.Core.Release(thread)
					f.o.End(ctx, thread)
					ended = thread
				}
				f.endItem(ctx, deviceKey(id), ended)
				f.log.Info("found device ignored", "device", id, "name", d.name())
				return fmt.Sprintf(t.ignored, d.name()), nil
			}
		}
	}
	f.log.Error("could not ignore the found device", "device", id, "error", err)
	return fmt.Sprintf(t.ignoreFailed, err), nil
}

// ThreadEnded forgets a thread's flow.
func (f *Found) ThreadEnded(thread string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.flowOf, thread)
}

// Await waits up to limit for something Home Assistant finds that a dialog
// brought into the network (docs/spec/wifi-onboarding.md): an open flow
// flowOK accepts, or a device created from now on that deviceOK accepts. It
// returns its reference (flow:<id>, device:<id>) and name; "" when nothing
// came. The signal is the found devices' subscriptions; the limit a safety
// net.
func (f *Found) Await(ctx context.Context, limit time.Duration, flowOK func(haws.Flow) bool,
	deviceOK func(manufacturer, model, name string) bool) (string, string, error) {
	f.mu.Lock()
	start := len(f.recent)
	f.mu.Unlock()
	safety := time.NewTimer(limit)
	defer safety.Stop()
	for {
		f.mu.Lock()
		changed := f.changed
		var flows []haws.Flow
		for _, fl := range f.flows {
			flows = append(flows, fl)
		}
		if start > len(f.recent) {
			start = 0
		}
		created := slices.Clone(f.recent[start:])
		f.mu.Unlock()
		slices.SortFunc(flows, func(a, b haws.Flow) int { return strings.Compare(a.FlowID, b.FlowID) })
		for _, fl := range flows {
			if flowOK != nil && counted(fl) && flowOK(fl) {
				return "flow:" + fl.FlowID, f.flowName(ctx, fl), nil
			}
		}
		if len(created) > 0 && deviceOK != nil {
			h, err := f.readHouse(ctx)
			if err != nil {
				return "", "", err
			}
			for _, id := range created {
				if d, ok := h.devices[id]; ok && deviceOK(d.Manufacturer, d.Model, d.name()) {
					return "device:" + id, d.name(), nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-safety.C:
			return "", "", nil
		case <-changed:
		}
	}
}

// Assign makes what Await found the thread's: a flow waits for
// device_confirm, a device for its room. It is not said as a find.
func (f *Found) Assign(ctx context.Context, thread, ref, name string) {
	kind, id, _ := strings.Cut(ref, ":")
	if kind == "flow" {
		f.mu.Lock()
		fl, open := f.flows[id]
		f.flowOf[thread] = id
		delete(f.pending, ref)
		f.mu.Unlock()
		if open {
			f.markSaid(ctx, flowKey(fl), name)
		}
		return
	}
	f.o.Core.Hold(thread, Device{ID: id, RenameEntities: true})
	f.mu.Lock()
	delete(f.pending, ref)
	f.mu.Unlock()
	f.markSaid(ctx, deviceKey(id), name)
	f.tick(ctx, thread)
}
