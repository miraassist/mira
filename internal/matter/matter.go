// Package matter adds a Matter device over Wi-Fi by voice
// (docs/spec/device-onboarding.md, Matter over Wi-Fi): the setup code, the
// commissioning, and the wait for the device between two turns. Everything
// goes through Home Assistant's own Matter integration, which talks to the
// Matter server; Mira commissions nothing herself.
//
// The code comes from the sticker or the device the user holds: at a
// satellite with a camera Mira reads the QR code (the TECHO5 action
// qr_scan), everywhere else the user reads the eleven digits, whose last one
// is a check digit (Verhoeff, as Matter's specification prescribes).
//
// Once the device is in Home Assistant, the room and the name are the
// devices core's, as for every device.
package matter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/miraassist/mira/internal/devices"
	"github.com/miraassist/mira/internal/excerpt"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

// Skill is the skill whose tools these are.
const Skill = devices.Skill

// Tool names: the dialog starts with one, the code comes with the other.
const (
	ToolStart = "matter_pairing_start"
	ToolCode  = "matter_code"
)

// Plan steps, matched to tick them (the plan's text in texts.plan).
var steps = map[i18n.Lang][3]string{
	i18n.DE: {"Gerät anmelden", "Raum", "Name"},
	i18n.EN: {"Device joins", "Room", "Name"},
}

// HA is what the package needs of Home Assistant's websocket.
type HA interface {
	Call(ctx context.Context, command map[string]any) (json.RawMessage, error)
	// CallFor is a command with a bound of its own: the commissioning and
	// the QR scan take longer than every other call.
	CallFor(ctx context.Context, command map[string]any, timeout time.Duration) (json.RawMessage, error)
	Subscribe(ctx context.Context, eventType string, handler haws.EventHandler) (func(), error)
}

type Options struct {
	Lang   i18n.Lang
	Logger *slog.Logger
	HA     HA
	// Core takes the device in once it is there: room, name, exposed.
	Core *devices.Core
	// Wifi is the home Wi-Fi Mira keeps (the skill's settings wlan_ssid and
	// wlan_password); without both the dialog does not start.
	Wifi func() (ssid, password string)
	// Node is the satellite's ESPHome node of a device id ("" for none);
	// HasAction says whether Home Assistant knows an action.
	Node      func(deviceID string) string
	HasAction func(ctx context.Context, domain, service string) bool
	// Devices are Home Assistant's devices by id (the structure Mira keeps).
	Devices func() map[string]haws.Device
	// Start starts the turn's thread of the skill with its plan (or takes
	// the open one) and returns its id; Thread finds the turn's; Device the
	// satellite a thread began at.
	Start  func(ctx context.Context, inv tools.Invocation, plan string) (string, error)
	Thread func(ctx context.Context, inv tools.Invocation) string
	Device func(ctx context.Context, thread string) string
	// Tick ticks a step of the plan; Note writes a fact into the thread's
	// log; Ask says a text at the thread's satellite as its announcement, so
	// the answer joins the thread.
	Tick func(ctx context.Context, thread, match string) error
	Note func(ctx context.Context, thread, text string) error
	Ask  func(ctx context.Context, thread, text string) error
	// Question writes the question for a device found, from its facts, in
	// the household's language (one model round, the skill's agent).
	Question func(ctx context.Context, facts string) (string, error)
	// Waiting shows at the thread's satellite that Mira waits for the device
	// (docs/spec/waiting-indicator.md); nil shows nothing.
	Waiting Waiter

	// Pause is the wait between two attempts (2 s), so an attempt that
	// fails at once does not become a hot loop.
	Pause time.Duration
	// Scan is how long the Show's camera looks for a QR code (20 s);
	// Attempt bounds one commissioning call (60 s: it gives up by itself
	// after about 30 s without a device); Window is how long Mira tries,
	// three minutes as a Zigbee pairing lasts.
	Scan    time.Duration
	Attempt time.Duration
	Window  time.Duration
}

// Waiter shows a wait at a thread's satellite and stops it.
type Waiter interface {
	Start(ctx context.Context, thread string)
	Stop(ctx context.Context, thread string)
}

// Onboarding adds Matter devices, one run per thread.
type Onboarding struct {
	o   Options
	t   texts
	log *slog.Logger

	mu   sync.Mutex
	runs map[string]*run // by thread id
}

// run is one thread's commissioning.
type run struct {
	thread string
	stop   context.CancelFunc
	// known are the devices Home Assistant had when the commissioning
	// started: a device created while it runs is the thread's.
	known map[string]bool
	// created carries the device ids Home Assistant creates meanwhile.
	created chan string
	// deviceID is the device the commissioning brought, once it is there:
	// the run stays registered with it until the thread ends, so the found
	// devices leave it alone (decision 21).
	mu       sync.Mutex
	deviceID string
}

func New(o Options) *Onboarding {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Scan == 0 {
		o.Scan = 20 * time.Second
	}
	if o.Attempt == 0 {
		o.Attempt = 60 * time.Second
	}
	if o.Window == 0 {
		o.Window = 3 * time.Minute
	}
	if o.Pause == 0 {
		o.Pause = 2 * time.Second
	}
	return &Onboarding{o: o, t: i18n.Pick(catalog, o.Lang), log: o.Logger.With("component", "matter"),
		runs: map[string]*run{}}
}

// Tools are the skill's Matter tools.
func (m *Onboarding) Tools() []tools.InternalTool {
	t := m.t
	return []tools.InternalTool{
		{Name: ToolStart, Description: t.startDesc, Skill: Skill,
			Schema: `{"type":"object","properties":{},"required":[]}`,
			Run:    m.start},
		{Name: ToolCode, Description: t.codeDesc, Skill: Skill,
			Schema: fmt.Sprintf(`{"type":"object","properties":{"code":{"type":"string","description":%q}},"required":["code"]}`, t.argCode),
			Run:    m.code},
	}
}

// start begins the dialog: Matter must be there, the Wi-Fi must be known,
// and then the code comes, scanned at a satellite with a camera or read out
// everywhere else.
func (m *Onboarding) start(ctx context.Context, inv tools.Invocation) (string, error) {
	t := m.t
	if inv.DeviceID == "" {
		return t.onlyAtSatellite, nil
	}
	loaded, err := m.matterLoaded(ctx)
	if err != nil {
		m.log.Error("matter not available", "error", err)
		return fmt.Sprintf(t.noMatter, err), nil
	}
	if !loaded {
		m.log.Error("matter not available", "error", "no loaded config entry")
		return t.noMatterEntry, nil
	}
	ssid, password := m.wifi()
	if ssid == "" || password == "" {
		m.log.Error("matter onboarding without wi-fi settings", "ssid", ssid != "", "password", password != "")
		return t.noWifi, nil
	}
	thread, err := m.o.Start(ctx, inv, t.plan)
	if err != nil {
		return "", err
	}
	node := m.node(inv.DeviceID)
	if node == "" || !m.o.HasAction(ctx, "esphome", node+"_qr_scan") {
		// The user now reads the digits out: the satellite keeps listening
		// (docs/spec/keep-listening.md).
		tools.TurnOf(ctx).WaitForUser()
		return t.readCode, nil
	}
	code, err := m.scan(ctx, node)
	if err != nil {
		m.log.Warn("qr scan found no code", "satellite", node, "error", err)
		tools.TurnOf(ctx).WaitForUser()
		return t.scanAgain, nil
	}
	return m.commission(ctx, thread, code)
}

// code takes the code the user read out (or a QR code's content the model
// passes on), checks it and starts the commissioning.
func (m *Onboarding) code(ctx context.Context, inv tools.Invocation) (string, error) {
	t := m.t
	var args struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(inv.Args, &args); err != nil {
		return "", fmt.Errorf("%q not understood: %w", excerpt.Bytes(inv.Args), err)
	}
	thread := m.o.Thread(ctx, inv)
	if thread == "" {
		return t.noThread, nil
	}
	code, err := Code(args.Code)
	if err != nil {
		m.log.Warn("matter code refused", "digits", len(digitsOf(args.Code)), "error", err)
		tools.TurnOf(ctx).WaitForUser() // the code comes again
		return t.codeRefused, nil
	}
	return m.commission(ctx, thread, code)
}

// scan reads a QR code at the satellite: the action answers as soon as it
// has one, and fails when it read nothing within its seconds.
func (m *Onboarding) scan(ctx context.Context, node string) (string, error) {
	cmd := map[string]any{"type": "call_service", "domain": "esphome", "service": node + "_qr_scan",
		"return_response": true, "service_data": map[string]any{"seconds": int(m.o.Scan.Seconds())}}
	raw, err := m.o.HA.CallFor(ctx, cmd, m.o.Scan+10*time.Second)
	if err != nil {
		return "", err
	}
	var answer struct {
		Response struct {
			Code string `json:"code"`
		} `json:"response"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return "", fmt.Errorf("%q not understood: %w", excerpt.Bytes(raw), err)
	}
	if strings.TrimSpace(answer.Response.Code) == "" {
		return "", errors.New("no code was read")
	}
	return strings.TrimSpace(answer.Response.Code), nil
}

// commission hands the Wi-Fi to the Matter server and starts the
// commissioning in the background: the turn only says that the device may be
// plugged in now.
func (m *Onboarding) commission(ctx context.Context, thread, code string) (string, error) {
	t := m.t
	ssid, password := m.wifi()
	if _, err := m.o.HA.Call(ctx, map[string]any{"type": "matter/set_wifi_credentials",
		"network_name": ssid, "password": password}); err != nil {
		m.log.Error("could not give the matter server the wi-fi", "error", err)
		return fmt.Sprintf(t.wifiFailed, err), nil
	}
	m.endRun(context.WithoutCancel(ctx), thread)
	runCtx, stop := context.WithCancel(context.WithoutCancel(ctx))
	r := &run{thread: thread, stop: stop, known: map[string]bool{}, created: make(chan string, 64)}
	for id := range m.o.Devices() {
		r.known[id] = true
	}
	unsubscribe, err := m.o.HA.Subscribe(runCtx, "device_registry_updated", func(ev haws.Event) {
		var data struct {
			Action   string `json:"action"`
			DeviceID string `json:"device_id"`
		}
		if json.Unmarshal(ev.Data, &data) != nil || data.Action != "create" {
			return
		}
		select { // on the connection's read loop: only pass it on
		case r.created <- data.DeviceID:
		default:
			m.log.Warn("a new device was dropped, too many at once")
		}
	})
	if err != nil {
		stop()
		m.log.Error("matter commissioning failed", "thread", thread, "error", err)
		return fmt.Sprintf(t.commissionFailed, err), nil
	}
	m.mu.Lock()
	m.runs[thread] = r
	m.mu.Unlock()
	m.log.Info("matter commissioning started", "thread", thread, "window", m.o.Window.String())
	m.waitStart(ctx, thread)
	go func() {
		defer unsubscribe()
		defer stop()
		m.watch(runCtx, r, code)
	}()
	return t.plugItIn, nil
}

// watch commissions until the device is there, the window is over or the
// thread ended. One attempt gives up by itself after about 30 seconds
// without a device in reach, so the next one follows (decision 22).
func (m *Onboarding) watch(ctx context.Context, r *run, code string) {
	// What is said at the end outlives the run: ending it cancels ctx, and
	// a cancelled one neither stops the wait nor speaks.
	tell := context.WithoutCancel(ctx)
	deadline := time.Now().Add(m.o.Window)
	answers := make(chan error, 1)
	attempt := 0
	for {
		if time.Now().After(deadline) {
			m.log.Warn("matter commissioning gave up", "thread", r.thread, "attempts", attempt)
			m.endRun(tell, r.thread)
			m.sayTo(tell, r.thread, m.t.nothingFound)
			return
		}
		attempt++
		go func(n int) {
			_, err := m.o.HA.CallFor(ctx, map[string]any{"type": "matter/commission",
				"code": code, "network_only": false}, m.o.Attempt)
			answers <- err
		}(attempt)
		for waiting := true; waiting; {
			select {
			case <-ctx.Done():
				return
			case id := <-r.created:
				if r.known[id] {
					continue
				}
				m.found(ctx, r, id)
				return
			case err := <-answers:
				if err == nil {
					// The device is there; its registry event comes in the
					// same moment (state table M6) or came already.
					select {
					case id := <-r.created:
						if !r.known[id] {
							m.found(ctx, r, id)
							return
						}
					case <-time.After(5 * time.Second):
					case <-ctx.Done():
						return
					}
					m.log.Warn("matter commissioned a device home assistant does not show", "thread", r.thread)
					m.endRun(tell, r.thread)
					m.sayTo(tell, r.thread, m.t.nothingFound)
					return
				}
				if !noDevice(err) {
					m.log.Error("matter commissioning failed", "thread", r.thread, "attempt", attempt, "error", err)
					m.endRun(tell, r.thread)
					m.sayTo(tell, r.thread, fmt.Sprintf(m.t.commissionFailed, err))
					return
				}
				m.log.Debug("matter commissioning found no device, trying again", "thread", r.thread, "attempt", attempt)
				// A pause, so an attempt that fails at once (no Bluetooth,
				// say) is no hot loop; a real one takes about 30 s anyway.
				select {
				case <-time.After(m.o.Pause):
				case <-ctx.Done():
					return
				case id := <-r.created:
					if !r.known[id] {
						m.found(ctx, r, id)
						return
					}
				}
				waiting = false
			}
		}
	}
}

// found takes the device Home Assistant created: the wait stops, its facts
// go into the thread, and Mira asks for its room at the satellite.
func (m *Onboarding) found(ctx context.Context, r *run, deviceID string) {
	d := m.device(ctx, deviceID)
	r.mu.Lock()
	r.deviceID = deviceID
	r.mu.Unlock()
	m.log.Info("matter device commissioned", "thread", r.thread, "device", deviceID,
		"manufacturer", d.Manufacturer, "model", d.Model)
	// Named in Home Assistant, with its entity ids, as every device that is
	// not Zigbee2MQTT's (docs/spec/found-devices.md, decision 5).
	m.o.Core.Hold(r.thread, devices.Device{ID: deviceID, RenameEntities: true})
	m.waitStop(ctx, r.thread)
	facts := strings.TrimSpace(strings.Join([]string{d.Manufacturer, d.Model, d.DisplayName()}, " "))
	if err := m.o.Note(ctx, r.thread, fmt.Sprintf(m.t.note, facts)); err != nil {
		m.log.Warn("could not write the device into the thread", "thread", r.thread, "error", err)
	}
	if err := m.o.Tick(ctx, r.thread, steps[m.lang()][0]); err != nil {
		m.log.Warn("could not tick the step", "thread", r.thread, "error", err)
	}
	question, err := m.o.Question(ctx, facts)
	if err != nil || strings.TrimSpace(question) == "" {
		m.log.Warn("could not word the question for the device", "thread", r.thread, "error", err)
		question = fmt.Sprintf(m.t.foundFixed, facts)
	}
	m.sayTo(ctx, r.thread, question)
}

// device reads the device Home Assistant just created from its registry, not
// from the structure Mira keeps: that one is rebuilt 30 s after a registry
// change (ha.structure_debounce), so a device seconds old has no vendor and
// no model there and the question would be asked about nothing (seen
// 2026-11-11 at the Show: manufacturer="" model=""). One call per device.
func (m *Onboarding) device(ctx context.Context, deviceID string) haws.Device {
	raw, err := m.o.HA.Call(ctx, map[string]any{"type": "config/device_registry/list"})
	if err != nil {
		m.log.Warn("could not read the new device from home assistant; its facts stay empty",
			"device", deviceID, "error", err)
		return m.o.Devices()[deviceID]
	}
	var list []haws.Device
	if err := json.Unmarshal(raw, &list); err != nil {
		m.log.Warn("home assistant's device registry was not understood", "error", err,
			"answer", excerpt.Bytes(raw))
		return m.o.Devices()[deviceID]
	}
	for _, d := range list {
		if d.ID == deviceID {
			return d
		}
	}
	return m.o.Devices()[deviceID]
}

// noDevice reports an error of the commissioning that only says no device
// answered: the next attempt may find it (the user is still plugging it in).
func noDevice(err error) bool {
	var haErr *haws.Error
	if errors.As(err, &haErr) {
		return strings.Contains(strings.ToLower(haErr.Message), "commission with code failed")
	}
	return errors.Is(err, context.DeadlineExceeded)
}

func (m *Onboarding) matterLoaded(ctx context.Context) (bool, error) {
	raw, err := m.o.HA.Call(ctx, map[string]any{"type": "config_entries/get", "domain": "matter"})
	if err != nil {
		return false, err
	}
	var entries []struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return false, fmt.Errorf("%q not understood: %w", excerpt.Bytes(raw), err)
	}
	for _, e := range entries {
		if e.State == "loaded" {
			return true, nil
		}
	}
	return false, nil
}

func (m *Onboarding) wifi() (string, string) {
	if m.o.Wifi == nil {
		return "", ""
	}
	return m.o.Wifi()
}

func (m *Onboarding) node(deviceID string) string {
	if m.o.Node == nil || m.o.HasAction == nil {
		return ""
	}
	return m.o.Node(deviceID)
}

func (m *Onboarding) lang() i18n.Lang {
	if m.o.Lang == "" {
		return i18n.DE
	}
	return m.o.Lang
}

func (m *Onboarding) sayTo(ctx context.Context, thread, text string) {
	if err := m.o.Ask(ctx, thread, text); err != nil {
		m.log.Warn("could not say it at the satellite", "thread", thread, "error", err)
	}
}

func (m *Onboarding) waitStart(ctx context.Context, thread string) {
	if m.o.Waiting != nil {
		m.o.Waiting.Start(ctx, thread)
	}
}

func (m *Onboarding) waitStop(ctx context.Context, thread string) {
	if m.o.Waiting != nil {
		m.o.Waiting.Stop(ctx, thread)
	}
}

// endRun stops a thread's commissioning if one runs (a second start, the
// thread ending).
func (m *Onboarding) endRun(ctx context.Context, thread string) {
	m.mu.Lock()
	r := m.runs[thread]
	delete(m.runs, thread)
	m.mu.Unlock()
	if r == nil {
		return
	}
	r.stop()
	m.waitStop(ctx, thread)
}

// End is called when a thread ends (done, cancelled, expired): the
// commissioning stops with it.
func (m *Onboarding) End(ctx context.Context, thread string) {
	m.mu.Lock()
	running := m.runs[thread] != nil
	m.mu.Unlock()
	if running {
		m.log.Info("matter commissioning cancelled", "thread", thread)
	}
	m.endRun(ctx, thread)
}

// Claims reports whether a commissioning that runs brought this device: the
// found devices leave it to this dialog (docs/spec/found-devices.md, what is
// not counted; decision 21).
func (m *Onboarding) Claims(deviceID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.runs {
		r.mu.Lock()
		found := r.deviceID
		r.mu.Unlock()
		if found == deviceID || (found == "" && !r.known[deviceID]) {
			return true
		}
	}
	return false
}
