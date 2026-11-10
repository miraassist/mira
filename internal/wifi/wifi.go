package wifi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

// Skill is the skill whose tools these are.
const Skill = "geraet-einbinden"

// Tool names.
const (
	ToolDevices  = "wifi_devices"
	ToolHandover = "wifi_handover"
)

var stepName = map[i18n.Lang]string{i18n.DE: "Ins WLAN", i18n.EN: "Into the Wi-Fi"}

type Options struct {
	Lang   i18n.Lang
	Logger *slog.Logger
	// Radio is the machine's Wi-Fi; nil with Off saying why (the tools are
	// then not offered).
	Radio Radio
	Off   string
	// Home is the home Wi-Fi and the MQTT broker as the skill's settings have
	// them now.
	Home func() Home
	// The thread of the skill, as for the other ways of adding a device.
	Start func(ctx context.Context, inv tools.Invocation, plan string) (string, error)
	Tick  func(ctx context.Context, thread, match string) error
	Note  func(ctx context.Context, thread, text string) error
	Ask   func(ctx context.Context, thread, text string) error
	// Await and Assign are the found devices': wait for Home Assistant to
	// discover the device, then make it the thread's.
	Await  func(ctx context.Context, limit time.Duration, flowOK func(haws.Flow) bool, deviceOK func(manufacturer, model, name string) bool) (string, string, error)
	Assign func(ctx context.Context, thread, ref, name string)
	// HTTP talks to the device (10 s); Base overrides a family's address
	// (tests); Addrs are the machine's own addresses (the same-subnet check).
	// Waiting shows at the thread's satellite that Mira waits for the device
	// (docs/spec/waiting-indicator.md); nil shows nothing.
	Waiting interface {
		Start(ctx context.Context, thread string)
		Stop(ctx context.Context, thread string)
	}
	HTTP     *http.Client
	Base     func(Family) string
	Addrs    func() []net.Addr
	Discover time.Duration // 2 minutes, a safety net
}

// Onboarding brings Wi-Fi devices into the home Wi-Fi.
type Onboarding struct {
	o   Options
	t   texts
	log *slog.Logger

	mu       sync.Mutex
	busy     bool
	free     string // why the radio is not free, "" when it is
	freeAt   time.Time
	lastScan []AP
	scanned  time.Time
}

func New(o Options) *Onboarding {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.HTTP == nil {
		o.HTTP = &http.Client{Timeout: 10 * time.Second}
	}
	if o.Base == nil {
		o.Base = func(Family) string { return "" }
	}
	if o.Addrs == nil {
		o.Addrs = func() []net.Addr {
			addrs, _ := net.InterfaceAddrs()
			return addrs
		}
	}
	if o.Discover == 0 {
		o.Discover = 2 * time.Minute
	}
	return &Onboarding{o: o, t: i18n.Pick(catalog, o.Lang), log: o.Logger.With("component", "wifi-onboarding")}
}

// Start tidies what a run before left behind, and logs once when the
// function is off.
func (w *Onboarding) Start(ctx context.Context) {
	if w.o.Radio == nil {
		w.log.Info("wifi onboarding off", "reason", w.o.Off)
		return
	}
	if err := w.o.Radio.Tidy(ctx); err != nil {
		w.log.Error("could not leave the device's wifi a run before left", "error", err)
	}
	w.log.Info("wifi onboarding on", "via", w.o.Radio.Kind())
}

// on: the tools are offered only with a radio that is free; whether it is
// is read at most once a minute (one property of NetworkManager or one call
// of the Supervisor), as the tools are offered at every turn.
func (w *Onboarding) on() bool {
	if w.o.Radio == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if time.Since(w.freeAt) > time.Minute {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		why := w.o.Radio.Free(ctx)
		cancel()
		if why != w.free && why != "" {
			w.log.Info("wifi onboarding not offered", "reason", why)
		}
		w.free, w.freeAt = why, time.Now()
	}
	return w.free == ""
}

// Tools are wifi_devices and wifi_handover, offered only with a radio.
func (w *Onboarding) Tools() []tools.InternalTool {
	t := w.t
	return []tools.InternalTool{
		{Name: ToolDevices, Description: t.devicesDesc, Skill: Skill, ReadOnly: true, Enabled: w.on,
			Schema: `{"type":"object","properties":{},"required":[]}`, Run: w.devices},
		{Name: ToolHandover, Description: t.handoverDesc, Skill: Skill, Enabled: w.on,
			Schema: fmt.Sprintf(`{"type":"object","properties":{"device":{"type":"string","description":%q}},"required":["device"]}`, t.argDevice),
			Run:    w.handover},
	}
}

func (w *Onboarding) scan(ctx context.Context) ([]AP, error) {
	if reason := w.o.Radio.Free(ctx); reason != "" {
		return nil, errors.New(reason)
	}
	aps, err := w.o.Radio.Scan(ctx)
	if err != nil {
		return nil, err
	}
	w.mu.Lock()
	w.lastScan, w.scanned = aps, time.Now()
	w.mu.Unlock()
	return aps, nil
}

// homeSeen says whether the home Wi-Fi is in the scan on 2.4 GHz: "" when
// it is, else why not.
func (w *Onboarding) homeSeen(aps []AP, ssid string) string {
	seen, low := false, false
	for _, ap := range aps {
		if ap.SSID == ssid {
			seen = true
			if ap.Frequency > 0 && ap.Frequency < 3000 {
				low = true
			}
		}
	}
	switch {
	case !seen:
		return fmt.Sprintf(w.t.homeNotSeen, ssid)
	case !low:
		return fmt.Sprintf(w.t.home5GHz, ssid)
	}
	return ""
}

func (w *Onboarding) devices(ctx context.Context, _ tools.Invocation) (string, error) {
	t := w.t
	home := w.o.Home()
	if home.SSID == "" || home.Password == "" {
		return t.noHome, nil
	}
	aps, err := w.scan(ctx)
	if err != nil {
		w.log.Error("wifi scan failed", "error", err)
		return fmt.Sprintf(t.scanFailed, err), nil
	}
	var lines []string
	for _, ap := range aps {
		f, ok := Recognize(ap.SSID)
		if !ok || (f.APPassword == "" && !ap.Open) {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s (%s, %d %%)", ap.SSID, f.Name, ap.Strength))
	}
	sort.Strings(lines)
	note := ""
	if why := w.homeSeen(aps, home.SSID); why != "" {
		note = " " + why
	}
	if len(lines) == 0 {
		return t.noneFound + note, nil
	}
	return t.found + "\n" + strings.Join(lines, "\n") + note, nil
}

// sameSubnet: the machine already has an address in the device's network
// (decision 5).
func (w *Onboarding) sameSubnet(f Family) bool {
	ip := net.ParseIP(f.Address)
	if ip == nil {
		return false
	}
	for _, a := range w.o.Addrs() {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
			if n.IP.Mask(net.CIDRMask(24, 32)).Equal(ip.Mask(net.CIDRMask(24, 32))) {
				return true
			}
		}
	}
	return false
}

func (w *Onboarding) handover(ctx context.Context, inv tools.Invocation) (string, error) {
	var args struct {
		Device string `json:"device"`
	}
	if err := json.Unmarshal(inv.Args, &args); err != nil || strings.TrimSpace(args.Device) == "" {
		return "", errors.New("device is required")
	}
	t := w.t
	ssid := strings.TrimSpace(args.Device)
	f, ok := Recognize(ssid)
	if !ok {
		return fmt.Sprintf(t.unknownDevice, ssid), nil
	}
	home := w.o.Home()
	if home.SSID == "" || home.Password == "" {
		return t.noHome, nil
	}
	if f.NeedsMQTT && home.MQTTHost == "" {
		return t.noMQTT, nil
	}
	if w.sameSubnet(f) {
		w.log.Warn("the device's wifi has the home network's addresses; nothing handed over", "family", f.Name, "address", f.Address)
		return fmt.Sprintf(t.sameSubnet, f.Address), nil
	}
	w.mu.Lock()
	aps, fresh := w.lastScan, time.Since(w.scanned) < 2*time.Minute
	w.mu.Unlock()
	if !fresh {
		var err error
		if aps, err = w.scan(ctx); err != nil {
			w.log.Error("wifi scan failed", "error", err)
			return fmt.Sprintf(t.scanFailed, err), nil
		}
	}
	if why := w.homeSeen(aps, home.SSID); why != "" {
		return why, nil
	}
	w.mu.Lock()
	if w.busy {
		w.mu.Unlock()
		return t.busy, nil
	}
	w.busy = true
	w.mu.Unlock()
	thread, err := w.o.Start(ctx, inv, t.plan)
	if err != nil {
		w.mu.Lock()
		w.busy = false
		w.mu.Unlock()
		return "", err
	}
	if err := w.o.Note(ctx, thread, fmt.Sprintf(t.facts, f.Name, ssid)); err != nil {
		w.log.Warn("could not note the wifi device in its thread", "thread", thread, "error", err)
	}
	// The turn does not wait: its answer is said, the rest runs in the
	// thread.
	go w.run(context.WithoutCancel(ctx), thread, f, ssid, home)
	return t.handingOver, nil
}

// run joins the device's Wi-Fi, hands the home Wi-Fi over, leaves, and
// waits for Home Assistant to discover the device.
func (w *Onboarding) run(ctx context.Context, thread string, f Family, ssid string, home Home) {
	t := w.t
	defer func() {
		w.mu.Lock()
		w.busy = false
		w.mu.Unlock()
	}()
	if w.o.Waiting != nil {
		w.o.Waiting.Start(ctx, thread)
	}
	joined, err := w.o.Radio.Join(ctx, ssid, f.APPassword)
	if err != nil {
		w.log.Error("could not join the device's wifi", "device", ssid, "error", err)
		w.ask(ctx, thread, t.joinFailed)
		return
	}
	hctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = f.Handover(hctx, w.o.HTTP, w.o.Base(f), home)
	cancel()
	// Always left, also after an error.
	if lerr := w.o.Radio.Leave(ctx, joined); lerr != nil {
		w.log.Error("could not leave the device's wifi", "device", ssid, "error", lerr)
	}
	if err != nil {
		w.log.Error("the device did not take the wifi", "device", ssid, "family", f.Name, "error", err)
		w.ask(ctx, thread, t.handoverFailed)
		return
	}
	w.log.Info("wifi handed over", "device", ssid, "family", f.Name, "home", home.SSID)
	if err := w.o.Tick(ctx, thread, w.step()); err != nil {
		w.log.Warn("could not tick the wifi plan", "thread", thread, "error", err)
	}
	id := f.ID(ssid)
	flowOK := func(fl haws.Flow) bool {
		if fl.Handler != f.Integration {
			return false
		}
		return id == "" || strings.Contains(normalize(fl.Context.UniqueID+" "+fl.Name()), id)
	}
	var deviceOK func(string, string, string) bool
	if f.NeedsMQTT {
		// Tasmota is added by its integration without a flow.
		deviceOK = func(manufacturer, _, _ string) bool {
			return strings.Contains(strings.ToLower(manufacturer), "tasmota")
		}
		flowOK = nil
	}
	ref, name, err := w.o.Await(ctx, w.o.Discover, flowOK, deviceOK)
	if err != nil {
		w.log.Error("could not watch for the wifi device in home assistant", "device", ssid, "error", err)
		w.ask(ctx, thread, t.notDiscovered)
		return
	}
	if ref == "" {
		w.log.Warn("wifi device not discovered after the handover", "device", ssid, "limit", w.o.Discover)
		w.ask(ctx, thread, t.notDiscovered)
		return
	}
	w.log.Info("wifi device discovered", "device", ssid, "found", ref)
	w.o.Assign(ctx, thread, ref, name)
	if strings.HasPrefix(ref, "flow:") {
		w.ask(ctx, thread, fmt.Sprintf(t.inWifiConfirm, name))
		return
	}
	w.ask(ctx, thread, fmt.Sprintf(t.inWifiRoom, name))
}

func normalize(s string) string {
	return strings.NewReplacer(":", "", "-", "", "_", "").Replace(strings.ToLower(s))
}

func (w *Onboarding) step() string {
	if s, ok := stepName[w.o.Lang]; ok {
		return s
	}
	return stepName[i18n.DE]
}

// ask says what ends the wait, so the wait stops first.
func (w *Onboarding) ask(ctx context.Context, thread, text string) {
	if w.o.Waiting != nil {
		w.o.Waiting.Stop(ctx, thread)
	}
	if err := w.o.Ask(ctx, thread, text); err != nil {
		w.log.Error("could not say the wifi thread's announcement", "thread", thread, "error", err)
	}
}
