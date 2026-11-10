// Package zigbee adds a Zigbee device by voice through Zigbee2MQTT
// (docs/spec/device-onboarding.md): the skill geraet-einbinden's tools, the
// wait for the device between two turns, and the thread's announcements.
// Everything goes through Home Assistant: mqtt.publish and an MQTT
// subscription to talk to Zigbee2MQTT, the registries for the device.
package zigbee

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

	"github.com/oklog/ulid/v2"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/devices"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

// Skill is the skill whose tools these are.
const Skill = devices.Skill

// ToolStart opens pairing; the room and the name are the devices core's
// (device_assign, device_finish).
const ToolStart = "zigbee_pairing_start"

// Plan steps, matched to tick them (the plan's text in texts.plan).
var steps = map[i18n.Lang][3]string{
	i18n.DE: {"Gerät anmelden", "Raum", "Name"},
	i18n.EN: {"Device joins", "Room", "Name"},
}

// HA is what the package needs of Home Assistant's websocket.
type HA interface {
	Call(ctx context.Context, command map[string]any) (json.RawMessage, error)
	CallService(ctx context.Context, domain, service string, data map[string]any, target *haws.Target) error
	Subscribe(ctx context.Context, eventType string, handler haws.EventHandler) (func(), error)
	SubscribeMQTT(ctx context.Context, topic string, handler func(haws.MQTTMessage)) (func(), error)
	SubscribeRaw(ctx context.Context, cmd map[string]any, handler func(json.RawMessage)) (func(), error)
}

type Options struct {
	Lang   i18n.Lang
	Logger *slog.Logger
	HA     HA
	// Core takes the device in once it is there: room, name, exposed.
	Core *devices.Core
	// Devices are Home Assistant's devices by id, DeviceArea their areas,
	// AreaKeys every lower-cased area id, name and alias to the area id, and
	// AreaName an area's name (the structure Mira keeps).
	Devices    func() map[string]haws.Device
	DeviceArea func() map[string]string
	AreaKeys   func() map[string]string
	AreaName   func(id string) string
	// Start starts the turn's thread of the skill with its plan (or takes
	// the open one) and returns its id; Thread finds the turn's.
	Start  func(ctx context.Context, inv tools.Invocation, plan string) (string, error)
	Thread func(ctx context.Context, inv tools.Invocation) string
	// Tick ticks a step of the plan; Note writes a fact into the thread's
	// log; Ask says a text at the thread's satellite as its announcement, so
	// the answer joins the thread; End ends the thread as done.
	Tick func(ctx context.Context, thread, match string) error
	Note func(ctx context.Context, thread, text string) error
	Ask  func(ctx context.Context, thread, text string) error
	End  func(ctx context.Context, thread string)
	// Question writes the question for a device found, from its facts, in
	// the household's language (one model round, the skill's agent).
	Question func(ctx context.Context, facts string) (string, error)

	// Seconds pairing stays open (180); Wait for an answer of Zigbee2MQTT
	// (10 s); Interview, how long a device that joined in the last moment
	// may take for its interview after pairing ended (2 min). Wait and
	// Interview are safety nets: the signals are Zigbee2MQTT's answers and
	// events.
	Seconds   int
	Wait      time.Duration
	Interview time.Duration
}

// Pairing is the adding of devices, one run per thread.
type Pairing struct {
	o   Options
	t   texts
	log *slog.Logger

	mu    sync.Mutex
	runs  map[string]*run    // by thread id
	zruns map[string]*zhaRun // pairings on ZHA, by thread id
}

func New(o Options) *Pairing {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Seconds == 0 {
		o.Seconds = 180
	}
	if o.Wait == 0 {
		o.Wait = 10 * time.Second
	}
	if o.Interview == 0 {
		o.Interview = 2 * time.Minute
	}
	return &Pairing{o: o, t: i18n.Pick(catalog, o.Lang), log: o.Logger.With("component", "zigbee"), runs: map[string]*run{},
		zruns: map[string]*zhaRun{}}
}

// Tools are the skill's tools.
func (p *Pairing) Tools() []tools.InternalTool {
	t := p.t
	return []tools.InternalTool{
		{Name: ToolStart, Description: t.startDesc, Skill: Skill,
			Schema: fmt.Sprintf(`{"type":"object","properties":{"network":{"type":"string","enum":["zigbee2mqtt","zha"],"description":%q}},"required":[]}`, t.argNetwork),
			Run:    p.start},
	}
}

// definition is what Zigbee2MQTT says of a device after its interview.
type definition struct {
	Model       string `json:"model"`
	Vendor      string `json:"vendor"`
	Description string `json:"description"`
	Exposes     []struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		Category string `json:"category"`
		Features []struct {
			Name string `json:"name"`
		} `json:"features"`
	} `json:"exposes"`
}

// values names what a device measures or switches, for its facts.
func (d definition) values() string {
	var out []string
	for _, e := range d.Exposes {
		name := e.Name
		if name == "" {
			name = e.Type
		}
		if e.Category != "" {
			name += " (" + e.Category + ")"
		}
		out = append(out, name)
	}
	return strings.Join(out, ", ")
}

// run is one thread's pairing: its subscriptions while pairing is open,
// and the device once it is there.
type run struct {
	thread, base string
	stop         context.CancelFunc
	// in are the bridge's messages and the entities Home Assistant creates,
	// in the order they came over the one connection.
	in chan input
	// stopAnswers ends the subscription to Zigbee2MQTT's answers, stopWatch
	// those to its events, its info and the entity registry.
	stopAnswers func()
	stopWatch   []func()
	// waiting are the requests waiting for their answer, by transaction.
	waiting map[string]chan response

	mu   sync.Mutex
	open bool
	// seen: Zigbee2MQTT's info said pairing is open; an info saying it is
	// closed counts only after that (the retained one comes first).
	seen   bool
	joined map[string]bool
	// told: Mira said that a device is being learned (once per run).
	told    bool
	defs    map[string]definition
	devices map[string]string // ieee -> device id, from the registry
}

type input struct {
	msg     haws.MQTTMessage
	created string // an entity id Home Assistant created
}

type bridgeEvent struct {
	Type string `json:"type"`
	Data struct {
		FriendlyName string          `json:"friendly_name"`
		IEEE         string          `json:"ieee_address"`
		Status       string          `json:"status"`
		Supported    bool            `json:"supported"`
		Definition   json.RawMessage `json:"definition"`
	} `json:"data"`
}

type response struct {
	Status      string          `json:"status"`
	Error       string          `json:"error"`
	Transaction string          `json:"transaction"`
	Data        json.RawMessage `json:"data"`
}

// bridge finds Zigbee2MQTT's bridge device and its base topic. Several
// bridges: the first by id (docs/spec/device-onboarding.md).
func (p *Pairing) bridge(ctx context.Context) (string, error) {
	var ids []string
	for id, d := range p.o.Devices() {
		if d.Manufacturer == "Zigbee2MQTT" && d.Model == "Bridge" && d.DisabledBy == "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return "", errNoBridge
	}
	sort.Strings(ids)
	info, err := p.debugInfo(ctx, ids[0])
	if err != nil {
		return "", err
	}
	for _, e := range info {
		for _, suffix := range []string{"/bridge/state", "/bridge/info"} {
			if base, ok := strings.CutSuffix(e.StateTopic, suffix); ok && base != "" {
				return base, nil
			}
		}
	}
	return "", errors.New("the bridge's topics are not known to Home Assistant")
}

var errNoBridge = errors.New("zigbee bridge not found")

type discovered struct {
	EntityID   string
	StateTopic string
}

// debugInfo reads the MQTT discovery of a device's entities.
func (p *Pairing) debugInfo(ctx context.Context, deviceID string) ([]discovered, error) {
	raw, err := p.o.HA.Call(ctx, map[string]any{"type": "mqtt/device/debug_info", "device_id": deviceID})
	if err != nil {
		return nil, err
	}
	var out struct {
		Entities []struct {
			EntityID  string `json:"entity_id"`
			Discovery struct {
				Payload struct {
					StateTopic string `json:"state_topic"`
				} `json:"payload"`
			} `json:"discovery_data"`
		} `json:"entities"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	var list []discovered
	for _, e := range out.Entities {
		list = append(list, discovered{EntityID: e.EntityID, StateTopic: e.Discovery.Payload.StateTopic})
	}
	return list, nil
}

// request sends a request to Zigbee2MQTT and waits for its answer on the
// run's subscription, matched by the transaction.
func (p *Pairing) request(ctx context.Context, r *run, endpoint string, payload map[string]any) (response, error) {
	tx := strings.ToLower(ulid.Make().String())
	payload["transaction"] = tx
	answer := make(chan response, 1)
	r.mu.Lock()
	r.waiting[tx] = answer
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.waiting, tx)
		r.mu.Unlock()
	}()
	body, _ := json.Marshal(payload)
	if err := p.o.HA.CallService(ctx, "mqtt", "publish", map[string]any{
		"topic": r.base + "/bridge/request/" + endpoint, "payload": string(body),
	}, nil); err != nil {
		return response{}, err
	}
	timer := time.NewTimer(p.o.Wait)
	defer timer.Stop()
	select {
	case a := <-answer:
		if a.Status != "ok" {
			return a, errors.New(a.Error)
		}
		return a, nil
	case <-timer.C:
		return response{}, fmt.Errorf("no answer from Zigbee2MQTT within %s", p.o.Wait)
	case <-ctx.Done():
		return response{}, ctx.Err()
	}
}

// subscribe opens a run's subscriptions: Zigbee2MQTT's answers (for the
// run's whole life), its events and its info (until a device is there),
// and the entities Home Assistant creates. It returns once they are live:
// Home Assistant subscribes at the broker a moment after it confirms
// (seen on the test HA 2026-11-09: an answer right after was lost), and the
// retained info, subscribed last, comes only once they are.
func (p *Pairing) subscribe(ctx context.Context, r *run) error {
	ready := make(chan struct{})
	var once sync.Once
	pass := func(m haws.MQTTMessage) {
		select { // on the connection's read loop: only pass it on
		case r.in <- input{msg: m}:
		default:
			p.log.Warn("zigbee event dropped, too many at once", "topic", m.Topic)
		}
	}
	s, err := p.o.HA.SubscribeMQTT(ctx, r.base+"/bridge/response/#", func(m haws.MQTTMessage) {
		var a response
		if json.Unmarshal([]byte(m.Payload), &a) != nil || a.Transaction == "" {
			return
		}
		r.mu.Lock()
		ch := r.waiting[a.Transaction]
		r.mu.Unlock()
		if ch != nil {
			select {
			case ch <- a:
			default:
			}
		}
	})
	if err != nil {
		return err
	}
	r.stopAnswers = s
	s, err = p.o.HA.Subscribe(ctx, "entity_registry_updated", func(ev haws.Event) {
		var d struct {
			Action   string `json:"action"`
			EntityID string `json:"entity_id"`
		}
		if json.Unmarshal(ev.Data, &d) == nil && d.Action == "create" {
			select {
			case r.in <- input{created: d.EntityID}:
			default:
				p.log.Warn("zigbee: a created entity dropped, too many at once", "entity", d.EntityID)
			}
		}
	})
	if err != nil {
		return err
	}
	r.stopWatch = append(r.stopWatch, s)
	s, err = p.o.HA.SubscribeMQTT(ctx, r.base+"/bridge/event", pass)
	if err != nil {
		return err
	}
	r.stopWatch = append(r.stopWatch, s)
	s, err = p.o.HA.SubscribeMQTT(ctx, r.base+"/bridge/info", func(m haws.MQTTMessage) {
		once.Do(func() { close(ready) })
		pass(m)
	})
	if err != nil {
		return err
	}
	r.stopWatch = append(r.stopWatch, s)
	timer := time.NewTimer(p.o.Wait)
	defer timer.Stop()
	select {
	case <-ready:
		return nil
	case <-timer.C:
		return fmt.Errorf("no bridge info from Zigbee2MQTT within %s", p.o.Wait)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// unsubscribe ends what a run watches; with all, Zigbee2MQTT's answers too.
func (r *run) unsubscribe(all bool) {
	r.mu.Lock()
	watch := r.stopWatch
	r.stopWatch = nil
	answers := r.stopAnswers
	if all {
		r.stopAnswers = nil
	}
	r.mu.Unlock()
	for _, s := range watch {
		s()
	}
	if all && answers != nil {
		answers()
	}
}

func (p *Pairing) start(ctx context.Context, inv tools.Invocation) (string, error) {
	t := p.t
	if inv.Channel != config.ChannelVoice || inv.DeviceID == "" {
		return t.notVoice, nil
	}
	var args struct {
		Network string `json:"network"`
	}
	_ = json.Unmarshal(inv.Args, &args)
	base, err := p.bridge(ctx)
	z2m := err == nil
	zha, zerr := p.zhaLoaded(ctx)
	if zerr != nil {
		p.log.Warn("could not read whether Home Assistant runs ZHA; only Zigbee2MQTT is tried", "error", zerr)
	}
	switch {
	case z2m && zha && args.Network == "":
		// Both networks: Mira asks which (decision 13).
		return t.askNetwork, nil
	case zha && (args.Network == "zha" || !z2m):
		return p.startZHA(ctx, inv)
	case errors.Is(err, errNoBridge):
		p.log.Warn("zigbee bridge not found; nothing paired")
		return t.noBridge, nil
	case err != nil:
		p.log.Error("zigbee pairing failed: the bridge could not be read", "error", err)
		return fmt.Sprintf(t.pairingFailed, err), nil
	}
	thread, err := p.o.Start(ctx, inv, t.plan)
	if err != nil {
		return "", err
	}
	// A run of this thread still going (asked again) ends first.
	p.endRun(context.WithoutCancel(ctx), thread)

	runCtx, stop := context.WithCancel(context.WithoutCancel(ctx))
	r := &run{thread: thread, base: base, stop: stop, in: make(chan input, 128), waiting: map[string]chan response{},
		joined: map[string]bool{}, defs: map[string]definition{}, devices: map[string]string{}}
	// Open before pairing opens, so nothing is missed.
	if err := p.subscribe(runCtx, r); err != nil {
		stop()
		r.unsubscribe(true)
		p.log.Error("zigbee pairing failed: no subscription to the bridge", "error", err)
		return fmt.Sprintf(t.pairingFailed, err), nil
	}
	if _, err := p.request(ctx, r, "permit_join", map[string]any{"time": p.o.Seconds}); err != nil {
		stop()
		r.unsubscribe(true)
		p.log.Error("zigbee pairing failed", "error", err)
		return fmt.Sprintf(t.pairingFailed, err), nil
	}
	r.mu.Lock()
	r.open = true
	r.mu.Unlock()
	p.mu.Lock()
	p.runs[thread] = r
	p.mu.Unlock()
	p.log.Info("zigbee pairing open", "thread", thread, "seconds", p.o.Seconds, "base", base)
	go func() {
		defer r.unsubscribe(false)
		p.watch(runCtx, r)
	}()
	return t.pairingOpen, nil
}

// watch follows a run's pairing until a device is there, pairing ended
// without one, or the thread ended.
func (p *Pairing) watch(ctx context.Context, r *run) {
	var safety <-chan time.Time // after pairing ended, while a device is still in its interview
	for {
		select {
		case <-ctx.Done():
			return
		case <-safety:
			p.log.Warn("zigbee device did not finish its interview in time", "thread", r.thread, "limit", p.o.Interview)
			p.say(ctx, r, p.t.interviewFailed)
			return
		case in := <-r.in:
			if in.created != "" {
				if p.created(ctx, r, in.created) {
					return
				}
				continue
			}
			m := in.msg
			if strings.HasSuffix(m.Topic, "/bridge/info") {
				var info struct {
					PermitJoin bool `json:"permit_join"`
				}
				if json.Unmarshal([]byte(m.Payload), &info) != nil {
					continue
				}
				r.mu.Lock()
				if info.PermitJoin {
					r.seen = true
					r.mu.Unlock()
					continue
				}
				wasOpen, waiting := r.open && r.seen, len(r.joined) > 0
				if wasOpen {
					r.open = false
				}
				r.mu.Unlock()
				if !wasOpen {
					continue // the retained info from before pairing opened, or closed by Mira
				}
				if waiting {
					safety = time.After(p.o.Interview)
					continue
				}
				p.log.Info("zigbee pairing ended without a device", "thread", r.thread)
				p.say(ctx, r, p.t.nothingJoined)
				return
			}
			var ev bridgeEvent
			if json.Unmarshal([]byte(m.Payload), &ev) != nil {
				continue
			}
			if p.event(ctx, r, ev) {
				return
			}
		}
	}
}

// event handles one of the bridge's events; true when the run is over.
func (p *Pairing) event(ctx context.Context, r *run, ev bridgeEvent) bool {
	ieee := ev.Data.IEEE
	switch ev.Type {
	case "device_joined":
		r.mu.Lock()
		r.joined[ieee] = true
		tell := !r.told
		r.told = true
		r.mu.Unlock()
		p.log.Info("zigbee device joined", "thread", r.thread, "ieee", ieee)
		// The interview of a battery device can take a minute or two:
		// without a word the wait feels broken.
		if tell {
			p.say(ctx, r, p.t.joining)
		}
	case "device_announce":
		r.mu.Lock()
		isNew := r.joined[ieee]
		r.mu.Unlock()
		if isNew {
			return false
		}
		// A known device paired again announces itself without joining.
		id, d, ok := p.byName(ev.Data.FriendlyName)
		if !ok {
			return false
		}
		p.closePairing(ctx, r)
		area := p.o.AreaName(p.o.DeviceArea()[id])
		p.log.Info("zigbee device known already", "thread", r.thread, "ieee", ieee, "device", id)
		p.say(ctx, r, fmt.Sprintf(p.t.known, d.DisplayName(), area))
		p.o.End(ctx, r.thread)
		return true
	case "device_interview":
		switch ev.Data.Status {
		case "failed":
			p.log.Warn("zigbee interview failed", "thread", r.thread, "ieee", ieee)
			r.mu.Lock()
			delete(r.joined, ieee)
			open := r.open
			r.mu.Unlock()
			p.say(ctx, r, p.t.interviewFailed)
			return !open
		case "successful":
			var def definition
			_ = json.Unmarshal(ev.Data.Definition, &def)
			r.mu.Lock()
			r.defs[ieee] = def
			id := r.devices[ieee]
			r.mu.Unlock()
			if id != "" {
				p.found(ctx, r, ieee, id)
				return true
			}
		}
	}
	return false
}

// created looks at an entity Home Assistant created while pairing: of a
// device that joined, its device is that device; true when the run is over.
func (p *Pairing) created(ctx context.Context, r *run, entityID string) bool {
	r.mu.Lock()
	waiting := len(r.joined) > 0
	r.mu.Unlock()
	if !waiting {
		return false
	}
	entries, err := p.entries(ctx, []string{entityID})
	if err != nil {
		p.log.Warn("could not read a new entity while pairing", "entity", entityID, "error", err)
		return false
	}
	for _, e := range entries {
		r.mu.Lock()
		var ieee string
		for joined := range r.joined {
			if strings.HasPrefix(e.UniqueID, joined+"_") {
				ieee = joined
			}
		}
		if ieee == "" || e.DeviceID == "" || r.devices[ieee] != "" {
			r.mu.Unlock()
			continue
		}
		r.devices[ieee] = e.DeviceID
		_, interviewed := r.defs[ieee]
		r.mu.Unlock()
		if interviewed {
			p.found(ctx, r, ieee, e.DeviceID)
			return true
		}
	}
	return false
}

// found: the device is in Home Assistant. Pairing closes, its facts go into
// the thread, and the thread asks for the room.
func (p *Pairing) found(ctx context.Context, r *run, ieee, deviceID string) {
	p.closePairing(ctx, r)
	r.mu.Lock()
	def := r.defs[ieee]
	r.mu.Unlock()
	// Renamed in Zigbee2MQTT, so Home Assistant's entity ids follow
	// (decision 8); over the run's subscription to the answers.
	p.o.Core.Hold(r.thread, devices.Device{ID: deviceID, Rename: func(ctx context.Context, name string) error {
		_, err := p.request(ctx, r, "device/rename", map[string]any{"from": ieee, "to": name, "homeassistant_rename": true})
		if err != nil {
			return fmt.Errorf("Zigbee2MQTT: %w", err)
		}
		return nil
	}})
	p.log.Info("zigbee device found", "thread", r.thread, "ieee", ieee, "device", deviceID, "vendor", def.Vendor, "model", def.Model)
	facts := fmt.Sprintf(p.t.facts, def.Vendor, def.Model, def.Description, def.values())
	if err := p.o.Note(ctx, r.thread, facts); err != nil {
		p.log.Warn("could not note the device in its thread", "thread", r.thread, "error", err)
	}
	p.tick(ctx, r.thread, 0)
	question, err := p.o.Question(ctx, facts)
	if err != nil || strings.TrimSpace(question) == "" {
		// docs/spec/device-onboarding.md: the fixed question, with vendor and model.
		p.log.Warn("could not word the question for the device; asking with vendor and model", "error", err)
		question = fmt.Sprintf(p.t.found, def.Vendor, def.Model)
	}
	p.say(ctx, r, question)
}

// closePairing ends pairing at Zigbee2MQTT if this run opened it.
func (p *Pairing) closePairing(ctx context.Context, r *run) {
	r.mu.Lock()
	open := r.open
	r.open = false
	r.mu.Unlock()
	if !open {
		return
	}
	if _, err := p.request(ctx, r, "permit_join", map[string]any{"time": 0}); err != nil {
		p.log.Warn("could not close zigbee pairing; it ends by itself", "seconds", p.o.Seconds, "error", err)
	}
}

func (p *Pairing) say(ctx context.Context, r *run, text string) {
	if err := p.o.Ask(ctx, r.thread, text); err != nil {
		p.log.Error("could not say the zigbee thread's announcement", "thread", r.thread, "error", err)
	}
}

func (p *Pairing) tick(ctx context.Context, thread string, step int) {
	if err := p.o.Tick(ctx, thread, steps[p.lang()][step]); err != nil {
		p.log.Warn("could not tick the zigbee plan", "thread", thread, "error", err)
	}
}

func (p *Pairing) lang() i18n.Lang {
	if _, ok := steps[p.o.Lang]; ok {
		return p.o.Lang
	}
	return i18n.DE
}

// byName finds a device of Zigbee2MQTT by the name it has there.
func (p *Pairing) byName(name string) (string, haws.Device, bool) {
	for id, d := range p.o.Devices() {
		if d.Name == name || d.NameByUser == name {
			return id, d, true
		}
	}
	return "", haws.Device{}, false
}

type entry struct {
	EntityID string `json:"entity_id"`
	DeviceID string `json:"device_id"`
	UniqueID string `json:"unique_id"`
	Category string `json:"entity_category"`
}

// entries reads entities of the entity registry, by id.
func (p *Pairing) entries(ctx context.Context, ids []string) ([]entry, error) {
	raw, err := p.o.HA.Call(ctx, map[string]any{"type": "config/entity_registry/get_entries", "entity_ids": ids})
	if err != nil {
		return nil, err
	}
	var out map[string]*entry
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	var list []entry
	for id, e := range out {
		if e != nil {
			e.EntityID = id
			list = append(list, *e)
		}
	}
	slices.SortFunc(list, func(a, b entry) int { return strings.Compare(a.EntityID, b.EntityID) })
	return list, nil
}

// Claims reports whether a pairing that runs is waiting for the device or
// has it: the found devices leave it to this dialog
// (docs/spec/found-devices.md, what is not counted).
func (p *Pairing) Claims(deviceID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, r := range p.zruns {
		r.mu.Lock()
		mine := r.deviceID == deviceID
		r.mu.Unlock()
		if mine {
			return true
		}
	}
	for _, r := range p.runs {
		r.mu.Lock()
		for _, id := range r.devices {
			if id == deviceID {
				r.mu.Unlock()
				return true
			}
		}
		r.mu.Unlock()
	}
	return false
}

// ThreadEnded ends the run of a thread that ended (cancelled, expired,
// done); pairing it opened closes.
func (p *Pairing) ThreadEnded(ctx context.Context, thread string) {
	p.endRun(ctx, thread)
	p.endZHA(ctx, thread)
}

func (p *Pairing) endRun(ctx context.Context, thread string) {
	p.mu.Lock()
	r := p.runs[thread]
	if r != nil {
		delete(p.runs, thread)
	}
	p.mu.Unlock()
	if r == nil {
		return
	}
	r.stop()
	p.closePairing(ctx, r)
	r.unsubscribe(true)
}
