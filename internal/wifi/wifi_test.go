package wifi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

// Bringing a Wi-Fi device into the Wi-Fi: docs/spec/wifi-onboarding.md,
// steps 2 and 3. The devices answer as their sources and documentation say.

const password = "geheim-Heim-42"

type fakeRadio struct {
	mu     sync.Mutex
	aps    []AP
	free   string
	joined []string
	left   int
}

func (r *fakeRadio) Kind() string                { return "fake" }
func (r *fakeRadio) Free(context.Context) string { return r.free }
func (r *fakeRadio) Tidy(context.Context) error  { return nil }
func (r *fakeRadio) Scan(context.Context) ([]AP, error) {
	return r.aps, nil
}
func (r *fakeRadio) Join(_ context.Context, ssid, _ string) (any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.joined = append(r.joined, ssid)
	return ssid, nil
}
func (r *fakeRadio) Leave(context.Context, any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.left++
	return nil
}

type request struct {
	Method, Path, Query, Body string
}

type wenv struct {
	w      *Onboarding
	radio  *fakeRadio
	srv    *httptest.Server
	status int
	reqs   []request
	asked  chan string
	ticked []string
	ref    string
	assign []string
	logs   *bytes.Buffer
	mu     sync.Mutex
}

func newWEnv(t *testing.T, radio *fakeRadio) *wenv {
	t.Helper()
	e := &wenv{radio: radio, status: 200, asked: make(chan string, 8), logs: &bytes.Buffer{}}
	e.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		e.mu.Lock()
		e.reqs = append(e.reqs, request{r.Method, r.URL.Path, r.URL.RawQuery, string(body)})
		status := e.status
		e.mu.Unlock()
		w.WriteHeader(status)
		if r.URL.Path == "/rpc" {
			_, _ = w.Write([]byte(`{"id":1,"result":{"restart_required":true}}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(e.srv.Close)
	var logMu sync.Mutex
	logger := slog.New(slog.NewTextHandler(writer(func(b []byte) (int, error) {
		logMu.Lock()
		defer logMu.Unlock()
		return e.logs.Write(b)
	}), nil))
	var r Radio
	if radio != nil {
		r = radio
	}
	e.w = New(Options{Lang: i18n.DE, Logger: logger, Radio: r, Off: "no system bus",
		Home: func() Home {
			return Home{SSID: "Heimnetz", Password: password, MQTTHost: "192.0.2.10", MQTTUser: "mira", MQTTPassword: "mqtt-geheim"}
		},
		Start: func(context.Context, tools.Invocation, string) (string, error) { return "th-wifi", nil },
		Tick: func(_ context.Context, _, m string) error {
			e.mu.Lock()
			defer e.mu.Unlock()
			e.ticked = append(e.ticked, m)
			return nil
		},
		Note: func(context.Context, string, string) error { return nil },
		Ask:  func(_ context.Context, _, text string) error { e.asked <- text; return nil },
		Await: func(context.Context, time.Duration, func(haws.Flow) bool, func(string, string, string) bool) (string, string, error) {
			e.mu.Lock()
			defer e.mu.Unlock()
			return e.ref, "Shelly Plus 1", nil
		},
		Assign: func(_ context.Context, thread, ref, name string) {
			e.mu.Lock()
			defer e.mu.Unlock()
			e.assign = append(e.assign, ref)
		},
		Base: func(Family) string { return e.srv.URL },
		Addrs: func() []net.Addr {
			return []net.Addr{&net.IPNet{IP: net.IPv4(192, 168, 178, 20), Mask: net.CIDRMask(24, 32)}}
		},
	})
	return e
}

type writer func([]byte) (int, error)

func (w writer) Write(b []byte) (int, error) { return w(b) }

func (e *wenv) run(t *testing.T, name, args string) string {
	t.Helper()
	for _, tool := range e.w.Tools() {
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

func (e *wenv) said(t *testing.T) string {
	t.Helper()
	select {
	case s := <-e.asked:
		return s
	case <-time.After(3 * time.Second):
		t.Fatal("Mira said nothing")
	}
	return ""
}

func homeAPs(extra ...AP) []AP {
	return append([]AP{{SSID: "Heimnetz", Frequency: 2437}, {SSID: "Heimnetz", Frequency: 5180}}, extra...)
}

func TestWL01_WithoutAFreeWifiTheFunctionIsOff(t *testing.T) {
	e := newWEnv(t, nil)
	for _, tool := range e.w.Tools() {
		if tool.Enabled() {
			t.Errorf("%s is offered", tool.Name)
		}
	}
	e.w.Start(context.Background())
	if !strings.Contains(e.logs.String(), "wifi onboarding off") || !strings.Contains(e.logs.String(), "no system bus") {
		t.Errorf("log: %s", e.logs)
	}
	// The machine's Wi-Fi in use: not offered either.
	busy := newWEnv(t, &fakeRadio{free: "the machine's Wi-Fi is in use (connected)"})
	for _, tool := range busy.w.Tools() {
		if tool.Enabled() {
			t.Errorf("%s is offered with the Wi-Fi in use", tool.Name)
		}
	}
}

func TestWL02_TheScanFindsDevicesInSetupModeByTheNameOfTheirWifi(t *testing.T) {
	e := newWEnv(t, &fakeRadio{aps: homeAPs(
		AP{SSID: "ShellyPlus1-A8032AB12345", Open: true, Frequency: 2412},
		AP{SSID: "tasmota-1A2B3C-4567", Open: true, Frequency: 2412},
		AP{SSID: "WLED-AP", Frequency: 2412},
		AP{SSID: "Küche Fallback Hotspot", Frequency: 2412}, // with a password: left out (decision 4)
		AP{SSID: "Nachbar", Open: true, Frequency: 2412})})
	out := e.run(t, ToolDevices, `{}`)
	for _, want := range []string{"ShellyPlus1-A8032AB12345 (Shelly", "tasmota-1A2B3C-4567 (Tasmota", "WLED-AP (WLED"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
	if strings.Contains(out, "Nachbar") || strings.Contains(out, "Fallback") {
		t.Errorf("too much: %q", out)
	}
}

func TestWL03_TheHomeWifiOnlyOn5GHzStopsTheHandOver(t *testing.T) {
	radio := &fakeRadio{aps: []AP{{SSID: "Heimnetz", Frequency: 5180}, {SSID: "ShellyPlus1-A8032AB12345", Open: true}}}
	e := newWEnv(t, radio)
	out := e.run(t, ToolHandover, `{"device":"ShellyPlus1-A8032AB12345"}`)
	if !strings.Contains(out, "5 GHz") {
		t.Fatalf("handover = %q", out)
	}
	if len(radio.joined) != 0 {
		t.Errorf("joined %v", radio.joined)
	}
}

func TestWL04_EachFamilyGetsTheHomeWifiItsWay(t *testing.T) {
	cases := []struct {
		ssid string
		want []string // method path query/body parts
	}{
		{"shellyplug-s-A1B2C3", []string{"GET /settings/sta", "enabled=1", "ssid=Heimnetz", "key=" + password}},
		{"ShellyPlus1-A8032AB12345", []string{"POST /rpc", `"method":"WiFi.SetConfig"`, `"ssid":"Heimnetz"`, `"pass":"` + password + `"`, "Shelly.Reboot"}},
		{"tasmota-1A2B3C-4567", []string{"GET /cm", "Backlog+SSID1+Heimnetz", "Password1+" + password, "MqttHost+192.0.2.10", "MqttUser+mira", "MqttPassword+mqtt-geheim"}},
		{"Flur Fallback Hotspot", []string{"POST /wifisave", "ssid=Heimnetz", "psk=" + password}},
		{"WLED-AP", []string{"POST /json/cfg", `"ins":[{"psk":"` + password + `","ssid":"Heimnetz"}]`, "POST /json/state", `"rb":true`}},
	}
	for _, c := range cases {
		t.Run(c.ssid, func(t *testing.T) {
			f, ok := Recognize(c.ssid)
			if !ok {
				t.Fatalf("%s not recognised", c.ssid)
			}
			e := newWEnv(t, nil)
			if err := f.Handover(context.Background(), http.DefaultClient, e.srv.URL, e.w.o.Home()); err != nil {
				t.Fatalf("handover: %v", err)
			}
			var all strings.Builder
			for _, r := range e.reqs {
				all.WriteString(r.Method + " " + r.Path + "?" + r.Query + " " + r.Body + "\n")
			}
			for _, w := range c.want {
				if !strings.Contains(all.String(), w) {
					t.Errorf("missing %q in\n%s", w, all.String())
				}
			}
		})
	}
}

func TestWL05_TheDevicesWifiNeverBecomesTheDefaultRoute(t *testing.T) {
	p := nmProfile("ShellyPlus1-A8032AB12345", "")
	if p["ipv4"]["never-default"].Value() != true || p["connection"]["autoconnect"].Value() != false {
		t.Fatalf("profile = %v", p)
	}
	if !strings.HasPrefix(p["connection"]["id"].Value().(string), profilePrefix) {
		t.Errorf("id = %v", p["connection"]["id"])
	}
	if _, secured := p["802-11-wireless-security"]; secured {
		t.Error("an open Wi-Fi with a key")
	}
	if p := nmProfile("WLED-AP", "wled1234"); p["802-11-wireless-security"]["psk"].Value() != "wled1234" {
		t.Errorf("WLED's Wi-Fi without its password: %v", p)
	}
}

func TestWL06_MiraAlwaysLeavesTheDevicesWifi(t *testing.T) {
	radio := &fakeRadio{aps: homeAPs(AP{SSID: "ShellyPlus1-A8032AB12345", Open: true})}
	e := newWEnv(t, radio)
	e.status = 500
	e.run(t, ToolHandover, `{"device":"ShellyPlus1-A8032AB12345"}`)
	if s := e.said(t); s != e.w.t.handoverFailed {
		t.Fatalf("said %q", s)
	}
	radio.mu.Lock()
	defer radio.mu.Unlock()
	if len(radio.joined) != 1 || radio.left != 1 {
		t.Fatalf("joined %v, left %d", radio.joined, radio.left)
	}
}

func TestWL07_ThePasswordIsNeverSaidNorLogged(t *testing.T) {
	radio := &fakeRadio{aps: homeAPs(AP{SSID: "ShellyPlus1-A8032AB12345", Open: true})}
	e := newWEnv(t, radio)
	e.status = 500
	out := e.run(t, ToolHandover, `{"device":"ShellyPlus1-A8032AB12345"}`)
	said := e.said(t)
	for _, s := range []string{out, said, e.logs.String()} {
		if strings.Contains(s, password) || strings.Contains(s, "mqtt-geheim") {
			t.Fatalf("a password in %q", s)
		}
	}
}

func TestWL08_AfterTheHandOverTheFoundDeviceIsTakenIn(t *testing.T) {
	radio := &fakeRadio{aps: homeAPs(AP{SSID: "ShellyPlus1-A8032AB12345", Open: true})}
	e := newWEnv(t, radio)
	e.ref = "flow:f1"
	if out := e.run(t, ToolHandover, `{"device":"ShellyPlus1-A8032AB12345"}`); out != e.w.t.handingOver {
		t.Fatalf("handover = %q", out)
	}
	if s := e.said(t); !strings.Contains(s, "Shelly Plus 1 ist jetzt im WLAN. Soll ich es einbinden?") {
		t.Fatalf("said %q", s)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.assign) != 1 || e.assign[0] != "flow:f1" || len(e.ticked) != 1 || e.ticked[0] != "Ins WLAN" {
		t.Fatalf("assigned %v, ticked %v", e.assign, e.ticked)
	}
}

func TestWL09_NotDiscoveredAfterTwoMinutes(t *testing.T) {
	radio := &fakeRadio{aps: homeAPs(AP{SSID: "ShellyPlus1-A8032AB12345", Open: true})}
	e := newWEnv(t, radio)
	e.run(t, ToolHandover, `{"device":"ShellyPlus1-A8032AB12345"}`)
	if s := e.said(t); s != e.w.t.notDiscovered {
		t.Fatalf("said %q", s)
	}
	if !strings.Contains(e.logs.String(), "wifi device not discovered after the handover") {
		t.Errorf("log: %s", e.logs)
	}
}

func TestWL10_NothingInSetupMode(t *testing.T) {
	e := newWEnv(t, &fakeRadio{aps: homeAPs()})
	if out := e.run(t, ToolDevices, `{}`); out != e.w.t.noneFound {
		t.Fatalf("devices = %q", out)
	}
}

func TestSameAddressesAreRefused(t *testing.T) {
	radio := &fakeRadio{aps: homeAPs(AP{SSID: "tasmota-1A2B3C-4567", Open: true})}
	e := newWEnv(t, radio)
	e.w.o.Addrs = func() []net.Addr {
		return []net.Addr{&net.IPNet{IP: net.IPv4(192, 168, 4, 20), Mask: net.CIDRMask(24, 32)}}
	}
	if out := e.run(t, ToolHandover, `{"device":"tasmota-1A2B3C-4567"}`); !strings.Contains(out, "192.168.4.1") {
		t.Fatalf("handover = %q", out)
	}
	if len(radio.joined) != 0 {
		t.Errorf("joined %v", radio.joined)
	}
}
