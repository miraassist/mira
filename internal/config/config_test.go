package config

import (
	"os"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func fakeEnv(pairs map[string]string) Lookup {
	return func(name string) (string, bool) {
		v, ok := pairs[name]
		return v, ok
	}
}

const minimalYAML = `
ha:
  url: "http://ha.local:8123"
  token: ${HA_TOKEN}
telegram:
  token: ${TELEGRAM_TOKEN}
  allowed_user_ids: [42]
`

func TestParseAppliesDefaults(t *testing.T) {
	cfg, err := Parse([]byte(minimalYAML), fakeEnv(map[string]string{
		"HA_TOKEN": "ha-secret", "TELEGRAM_TOKEN": "tg-secret",
	}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.LLM.MaxToolRounds != 8 {
		t.Errorf("max_tool_rounds = %d, want 8", cfg.LLM.MaxToolRounds)
	}
	if got := cfg.SessionTTL(ChannelVoice).Duration(); got != 5*time.Minute {
		t.Errorf("voice ttl = %v, want 5m", got)
	}
	if got := cfg.SlotFor(ChannelBackground); got != 2 {
		t.Errorf("background slot = %d, want 2", got)
	}
	if cfg.HA.Token != "ha-secret" {
		t.Errorf("ha token = %q, want expanded value", cfg.HA.Token)
	}
}

func TestParseRejectsUnsetEnv(t *testing.T) {
	_, err := Parse([]byte(minimalYAML), fakeEnv(map[string]string{"HA_TOKEN": "x"}))
	if err == nil || !strings.Contains(err.Error(), "TELEGRAM_TOKEN") {
		t.Fatalf("want error naming TELEGRAM_TOKEN, got %v", err)
	}
}

func TestParseEnvFallback(t *testing.T) {
	cfg, err := Parse([]byte(`
ha: { url: "http://ha", token: t }
server:
  listen: ${MIRA_LISTEN:-0.0.0.0:9999}
`), fakeEnv(nil))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Server.Listen != "0.0.0.0:9999" {
		t.Errorf("listen = %q, want the fallback", cfg.Server.Listen)
	}
}

func TestParseRejectsUnknownFields(t *testing.T) {
	_, err := Parse([]byte("ha: { url: \"http://ha\", token: t, nonsense: 1 }\n"), fakeEnv(nil))
	if err == nil || !strings.Contains(err.Error(), "nonsense") {
		t.Fatalf("want error about the unknown field, got %v", err)
	}
}

func TestValidateRejectsServerNameWithSeparator(t *testing.T) {
	_, err := Parse([]byte(`
ha: { url: "http://ha", token: t }
mcp_servers:
  - name: "a__b"
    transport: http
    url: "http://x/mcp"
`), fakeEnv(nil))
	if err == nil || !strings.Contains(err.Error(), `must not contain`) {
		t.Fatalf("want separator error, got %v", err)
	}
}

// Persons and satellites come from Home Assistant: a config without them
// passes.
func TestValidatePersonsAndSatellitesFromHA(t *testing.T) {
	if _, err := Parse([]byte(`
ha: { url: "http://ha", token: t }
notifier:
  confidential_voice: when_alone
  morning_trigger:
    areas: [wohnzimmer]
    presence: { wohnzimmer: binary_sensor.pir }
`), fakeEnv(nil)); err != nil {
		t.Fatal(err)
	}
}

func TestClockTimeAndDuration(t *testing.T) {
	cfg, err := Parse([]byte(`
ha: { url: "http://ha", token: t }
notifier:
  quiet_hours: { from: "23:15", to: "07:00" }
  cooldown: 45m
`), fakeEnv(nil))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := cfg.Notifier.QuietHours.From.MinutesOfDay(); got != 23*60+15 {
		t.Errorf("quiet from = %d", got)
	}
	if got := cfg.Notifier.Cooldown.Duration(); got != 45*time.Minute {
		t.Errorf("cooldown = %v", got)
	}
}

func TestSlotPinningOffYieldsNegativeSlot(t *testing.T) {
	cfg, err := Parse([]byte(`
ha: { url: "http://ha", token: t }
llm:
  slot_pinning: false
  cache_prompt: false
`), fakeEnv(nil))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := cfg.SlotFor(ChannelVoice); got != -1 {
		t.Errorf("slot = %d, want -1 when pinning is off", got)
	}
}

// The shipped example must always load, so it cannot rot as the config grows.
func TestExampleConfigLoads(t *testing.T) {
	raw, err := os.ReadFile("../../mira.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"MIRA_API_TOKEN": "a", "HA_TOKEN": "b", "TELEGRAM_TOKEN": "c", "GEMINI_API_TOKEN": "d"}
	cfg, err := Parse(raw, func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	if err != nil {
		t.Fatalf("example config: %v", err)
	}
	if len(cfg.MCPServers) != 2 || !cfg.MCPServers[1].WebContent || cfg.HA.Token != "b" {
		t.Errorf("example parsed wrong: %+v", cfg.MCPServers)
	}
	// Further models inherit what they leave out from main, but never the
	// local-only settings.
	smart := cfg.Models["smart"]
	if smart.MaxToolRounds != cfg.LLM.MaxToolRounds || smart.Timeout != cfg.LLM.Timeout || smart.CachePrompt || smart.WarmupOnStart {
		t.Errorf("smart = %+v", smart)
	}
	if cfg.RouteFor(ChannelBackground) != "lite" || cfg.RouteFor(ChannelVoice) != MainModel || cfg.SlotFor(ChannelBackground) != -1 {
		t.Error("routing parsed wrong")
	}
}

func TestEnvReferencesInCommentsAreIgnored(t *testing.T) {
	raw := []byte("# set ${UNSET_IN_COMMENT} before starting\nllm:\n  model: ${MODEL}\nha:\n  url: http://ha\n  token: t\n")
	cfg, err := Parse(raw, func(k string) (string, bool) {
		if k == "MODEL" {
			return "qwen", true
		}
		return "", false
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.Model != "qwen" {
		t.Errorf("model = %q", cfg.LLM.Model)
	}
}

func TestRoutingNeedsKnownModels(t *testing.T) {
	c := Default()
	c.HA.URL, c.HA.Token = "http://ha", "t"
	c.Routing.Delegate = "nope"
	c.Models = map[string]LLM{"main": {BaseURL: "x", Model: "y"}, "half": {Model: "y"}}
	err := c.Validate()
	for _, want := range []string{`routing.delegate: no model "nope"`, "models.main: the name main", "models.half needs base_url"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %v", want, err)
		}
	}
}

// A presence sensor may be one entity or a list of them.
func TestPresenceOneOrMany(t *testing.T) {
	var mt MorningTrigger
	raw := "areas: [bad]\npresence:\n  bad: binary_sensor.bad\n  wohnzimmer: [binary_sensor.flur, binary_sensor.wz]\n"
	if err := yaml.Unmarshal([]byte(raw), &mt); err != nil {
		t.Fatal(err)
	}
	if got := mt.Presence["bad"]; len(got) != 1 || got[0] != "binary_sensor.bad" {
		t.Errorf("one: %v", got)
	}
	if got := mt.Presence["wohnzimmer"]; len(got) != 2 {
		t.Errorf("many: %v", got)
	}
}

// Home Assistant's Assist MCP server gets the satellite by default; others
// only when asked.
func TestDeviceContextByDefaultOnlyForHomeAssistant(t *testing.T) {
	no, yes := false, true
	for name, c := range map[string]struct {
		s    MCPServer
		want bool
	}{
		"assist url":     {MCPServer{URL: "http://ha:8123/api/mcp/assist"}, true},
		"with a slash":   {MCPServer{URL: "http://ha:8123/api/mcp/assist/"}, true},
		"search":         {MCPServer{URL: "http://localhost:9100/mcp"}, false},
		"stdio":          {MCPServer{Command: "node"}, false},
		"explicitly on":  {MCPServer{URL: "http://x/mcp", DeviceContext: &yes}, true},
		"explicitly off": {MCPServer{URL: "http://ha:8123/api/mcp/assist", DeviceContext: &no}, false},
	} {
		if got := c.s.WantsDevice(); got != c.want {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}
