package ui

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHomeAssistantManagesWatchersPersonsRooms(t *testing.T) {
	srv, cfg, _ := setup(t)
	cfg.sections["notifier"] = json.RawMessage(`{"cooldown":"30m","morning_trigger":{"areas":["bad"],"presence":{"bad":"binary_sensor.bad_motion"}}}`)
	cfg.sections["satellites"] = json.RawMessage(`{"bad":"assist_satellite.bad","flur":"assist_satellite.flur"}`)
	cfg.sections["watchers"] = json.RawMessage(`[{"name":"leck"}]`)
	cfg.sections["persons"] = json.RawMessage(`{"anna":{"entity":"person.anna"}}`)

	// Import: rooms joined from satellites and the morning trigger.
	code, body := call(t, srv, "GET", "/v1/ui/ha", "secret", "")
	if code != 200 || !strings.Contains(body, `{"area":"bad","satellite":"assist_satellite.bad","presence":"binary_sensor.bad_motion","morning":true}`) ||
		!strings.Contains(body, `{"area":"flur","satellite":"assist_satellite.flur","morning":false}`) || !strings.Contains(body, `"managed":false`) {
		t.Fatalf("get: %d %s", code, body)
	}

	code, body = call(t, srv, "PUT", "/v1/ui/ha", "secret", `{"watchers":[{"name":"neu"}],"persons":{},
		"rooms":[{"area":"kueche","satellite":"assist_satellite.kueche","presence":"binary_sensor.k","morning":true},{"area":"flur"}]}`)
	if code != 200 || !strings.Contains(body, `"managed":true`) {
		t.Fatalf("put: %d %s", code, body)
	}
	var notifier map[string]any
	json.Unmarshal(cfg.sections["notifier"], &notifier)
	if notifier["cooldown"] != "30m" || !strings.Contains(string(cfg.sections["notifier"]), `"morning_trigger":{"areas":["kueche"],"presence":{"kueche":"binary_sensor.k"}}`) {
		t.Errorf("notifier = %s", cfg.sections["notifier"])
	}
	if string(cfg.sections["satellites"]) != `{"kueche":"assist_satellite.kueche"}` || string(cfg.sections["watchers"]) != `[{"name":"neu"}]` {
		t.Errorf("satellites %s watchers %s", cfg.sections["satellites"], cfg.sections["watchers"])
	}

	// The web UI now sees them as managed and cannot change them, but may
	// still change the rest of the notifier.
	if _, body := call(t, srv, "GET", "/v1/ui/config", "secret", ""); !strings.Contains(body, `"managed_by":"home_assistant"`) {
		t.Errorf("config: %s", body)
	}
	if code, _ := call(t, srv, "PUT", "/v1/ui/config", "secret", `{"watchers":[]}`); code != 409 {
		t.Errorf("managed section changed in the UI: %d", code)
	}
	if code, body := call(t, srv, "PUT", "/v1/ui/config", "secret", `{"notifier":{"cooldown":"10m","morning_trigger":{"areas":[]}}}`); code != 200 {
		t.Fatalf("notifier: %d %s", code, body)
	}
	if s := string(cfg.sections["notifier"]); !strings.Contains(s, `"cooldown":"10m"`) || !strings.Contains(s, `"kueche"`) {
		t.Errorf("morning trigger not kept: %s", s)
	}

	if code, _ := call(t, srv, "PUT", "/v1/ui/ha", "secret", `{"rooms":[{"area":"a"},{"area":"a"}]}`); code != 422 {
		t.Errorf("duplicate area accepted: %d", code)
	}

	// Removing the integration hands the sections back.
	call(t, srv, "DELETE", "/v1/ui/ha", "secret", "")
	if code, _ := call(t, srv, "PUT", "/v1/ui/config", "secret", `{"watchers":[]}`); code != 200 {
		t.Errorf("after release: %d", code)
	}
}
