package matter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/devices"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

// Adding a Matter device by voice: docs/spec/device-onboarding.md, Matter
// over Wi-Fi, step 7.

// fakeHA answers the four commands the onboarding uses: whether Matter is
// loaded, the Wi-Fi, the QR scan and the commissioning.
type fakeHA struct {
	mu sync.Mutex
	// loaded is Home Assistant's Matter entry; scan what the camera reads
	// ("" fails); commissions are the answers of the commissioning calls, in
	// order (the last one is repeated).
	loaded      bool
	scan        string
	commissions []error
	// registry is Home Assistant's device registry, which holds a new
	// device at once (the structure Mira keeps lags 30 s behind).
	registry []haws.Device

	calls    []string
	attempts int
	handler  haws.EventHandler
}

func (f *fakeHA) Call(ctx context.Context, cmd map[string]any) (json.RawMessage, error) {
	return f.CallFor(ctx, cmd, time.Minute)
}

func (f *fakeHA) CallFor(_ context.Context, cmd map[string]any, _ time.Duration) (json.RawMessage, error) {
	f.mu.Lock()
	kind := fmt.Sprint(cmd["type"])
	if kind == "call_service" {
		kind = fmt.Sprint(cmd["domain"]) + "." + fmt.Sprint(cmd["service"])
	}
	f.calls = append(f.calls, kind)
	f.mu.Unlock()
	switch {
	case kind == "config_entries/get":
		if !f.loaded {
			return json.RawMessage(`[]`), nil
		}
		return json.RawMessage(`[{"state":"loaded"}]`), nil
	case kind == "config/device_registry/list":
		raw, _ := json.Marshal(f.registry)
		return raw, nil
	case kind == "matter/set_wifi_credentials":
		return json.RawMessage(`null`), nil
	case strings.HasSuffix(kind, "_qr_scan"):
		if f.scan == "" {
			return nil, &haws.Error{Command: kind, Message: "qr scan: no code was read"}
		}
		return json.RawMessage(fmt.Sprintf(`{"response":{"code":%q}}`, f.scan)), nil
	case kind == "matter/commission":
		f.mu.Lock()
		n := f.attempts
		f.attempts++
		f.mu.Unlock()
		if n >= len(f.commissions) {
			n = len(f.commissions) - 1
		}
		if n < 0 {
			return json.RawMessage(`null`), nil
		}
		return json.RawMessage(`null`), f.commissions[n]
	}
	return json.RawMessage(`null`), nil
}

func (f *fakeHA) Subscribe(_ context.Context, _ string, handler haws.EventHandler) (func(), error) {
	f.mu.Lock()
	f.handler = handler
	f.mu.Unlock()
	return func() {}, nil
}

// created fires the registry event of a new device.
func (f *fakeHA) created(id string) {
	f.mu.Lock()
	h := f.handler
	f.mu.Unlock()
	if h == nil {
		return
	}
	h(haws.Event{EventType: "device_registry_updated",
		Data: json.RawMessage(fmt.Sprintf(`{"action":"create","device_id":%q}`, id))})
}

func (f *fakeHA) called(kind string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == kind {
			n++
		}
	}
	return n
}

// noDeviceError is what Home Assistant answers while nothing is in reach
// (state table M8).
var noDeviceError = &haws.Error{Command: "matter/commission", Code: "1", Message: "Commission with code failed for node 1."}

type harness struct {
	ha      *fakeHA
	m       *Onboarding
	said    chan string
	devices map[string]haws.Device
	waits   chan string
}

func newHarness(t *testing.T, ha *fakeHA, node string) *harness {
	t.Helper()
	h := &harness{ha: ha, said: make(chan string, 8), devices: map[string]haws.Device{"sat": {ID: "sat", Name: "Show"}},
		waits: make(chan string, 8)}
	core := devices.NewCore(devices.CoreOptions{Lang: i18n.DE, HA: nopHA{}})
	h.m = New(Options{
		Lang: i18n.DE, HA: ha, Core: core, Waiting: h,
		Wifi:      func() (string, string) { return "Haus", "geheim" },
		Node:      func(string) string { return node },
		HasAction: func(context.Context, string, string) bool { return node != "" },
		Devices:   func() map[string]haws.Device { return h.devices },
		Start:     func(context.Context, tools.Invocation, string) (string, error) { return "th1", nil },
		Thread:    func(context.Context, tools.Invocation) string { return "th1" },
		Device:    func(context.Context, string) string { return "sat" },
		Tick:      func(context.Context, string, string) error { return nil },
		Note:      func(context.Context, string, string) error { return nil },
		// As Home Assistant does: a cancelled context says nothing (the run
		// that ended must not take the sentence with it).
		Ask: func(ctx context.Context, _, text string) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			h.said <- text
			return nil
		},
		Question: func(_ context.Context, facts string) (string, error) { return "Ich hab's: " + facts + " Raum?", nil },
		Scan:     time.Second, Attempt: 200 * time.Millisecond, Window: 600 * time.Millisecond, Pause: 50 * time.Millisecond,
	})
	return h
}

func (h *harness) Start(context.Context, string) { h.waits <- "start" }
func (h *harness) Stop(ctx context.Context, _ string) {
	if ctx.Err() != nil {
		return // a cancelled context reaches no satellite
	}
	h.waits <- "stop"
}

// nopHA is the devices core's Home Assistant; the tests never let it act.
type nopHA struct{}

func (nopHA) Call(context.Context, map[string]any) (json.RawMessage, error) {
	return json.RawMessage(`null`), nil
}

func (h *harness) start(t *testing.T) string {
	t.Helper()
	out, err := h.m.Tools()[0].Run(context.Background(), tools.Invocation{Name: ToolStart, DeviceID: "sat"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	return out
}

func (h *harness) code(t *testing.T, code string) string {
	t.Helper()
	args, _ := json.Marshal(map[string]string{"code": code})
	out, err := h.m.Tools()[1].Run(context.Background(), tools.Invocation{Name: ToolCode, DeviceID: "sat", Args: args})
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	return out
}

func (h *harness) waitSaid(t *testing.T) string {
	t.Helper()
	select {
	case s := <-h.said:
		return s
	case <-time.After(3 * time.Second):
		t.Fatal("Mira said nothing at the satellite")
		return ""
	}
}

// A code that checks out: the manual pairing code of a Tapo plug's shape
// (eleven digits, the last one after Verhoeff).
const goodCode = "34970112332"

func TestDV20_TheCodeIsScannedAtASatelliteWithACamera(t *testing.T) {
	ha := &fakeHA{loaded: true, scan: "MT:4CNT18O614FQYV5PD00", commissions: []error{noDeviceError}}
	h := newHarness(t, ha, "show")
	out := h.start(t)
	if !strings.Contains(out, "Steck das Gerät jetzt ein") {
		t.Fatalf("answer: %q", out)
	}
	if ha.called("esphome.show_qr_scan") != 1 || ha.called("matter/set_wifi_credentials") != 1 {
		t.Errorf("calls: %v", ha.calls)
	}
	if got := <-h.waits; got != "start" {
		t.Errorf("the wait was not shown: %q", got)
	}
	h.m.End(context.Background(), "th1")
}

func TestDV21_ASatelliteWithoutACameraAsksForTheDigits(t *testing.T) {
	ha := &fakeHA{loaded: true}
	h := newHarness(t, ha, "")
	out := h.start(t)
	if !strings.Contains(out, "elf Ziffern") {
		t.Fatalf("answer: %q", out)
	}
	if ha.called("matter/commission") != 0 || ha.called("matter/set_wifi_credentials") != 0 {
		t.Errorf("something was commissioned: %v", ha.calls)
	}
}

func TestDV22_AScanThatReadsNothingIsSaidAndTriedAgain(t *testing.T) {
	ha := &fakeHA{loaded: true, scan: ""}
	h := newHarness(t, ha, "show")
	out := h.start(t)
	if !strings.Contains(out, "noch einmal") || !strings.Contains(out, "näher") {
		t.Fatalf("answer: %q", out)
	}
	if ha.called("matter/commission") != 0 {
		t.Errorf("commissioned without a code: %v", ha.calls)
	}
}

func TestDV23_ACodeThatDoesNotCheckOutIsRefused(t *testing.T) {
	for _, bad := range []string{"34970112331", "3497011233", "keine ziffern"} {
		ha := &fakeHA{loaded: true}
		h := newHarness(t, ha, "")
		if out := h.code(t, bad); !strings.Contains(out, "passt nicht") {
			t.Errorf("%q: answer %q", bad, out)
		}
		if ha.called("matter/commission") != 0 || ha.called("matter/set_wifi_credentials") != 0 {
			t.Errorf("%q: something was sent: %v", bad, ha.calls)
		}
	}
	// The digits may come with spaces or dashes, and a QR code as it is.
	if _, err := Code("3497-0112 332"); err != nil {
		t.Errorf("a code with separators: %v", err)
	}
	if got, err := Code("mt:4cnt18o614fqyv5pd00"); err != nil || got != "MT:4CNT18O614FQYV5PD00" {
		t.Errorf("a qr code: %q %v", got, err)
	}
}

func TestDV24_WithoutMatterOrWithoutTheWifiNothingStarts(t *testing.T) {
	ha := &fakeHA{loaded: false}
	h := newHarness(t, ha, "show")
	if out := h.start(t); !strings.Contains(out, "Matter nicht eingerichtet") {
		t.Errorf("no matter: %q", out)
	}
	ha = &fakeHA{loaded: true, scan: "MT:X"}
	h = newHarness(t, ha, "show")
	h.m.o.Wifi = func() (string, string) { return "", "" }
	if out := h.start(t); !strings.Contains(out, "wlan_ssid") {
		t.Errorf("no wi-fi: %q", out)
	}
	if ha.called("esphome.show_qr_scan") != 0 {
		t.Errorf("scanned without the wi-fi: %v", ha.calls)
	}
}

func TestDV25_AnAttemptWithoutADeviceIsRepeatedWhileTheWindowLasts(t *testing.T) {
	ha := &fakeHA{loaded: true, commissions: []error{noDeviceError}}
	h := newHarness(t, ha, "")
	h.code(t, goodCode)
	time.Sleep(400 * time.Millisecond)
	if n := ha.called("matter/commission"); n < 2 {
		t.Errorf("%d attempts in 400 ms, want more than one", n)
	}
	select {
	case s := <-h.said:
		t.Errorf("Mira said something while trying: %q", s)
	default:
	}
	h.m.End(context.Background(), "th1")
}

func TestDV26_TheDeviceHomeAssistantCreatesIsTheThreads(t *testing.T) {
	ha := &fakeHA{loaded: true, commissions: []error{noDeviceError}}
	h := newHarness(t, ha, "")
	h.code(t, goodCode)
	if got := <-h.waits; got != "start" {
		t.Fatalf("the wait was not shown: %q", got)
	}
	// As it really is: Home Assistant's registry has the device, the
	// structure Mira keeps does not yet.
	ha.mu.Lock()
	ha.registry = []haws.Device{{ID: "plug", Name: "Matter plug", Manufacturer: "TP-Link", Model: "Tapo P110M"}}
	ha.mu.Unlock()
	ha.created("plug")
	if said := h.waitSaid(t); !strings.Contains(said, "TP-Link") {
		t.Errorf("question: %q", said)
	}
	if got := <-h.waits; got != "stop" {
		t.Errorf("the wait was not stopped: %q", got)
	}
	if !h.m.Claims("plug") {
		t.Error("the device is not claimed by the running dialog")
	}
}

func TestDV27_NothingWithinTheWindowIsSaidWithTheOfferToTryAgain(t *testing.T) {
	ha := &fakeHA{loaded: true, commissions: []error{noDeviceError}}
	h := newHarness(t, ha, "")
	h.code(t, goodCode)
	if said := h.waitSaid(t); !strings.Contains(said, "nicht gefunden") || !strings.Contains(said, "zurück") {
		t.Errorf("said: %q", said)
	}
	stopped := false
	for range 2 {
		select {
		case got := <-h.waits:
			stopped = stopped || got == "stop"
		case <-time.After(time.Second):
		}
	}
	if !stopped {
		t.Error("the wait was not stopped")
	}
}

func TestDV28_AnotherErrorOfTheCommissioningEndsItAtOnce(t *testing.T) {
	ha := &fakeHA{loaded: true, commissions: []error{errors.New("device already commissioned")}}
	h := newHarness(t, ha, "")
	h.code(t, goodCode)
	if said := h.waitSaid(t); !strings.Contains(said, "already commissioned") {
		t.Errorf("said: %q", said)
	}
	if n := ha.called("matter/commission"); n != 1 {
		t.Errorf("%d attempts, want one", n)
	}
}

func TestDV29_TheFoundDevicesLeaveTheDeviceAlone(t *testing.T) {
	ha := &fakeHA{loaded: true, commissions: []error{noDeviceError}}
	h := newHarness(t, ha, "")
	h.code(t, goodCode)
	time.Sleep(50 * time.Millisecond)
	if !h.m.Claims("new-one") {
		t.Error("a device created while commissioning runs is not claimed")
	}
	if h.m.Claims("sat") {
		t.Error("a device Home Assistant had before is claimed")
	}
	h.m.End(context.Background(), "th1")
	if h.m.Claims("new-one") {
		t.Error("still claimed after the thread ended")
	}
}

// KL-05 and KL-06: the answers that ask for something keep the satellite
// listening, the one that only reports does not
// (docs/spec/keep-listening.md, decision 5).
func TestKL05_TheQuestionsForADeviceWaitForTheUser(t *testing.T) {
	ha := &fakeHA{loaded: true}
	h := newHarness(t, ha, "") // no camera: the digits are asked for
	ts := &tools.TurnState{}
	ctx := tools.WithTurn(context.Background(), ts)
	if _, err := h.m.Tools()[0].Run(ctx, tools.Invocation{Name: ToolStart, DeviceID: "sat"}); err != nil {
		t.Fatal(err)
	}
	if !ts.WaitsForUser() {
		t.Error("asking for the digits does not wait for the user")
	}

	// A code that does not fit is asked for again.
	ts2 := &tools.TurnState{}
	args, _ := json.Marshal(map[string]string{"code": "34970112331"})
	if _, err := h.m.Tools()[1].Run(tools.WithTurn(context.Background(), ts2),
		tools.Invocation{Name: ToolCode, DeviceID: "sat", Args: args}); err != nil {
		t.Fatal(err)
	}
	if !ts2.WaitsForUser() {
		t.Error("a refused code does not wait for the user")
	}
}

func TestKL06_PlugItInDoesNotWaitForTheUser(t *testing.T) {
	ha := &fakeHA{loaded: true, commissions: []error{noDeviceError}}
	h := newHarness(t, ha, "")
	ts := &tools.TurnState{}
	args, _ := json.Marshal(map[string]string{"code": goodCode})
	out, err := h.m.Tools()[1].Run(tools.WithTurn(context.Background(), ts),
		tools.Invocation{Name: ToolCode, DeviceID: "sat", Args: args})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Steck das Gerät jetzt ein") {
		t.Fatalf("answer: %q", out)
	}
	if ts.WaitsForUser() {
		t.Error("the turn waits for the user although Mira only reports")
	}
	h.m.End(context.Background(), "th1")
}
