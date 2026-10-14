// Package config loads and validates the agent's YAML configuration.
//
// Secrets are never stored in the file itself; they are referenced as ${ENV}
// and expanded at load time. Load always returns a fully defaulted Config, so
// the rest of the agent can read fields without nil or zero checks.
package config

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Channel names a conversation channel. Each one gets its own llama.cpp slot
// and its own style layer, so the cached prompt prefix stays stable per channel.
type Channel string

const (
	ChannelVoice      Channel = "voice"
	ChannelTelegram   Channel = "telegram"
	ChannelBackground Channel = "background"
)

type Config struct {
	Server     Server            `yaml:"server"`
	LLM        LLM               `yaml:"llm"`
	Store      Store             `yaml:"store"`
	Prompt     Prompt            `yaml:"prompt"`
	HA         HA                `yaml:"ha"`
	MCPServers []MCPServer       `yaml:"mcp_servers"`
	Telegram   Telegram          `yaml:"telegram"`
	Sessions   Sessions          `yaml:"sessions"`
	Satellites map[string]string `yaml:"satellites"`
	Watchers   []Watcher         `yaml:"watchers"`
	Notifier   Notifier          `yaml:"notifier"`
	Security   Security          `yaml:"security"`
	Memory     Memory            `yaml:"memory"`
	Telemetry  Telemetry         `yaml:"telemetry"`
}

type Server struct {
	Listen string `yaml:"listen"`
	// Token authenticates the HA integration against the agent. Empty disables
	// the check, which is only sensible for local development.
	Token string `yaml:"token"`
}

type LLM struct {
	BaseURL string `yaml:"base_url"`
	Model   string `yaml:"model"`
	APIKey  string `yaml:"api_key"`
	// Slots pins each channel to a llama.cpp slot so its KV cache survives
	// between turns. Ignored when SlotPinning is false.
	Slots map[Channel]int `yaml:"slots"`
	// SlotPinning and CachePrompt are llama.cpp extensions to the OpenAI API.
	// Turn them off to talk to a server that rejects unknown fields
	// (LM Studio, vLLM, the OpenAI API itself).
	SlotPinning   bool     `yaml:"slot_pinning"`
	CachePrompt   bool     `yaml:"cache_prompt"`
	MaxToolRounds int      `yaml:"max_tool_rounds"`
	Temperature   *float64 `yaml:"temperature"`
	MaxTokens     int      `yaml:"max_tokens"`
	Timeout       Duration `yaml:"timeout"`
	// ContextTokens is the per-slot context size. History compaction starts at
	// CompactAtFraction of it.
	ContextTokens        int     `yaml:"context_tokens"`
	CompactAtFraction    float64 `yaml:"compact_at_fraction"`
	WarmupOnStart        bool    `yaml:"warmup_on_start"`
	WarmupOnPrefixChange bool    `yaml:"warmup_on_prefix_change"`
}

type Store struct {
	Path string `yaml:"path"`
}

type Prompt struct {
	// Persona is appended to the built-in layer 1 text. It is part of the
	// cached prefix, so it must not contain anything volatile.
	Persona string `yaml:"persona"`
	// Language is the language the agent answers in by default.
	Language string `yaml:"language"`
}

type HA struct {
	URL   string `yaml:"url"`
	Token string `yaml:"token"`
	// StructureDebounce collapses bursts of registry updates into one rebuild.
	StructureDebounce Duration `yaml:"structure_debounce"`
	Enabled           bool     `yaml:"enabled"`
}

type MCPTransport string

const (
	TransportHTTP  MCPTransport = "http"
	TransportStdio MCPTransport = "stdio"
)

type MCPServer struct {
	Name      string       `yaml:"name"`
	Transport MCPTransport `yaml:"transport"`
	URL       string       `yaml:"url"`
	Token     string       `yaml:"token"`
	Command   string       `yaml:"command"`
	Args      []string     `yaml:"args"`
	Env       []string     `yaml:"env"`
	// ReadOnly marks every tool of this server as safe for background jobs.
	// Servers that are not read-only are hidden from watchers entirely.
	ReadOnly bool `yaml:"read_only"`
	// WebContent marks servers that return untrusted third-party text
	// (search, fetch). Using one locks critical tools for the rest of the turn.
	WebContent bool `yaml:"web_content"`
	Disabled   bool `yaml:"disabled"`
}

type Telegram struct {
	Token          string   `yaml:"token"`
	AllowedUserIDs []int64  `yaml:"allowed_user_ids"`
	PollTimeout    Duration `yaml:"poll_timeout"`
	Enabled        bool     `yaml:"enabled"`
	APIBaseURL     string   `yaml:"api_base_url"`
}

type Sessions struct {
	VoiceTTL    Duration `yaml:"voice_ttl"`
	TelegramTTL Duration `yaml:"telegram_ttl"`
}

type Watcher struct {
	Name      string        `yaml:"name"`
	Trigger   *StateTrigger `yaml:"trigger"`
	Schedule  string        `yaml:"schedule"`
	Kind      string        `yaml:"kind"`
	DedupeKey string        `yaml:"dedupe_key"`
}

type StateTrigger struct {
	EntityID []string `yaml:"entity_id"`
	To       []string `yaml:"to"`
	From     []string `yaml:"from"`
}

// UnmarshalYAML accepts entity_id as either a single string or a list.
func (t *StateTrigger) UnmarshalYAML(value *yaml.Node) error {
	var raw struct {
		EntityID yaml.Node `yaml:"entity_id"`
		To       []string  `yaml:"to"`
		From     []string  `yaml:"from"`
	}
	if err := value.Decode(&raw); err != nil {
		return err
	}
	t.To, t.From = raw.To, raw.From
	if raw.EntityID.IsZero() {
		return nil
	}
	var one string
	if err := raw.EntityID.Decode(&one); err == nil {
		t.EntityID = []string{one}
		return nil
	}
	return raw.EntityID.Decode(&t.EntityID)
}

type Notifier struct {
	QuietHours              QuietHours     `yaml:"quiet_hours"`
	MorningTrigger          MorningTrigger `yaml:"morning_trigger"`
	Cooldown                Duration       `yaml:"cooldown"`
	MaxItemsPerAnnouncement int            `yaml:"max_items_per_announcement"`
	EscalateToTelegram      Escalation     `yaml:"escalate_to_telegram"`
	// FollowUpWindow is how long after an announcement a turn from the same
	// satellite is treated as a reply to it.
	FollowUpWindow Duration `yaml:"follow_up_window"`
	MinImportance  int      `yaml:"min_importance"`
}

type QuietHours struct {
	From ClockTime `yaml:"from"`
	To   ClockTime `yaml:"to"`
}

type MorningTrigger struct {
	Areas    []string          `yaml:"areas"`
	Presence map[string]string `yaml:"presence"`
}

type Escalation struct {
	MinImportance int       `yaml:"min_importance"`
	After         ClockTime `yaml:"after"`
}

type Security struct {
	ConfirmDomains  []string `yaml:"confirm_domains"`
	ConfirmEntities []string `yaml:"confirm_entities"`
	// ConfirmTools names agent-level tools (after prefixing) that always need
	// a confirmation, regardless of their arguments.
	ConfirmTools []string `yaml:"confirm_tools"`
	// ConfirmTTL is how long a pending confirmation stays answerable.
	ConfirmTTL Duration `yaml:"confirm_ttl"`
	// ReadOnlyTools are qualified tool names that only read, for servers that
	// do not annotate their tools. Only read-only tools reach background jobs.
	ReadOnlyTools []string `yaml:"read_only_tools"`
}

type Memory struct {
	// ProfileSchedule is the cron expression for the nightly profile rebuild.
	ProfileSchedule string `yaml:"profile_schedule"`
	MaxFactsInLayer int    `yaml:"max_facts_in_layer"`
	MaxProfileChars int    `yaml:"max_profile_chars"`
}

type Telemetry struct {
	OTLPEndpoint string `yaml:"otlp_endpoint"`
	ServiceName  string `yaml:"service_name"`
	// CaptureContent controls whether prompts and completions are recorded on
	// spans. Off by default: the content is the user's private household data.
	CaptureContent bool   `yaml:"capture_content"`
	LogLevel       string `yaml:"log_level"`
}

// Default returns the configuration the agent runs with when the YAML says
// nothing. Every Load result starts from this.
func Default() Config {
	temp := 0.3
	return Config{
		Server: Server{Listen: "127.0.0.1:8787"},
		LLM: LLM{
			BaseURL:              "http://localhost:8080/v1",
			Model:                "local",
			Slots:                map[Channel]int{ChannelVoice: 0, ChannelTelegram: 1, ChannelBackground: 2},
			SlotPinning:          true,
			CachePrompt:          true,
			MaxToolRounds:        8,
			Temperature:          &temp,
			MaxTokens:            1024,
			Timeout:              Duration(120e9), // 2m
			ContextTokens:        16384,
			CompactAtFraction:    0.75,
			WarmupOnStart:        true,
			WarmupOnPrefixChange: true,
		},
		Store:  Store{Path: "lares.db"},
		Prompt: Prompt{Language: "de"},
		HA: HA{
			Enabled:           true,
			StructureDebounce: Duration(30e9), // 30s
		},
		Telegram: Telegram{
			Enabled:     true,
			PollTimeout: Duration(50e9), // 50s
			APIBaseURL:  "https://api.telegram.org",
		},
		Sessions: Sessions{
			VoiceTTL:    Duration(5 * 60e9),
			TelegramTTL: Duration(12 * 3600e9),
		},
		Notifier: Notifier{
			QuietHours:              QuietHours{From: ClockTime{22, 30}, To: ClockTime{6, 30}},
			Cooldown:                Duration(30 * 60e9),
			MaxItemsPerAnnouncement: 3,
			EscalateToTelegram:      Escalation{MinImportance: 2, After: ClockTime{9, 0}},
			FollowUpWindow:          Duration(2 * 60e9),
			MinImportance:           1,
		},
		Security: Security{
			ConfirmDomains: []string{"lock", "alarm_control_panel"},
			ConfirmTTL:     Duration(5 * 60e9),
			ReadOnlyTools: []string{
				"ha__GetDateTime", "ha__GetLiveContext", "ha__HassGetWeather", "ha__todo_get_items",
			},
		},
		Memory: Memory{
			ProfileSchedule: "30 3 * * *",
			MaxFactsInLayer: 8,
			MaxProfileChars: 2000,
		},
		Telemetry: Telemetry{ServiceName: "lares", LogLevel: "info"},
	}
}

// Load reads, expands and validates a config file.
func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	return Parse(raw, OSLookup)
}

// Parse is Load without the filesystem, so tests can drive it directly.
func Parse(raw []byte, lookup Lookup) (Config, error) {
	expanded, err := expandEnv(raw, lookup)
	if err != nil {
		return Config{}, err
	}
	cfg := Default()
	dec := yaml.NewDecoder(strings.NewReader(string(expanded)))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && err.Error() != "EOF" {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	cfg.normalize()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) normalize() {
	c.LLM.BaseURL = strings.TrimRight(c.LLM.BaseURL, "/")
	c.HA.URL = strings.TrimRight(c.HA.URL, "/")
	c.Telegram.APIBaseURL = strings.TrimRight(c.Telegram.APIBaseURL, "/")
	for i := range c.MCPServers {
		if c.MCPServers[i].Transport == "" {
			if c.MCPServers[i].Command != "" {
				c.MCPServers[i].Transport = TransportStdio
			} else {
				c.MCPServers[i].Transport = TransportHTTP
			}
		}
	}
	for i, d := range c.Security.ConfirmDomains {
		c.Security.ConfirmDomains[i] = strings.ToLower(strings.TrimSpace(d))
	}
	for i, e := range c.Security.ConfirmEntities {
		c.Security.ConfirmEntities[i] = strings.ToLower(strings.TrimSpace(e))
	}
	if c.Satellites == nil {
		c.Satellites = map[string]string{}
	}
}

func (c Config) Validate() error {
	var errs []string
	add := func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) }

	if c.Server.Listen == "" {
		add("server.listen must not be empty")
	}
	if c.LLM.BaseURL == "" {
		add("llm.base_url must not be empty")
	}
	if c.LLM.Model == "" {
		add("llm.model must not be empty")
	}
	if c.LLM.MaxToolRounds < 1 {
		add("llm.max_tool_rounds must be at least 1")
	}
	if c.LLM.ContextTokens < 512 {
		add("llm.context_tokens must be at least 512")
	}
	if c.LLM.CompactAtFraction <= 0 || c.LLM.CompactAtFraction >= 1 {
		add("llm.compact_at_fraction must be between 0 and 1 (exclusive)")
	}
	if c.Store.Path == "" {
		add("store.path must not be empty")
	}
	if c.HA.Enabled {
		if c.HA.URL == "" {
			add("ha.url must be set when ha.enabled is true")
		}
		if c.HA.Token == "" {
			add("ha.token must be set when ha.enabled is true")
		}
	}
	if c.Telegram.Enabled && c.Telegram.Token == "" {
		add("telegram.token must be set when telegram.enabled is true")
	}
	if c.Telegram.Enabled && len(c.Telegram.AllowedUserIDs) == 0 {
		add("telegram.allowed_user_ids must list at least one user; an open bot is not acceptable")
	}

	seen := map[string]bool{}
	for i, s := range c.MCPServers {
		switch {
		case s.Name == "":
			add("mcp_servers[%d].name must not be empty", i)
		case seen[s.Name]:
			add("mcp_servers[%d]: duplicate name %q", i, s.Name)
		case strings.Contains(s.Name, "__"):
			add("mcp_servers[%d]: name %q must not contain \"__\"; it separates server and tool", i, s.Name)
		}
		seen[s.Name] = true
		switch s.Transport {
		case TransportHTTP:
			if s.URL == "" {
				add("mcp_servers[%d] (%s): url is required for transport http", i, s.Name)
			}
		case TransportStdio:
			if s.Command == "" {
				add("mcp_servers[%d] (%s): command is required for transport stdio", i, s.Name)
			}
		default:
			add("mcp_servers[%d] (%s): unknown transport %q", i, s.Name, s.Transport)
		}
	}

	for _, ch := range []Channel{ChannelVoice, ChannelTelegram, ChannelBackground} {
		if _, ok := c.LLM.Slots[ch]; !ok && c.LLM.SlotPinning {
			add("llm.slots is missing channel %q", ch)
		}
	}

	names := map[string]bool{}
	for i, w := range c.Watchers {
		if w.Name == "" {
			add("watchers[%d].name must not be empty", i)
		}
		if names[w.Name] {
			add("watchers[%d]: duplicate name %q", i, w.Name)
		}
		names[w.Name] = true
		if w.Trigger == nil && w.Schedule == "" {
			add("watchers[%d] (%s): needs either trigger or schedule", i, w.Name)
		}
		if w.Trigger != nil && len(w.Trigger.EntityID) == 0 {
			add("watchers[%d] (%s): trigger needs at least one entity_id", i, w.Name)
		}
	}

	for area, sat := range c.Satellites {
		if !strings.HasPrefix(sat, "assist_satellite.") {
			add("satellites[%s]: %q is not an assist_satellite entity", area, sat)
		}
	}

	if c.Notifier.MaxItemsPerAnnouncement < 1 {
		add("notifier.max_items_per_announcement must be at least 1")
	}

	if len(errs) == 0 {
		return nil
	}
	sort.Strings(errs)
	return fmt.Errorf("invalid config:\n  - %s", strings.Join(errs, "\n  - "))
}

// SlotFor returns the llama.cpp slot for a channel, or -1 when slot pinning is
// off. A negative slot means "let the server choose".
func (c Config) SlotFor(ch Channel) int {
	if !c.LLM.SlotPinning {
		return -1
	}
	if slot, ok := c.LLM.Slots[ch]; ok {
		return slot
	}
	return -1
}

// SessionTTL returns how long a session of this channel survives inactivity.
func (c Config) SessionTTL(ch Channel) Duration {
	if ch == ChannelTelegram {
		return c.Sessions.TelegramTTL
	}
	return c.Sessions.VoiceTTL
}
