package config

import (
	"os"
	"path/filepath"
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
`

func TestParseAppliesDefaults(t *testing.T) {
	cfg, err := Parse([]byte(minimalYAML), fakeEnv(map[string]string{
		"HA_TOKEN": "ha-secret", "TELEGRAM_TOKEN": "tg-secret",
	}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Main().MaxToolRounds != 8 || cfg.Main().Privacy != PrivacyLocal || cfg.Main().Parallel != 1 {
		t.Errorf("main = %+v", cfg.Main())
	}
	if got := cfg.SessionTTL(ChannelVoice).Duration(); got != 5*time.Minute {
		t.Errorf("voice ttl = %v, want 5m", got)
	}
	if got := cfg.SlotFor(MainModel, ChannelBackground); got != -1 {
		t.Errorf("background slot = %d, want none without pin_slots", got)
	}
	if cfg.HA.Token != "ha-secret" {
		t.Errorf("ha token = %q, want expanded value", cfg.HA.Token)
	}
}

func TestParseRejectsUnsetEnv(t *testing.T) {
	_, err := Parse([]byte(minimalYAML), fakeEnv(map[string]string{}))
	if err == nil || !strings.Contains(err.Error(), "HA_TOKEN") {
		t.Fatalf("want error naming HA_TOKEN, got %v", err)
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
	smart := cfg.Models["smart"]
	if smart.APIKey != "d" || smart.Privacy != PrivacyCloud || smart.Parallel != 4 || smart.CachePrompt || smart.WarmupOnStart {
		t.Errorf("smart = %+v", smart)
	}
	if cfg.ModelFor("delegate") != "smart" || cfg.ModelFor("conversation.voice") != MainModel {
		t.Errorf("agents' models: delegate %s, voice %s", cfg.ModelFor("delegate"), cfg.ModelFor("conversation.voice"))
	}
}

func TestEnvReferencesInCommentsAreIgnored(t *testing.T) {
	raw := []byte("# set ${UNSET_IN_COMMENT} before starting\nmodels:\n  main: { endpoint: http://x, model: ${MODEL}, privacy: local }\nha:\n  url: http://ha\n  token: t\n")
	cfg, err := Parse(raw, func(k string) (string, bool) {
		if k == "MODEL" {
			return "qwen", true
		}
		return "", false
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Main().Model != "qwen" {
		t.Errorf("model = %q", cfg.Main().Model)
	}
}

func parseModels(t *testing.T, models string) (Config, error) {
	t.Helper()
	return Parse([]byte("ha: { url: http://ha, token: t }\n"+models), func(k string) (string, bool) {
		if k == "GEMINI_API_TOKEN" {
			return "geheim", true
		}
		return "", false
	})
}

// MO-01 A model is a named entry
func TestMO01_AModelIsANamedEntry(t *testing.T) {
	// Given mira.yaml has the models main and smart with endpoint, model, token and privacy
	// When Mira loads it
	cfg, err := parseModels(t, `
models:
  main: { endpoint: "http://lmstudio:1234/v1/", model: qwen, privacy: local }
  smart: { endpoint: https://gemini/v1, model: flash, token: "${GEMINI_API_TOKEN}", privacy: cloud }
`)
	// Then both models exist under their names, the token taken from the environment
	if err != nil {
		t.Fatal(err)
	}
	main, smart := cfg.Models["main"], cfg.Models["smart"]
	if main.BaseURL != "http://lmstudio:1234/v1" || main.Model != "qwen" || smart.APIKey != "geheim" || smart.Privacy != PrivacyCloud {
		t.Errorf("main %+v\nsmart %+v", main, smart)
	}
}

// MO-02 Without main in the file, main comes from the integration
func TestMO02_WithoutMainInTheFileMainComesFromTheIntegration(t *testing.T) {
	// Given mira.yaml has models but none called main
	// When Mira loads it
	cfg, err := parseModels(t, "models:\n  smart: { endpoint: http://x, model: y, privacy: cloud }\n")
	// Then it loads with smart, and says that main has to come from the setup
	if err != nil || !cfg.LacksMain() || cfg.Models["smart"].Model != "y" {
		t.Errorf("err = %v, lacks main %v, models %v", err, cfg.LacksMain(), cfg.Models)
	}
	// And the file does not count as naming the model main
	path := filepath.Join(t.TempDir(), "mira.yaml")
	os.WriteFile(path, []byte("models:\n  smart: { endpoint: http://x, model: y, privacy: cloud }\n"), 0o644)
	if _, hasMain, err := LoadOptional(path); err != nil || hasMain {
		t.Errorf("has main %v, err %v", hasMain, err)
	}
}

// MO-03 A model needs its privacy
func TestMO03_AModelNeedsItsPrivacy(t *testing.T) {
	// Given a model without privacy
	// When Mira loads it
	_, err := parseModels(t, "models:\n  main: { endpoint: http://x, model: y }\n")
	// Then loading fails, and the error names the model and that privacy is local or cloud
	if err == nil || !strings.Contains(err.Error(), "models.main needs privacy: local") {
		t.Errorf("err = %v", err)
	}
}

// MO-04 The old llm and routing are refused with what to write instead
func TestMO04_TheOldLLMAndRoutingAreRefused(t *testing.T) {
	// Given mira.yaml still has llm or routing
	// When Mira loads it
	_, err := parseModels(t, "llm: { base_url: http://x, model: y }\nrouting: { delegate: smart }\n")
	// Then loading fails, and the error says to write models.main instead of llm, agents.<name>.model instead of routing
	if err == nil || !strings.Contains(err.Error(), "llm is now models.main") || !strings.Contains(err.Error(), "routing is gone") {
		t.Errorf("err = %v", err)
	}
}

// MO-06 Settings a model leaves out come from main
func TestMO06_SettingsAModelLeavesOutComeFromMain(t *testing.T) {
	// Given main with max_tokens 1024 and a model smart without max_tokens
	cfg, err := parseModels(t, `
models:
  main: { endpoint: http://x, model: y, privacy: local, max_tokens: 1024, cache_prompt: true, pin_slots: { voice: 0 } }
  smart: { endpoint: http://z, model: w, privacy: cloud }
`)
	if err != nil {
		t.Fatal(err)
	}
	// When smart is called
	// Then it is called with max_tokens 1024
	smart := cfg.Models["smart"]
	if smart.MaxTokens != 1024 || smart.CachePrompt || len(smart.PinSlots) != 0 {
		t.Errorf("smart = %+v", smart)
	}
	if cfg.SlotFor("main", ChannelVoice) != 0 || cfg.SlotFor("smart", ChannelVoice) != -1 {
		t.Error("slots are not each model's own")
	}
}

// MO-32 A circle of fallbacks does not load
func TestMO32_ACircleOfFallbacksDoesNotLoad(t *testing.T) {
	// Given smart with fallback lite and lite with fallback smart
	// When Mira loads it
	_, err := parseModels(t, `
models:
  main: { endpoint: http://x, model: y, privacy: local }
  smart: { endpoint: http://x, model: y, privacy: cloud, fallback: lite }
  lite: { endpoint: http://x, model: y, privacy: cloud, fallback: smart }
`)
	// Then loading fails, and the error names the circle
	if err == nil || !strings.Contains(err.Error(), "lite → smart → lite") {
		t.Errorf("err = %v", err)
	}
	if _, err := parseModels(t, "models:\n  main: { endpoint: http://x, model: y, privacy: local, fallback: nope }\n"); err == nil ||
		!strings.Contains(err.Error(), `models.main.fallback: no model "nope"`) {
		t.Errorf("unknown fallback: %v", err)
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

// Under agents, max_depth sits beside the agents described in YAML; an agent
// with a mistake does not stop the config from loading (it is checked on its
// own later), and the default depth stays when max_depth is not given.
func TestAgentsBesideMaxDepth(t *testing.T) {
	env := fakeEnv(map[string]string{"HA_TOKEN": "x", "TELEGRAM_TOKEN": "y"})
	cfg, err := Parse([]byte(minimalYAML+`
agents:
  max_depth: 2
  rezept:
    description: Zerlegt Rezepte.
    instructions: Zerlege.
    tools: [web_page]
  kaputt:
    descripton: Tippfehler
`), env)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Agents.MaxDepth != 2 || len(cfg.Agents.Defined) != 2 {
		t.Fatalf("agents %+v", cfg.Agents)
	}
	node := cfg.Agents.Defined["rezept"]
	d, err := DecodeAgent(&node)
	if err != nil || d.Description != "Zerlegt Rezepte." || len(d.Tools.Names) != 1 {
		t.Errorf("rezept %+v, %v", d, err)
	}
	broken := cfg.Agents.Defined["kaputt"]
	if _, err := DecodeAgent(&broken); err == nil {
		t.Error("a misspelt field was taken")
	}
	cfg, _ = Parse([]byte(minimalYAML+"agents:\n  rezept: { description: x }\n"), env)
	if cfg.Agents.MaxDepth != 3 {
		t.Errorf("max_depth = %d, want the default 3", cfg.Agents.MaxDepth)
	}
}

// An old telegram section is refused with a hint, not ignored with its token.
func TestOldTelegramSectionIsRefused(t *testing.T) {
	_, err := Parse([]byte(minimalYAML+"telegram:\n  token: x\n"), fakeEnv(map[string]string{"HA_TOKEN": "x"}))
	if err == nil || !strings.Contains(err.Error(), "telegram is gone") {
		t.Errorf("err = %v", err)
	}
}

// HK-62 The guard's old settings are no longer read
func TestHK62_TheGuardsOldSettingsAreNoLongerRead(t *testing.T) {
	cfg, err := Parse([]byte(minimalYAML+`
security:
  confirm_domains: [lock]
  confirm_ttl: 2m
`), fakeEnv(map[string]string{"HA_TOKEN": "x"}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := cfg.Security.Unread(); len(got) != 1 || got[0] != "confirm_domains" {
		t.Errorf("unread = %v", got)
	}
	if cfg.Security.ConfirmTTL.Duration() != 2*time.Minute {
		t.Errorf("confirm_ttl = %v", cfg.Security.ConfirmTTL)
	}
	if len(Default().Security.Unread()) != 0 {
		t.Error("the defaults set a key that is no longer read")
	}
}
