package ui

import (
	"context"
	"embed"
	"net/http"
	"path"
	"time"
)

// The skills and agents at a glance (docs/spec/uebersicht.md): what Mira
// loaded, read only, for admins.

// Catalog is every skill and agent with what the view shows of it.
type Catalog struct {
	Skills []SkillInfo `json:"skills"`
	Agents []AgentInfo `json:"agents"`
	Models []ModelInfo `json:"models"`
	Hooks  []HookInfo  `json:"hooks"`
}

// HookInfo is one hook (docs/spec/hooks.md): its level (system, default,
// household), its description and its code; a default hook the household's
// file of the same name replaces; a household file that did not load.
type HookInfo struct {
	Level       string `json:"level"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source"`
	Replaced    bool   `json:"replaced,omitempty"`
	Problem     string `json:"problem,omitempty"`
}

// ModelInfo is one model: where it is, how it is set, who takes it and how
// its calls went. The token is never shown, only whether there is one.
type ModelInfo struct {
	Name     string `json:"name"`
	Endpoint string `json:"endpoint"`
	Model    string `json:"model"`
	TokenSet bool   `json:"token_set"`
	Privacy  string `json:"privacy"`
	Parallel int    `json:"parallel"`
	Fallback string `json:"fallback,omitempty"`
	// FromHA: main as the integration's setup sets it, not from mira.yaml.
	FromHA          bool     `json:"from_ha,omitempty"`
	ContextTokens   int      `json:"context_tokens,omitempty"`
	ReasoningEffort string   `json:"reasoning_effort,omitempty"`
	Temperature     *float64 `json:"temperature,omitempty"`
	MaxToolRounds   int      `json:"max_tool_rounds,omitempty"`
	Timeout         string   `json:"timeout,omitempty"`
	CachePrompt     bool     `json:"cache_prompt,omitempty"`
	Warmup          bool     `json:"warmup,omitempty"`
	// UsedBy are the agents that take it; FallbackFor the models it stands in
	// for.
	UsedBy      []string `json:"used_by,omitempty"`
	FallbackFor []string `json:"fallback_for,omitempty"`
	// LastOK and LastFailed are when it last answered and last failed
	// ("2006-01-02 15:04:05"), Problem the last error.
	LastOK     string `json:"last_ok,omitempty"`
	LastFailed string `json:"last_failed,omitempty"`
	Problem    string `json:"problem,omitempty"`
	// Runs and Tokens are what the stored agent runs of the last hour used.
	Runs   int `json:"runs,omitempty"`
	Tokens int `json:"tokens,omitempty"`
}

// ModelCheck sends a model a short request, the way Mira calls it.
type ModelCheck func(ctx context.Context, name string) CheckResult

// CheckResult is how a check went: the time it took, and the model's answer
// or the error.
type CheckResult struct {
	OK     bool   `json:"ok"`
	MS     int64  `json:"ms"`
	Answer string `json:"answer,omitempty"`
	Error  string `json:"error,omitempty"`
}

// CatalogSource builds the catalog from what Mira loaded.
type CatalogSource func(ctx context.Context) Catalog

// SkillInfo is one skill.
type SkillInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// State is active, problem (switched on, not started) or available
	// (a folder, not switched on).
	State     string         `json:"state"`
	Problem   string         `json:"problem,omitempty"`
	Kinds     []string       `json:"kinds,omitempty"`
	Settings  []SettingInfo  `json:"settings,omitempty"`
	Schedules []ScheduleInfo `json:"schedules,omitempty"`
	Target    *TargetInfo    `json:"target,omitempty"`
	MCP       *MCPInfo       `json:"mcp,omitempty"`
	Agents    []string       `json:"agents,omitempty"`
	Guide     string         `json:"guide,omitempty"`
	// Phonebook is the skill telefon's phone book.
	Phonebook *PhonebookInfo `json:"phonebook,omitempty"`
}

// PhonebookInfo is what the page shows of the phone book: the box to open,
// the books read and when, never a contact.
type PhonebookInfo struct {
	Link   string   `json:"link"`
	Books  []string `json:"books,omitempty"`
	ReadAt string   `json:"read_at,omitempty"`
	Error  string   `json:"error,omitempty"`
}

// SettingInfo is a skill's setting; a secret one shows only whether it is set.
type SettingInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Value       string `json:"value,omitempty"`
	Secret      bool   `json:"secret,omitempty"`
	Set         bool   `json:"set"`
}

// ScheduleInfo is a skill's schedule with its next run.
type ScheduleInfo struct {
	Cron      string `json:"cron"`
	PerPerson bool   `json:"per_person,omitempty"`
	Next      string `json:"next,omitempty"`
}

// TargetInfo is a skill's target: its fields or a plan, and how it ends.
type TargetInfo struct {
	Plan    bool        `json:"plan,omitempty"`
	Agent   string      `json:"agent,omitempty"`
	Fields  []FieldInfo `json:"fields,omitempty"`
	Finish  string      `json:"finish,omitempty"`
	Confirm bool        `json:"confirm,omitempty"`
	Timeout string      `json:"timeout"`
}

// FieldInfo is a target's field.
type FieldInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Optional    bool   `json:"optional,omitempty"`
	Check       string `json:"check,omitempty"`
	Agent       string `json:"agent,omitempty"`
}

// MCPInfo is a skill's MCP server and how it is doing.
type MCPInfo struct {
	Transport    string   `json:"transport"`
	Address      string   `json:"address,omitempty"`
	Connected    bool     `json:"connected"`
	Error        string   `json:"error,omitempty"`
	Tools        []string `json:"tools,omitempty"`
	Missing      []string `json:"missing,omitempty"`
	Conversation bool     `json:"conversation"`
	WebContent   bool     `json:"web_content,omitempty"`
}

// AgentInfo is one agent.
type AgentInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Origin is own (Mira's), config (mira.yaml) or skill.
	Origin       string   `json:"origin"`
	Skill        string   `json:"skill,omitempty"`
	Model        string   `json:"model,omitempty"`
	ModelPrivacy string   `json:"model_privacy,omitempty"`
	Fallback     string   `json:"fallback,omitempty"`
	Privacy      string   `json:"privacy,omitempty"`
	Context      string   `json:"context,omitempty"`
	Tools        string   `json:"tools,omitempty"`
	ToolNames    []string `json:"tool_names,omitempty"`
	Rights       []string `json:"rights,omitempty"`
	Conversation bool     `json:"conversation,omitempty"`
	// Threads are the targets that name the agent: "skill" for a plan,
	// "skill.field" for a field.
	Threads []string `json:"threads,omitempty"`
	// CalledBy are the agents whose tools list names it.
	CalledBy  []string `json:"called_by,omitempty"`
	MaxRounds int      `json:"max_rounds,omitempty"`
	Problem   string   `json:"problem,omitempty"`
}

func (h *Handler) catalog(w http.ResponseWriter, r *http.Request) {
	c := Catalog{Skills: []SkillInfo{}, Agents: []AgentInfo{}, Models: []ModelInfo{}, Hooks: []HookInfo{}}
	if h.opts.Catalog != nil {
		c = h.opts.Catalog(r.Context())
	}
	writeJSON(w, http.StatusOK, c)
}

// checkTimeout bounds a check; the page waits a little longer.
const checkTimeout = 30 * time.Second

// checkModel sends a model a short request now and says how it went.
func (h *Handler) checkModel(w http.ResponseWriter, r *http.Request) {
	if h.opts.CheckModel == nil {
		writeError(w, http.StatusNotFound, "no models")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), checkTimeout)
	defer cancel()
	writeJSON(w, http.StatusOK, h.opts.CheckModel(ctx, r.PathValue("name")))
}

//go:embed js/*.js
var scripts embed.FS

// script serves the page's modules (Lit and the parts built on it); they hold
// no data, like the page itself.
func (h *Handler) script(w http.ResponseWriter, r *http.Request) {
	name := path.Base(r.PathValue("name"))
	data, err := scripts.ReadFile("js/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}
