// Package config loads and validates the agent's YAML configuration.
//
// Secrets are never stored in the file itself; they are referenced as ${ENV}
// and expanded at load time. Load always returns a fully defaulted Config, so
// the rest of the agent can read fields without nil or zero checks.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/miraassist/mira/internal/schedule"
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
	// Models are the language models by name (docs/spec/modelle.md); main
	// must exist. An agent without a model takes main.
	Models     map[string]LLM    `yaml:"models"`
	Store      Store             `yaml:"store"`
	Prompt     Prompt            `yaml:"prompt"`
	HA         HA                `yaml:"ha"`
	MCPServers []MCPServer       `yaml:"mcp_servers"`
	Sessions   Sessions          `yaml:"sessions"`
	Satellites map[string]string `yaml:"satellites"`
	Persons    map[string]Person `yaml:"persons"`
	Notifier   Notifier          `yaml:"notifier"`
	Security   Security          `yaml:"security"`
	Memory     Memory            `yaml:"memory"`
	Skills     Skills            `yaml:"skills"`
	WebSearch  WebSearch         `yaml:"web_search"`
	SpeakerID  SpeakerID         `yaml:"speaker_id"`
	Agents     Agents            `yaml:"agents"`
	Telemetry  Telemetry         `yaml:"telemetry"`

	// mainMissing: the file has models but not main (LacksMain).
	mainMissing bool
}

// Agents are the settings of Mira's agents (docs/spec/agenten.md): next to
// max_depth, every other key is an agent described in YAML (Factory).
type Agents struct {
	// MaxDepth is how deep agents may call each other; an outside call has
	// depth 0. Default 3.
	MaxDepth int `yaml:"max_depth"`
	// Defined are the YAML agents by name, as written: each is read and
	// checked on its own when Mira starts, so a mistake in one leaves it out
	// instead of stopping the agent. Entries for Mira's own agents are here
	// too (only model and privacy).
	Defined map[string]yaml.Node `yaml:"-"`
	// models and privacy are what each entry names, for ModelFor and
	// PrivacyFor.
	models, privacy map[string]string
}

// UnmarshalYAML reads max_depth and the agents beside it.
func (a *Agents) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: agents must be a mapping", n.Line)
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, val := n.Content[i].Value, n.Content[i+1]
		if key == "max_depth" {
			if err := val.Decode(&a.MaxDepth); err != nil {
				return fmt.Errorf("agents.max_depth: %w", err)
			}
			continue
		}
		if a.Defined == nil {
			a.Defined = map[string]yaml.Node{}
			a.models, a.privacy = map[string]string{}, map[string]string{}
		}
		a.Defined[key] = *val
		var named struct {
			Model   string `yaml:"model"`
			Privacy string `yaml:"privacy"`
		}
		if val.Decode(&named) == nil {
			a.models[key], a.privacy[key] = named.Model, named.Privacy
		}
	}
	return nil
}

// AgentDef is an agent described in YAML, in mira.yaml under agents or in a
// skill under metadata.mira.agents (docs/spec/agenten.md, Factory).
type AgentDef struct {
	Description string `yaml:"description"`
	// Model is a model name (under models); empty is main.
	Model string `yaml:"model"`
	// Privacy local makes the agent run only on a local model
	// (docs/spec/modelle.md, Privacy as a rule).
	Privacy string `yaml:"privacy"`
	// Context is isolated (the default) or background.
	Context string `yaml:"context"`
	// Tools is none (the default), read_only, or a list of tools and agents.
	Tools        AgentTools     `yaml:"tools"`
	Rights       []string       `yaml:"rights"`
	Input        map[string]any `yaml:"input"`
	Instructions string         `yaml:"instructions"`
	Output       map[string]any `yaml:"output"`
	// Conversation true makes the agent a tool of the conversation (a
	// skill's once the skill is opened). Default off.
	Conversation bool `yaml:"conversation"`
	// MaxRounds bounds the agent's tool rounds (1 to 50); without it the
	// model's max_tool_rounds.
	MaxRounds int `yaml:"max_rounds"`
}

// DecodeAgent reads one agent's description strictly: an unknown field is a
// mistake to report, not to skip.
func DecodeAgent(n *yaml.Node) (AgentDef, error) {
	raw, err := yaml.Marshal(n)
	if err != nil {
		return AgentDef{}, err
	}
	var d AgentDef
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&d); err != nil {
		return AgentDef{}, err
	}
	return d, nil
}

// AgentTools is a word (none, read_only) or a list of names.
type AgentTools struct {
	Word  string
	Names []string
}

func (t *AgentTools) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.SequenceNode {
		return n.Decode(&t.Names)
	}
	return n.Decode(&t.Word)
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
	// Eval turns on eval mode (docs/spec/evals.md): every answer carries
	// what happened in the turn, and the state can be reset between
	// scenarios. Only for the eval runner's own agent.
	Eval bool `yaml:"eval"`
}

// LLM is a language model (docs/spec/modelle.md).
type LLM struct {
	BaseURL string `yaml:"endpoint"`
	Model   string `yaml:"model"`
	APIKey  string `yaml:"token"`
	// Privacy is PrivacyLocal (the data stays in the own network) or
	// PrivacyCloud; required.
	Privacy string `yaml:"privacy"`
	// Parallel is how many requests the model takes at once; without it 1
	// for a local model, 4 for a cloud model.
	Parallel int `yaml:"parallel"`
	// Fallback is the model taken when this one fails.
	Fallback string `yaml:"fallback"`
	// PinSlots pins each conversation channel to a llama.cpp slot so its
	// KV cache survives between turns; without it no id_slot is sent.
	PinSlots map[Channel]int `yaml:"pin_slots"`
	// CachePrompt is llama.cpp's cache_prompt; a server that rejects
	// unknown fields must not get it.
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

// Skills are folders with a SKILL.md (the Agent Skills format). Beside
// dirs, every key under skills is a skill with its settings; it is on
// unless it says enabled: false (docs/spec/bearbeiten.md).
type Skills struct {
	// Dirs are searched for skill folders, in order; the first match wins.
	Dirs []string `yaml:"dirs"`
	// Enabled maps a skill that is on to its settings, e.g. a calendar URL.
	Enabled map[string]map[string]any `yaml:"-"`
	// Off are the skills the file switches off (enabled: false), also
	// Mira's own ones that are otherwise always on.
	Off map[string]bool `yaml:"-"`
}

// SkillOn is the key that switches a skill on and off; no setting may be
// called so.
const SkillOn = "enabled"

// UnmarshalYAML reads dirs and the skills beside it.
func (s *Skills) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: skills must be a mapping", n.Line)
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, val := n.Content[i].Value, n.Content[i+1]
		switch {
		case key == "dirs":
			if err := val.Decode(&s.Dirs); err != nil {
				return fmt.Errorf("skills.dirs: %w", err)
			}
			continue
		case key == SkillOn:
			return errors.New("skills.enabled is gone: write each skill straight under skills, with its settings, e.g. skills: { abfall: { calendar: … } }; enabled: false switches one off")
		}
		settings := map[string]any{}
		if val.Kind != yaml.ScalarNode || val.Tag != "!!null" {
			if err := val.Decode(&settings); err != nil {
				return fmt.Errorf("skills.%s: a skill takes its settings as a mapping: %w", key, err)
			}
		}
		on := true
		if v, ok := settings[SkillOn]; ok {
			b, isBool := v.(bool)
			if !isBool {
				return fmt.Errorf("skills.%s.enabled must be true or false", key)
			}
			on = b
			delete(settings, SkillOn)
		}
		if !on {
			if s.Off == nil {
				s.Off = map[string]bool{}
			}
			s.Off[key] = true
			continue
		}
		if s.Enabled == nil {
			s.Enabled = map[string]map[string]any{}
		}
		if len(settings) == 0 {
			settings = nil
		}
		s.Enabled[key] = settings
	}
	return nil
}

// MainModel is the model every config has; an agent without a model takes
// it.
const MainModel = "main"

// Privacy of a model.
const (
	PrivacyLocal = "local"
	PrivacyCloud = "cloud"
)

// Requests at once when a model does not say (docs/spec/modelle.md).
const (
	localParallel = 1
	cloudParallel = 4
)

// Model returns the settings of a model by name; "" is main.
func (c Config) Model(name string) (LLM, bool) {
	if name == "" {
		name = MainModel
	}
	m, ok := c.Models[name]
	return m, ok
}

// Main is the model main.
func (c Config) Main() LLM { return c.Models[MainModel] }

// ModelFor names the model of an agent: what its entry under agents says
// (Mira's own agents: docs/spec/modelle.md), else main; a name no model
// has is main too (the agent's entry reports it).
func (c Config) ModelFor(agent string) string {
	if name := c.Agents.models[agent]; name != "" {
		if _, ok := c.Models[name]; ok {
			return name
		}
	}
	return MainModel
}

// PrivacyFor is what an agent's entry under agents says about privacy:
// PrivacyLocal when it must run on a local model, else "".
func (c Config) PrivacyFor(agent string) string {
	if c.Agents.privacy[agent] == PrivacyLocal {
		return PrivacyLocal
	}
	return ""
}

// Fallback is the model to take after name failed: its fallback, unless
// the caller must run locally and the fallback is not local
// (docs/spec/modelle.md). ok is false when there is none to take.
func (c Config) Fallback(name string, mustBeLocal bool) (string, bool) {
	m, ok := c.Model(name)
	if !ok || m.Fallback == "" {
		return "", false
	}
	next, ok := c.Models[m.Fallback]
	if !ok || (mustBeLocal && next.Privacy != PrivacyLocal) {
		return "", false
	}
	return m.Fallback, true
}

// ChannelAgent is the conversation agent of a channel; the background
// channel's prefix serves skill.task and the other background agents.
func ChannelAgent(ch Channel) string {
	if ch == ChannelBackground {
		return "skill.task"
	}
	return "conversation." + string(ch)
}

type Store struct {
	Path string `yaml:"path"`
}

// MaxPersonaChars bounds the persona: it is part of every request.
const MaxPersonaChars = 4000

type Prompt struct {
	// Persona is appended to the built-in layer 1 text. It is part of the
	// cached prefix, so it must not contain anything volatile.
	Persona string `yaml:"persona"`
}

type HA struct {
	URL   string `yaml:"url"`
	Token string `yaml:"token"`
	// StructureDebounce collapses bursts of registry updates into one rebuild.
	StructureDebounce Duration `yaml:"structure_debounce"`
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
	// DeviceContext sends the satellite's device id with a voice request's
	// tool calls and asks for the tool list as each satellite would get it.
	// Home Assistant's MCP server then offers the same tools as to its own
	// conversation agents (the timers, for one). On by default for Home
	// Assistant's Assist server; other servers do not know the field.
	DeviceContext *bool `yaml:"device_context"`
	// Tools names the tools Mira takes from the server; without it, all.
	Tools []string `yaml:"tools"`
	// Conversation false keeps a skill's server out of the conversation:
	// only the skill's agents and schedules get its tools. Default true.
	Conversation *bool `yaml:"conversation"`
	// Skill is the skill whose server this is (docs/spec/skill-mcp.md): its
	// tools are in no conversation until the skill is opened. Empty for the
	// servers under mcp_servers.
	Skill string `yaml:"-"`
}

// ForConversation reports whether the conversation gets the server's tools.
func (s MCPServer) ForConversation() bool { return s.Conversation == nil || *s.Conversation }

// WantsDevice reports whether the device id of the satellite goes to this
// server: set explicitly, else for Home Assistant's Assist MCP server.
func (s MCPServer) WantsDevice() bool {
	if s.DeviceContext != nil {
		return *s.DeviceContext
	}
	return strings.HasSuffix(strings.TrimRight(s.URL, "/"), "/api/mcp/assist")
}

// Sessions bound what the model sees of a conversation (docs/spec/kontext.md).
type Sessions struct {
	VoiceTTL Duration `yaml:"voice_ttl"`
	// TelegramTTL ends a Telegram conversation after that long without a
	// message; 0 means never: the history is bounded by MaxTurns instead.
	TelegramTTL Duration `yaml:"telegram_ttl"`
	// MaxTurns is from where the history is cut, down to KeepTurns: in
	// jumps, so its start stays the same in between and the cache holds.
	MaxTurns  int `yaml:"max_turns"`
	KeepTurns int `yaml:"keep_turns"`
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
	// VoiceThreshold is this person's own speaker_id threshold, for a voice
	// that scores lower or higher than most (docs/spec/sprecher.md).
	VoiceThreshold float64 `yaml:"voice_threshold,omitempty"`
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
	// OwnMessageHold is how long a message left at a satellite does not count
	// as waiting there: whoever left it is standing there. Other rooms hear it
	// at once. 0 turns the hold off.
	OwnMessageHold Duration `yaml:"own_message_hold"`
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
	// RoomMode is no longer used: whether Mira speaks when someone comes in
	// is MorningTrigger.Areas ("announce on entry"), and an indicator light
	// shows waiting messages either way. An old "indicate" takes the area
	// out of MorningTrigger.Areas when the config is read.
	RoomMode map[string]string `yaml:"room_mode,omitempty"`
	// Indicators maps an area to the light that shows waiting messages, such
	// as the LED ring of a satellite, whether or not Mira announces there.
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
// WebSearch is Mira's own web search (docs/spec/websuche.md); the
// integration switches it in its options.
type WebSearch struct {
	Enabled bool `yaml:"enabled"`
}

// SpeakerID is the speaker-id service that recognizes who speaks at a
// satellite (docs/spec/sprecher.md); without a URL the satellites stay as
// they are.
type SpeakerID struct {
	URL string `yaml:"url"`
	// Token is the service's SPEAKER_TOKEN; ${SPEAKER_ID_TOKEN} in mira.yaml.
	Token string `yaml:"token,omitempty"`
	// Threshold is the score a voice needs to count as a person (0 to 1);
	// 0 takes speakerid.Threshold. A person's voice_threshold wins.
	Threshold float64 `yaml:"threshold,omitempty"`
}

// mainDefaults are what main takes for the settings mira.yaml leaves out;
// the other models take them from main.
func mainDefaults() LLM {
	temp := 0.3
	return LLM{MaxToolRounds: 8, Temperature: &temp, MaxTokens: 1024, Timeout: Duration(120e9), // 2m
		ContextTokens: 16384, CompactAtFraction: 0.9}
}

func Default() Config {
	return Config{
		WebSearch: WebSearch{Enabled: true},
		Agents:    Agents{MaxDepth: 3},
		// The image sets MIRA_LISTEN (all interfaces) and MIRA_DATA (its
		// data folder): a container needs no config for either.
		Server: Server{Listen: envOr("MIRA_LISTEN", "127.0.0.1:8787"), Announce: true, Name: "Mira"},
		// main until mira.yaml or the integration says what it is.
		Models: map[string]LLM{MainModel: fillFrom(LLM{BaseURL: "http://localhost:8080/v1", Model: "local",
			Privacy: PrivacyLocal, Parallel: localParallel}, mainDefaults())},
		Store: Store{Path: dataPath("mira.db")},
		HA: HA{
			StructureDebounce:   Duration(30e9), // 30s
			AssistPromptServer:  "ha",
			AssistPromptRefresh: Duration(5 * 60e9),
		},
		Sessions: Sessions{
			VoiceTTL:    Duration(5 * 60e9),
			TelegramTTL: 0,
			MaxTurns:    50,
			KeepTurns:   30,
		},
		Notifier: Notifier{
			QuietHours:              QuietHours{From: ClockTime{22, 30}, To: ClockTime{6, 30}},
			Cooldown:                Duration(30 * 60e9),
			MaxItemsPerAnnouncement: 3,
			EscalateToTelegram:      Escalation{MinImportance: 2, After: ClockTime{9, 0}, Grace: Duration(30 * 60e9)},
			Urgent:                  Urgent{MinImportance: 3, Announce: true},
			FollowUpWindow:          Duration(2 * 60e9),
			OwnMessageHold:          Duration(10 * 60e9),
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

// LoadOptional is Load for a file that may be missing: then the defaults
// apply, and the integration sets the rest up. hasLLM says whether the file
// names the language models.
func LoadOptional(path string) (cfg Config, hasLLM bool, err error) {
	// The page's secrets (docs/spec/bearbeiten.md) come before the
	// environment; a file Mira cannot read is told, and the rest goes on.
	lookup := OSLookup
	secrets := DefaultSecrets()
	if _, err := secrets.Load(); err != nil {
		slog.Warn("secrets not read", "file", secrets.Path, "error", err)
	} else {
		lookup = secrets.Lookup(OSLookup)
	}
	raw, err := os.ReadFile(ConfigPath(path))
	if errors.Is(err, os.ErrNotExist) {
		cfg, err = Parse(nil, lookup)
		return cfg, false, err
	}
	if err != nil {
		return Config{}, false, fmt.Errorf("read config: %w", err)
	}
	cfg, err = Parse(raw, lookup)
	if err != nil {
		return Config{}, false, err
	}
	_, hasMain := fileModels(raw)
	return cfg, hasMain, nil
}

// fileModels says whether a config file has models, and whether main is
// among them.
func fileModels(raw []byte) (hasModels, hasMain bool) {
	var top struct {
		Models map[string]any `yaml:"models"`
	}
	if yaml.Unmarshal(raw, &top) != nil || top.Models == nil {
		return false, false
	}
	_, hasMain = top.Models[MainModel]
	return true, hasMain
}

// LacksMain says that the file names models but not main: main has to come
// from the integration's setup (docs/spec/modelle.md, MO-02).
func (c Config) LacksMain() bool { return c.mainMissing }

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// dataPath is a file in the data folder (MIRA_DATA), else in the working
// folder.
func dataPath(name string) string {
	if dir := os.Getenv("MIRA_DATA"); dir != "" {
		return filepath.Join(dir, name)
	}
	return name
}

// Parse is Load without the filesystem, so tests can drive it directly.
func Parse(raw []byte, lookup Lookup) (Config, error) {
	expanded, err := expandEnv(raw, lookup)
	if err != nil {
		return Config{}, err
	}
	if err := oldModelKeys(expanded); err != nil {
		return Config{}, err
	}
	cfg := Default()
	// The file's models replace the default main, not add to it. Models
	// without main keep it as a stand-in: main then comes from the
	// integration's setup (docs/spec/modelle.md, MO-02), and without that
	// Mira waits for the setup.
	switch hasModels, hasMain := fileModels(expanded); {
	case hasMain:
		cfg.Models = nil
	case hasModels:
		cfg.mainMissing = true
	}
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

// oldModelKeys refuses a config of before the named models
// (docs/spec/modelle.md, Decided 4), saying what to write instead.
func oldModelKeys(raw []byte) error {
	var top map[string]any
	_ = yaml.Unmarshal(raw, &top)
	var errs []string
	if _, ok := top["llm"]; ok {
		errs = append(errs, "llm is now models.main: move the section under models as main, with endpoint (was base_url), token (was api_key) and privacy (local or cloud)")
	}
	if _, ok := top["telegram"]; ok {
		errs = append(errs, "telegram is gone: Telegram runs through Home Assistant's telegram_bot integration; remove the section and set each person's channels in the integration")
	}
	if _, ok := top["routing"]; ok {
		errs = append(errs, "routing is gone: give each agent its model under agents, e.g. agents: { conversation.telegram: { model: smart } }")
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("invalid config:\n  - %s", strings.Join(errs, "\n  - "))
}

func (c *Config) normalize() {
	// main takes defaults for what it leaves out, the other models take it
	// from main; cache_prompt, warmups and slots stay each model's own.
	if main, ok := c.Models[MainModel]; ok {
		c.Models[MainModel] = fillFrom(main, mainDefaults())
	}
	for name, m := range c.Models {
		if name != MainModel {
			m = fillFrom(m, c.Models[MainModel])
		}
		m.BaseURL = strings.TrimRight(m.BaseURL, "/")
		if m.Parallel == 0 {
			m.Parallel = localParallel
			if m.Privacy == PrivacyCloud {
				m.Parallel = cloudParallel
			}
		}
		c.Models[name] = m
	}
	c.HA.URL = strings.TrimRight(c.HA.URL, "/")
	// An area once set to "indicate" did not speak on entry; it still doesn't.
	if len(c.Notifier.RoomMode) > 0 {
		c.Notifier.MorningTrigger.Areas = slices.DeleteFunc(c.Notifier.MorningTrigger.Areas, func(a string) bool {
			return c.Notifier.RoomMode[a] == RoomIndicate
		})
		c.Notifier.RoomMode = nil
	}
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

// fillFrom gives m the settings it leaves out from base.
func fillFrom(m, base LLM) LLM {
	if m.MaxToolRounds == 0 {
		m.MaxToolRounds = base.MaxToolRounds
	}
	if m.Temperature == nil {
		m.Temperature = base.Temperature
	}
	if m.MaxTokens == 0 {
		m.MaxTokens = base.MaxTokens
	}
	if m.Timeout == 0 {
		m.Timeout = base.Timeout
	}
	if m.ContextTokens == 0 {
		m.ContextTokens = base.ContextTokens
	}
	if m.CompactAtFraction == 0 {
		m.CompactAtFraction = base.CompactAtFraction
	}
	return m
}

func (c Config) Validate() error {
	var errs []string
	add := func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) }

	if c.Server.Listen == "" {
		add("server.listen must not be empty")
	}
	if _, ok := c.Models[MainModel]; !ok {
		add("models needs main: the model every agent takes that names none")
	}
	for name, m := range c.Models {
		if m.BaseURL == "" || m.Model == "" {
			add("models.%s needs endpoint and model", name)
		}
		if m.Privacy != PrivacyLocal && m.Privacy != PrivacyCloud {
			add("models.%s needs privacy: local (the data stays in the own network) or cloud", name)
		}
		if m.Parallel < 0 {
			add("models.%s: parallel must not be negative", name)
		}
		if m.MaxToolRounds < 1 {
			add("models.%s: max_tool_rounds must be at least 1", name)
		}
		if m.ContextTokens < 512 {
			add("models.%s: context_tokens must be at least 512", name)
		}
		if m.CompactAtFraction <= 0 || m.CompactAtFraction >= 1 {
			add("models.%s: compact_at_fraction must be between 0 and 1 (exclusive)", name)
		}
		for ch := range m.PinSlots {
			if ch != ChannelVoice && ch != ChannelTelegram && ch != ChannelBackground {
				add("models.%s.pin_slots: %q is no channel (voice, telegram, background)", name, ch)
			}
		}
		if m.Fallback != "" {
			if _, ok := c.Models[m.Fallback]; !ok {
				add("models.%s.fallback: no model %q", name, m.Fallback)
			}
		}
	}
	if circle := fallbackCircle(c.Models); circle != "" {
		add("models: the fallbacks go in a circle: %s", circle)
	}
	if c.Store.Path == "" {
		add("store.path must not be empty")
	}
	// Without both, the agent waits to be set up by the integration
	// (docs/spec/einrichtung.md); one without the other is a mistake.
	if (c.HA.URL == "") != (c.HA.Token == "") {
		add("ha.url and ha.token come together: Mira works through Home Assistant")
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
	// Persons and satellites come from Home Assistant once Mira runs; the
	// config alone does not know them, so nothing here checks against them.
	for _, area := range c.Notifier.MorningTrigger.Areas {
		if len(c.Notifier.MorningTrigger.Presence[area]) == 0 {
			add("notifier.morning_trigger.areas: %q has no presence sensor", area)
		}
	}
	for area, mode := range c.Notifier.RoomMode {
		if mode != RoomAnnounce && mode != RoomIndicate {
			add("notifier.room_mode[%s] must be announce or indicate, not %q", area, mode)
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

	if c.Sessions.KeepTurns < 1 || c.Sessions.MaxTurns <= c.Sessions.KeepTurns {
		add("sessions: keep_turns must be at least 1 and below max_turns (%d, %d)", c.Sessions.KeepTurns, c.Sessions.MaxTurns)
	}
	if n := len([]rune(c.Prompt.Persona)); n > MaxPersonaChars {
		add("prompt.persona is %d characters long; at most %d fit, since it goes with every request", n, MaxPersonaChars)
	}
	if c.Agents.MaxDepth < 0 {
		add("agents.max_depth must not be negative")
	}
	if c.Notifier.MaxItemsPerAnnouncement < 1 {
		add("notifier.max_items_per_announcement must be at least 1")
	}
	if c.Notifier.OwnMessageHold < 0 {
		add("notifier.own_message_hold must not be negative")
	}

	if len(errs) == 0 {
		return nil
	}
	sort.Strings(errs)
	return fmt.Errorf("invalid config:\n  - %s", strings.Join(errs, "\n  - "))
}

// SlotFor returns a model's llama.cpp slot for a channel, or -1 when the
// model pins none. A negative slot means "let the server choose".
func (c Config) SlotFor(model string, ch Channel) int {
	m, ok := c.Model(model)
	if !ok {
		return -1
	}
	if slot, ok := m.PinSlots[ch]; ok {
		return slot
	}
	return -1
}

// fallbackCircle names a circle of fallbacks ("a → b → a"), or "".
func fallbackCircle(models map[string]LLM) string {
	names := make([]string, 0, len(models))
	for n := range models {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, start := range names {
		path := []string{start}
		seen := map[string]bool{start: true}
		for next := models[start].Fallback; next != ""; next = models[next].Fallback {
			path = append(path, next)
			if seen[next] {
				return strings.Join(path, " → ")
			}
			seen[next] = true
		}
	}
	return ""
}

// SessionTTL returns how long a session of this channel survives inactivity.
func (c Config) SessionTTL(ch Channel) Duration {
	if ch == ChannelTelegram {
		return c.Sessions.TelegramTTL
	}
	return c.Sessions.VoiceTTL
}
