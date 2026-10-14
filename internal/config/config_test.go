package config

import (
	"strings"
	"testing"
	"time"
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
ha: { enabled: false }
telegram: { enabled: false }
server:
  listen: ${LARES_LISTEN:-0.0.0.0:9999}
`), fakeEnv(nil))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Server.Listen != "0.0.0.0:9999" {
		t.Errorf("listen = %q, want the fallback", cfg.Server.Listen)
	}
}

func TestParseRejectsUnknownFields(t *testing.T) {
	_, err := Parse([]byte("ha: { enabled: false, nonsense: 1 }\ntelegram: { enabled: false }\n"), fakeEnv(nil))
	if err == nil || !strings.Contains(err.Error(), "nonsense") {
		t.Fatalf("want error about the unknown field, got %v", err)
	}
}

func TestValidateRequiresTelegramAllowlist(t *testing.T) {
	_, err := Parse([]byte(`
ha: { enabled: false }
telegram:
  token: "abc"
`), fakeEnv(nil))
	if err == nil || !strings.Contains(err.Error(), "allowed_user_ids") {
		t.Fatalf("want allowlist error, got %v", err)
	}
}

func TestValidateRejectsServerNameWithSeparator(t *testing.T) {
	_, err := Parse([]byte(`
ha: { enabled: false }
telegram: { enabled: false }
mcp_servers:
  - name: "a__b"
    transport: http
    url: "http://x/mcp"
`), fakeEnv(nil))
	if err == nil || !strings.Contains(err.Error(), `must not contain`) {
		t.Fatalf("want separator error, got %v", err)
	}
}

func TestStateTriggerAcceptsScalarAndList(t *testing.T) {
	cfg, err := Parse([]byte(`
ha: { enabled: false }
telegram: { enabled: false }
watchers:
  - name: one
    trigger: { entity_id: sensor.a, to: [fault] }
  - name: many
    trigger: { entity_id: [sensor.b, sensor.c] }
`), fakeEnv(nil))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := cfg.Watchers[0].Trigger.EntityID; len(got) != 1 || got[0] != "sensor.a" {
		t.Errorf("scalar entity_id = %v", got)
	}
	if got := cfg.Watchers[1].Trigger.EntityID; len(got) != 2 {
		t.Errorf("list entity_id = %v", got)
	}
}

func TestClockTimeAndDuration(t *testing.T) {
	cfg, err := Parse([]byte(`
ha: { enabled: false }
telegram: { enabled: false }
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
ha: { enabled: false }
telegram: { enabled: false }
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
