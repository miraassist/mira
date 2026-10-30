package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/testsupport/fakeha"
	"github.com/miraassist/mira/internal/testsupport/fakellm"
)

// TestUIChangesApplyWithoutRestart edits the notifier and the satellites
// through the web UI's API while the agent runs: the change goes into
// mira.yaml, and the notifier comes back with its subscriptions.
func TestUIChangesApplyWithoutRestart(t *testing.T) {
	model := fakellm.New()
	defer model.Close()
	ha := fakeha.New()
	defer ha.Close()
	ha.SetRegistry([]haws.Area{{AreaID: "bad", Name: "Bad"}, {AreaID: "flur", Name: "Flur"}}, nil, nil)
	ha.SetStates([]haws.State{
		{EntityID: "binary_sensor.bad_motion", State: "off"},
		{EntityID: "binary_sensor.flur_motion", State: "off"},
		{EntityID: "binary_sensor.leck", State: "off"},
	})

	cfg := config.Default()
	cfg.Server = config.Server{Listen: freeAddr(t), Token: "api-token"}
	setMain(&cfg, func(m *config.LLM) { m.BaseURL = model.BaseURL() })
	cfg.Store.Path = filepath.Join(t.TempDir(), "mira.db")
	seedExposed(t, cfg.Store.Path, "assist_satellite.bad", "binary_sensor.bad_motion", "binary_sensor.flur_motion", "binary_sensor.leck")
	cfg.HA = config.HA{URL: ha.URL, Token: ha.Token, StructureDebounce: config.Duration(time.Second)}
	cfg.Telemetry.LogLevel = "error"
	cfg.Memory.ProfileSchedule = ""
	cfg.Satellites = map[string]string{"bad": "assist_satellite.bad"}
	cfg.Notifier.MorningTrigger = config.MorningTrigger{Areas: []string{"bad"}, Presence: map[string]config.Entities{"bad": {"binary_sensor.bad_motion"}}}

	path := filepath.Join(t.TempDir(), "mira.yaml")
	if err := os.WriteFile(path, []byte(editConfig+"satellites:\n  bad: assist_satellite.bad\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	start := func() (stop func()) {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- runAgent(ctx, cfg, prepared{path: path}) }()
		return func() {
			http.DefaultClient.CloseIdleConnections()
			cancel()
			select {
			case <-done:
			case <-time.After(15 * time.Second):
				t.Error("agent did not shut down")
			}
		}
	}
	api := func(method, path, body string) (int, string) {
		req, _ := http.NewRequest(method, "http://"+cfg.Server.Listen+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer api-token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, err.Error()
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}

	stop := start()
	waitUntil(t, "the API", func() bool { code, _ := api("GET", "/v1/health", ""); return code == 200 })
	waitUntil(t, "presence subscriptions", func() bool { return ha.SubscriptionCount() >= 1 })
	time.Sleep(200 * time.Millisecond) // let the structure cache subscribe too
	before := ha.SubscriptionCount()

	if code, body := api("GET", "/ui", ""); code != 200 || !strings.Contains(body, "<title>Mira</title>") {
		t.Fatalf("page: %d", code)
	}
	// A second room: satellite and presence sensor, saved together.
	code, body := api("PUT", "/v1/ui/config", `{
		"satellites": {"bad": "assist_satellite.bad", "flur": "assist_satellite.flur"},
		"notifier": {"quiet_hours": {"from": "23:00", "to": "06:00"},
			"morning_trigger": {"areas": ["bad", "flur"], "presence": {"bad": "binary_sensor.bad_motion", "flur": "binary_sensor.flur_motion"}}}}`)
	if code != 200 || !strings.Contains(body, `"23:00"`) {
		t.Fatalf("put: %d %s", code, body)
	}
	// Two presence sensors after the restart of the notifier. The old
	// subscriptions are gone, the new presence sensor is added.
	waitUntil(t, "subscriptions after the change", func() bool { return ha.SubscriptionCount() == before+1 })

	// An invalid change is refused and changes nothing.
	if code, body := api("PUT", "/v1/ui/config", `{"notifier": {"morning_trigger": {"areas": ["keller"]}}}`); code != 422 || !strings.Contains(body, "keller") {
		t.Fatalf("invalid change: %d %s", code, body)
	}
	stop()

	// The change is in mira.yaml, so it holds after a restart.
	saved, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Notifier.QuietHours.From.String() != "23:00" || saved.Satellites["flur"] != "assist_satellite.flur" {
		t.Errorf("file: quiet hours %v, satellites %v", saved.Notifier.QuietHours, saved.Satellites)
	}
}
