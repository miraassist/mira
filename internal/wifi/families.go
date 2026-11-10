// Package wifi brings a Wi-Fi device in its setup mode into the home Wi-Fi
// by voice (docs/spec/wifi-onboarding.md): it scans with the Wi-Fi of the
// machine Mira runs on, joins the device's own Wi-Fi for a minute, hands the
// home Wi-Fi over the way the device's family takes it, and leaves again.
// What follows (Home Assistant discovers the device) is the found devices'.
package wifi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/miraassist/mira/internal/excerpt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// Home is what is handed over: the home Wi-Fi, and for Tasmota the MQTT
// broker (decision 3).
type Home struct {
	SSID, Password                   string
	MQTTHost, MQTTUser, MQTTPassword string
}

// Family is a kind of device that sets up over a Wi-Fi of its own
// (docs/spec/wifi-onboarding.md, What is known).
type Family struct {
	Name string
	// Integration is Home Assistant's integration that finds it afterwards.
	Integration string
	// Address is the device's address in its own Wi-Fi.
	Address string
	// APPassword is the password of its own Wi-Fi ("" when open).
	APPassword string
	// NeedsMQTT: Home Assistant finds it only over MQTT.
	NeedsMQTT bool
	match     *regexp.Regexp
	// id is the part of its Wi-Fi's name that names the device (its MAC or
	// part of it), lower case; "" when the name holds none.
	id func(ssid string) string
	// handover sends the home Wi-Fi to the device at base (http://address).
	handover func(ctx context.Context, c *http.Client, base string, h Home) error
}

// ID is the device's id from the name of its Wi-Fi, lower case, "" if none.
func (f Family) ID(ssid string) string {
	if f.id == nil {
		return ""
	}
	return f.id(ssid)
}

// Handover sends the home Wi-Fi to the device; base overrides the address
// (tests), "" for the family's.
func (f Family) Handover(ctx context.Context, c *http.Client, base string, h Home) error {
	if base == "" {
		base = "http://" + f.Address
	}
	return f.handover(ctx, c, base, h)
}

var (
	shellyGen1 = regexp.MustCompile(`^shelly[a-z0-9-]*-([0-9A-Fa-f]{6,12})$`)
	shellyGen2 = regexp.MustCompile(`^Shelly[A-Za-z0-9]+-([0-9A-Fa-f]{12})$`)
	tasmota    = regexp.MustCompile(`^tasmota[-_]([0-9A-Fa-f]{6})-[0-9]{4}$`)
	wled       = regexp.MustCompile(`^WLED-AP$`)
	esphome    = regexp.MustCompile(`(?i)^.+ Fallback Hotspot$`)
)

func group1(re *regexp.Regexp) func(string) string {
	return func(ssid string) string {
		if m := re.FindStringSubmatch(ssid); len(m) > 1 {
			return strings.ToLower(m[1])
		}
		return ""
	}
}

// Families are the four big families (Shelly in two generations).
var Families = []Family{
	{Name: "Shelly", Integration: "shelly", Address: "192.168.33.1", match: shellyGen2, id: group1(shellyGen2), handover: shellyRPC},
	{Name: "Shelly (Gen1)", Integration: "shelly", Address: "192.168.33.1", match: shellyGen1, id: group1(shellyGen1), handover: shellyGen1Settings},
	{Name: "Tasmota", Integration: "tasmota", Address: "192.168.4.1", NeedsMQTT: true, match: tasmota, id: group1(tasmota), handover: tasmotaBacklog},
	{Name: "WLED", Integration: "wled", Address: "4.3.2.1", APPassword: "wled1234", match: wled, handover: wledConfig},
	{Name: "ESPHome", Integration: "esphome", Address: "192.168.4.1", match: esphome, handover: esphomeWifiSave},
}

// Recognize finds the family whose setup Wi-Fi has this name.
func Recognize(ssid string) (Family, bool) {
	for _, f := range Families {
		if f.match.MatchString(ssid) {
			return f, true
		}
	}
	return Family{}, false
}

// answer reads a device's answer; an answer of 300 or more is an error.
func answer(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode >= 300 {
		return body, fmt.Errorf("the device answered %s %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func get(ctx context.Context, c *http.Client, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	return answer(resp)
}

func postJSON(ctx context.Context, c *http.Client, u string, body any) ([]byte, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	return answer(resp)
}

// shellyRPC: Gen2 and later, WiFi.SetConfig over JSON-RPC
// (shelly-api-docs.shelly.cloud, gen2, WiFi); a reboot when it asks for one.
func shellyRPC(ctx context.Context, c *http.Client, base string, h Home) error {
	body, err := postJSON(ctx, c, base+"/rpc", map[string]any{"id": 1, "method": "WiFi.SetConfig",
		"params": map[string]any{"config": map[string]any{"sta": map[string]any{"ssid": h.SSID, "pass": h.Password, "enable": true}}}})
	if err != nil {
		return err
	}
	var out struct {
		Result *struct {
			RestartRequired bool `json:"restart_required"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return fmt.Errorf("the device's answer is not understood %q: %w", excerpt.Bytes(body), err)
	}
	if out.Error != nil {
		return fmt.Errorf("the device refused: %s", out.Error.Message)
	}
	if out.Result != nil && out.Result.RestartRequired {
		if _, err := postJSON(ctx, c, base+"/rpc", map[string]any{"id": 2, "method": "Shelly.Reboot"}); err != nil {
			return fmt.Errorf("the device took the Wi-Fi but did not restart: %w", err)
		}
	}
	return nil
}

// shellyGen1Settings: Gen1, /settings/sta (shelly-api-docs.shelly.cloud,
// gen1; checked 2026-11-10 with a Shelly 1).
func shellyGen1Settings(ctx context.Context, c *http.Client, base string, h Home) error {
	q := url.Values{"enabled": {"1"}, "ssid": {h.SSID}, "key": {h.Password}}
	_, err := get(ctx, c, base+"/settings/sta?"+q.Encode())
	return err
}

// tasmotaBacklog: the Wi-Fi and the MQTT broker in one command (Tasmota's
// command page; it answers while the Wi-Fi manager runs: checked 2026-11-10
// with Tasmota 10.1.0, docs/spec/wifi-onboarding.md, step 1).
func tasmotaBacklog(ctx context.Context, c *http.Client, base string, h Home) error {
	cmd := fmt.Sprintf("Backlog SSID1 %s; Password1 %s; MqttHost %s; MqttUser %s; MqttPassword %s",
		h.SSID, h.Password, h.MQTTHost, h.MQTTUser, h.MQTTPassword)
	_, err := get(ctx, c, base+"/cm?"+url.Values{"cmnd": {cmd}}.Encode())
	return err
}

// esphomeWifiSave: the captive portal's /wifisave (ESPHome's
// captive_portal.cpp: arguments ssid and psk).
func esphomeWifiSave(ctx context.Context, c *http.Client, base string, h Home) error {
	q := url.Values{"ssid": {h.SSID}, "psk": {h.Password}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/wifisave?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	_, err = answer(resp)
	return err
}

// wledConfig: /json/cfg with the Wi-Fi (WLED's cfg.cpp: nw.ins[].ssid, psk),
// then a restart (/json/state rb).
func wledConfig(ctx context.Context, c *http.Client, base string, h Home) error {
	if _, err := postJSON(ctx, c, base+"/json/cfg", map[string]any{"nw": map[string]any{"ins": []map[string]any{{"ssid": h.SSID, "psk": h.Password}}}}); err != nil {
		return err
	}
	if _, err := postJSON(ctx, c, base+"/json/state", map[string]any{"rb": true}); err != nil {
		return fmt.Errorf("the device took the Wi-Fi but did not restart: %w", err)
	}
	return nil
}
