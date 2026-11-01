package ui

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHomeAssistantManagesPersonsRooms(t *testing.T) {
	srv, cfg, _ := setup(t)
	cfg.sections["notifier"] = json.RawMessage(`{"cooldown":"30m","morning_trigger":{"areas":["bad"],"presence":{"bad":"binary_sensor.bad_motion"}}}`)
	cfg.sections["satellites"] = json.RawMessage(`{"bad":"assist_satellite.bad","flur":"assist_satellite.flur"}`)
	cfg.sections["persons"] = json.RawMessage(`{"anna":{"entity":"person.anna"}}`)

	// Import: rooms joined from satellites and the morning trigger.
	code, body := call(t, srv, "GET", "/v1/ui/ha", "secret", "")
	if code != 200 || !strings.Contains(body, `{"area":"bad","satellite":"assist_satellite.bad","presence":["binary_sensor.bad_motion"],"morning":true}`) ||
		!strings.Contains(body, `{"area":"flur","satellite":"assist_satellite.flur","morning":false}`) || !strings.Contains(body, `"managed":false`) {
		t.Fatalf("get: %d %s", code, body)
	}

	code, body = call(t, srv, "PUT", "/v1/ui/ha", "secret", `{"persons":{"ben":{"entity":"person.ben"}},
		"rooms":[{"area":"kueche","satellite":"assist_satellite.kueche","presence":["binary_sensor.k","binary_sensor.k2"],"morning":true,"mode":"indicate","indicator":"light.ring"},{"area":"flur"}]}`)
	if code != 200 || !strings.Contains(body, `"managed":true`) {
		t.Fatalf("put: %d %s", code, body)
	}
	if !cfg.fromHA["persons"] || !cfg.fromHA["satellites"] || cfg.written["persons"] {
		t.Errorf("what HA sends is not kept apart from what the page writes: ha %v page %v", cfg.fromHA, cfg.written)
	}
	var notifier map[string]any
	json.Unmarshal(cfg.sections["notifier"], &notifier)
	if notifier["cooldown"] != "30m" || !strings.Contains(string(cfg.sections["notifier"]), `"morning_trigger":{"areas":["kueche"],"presence":{"kueche":["binary_sensor.k","binary_sensor.k2"]}}`) {
		t.Errorf("notifier = %s", cfg.sections["notifier"])
	}
	if !strings.Contains(string(cfg.sections["notifier"]), `"room_mode":{"kueche":"indicate"}`) || !strings.Contains(string(cfg.sections["notifier"]), `"indicators":{"kueche":"light.ring"}`) {
		t.Errorf("mode/indicator not stored: %s", cfg.sections["notifier"])
	}
	if code, body := call(t, srv, "GET", "/v1/ui/ha", "secret", ""); code != 200 || !strings.Contains(body, `"mode":"indicate","indicator":"light.ring"`) {
		t.Errorf("rooms read back: %s", body)
	}
	if string(cfg.sections["satellites"]) != `{"kueche":"assist_satellite.kueche"}` || !strings.Contains(string(cfg.sections["persons"]), "person.ben") {
		t.Errorf("satellites %s persons %s", cfg.sections["satellites"], cfg.sections["persons"])
	}

	// The web UI now sees them as managed and cannot change them, but may
	// still change the rest of the notifier.
	if _, body := call(t, srv, "GET", "/v1/ui/config", "secret", ""); !strings.Contains(body, `"managed_by":"home_assistant"`) {
		t.Errorf("config: %s", body)
	}
	if code, _ := call(t, srv, "PUT", "/v1/ui/config", "secret", `{"persons":{}}`); code != 409 {
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
	if code, _ := call(t, srv, "PUT", "/v1/ui/config", "secret", `{"persons":{}}`); code != 200 {
		t.Errorf("after release: %d", code)
	}
}

// The login names of the persons' users come along and are kept; an older
// integration without them changes nothing.
func TestHomeAssistantSendsTheLogins(t *testing.T) {
	srv, cfg, _ := setup(t)
	if code, body := call(t, srv, "PUT", "/v1/ui/ha", "secret", `{"persons":{},"rooms":[],"logins":{"person.stefan":"stefan"}}`); code != 200 {
		t.Fatalf("put: %d %s", code, body)
	}
	if cfg.logins["person.stefan"] != "stefan" {
		t.Errorf("logins = %v", cfg.logins)
	}
	cfg.logins = nil
	call(t, srv, "PUT", "/v1/ui/ha", "secret", `{"persons":{},"rooms":[]}`)
	if cfg.logins != nil {
		t.Errorf("logins set without any: %v", cfg.logins)
	}
}

// A newer integration may send a person field this Mira does not know: the
// sync goes through, the field is left out (docs/spec/versions.md, VE-01).
func TestHomeAssistantFieldsFromANewerIntegration(t *testing.T) {
	srv, cfg, _ := setup(t)
	code, body := call(t, srv, "PUT", "/v1/ui/ha", "secret",
		`{"persons":{"anna":{"entity":"person.anna","alone_is_me":true,"from_the_future":7}},"rooms":[],"later":{"x":1}}`)
	if code != 200 {
		t.Fatalf("put: %d %s", code, body)
	}
	got := string(cfg.sections["persons"])
	if !strings.Contains(got, `"alone_is_me":true`) || !strings.Contains(got, `"entity":"person.anna"`) || strings.Contains(got, "from_the_future") {
		t.Errorf("persons = %s", got)
	}
}

func TestSR20_ARoomWithOthersBecomesAListThePrimaryFirst(t *testing.T) {
	srv, cfg, _ := setup(t)
	cfg.sections["notifier"] = json.RawMessage(`{"cooldown":"30m"}`)
	code, body := call(t, srv, "PUT", "/v1/ui/ha", "secret", `{"rooms":[{"area":"wz","satellite":"assist_satellite.sofa",
		"presence":["binary_sensor.sofa","binary_sensor.flur"],"morning":false,"indicator":"light.sofa_ring",
		"others":[{"satellite":"assist_satellite.essen","presence":["binary_sensor.essen","binary_sensor.flur"],"morning":true,"indicator":"light.essen_ring"},
		          {"satellite":"assist_satellite.fenster","presence":["binary_sensor.terrasse"],"morning":false}]}]}`)
	if code != 200 {
		t.Fatalf("put: %d %s", code, body)
	}
	if got := string(cfg.sections["satellites"]); got != `{"wz":["assist_satellite.sofa","assist_satellite.essen","assist_satellite.fenster"]}` {
		t.Errorf("satellites %s", got)
	}
	var n struct {
		Indicators     map[string][]string `json:"indicators"`
		MorningTrigger struct {
			Areas    []string            `json:"areas"`
			Presence map[string][]string `json:"presence"`
			SpeakAt  map[string]string   `json:"speak_at"`
		} `json:"morning_trigger"`
	}
	if err := json.Unmarshal(cfg.sections["notifier"], &n); err != nil {
		t.Fatal(err)
	}
	// every light of the room
	if strings.Join(n.Indicators["wz"], ",") != "light.sofa_ring,light.essen_ring" {
		t.Errorf("indicators %v", n.Indicators)
	}
	// every sensor of the room; one of an other speaks at it; one the
	// primary has too speaks at the primary
	if strings.Join(n.MorningTrigger.Presence["wz"], ",") != "binary_sensor.sofa,binary_sensor.flur,binary_sensor.essen,binary_sensor.terrasse" {
		t.Errorf("presence %v", n.MorningTrigger.Presence)
	}
	want := map[string]string{"binary_sensor.essen": "assist_satellite.essen", "binary_sensor.terrasse": "assist_satellite.fenster"}
	if len(n.MorningTrigger.SpeakAt) != 2 || n.MorningTrigger.SpeakAt["binary_sensor.essen"] != want["binary_sensor.essen"] ||
		n.MorningTrigger.SpeakAt["binary_sensor.terrasse"] != want["binary_sensor.terrasse"] {
		t.Errorf("speak_at %v", n.MorningTrigger.SpeakAt)
	}
	// one of them announces on entry: the room does
	if strings.Join(n.MorningTrigger.Areas, ",") != "wz" {
		t.Errorf("areas %v", n.MorningTrigger.Areas)
	}
	// read back with the others and their sensors
	if code, body := call(t, srv, "GET", "/v1/ui/ha", "secret", ""); code != 200 ||
		!strings.Contains(body, `"satellite":"assist_satellite.sofa","presence":["binary_sensor.sofa","binary_sensor.flur"]`) ||
		!strings.Contains(body, `{"satellite":"assist_satellite.essen","presence":["binary_sensor.essen"],"morning":false}`) {
		t.Errorf("rooms read back: %s", body)
	}
}

func TestSR21_ARoomWithoutOthersIsTakenAsBefore(t *testing.T) {
	srv, cfg, _ := setup(t)
	cfg.sections["notifier"] = json.RawMessage(`{"cooldown":"30m"}`)
	code, body := call(t, srv, "PUT", "/v1/ui/ha", "secret", `{"rooms":[{"area":"bad","satellite":"assist_satellite.bad",
		"presence":["binary_sensor.bad"],"morning":true,"indicator":"light.bad_ring"}]}`)
	if code != 200 {
		t.Fatalf("put: %d %s", code, body)
	}
	if got := string(cfg.sections["satellites"]); got != `{"bad":"assist_satellite.bad"}` {
		t.Errorf("satellites %s", got)
	}
	n := string(cfg.sections["notifier"])
	if !strings.Contains(n, `"indicators":{"bad":"light.bad_ring"}`) || !strings.Contains(n, `"presence":{"bad":"binary_sensor.bad"}`) ||
		strings.Contains(n, "speak_at") {
		t.Errorf("notifier %s", n)
	}
}
