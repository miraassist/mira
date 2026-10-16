// Package config loads and validates the agent's YAML configuration.
//
// Secrets are never stored in the file itself; they are referenced as ${ENV}
// and expanded at load time. Load always returns a fully defaulted Config, so
// the rest of the agent can read fields without nil or zero checks.
package config

import (
	"fmt"
	"github.com/sker65/mira/internal/i18n"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/sker65/mira/internal/schedule"
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
	Server Server `yaml:"server"`
	LLM    LLM    `yaml:"llm"`
	// Models are further models by name, e.g. a cloud model for harder
	// work. The model under llm is called "main".
	Models     map[string]LLM `yaml:"models"`
	Routing    Routing        `yaml:"routing"`
	Store      Store          `yaml:"store"`
	Prompt     Prompt         `yaml:"prompt"`
	HA         HA             `yaml:"ha"`
	MCPServers []MCPServer    `yaml:"mcp_servers"`
	// Telegram is no longer used: Telegram runs through Home Assistant's
	// integration (docs/spec/kanaele.md). An old section is ignored.
	Telegram   any               `yaml:"telegram,omitempty"`
	Sessions   Sessions          `yaml:"sessions"`
	Satellites map[string]string `yaml:"satellites"`
	Persons    map[string]Person `yaml:"persons"`
	Notifier   Notifier          `yaml:"notifier"`
	Security   Security          `yaml:"security"`
	Memory     Memory            `yaml:"memory"`
	Skills     Skills            `yaml:"skills"`
	Telemetry  Telemetry         `yaml:"telemetry"`
}

type Server struct {
	Listen string `yaml:"listen"`
	// Token authenticates the HA integration against the agent. Empty disables
	// the check, which is only sensible for local development.
	Token string `yaml:"token"`
	// Announce makes the agent discoverable by mDNS (_mira._tcp), so Home
	// Assistant offers it under "Discovered".
	Announce bool `yaml:"announce"`
	// Name is the announced name.
	Name string `yaml:"name"`
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
	// ReasoningEffort is passed as reasoning_effort when set ("none", "low",
	// ...). Cloud models that think by default are slower and dearer without
	// it; llama.cpp and LM Studio do not need it.
	ReasoningEffort string `yaml:"reasoning_effort"`
}

// Skills are folders with a SKILL.md (the Agent Skills format). Only the
// skills under enabled are used, each with its settings.
type Skills struct {
	// Dirs are searched for skill folders, in order; the first match wins.
	Dirs []string `yaml:"dirs"`
	// Enabled maps a skill name to its settings, e.g. a calendar URL.
	Enabled map[string]map[string]any `yaml:"enabled"`
}

// MainModel is the name of the model configured under llm.
const MainModel = "main"

// Routing says which model serves which kind of work. Empty means main;
// an empty Delegate turns the delegate tool off.
type Routing struct {
	Voice      string `yaml:"voice"`
	Telegram   string `yaml:"telegram"`
	Background string `yaml:"background"`
	// Delegate is the model the delegate tool hands harder tasks to.
	Delegate string `yaml:"delegate"`
	// Tasks is the model that plans standing orders; empty is the
	// background model. Planning reads the whole house once per order, so
	// a cloud model keeps it off the local GPU while someone is talking.
	Tasks string `yaml:"tasks"`
}

// Model returns the settings of a model by name; "" and "main" are llm.
func (c Config) Model(name string) (LLM, bool) {
	if name == "" || name == MainModel {
		return c.LLM, true
	}
	m, ok := c.Models[name]
	return m, ok
}

// RouteFor names the model that serves a channel.
func (c Config) RouteFor(ch Channel) string {
	var name string
	switch ch {
	case ChannelVoice:
		name = c.Routing.Voice
	case ChannelTelegram:
		name = c.Routing.Telegram
	case ChannelBackground:
		name = c.Routing.Background
	}
	if name == "" {
		return MainModel
	}
	return name
}

type Store struct {
	Path string `yaml:"path"`
}

type Prompt struct {
	// Persona is appended to the built-in layer 1 text. It is part of the
	// cached prefix, so it must not contain anything volatile.
	Persona string `yaml:"persona"`
	// Language is "de" or "en": the language of the prompt, of the messages
	// and of everything Mira writes on her own. She still answers in the
	// language she is spoken to in.
	Language string `yaml:"language"`
}

type HA struct {
	URL   string `yaml:"url"`
	Token string `yaml:"token"`
	// StructureDebounce collapses bursts of registry updates into one rebuild.
	StructureDebounce Duration `yaml:"structure_debounce"`
	Enabled           bool     `yaml:"enabled"`
	// AssistPromptServer names the MCP server whose prompt "Assist" becomes
	// layer 4: Home Assistant's own tool instructions and entity overview.
	// Empty uses the agent's own structure block instead.
	AssistPromptServer string `yaml:"assist_prompt_server"`
	// AssistPromptRefresh is how often that prompt is checked for changes.
	AssistPromptRefresh Duration `yaml:"assist_prompt_refresh"`
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

type Sessions struct {
	VoiceTTL    Duration `yaml:"voice_ttl"`
	TelegramTTL Duration `yaml:"telegram_ttl"`
}

// Person is someone the agent can address personally. The key in the
// persons map is the receiver id messages use.
//
// With Home Assistant every HA person is a person (docs/spec/benutzer.md):
// the key is the HA person's id, and Entity, Name and UserID come from
// Home Assistant. The config then holds only Mira's settings per person,
// under that id (an older entry is matched by its entity).
type Person struct {
	// Entity is the HA person entity; "home" means the person is at home.
	Entity string `yaml:"entity,omitempty"`
	// Name is how Mira names the person; from Home Assistant.
	Name string `yaml:"name,omitempty"`
	// UserID is the HA user linked to the person; from Home Assistant.
	UserID string `yaml:"-"`
	// Channels are the notify targets that are this person's, in order:
	// notify entities (e.g. a Telegram chat of HA's telegram_bot) or legacy
	// notify services (e.g. notify.mobile_app_anna). Personal and private
	// messages go to the first that works. A Telegram chat among them is
	// also how a message from that chat is known to be this person.
	Channels []string `yaml:"channels,omitempty"`
	// AcceptFrom says whose messages this person takes: "all" (default,
	// also someone anonymous at a satellite), "persons" (only from other
	// persons) or "none". Mira's own messages always arrive.
	AcceptFrom string `yaml:"accept_from,omitempty"`
	// AloneIsMe: when this person is the only one at home (by Entity), the
	// satellites count as them: their memory, their personal tasks.
	AloneIsMe bool `yaml:"alone_is_me,omitempty"`
}

// Whose messages a person takes (Person.AcceptFrom).
const (
	AcceptAll     = "all"
	AcceptPersons = "persons"
	AcceptNone    = "none"
)

type Notifier struct {
	QuietHours              QuietHours     `yaml:"quiet_hours"`
	MorningTrigger          MorningTrigger `yaml:"morning_trigger"`
	Cooldown                Duration       `yaml:"cooldown"`
	MaxItemsPerAnnouncement int            `yaml:"max_items_per_announcement"`
	EscalateToTelegram      Escalation     `yaml:"escalate_to_telegram"`
	// Urgent messages (a water leak, smoke) go out at once, quiet hours or
	// not: privately to everyone and spoken on every satellite.
	Urgent Urgent `yaml:"urgent"`
	// FollowUpWindow is how long after an announcement a turn from the same
	// satellite is treated as a reply to it.
	FollowUpWindow Duration `yaml:"follow_up_window"`
	MinImportance  int      `yaml:"min_importance"`
	// ConfidentialVoice decides when a confidential message may be spoken:
	// "never", "when_alone" (only its recipient is home, by the person
	// entities) or "always". Otherwise it goes to a private channel.
	ConfidentialVoice string `yaml:"confidential_voice"`
	// PrivateVia is no longer used: each person has channels.
	PrivateVia []string `yaml:"private_via,omitempty"`
	// Delivery is how a satellite speaks: "auto" picks start_conversation
	// when the satellite supports it, then announce; "tts" uses tts.speak on
	// a media_player with TTSEntity.
	Delivery  string `yaml:"delivery"`
	TTSEntity string `yaml:"tts_entity"`
	// RoomMode is per area "announce" (default: messages are spoken when
	// someone comes in) or "indicate": messages are only shown on the area's
	// indicator light, and the user asks for them ("Hast du eine
	// Benachrichtigung?"). Urgent messages and reminders are spoken anyway.
	RoomMode map[string]string `yaml:"room_mode"`
	// Indicators maps an area to the light that shows waiting messages, such
	// as the LED ring of a satellite.
	Indicators map[string]string `yaml:"indicators"`
	Indicator  IndicatorStyle    `yaml:"indicator"`
}

// IndicatorStyle is how an indicator light shows waiting messages.
type IndicatorStyle struct {
	RGBColor   []int  `yaml:"rgb_color"`
	Effect     string `yaml:"effect"`
	Brightness int    `yaml:"brightness"`
}

// Room modes.
const (
	RoomAnnounce = "announce"
	RoomIndicate = "indicate"
)

// Indicates reports whether messages in an area are only shown, not spoken.
func (n Notifier) Indicates(area string) bool { return n.RoomMode[area] == RoomIndicate }

// Values of Notifier.ConfidentialVoice.
const (
	ConfidentialVoiceNever     = "never"
	ConfidentialVoiceWhenAlone = "when_alone"
	ConfidentialVoiceAlways    = "always"
)

type QuietHours struct {
	From ClockTime `yaml:"from"`
	To   ClockTime `yaml:"to"`
}

type MorningTrigger struct {
	Areas []string `yaml:"areas"`
	// Presence are the motion sensors per area, one or several: any of
	// them means someone came in.
	Presence map[string]Entities `yaml:"presence"`
}

type Urgent struct {
	// MinImportance from which a message is urgent; 0 turns the rule off.
	MinImportance int `yaml:"min_importance"`
	// Announce speaks urgent messages on every satellite. Confidential ones
	// are never spoken this way.
	Announce bool `yaml:"announce"`
}

type Escalation struct {
	MinImportance int       `yaml:"min_importance"`
	After         ClockTime `yaml:"after"`
	// Grace is how long a message gets to be announced by voice before it
	// escalates, so one raised in the afternoon is not sent at once.
	Grace Duration `yaml:"grace"`
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
		Server: Server{Listen: "127.0.0.1:8787", Announce: true, Name: "Mira"},
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
		Store:  Store{Path: "mira.db"},
		Prompt: Prompt{Language: "de"},
		HA: HA{
			Enabled:             true,
			StructureDebounce:   Duration(30e9), // 30s
			AssistPromptServer:  "ha",
			AssistPromptRefresh: Duration(5 * 60e9),
		},
		Sessions: Sessions{
			VoiceTTL:    Duration(5 * 60e9),
			TelegramTTL: Duration(12 * 3600e9),
		},
		Notifier: Notifier{
			QuietHours:              QuietHours{From: ClockTime{22, 30}, To: ClockTime{6, 30}},
			Cooldown:                Duration(30 * 60e9),
			MaxItemsPerAnnouncement: 3,
			EscalateToTelegram:      Escalation{MinImportance: 2, After: ClockTime{9, 0}, Grace: Duration(30 * 60e9)},
			Urgent:                  Urgent{MinImportance: 3, Announce: true},
			FollowUpWindow:          Duration(2 * 60e9),
			MinImportance:           1,
			ConfidentialVoice:       ConfidentialVoiceNever,
			Delivery:                "auto",
			// Teal from the logo, gently pulsing.
			Indicator: IndicatorStyle{RGBColor: []int{32, 201, 176}, Effect: "Pulse", Brightness: 160},
		},
		Security: Security{
			ConfirmDomains: []string{"lock", "alarm_control_panel"},
			ConfirmTTL:     Duration(5 * 60e9),
			ReadOnlyTools: []string{
				"ha__GetDateTime", "ha__GetLiveContext", "ha__HassGetWeather", "ha__todo_get_items",
				// Names as HA 2026.10 serves them on /api/mcp/assist.
				"ha__llm__GetDateTime", "ha__homeassistant__GetLiveContext",
			},
		},
		Memory: Memory{
			ProfileSchedule: "30 3 * * *",
			MaxFactsInLayer: 8,
			MaxProfileChars: 2000,
		},
		Skills:    Skills{Dirs: []string{"skills"}},
		Telemetry: Telemetry{ServiceName: "mira", LogLevel: "info"},
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
	// Further models take what they leave out from main, except what only
	// suits a local llama.cpp server: slots, cache_prompt and warmups.
	for name, m := range c.Models {
		m.BaseURL = strings.TrimRight(m.BaseURL, "/")
		if m.MaxToolRounds == 0 {
			m.MaxToolRounds = c.LLM.MaxToolRounds
		}
		if m.Temperature == nil {
			m.Temperature = c.LLM.Temperature
		}
		if m.MaxTokens == 0 {
			m.MaxTokens = c.LLM.MaxTokens
		}
		if m.Timeout == 0 {
			m.Timeout = c.LLM.Timeout
		}
		if m.ContextTokens == 0 {
			m.ContextTokens = c.LLM.ContextTokens
		}
		if m.CompactAtFraction == 0 {
			m.CompactAtFraction = c.LLM.CompactAtFraction
		}
		c.Models[name] = m
	}
	c.HA.URL = strings.TrimRight(c.HA.URL, "/")
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
	if c.Persons == nil {
		c.Persons = map[string]Person{}
	}
}

func (c Config) Validate() error {
	var errs []string
	add := func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) }

	if c.Server.Listen == "" {
		add("server.listen must not be empty")
	}
	if _, err := i18n.Parse(c.Prompt.Language); err != nil {
		add("prompt.language: %v", err)
	}
	for name, m := range c.Models {
		if name == MainModel {
			add("models.%s: the name main is the model under llm", name)
		}
		if m.BaseURL == "" || m.Model == "" {
			add("models.%s needs base_url and model", name)
		}
	}
	for role, name := range map[string]string{"voice": c.Routing.Voice, "telegram": c.Routing.Telegram,
		"background": c.Routing.Background, "delegate": c.Routing.Delegate, "tasks": c.Routing.Tasks} {
		if _, ok := c.Model(name); !ok {
			add("routing.%s: no model %q (main or one of models)", role, name)
		}
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

	if c.Memory.ProfileSchedule != "" {
		if _, err := schedule.Parse(c.Memory.ProfileSchedule); err != nil {
			add("memory.profile_schedule: %v", err)
		}
	}

	for area, sat := range c.Satellites {
		switch {
		case strings.HasPrefix(sat, "assist_satellite."):
		case strings.HasPrefix(sat, "media_player."):
			if c.Notifier.TTSEntity == "" {
				add("satellites[%s]: %q is a media_player, which needs notifier.tts_entity", area, sat)
			}
		default:
			add("satellites[%s]: %q must be an assist_satellite or media_player entity", area, sat)
		}
	}
	for _, area := range c.Notifier.MorningTrigger.Areas {
		if _, ok := c.Satellites[area]; !ok {
			add("notifier.morning_trigger.areas: %q has no entry in satellites", area)
		}
		if len(c.Notifier.MorningTrigger.Presence[area]) == 0 {
			add("notifier.morning_trigger.areas: %q has no presence sensor", area)
		}
	}
	for area, mode := range c.Notifier.RoomMode {
		if mode != RoomAnnounce && mode != RoomIndicate {
			add("notifier.room_mode[%s] must be announce or indicate, not %q", area, mode)
		}
		if _, ok := c.Satellites[area]; !ok {
			add("notifier.room_mode[%s]: the area has no entry in satellites", area)
		}
	}
	for area, light := range c.Notifier.Indicators {
		if !strings.HasPrefix(light, "light.") {
			add("notifier.indicators[%s]: %q must be a light entity", area, light)
		}
	}
	if rgb := c.Notifier.Indicator.RGBColor; len(rgb) != 0 && len(rgb) != 3 {
		add("notifier.indicator.rgb_color needs three values")
	}
	switch c.Notifier.ConfidentialVoice {
	case ConfidentialVoiceNever, ConfidentialVoiceWhenAlone, ConfidentialVoiceAlways:
	default:
		add("notifier.confidential_voice must be never, when_alone or always, not %q", c.Notifier.ConfidentialVoice)
	}
	switch c.Notifier.Delivery {
	case "auto", "start_conversation", "announce", "tts":
	default:
		add("notifier.delivery must be auto, start_conversation, announce or tts, not %q", c.Notifier.Delivery)
	}
	if c.Notifier.ConfidentialVoice == ConfidentialVoiceWhenAlone && len(c.Persons) == 0 {
		add("notifier.confidential_voice when_alone needs persons with their person entities")
	}
	for id, p := range c.Persons {
		if strings.Contains(id, " ") || id == "household" || id == "house" {
			add("persons: %q is not a valid person id", id)
		}
		if p.Entity != "" && !strings.HasPrefix(p.Entity, "person.") {
			add("persons[%s].entity: %q is not a person entity", id, p.Entity)
		}
		for _, ch := range p.Channels {
			if !strings.HasPrefix(ch, "notify.") {
				add("persons[%s].channels: %q is not a notify target", id, ch)
			}
		}
		switch p.AcceptFrom {
		case "", AcceptAll, AcceptPersons, AcceptNone:
		default:
			add("persons[%s].accept_from must be all, persons or none, not %q", id, p.AcceptFrom)
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
	// Slots belong to the local main model.
	if !c.LLM.SlotPinning || c.RouteFor(ch) != MainModel {
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
