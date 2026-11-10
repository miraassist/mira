package zigbee

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/devices"
	haws "github.com/miraassist/mira/internal/ha/ws"
)

// ZHA: docs/spec/device-onboarding.md, step 6. The messages are ZHA's as
// Home Assistant's source gives them (decision 11): device_joined,
// raw_device_initialized, device_fully_initialized with device_reg_id.

const zhaIEEE = "00:15:8d:00:01:a2:b3:c4"

func zhaEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	// Home Assistant runs ZHA, not Zigbee2MQTT.
	delete(e.devices, bridgeID)
	e.ha.zha = true
	return e
}

func (e *env) zhaJoin(t *testing.T, deviceRegID string) {
	t.Helper()
	e.ha.zhaSend(map[string]any{"type": "device_joined", "device_info": map[string]any{"ieee": zhaIEEE, "nwk": 4711, "pairing_status": "PAIRED"}})
	e.ha.zhaSend(map[string]any{"type": "raw_device_initialized", "device_info": map[string]any{"ieee": zhaIEEE,
		"manufacturer": "LUMI", "model": "lumi.weather", "pairing_status": "INTERVIEW_COMPLETE"}})
	e.ha.zhaSend(map[string]any{"type": "device_fully_initialized", "device_info": map[string]any{"ieee": zhaIEEE,
		"manufacturer": "LUMI", "model": "lumi.weather", "name": "LUMI lumi.weather", "device_reg_id": deviceRegID,
		"pairing_status": "CONFIGURED"}})
}

func TestDV12_WithZHAPairingOpensThroughHomeAssistant(t *testing.T) {
	e := zhaEnv(t)
	if out := e.run(t, ToolStart, `{}`); out != e.p.t.pairingOpen {
		t.Fatalf("start = %q", out)
	}
	if len(e.ha.zhaPermits) != 1 || e.ha.zhaPermits[0] != 180 {
		t.Fatalf("permits = %v", e.ha.zhaPermits)
	}
	if len(e.ha.published) != 0 {
		t.Errorf("published over MQTT: %v", e.ha.published)
	}
	e.p.ThreadEnded(context.Background(), thread)
}

func TestDV13_ANewZHADeviceIsAnnouncedAndNamedWithItsEntityIDs(t *testing.T) {
	e := zhaEnv(t)
	e.run(t, ToolStart, `{}`)
	e.mu.Lock()
	e.devices["zha-dev"] = haws.Device{ID: "zha-dev", Name: "LUMI lumi.weather"}
	e.mu.Unlock()
	e.zhaJoin(t, "zha-dev")
	if s := e.said(t); s != e.p.t.joining {
		t.Fatalf("first said %q", s)
	}
	if q := e.said(t); !strings.Contains(q, "LUMI") {
		t.Fatalf("question = %q", q)
	}
	if got := e.ha.zhaPermits; len(got) != 2 || got[1] != 0 {
		t.Fatalf("permits = %v, want pairing closed", got)
	}
	e.ha.related = []string{"sensor.lumi_lumi_weather_temperature", "sensor.lumi_lumi_weather_battery"}
	e.ha.entities["sensor.lumi_lumi_weather_temperature"] = entry{EntityID: "sensor.lumi_lumi_weather_temperature"}
	e.ha.entities["sensor.lumi_lumi_weather_battery"] = entry{EntityID: "sensor.lumi_lumi_weather_battery", Category: "diagnostic"}
	e.run(t, devices.ToolAssign, `{"room":"Wohnzimmer"}`)
	out := e.run(t, devices.ToolFinish, `{"name":"Temperatursensor Wohnzimmer"}`)
	if !strings.Contains(out, "Temperatursensor Wohnzimmer") {
		t.Fatalf("finish = %q", out)
	}
	last := e.ha.updates[len(e.ha.updates)-1]
	if last["device_id"] != "zha-dev" || last["name_by_user"] != "Temperatursensor Wohnzimmer" {
		t.Errorf("rename = %v", last)
	}
	if e.ha.entityIDs["sensor.lumi_lumi_weather_temperature"] != "sensor.temperatursensor_wohnzimmer_temperature" ||
		e.ha.entityIDs["sensor.lumi_lumi_weather_battery"] != "sensor.temperatursensor_wohnzimmer_battery" {
		t.Errorf("entity ids = %v", e.ha.entityIDs)
	}
}

func TestDV14_ZHAAndZigbee2MQTTBothAsksWhichNetwork(t *testing.T) {
	e := newEnv(t)
	e.ha.zha = true
	if out := e.run(t, ToolStart, `{}`); out != e.p.t.askNetwork {
		t.Fatalf("start = %q", out)
	}
	if len(e.ha.zhaPermits) != 0 || len(e.ha.published) != 0 {
		t.Fatalf("something opened: zha %v, mqtt %v", e.ha.zhaPermits, e.ha.published)
	}
	if out := e.run(t, ToolStart, `{"network":"zha"}`); out != e.p.t.pairingOpen || len(e.ha.zhaPermits) != 1 {
		t.Fatalf("start on ZHA = %q, permits %v", out, e.ha.zhaPermits)
	}
	e.p.ThreadEnded(context.Background(), thread)
	if out := e.run(t, ToolStart, `{"network":"zigbee2mqtt"}`); out != e.p.t.pairingOpen || len(e.ha.permits()) != 1 {
		t.Fatalf("start on Zigbee2MQTT = %q, permits %v", out, e.ha.permits())
	}
	e.p.ThreadEnded(context.Background(), thread)
}

func TestDV15_NothingJoinsOnZHA(t *testing.T) {
	e := zhaEnv(t)
	e.p.o.Seconds = 1
	e.run(t, ToolStart, `{}`)
	select {
	case s := <-e.asked:
		if s != e.p.t.nothingJoined {
			t.Fatalf("said %q", s)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Mira said nothing")
	}
}

func TestDV16_AKnownDeviceRejoinsOnZHA(t *testing.T) {
	e := zhaEnv(t)
	e.mu.Lock()
	e.devices["old"] = haws.Device{ID: "old", Name: "LUMI lumi.weather", NameByUser: "Klimasensor Küche"}
	e.mu.Unlock()
	e.run(t, ToolStart, `{}`)
	e.ha.zhaSend(map[string]any{"type": "device_joined", "device_info": map[string]any{"ieee": zhaIEEE}})
	e.said(t) // the device is being learned
	e.ha.zhaSend(map[string]any{"type": "device_fully_initialized", "device_info": map[string]any{"ieee": zhaIEEE,
		"device_reg_id": "old", "user_given_name": "Klimasensor Küche"}})
	if s := e.said(t); s != fmt.Sprintf(e.p.t.known, "Klimasensor Küche", "Küche") {
		t.Fatalf("said %q", s)
	}
	select {
	case <-e.ended:
	case <-time.After(time.Second):
		t.Fatal("the thread is not done")
	}
}

func TestDV17_CancellingClosesZHAPairing(t *testing.T) {
	e := zhaEnv(t)
	e.run(t, ToolStart, `{}`)
	e.p.ThreadEnded(context.Background(), thread)
	if got := e.ha.zhaPermits; len(got) != 2 || got[1] != 0 {
		t.Fatalf("permits = %v, want pairing closed", got)
	}
}
