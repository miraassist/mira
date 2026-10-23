package ui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// Editing models, agents and skills (docs/spec/bearbeiten.md): the page
// changes mira.yaml and a skill's SKILL.md; the changes hold after a restart.

// Editor changes what Mira loads at start.
type Editor interface {
	// State is what the forms start from: the entries as the files say now,
	// not as Mira loaded them.
	State(ctx context.Context) EditState
	SaveModel(ctx context.Context, name string, m ModelEdit) error
	DeleteModel(ctx context.Context, name string) error
	SaveAgent(ctx context.Context, name string, a AgentEdit) error
	DeleteAgent(ctx context.Context, name string) error
	SaveSkill(ctx context.Context, name string, s SkillEdit) error
	// CheckMCP connects to a skill's MCP server with what the form says.
	CheckMCP(ctx context.Context, skill string, c MCPCheckRequest) MCPCheckResult
	// Restart starts Mira anew, after the answer went out.
	Restart()
}

// EditState is the editable entries and what holds for editing.
type EditState struct {
	// File is the mira.yaml the page writes; ReadOnly says why it cannot.
	File     string `json:"file"`
	ReadOnly string `json:"read_only,omitempty"`
	// Encrypted: the secrets file is encrypted (MIRA_SECRET_KEY).
	Encrypted bool `json:"encrypted"`
	// RestartNeeded: something was saved since Mira started.
	RestartNeeded bool                 `json:"restart_needed"`
	Models        map[string]ModelEdit `json:"models"`
	Agents        map[string]AgentEdit `json:"agents"`
	Skills        map[string]SkillEdit `json:"skills"`
}

// ModelEdit is a model's form. Token is write only: empty keeps it,
// TokenClear removes it; TokenSet says whether there is one.
type ModelEdit struct {
	Name            string   `json:"name,omitempty"`
	Endpoint        string   `json:"endpoint"`
	Model           string   `json:"model"`
	Privacy         string   `json:"privacy"`
	Parallel        int      `json:"parallel,omitempty"`
	Fallback        string   `json:"fallback,omitempty"`
	ContextTokens   int      `json:"context_tokens,omitempty"`
	ReasoningEffort string   `json:"reasoning_effort,omitempty"`
	Temperature     *float64 `json:"temperature,omitempty"`
	MaxToolRounds   int      `json:"max_tool_rounds,omitempty"`
	Timeout         string   `json:"timeout,omitempty"`
	Token           string   `json:"token,omitempty"`
	TokenClear      bool     `json:"token_clear,omitempty"`
	TokenSet        bool     `json:"token_set"`
	// FromHA: main as the integration sets it up; changed there, not here.
	FromHA bool `json:"from_ha,omitempty"`
	// InFile: the entry is in mira.yaml (else Mira's default).
	InFile bool `json:"in_file"`
}

// AgentEdit is an agent's form. Own agents (Mira's) take only model and
// privacy; Input and Output are YAML text.
type AgentEdit struct {
	Name         string   `json:"name,omitempty"`
	Own          bool     `json:"own,omitempty"`
	Description  string   `json:"description,omitempty"`
	Model        string   `json:"model,omitempty"`
	Privacy      string   `json:"privacy,omitempty"`
	Context      string   `json:"context,omitempty"`
	Tools        []string `json:"tools,omitempty"`
	ToolsWord    string   `json:"tools_word,omitempty"`
	Rights       []string `json:"rights,omitempty"`
	Conversation bool     `json:"conversation,omitempty"`
	MaxRounds    int      `json:"max_rounds,omitempty"`
	Instructions string   `json:"instructions,omitempty"`
	Input        string   `json:"input,omitempty"`
	Output       string   `json:"output,omitempty"`
	// UsedBy is who uses it; while there is someone it cannot be deleted.
	UsedBy []string `json:"used_by,omitempty"`
}

// SkillEdit is a skill's form. Settings holds the values as written (a
// secret one empty, SecretSet saying whether it is set); the rest only for
// the user's own skills (Own).
type SkillEdit struct {
	Enabled     bool              `json:"enabled"`
	Settings    map[string]string `json:"settings,omitempty"`
	SettingDefs []SettingDef      `json:"setting_defs,omitempty"`
	Own         bool              `json:"own,omitempty"`
	// ReadOnly says why the SKILL.md cannot be written.
	ReadOnly    string `json:"read_only,omitempty"`
	Description string `json:"description,omitempty"`
	Guide       string `json:"guide,omitempty"`
	Agents      string `json:"agents,omitempty"`
	Target      string `json:"target,omitempty"`
	MCP         string `json:"mcp,omitempty"`
	// Parts says which of Description … MCP a save changes; the others
	// stay as they are.
	Parts []string `json:"parts,omitempty"`
}

// SettingDef is a setting a skill declares.
type SettingDef struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Secret      bool   `json:"secret,omitempty"`
	SecretSet   bool   `json:"secret_set,omitempty"`
}

// MCPCheckRequest is the skill's settings as the form has them; an empty
// one takes what is set.
type MCPCheckRequest struct {
	Settings map[string]string `json:"settings,omitempty"`
}

// MCPCheckResult is how the connection went.
type MCPCheckResult struct {
	OK      bool     `json:"ok"`
	MS      int64    `json:"ms"`
	Tools   int      `json:"tools"`
	Missing []string `json:"missing,omitempty"`
	Error   string   `json:"error,omitempty"`
}

func (h *Handler) editRoutes() {
	h.mux.HandleFunc("GET /v1/ui/edit", h.auth(h.editState))
	h.mux.HandleFunc("PUT /v1/ui/edit/models/{name}", h.auth(h.editSave(func(ctx context.Context, e Editor, name string, raw json.RawMessage) error {
		var m ModelEdit
		if err := json.Unmarshal(raw, &m); err != nil {
			return errBadBody
		}
		return e.SaveModel(ctx, name, m)
	})))
	h.mux.HandleFunc("DELETE /v1/ui/edit/models/{name}", h.auth(h.editSave(func(ctx context.Context, e Editor, name string, _ json.RawMessage) error {
		return e.DeleteModel(ctx, name)
	})))
	h.mux.HandleFunc("PUT /v1/ui/edit/agents/{name}", h.auth(h.editSave(func(ctx context.Context, e Editor, name string, raw json.RawMessage) error {
		var a AgentEdit
		if err := json.Unmarshal(raw, &a); err != nil {
			return errBadBody
		}
		return e.SaveAgent(ctx, name, a)
	})))
	h.mux.HandleFunc("DELETE /v1/ui/edit/agents/{name}", h.auth(h.editSave(func(ctx context.Context, e Editor, name string, _ json.RawMessage) error {
		return e.DeleteAgent(ctx, name)
	})))
	h.mux.HandleFunc("PUT /v1/ui/edit/skills/{name}", h.auth(h.editSave(func(ctx context.Context, e Editor, name string, raw json.RawMessage) error {
		var s SkillEdit
		if err := json.Unmarshal(raw, &s); err != nil {
			return errBadBody
		}
		return e.SaveSkill(ctx, name, s)
	})))
	h.mux.HandleFunc("POST /v1/ui/edit/skills/{name}/mcp-check", h.auth(h.mcpCheck))
	h.mux.HandleFunc("POST /v1/ui/restart", h.auth(h.restart))
}

var errBadBody = errors.New("the body is not a valid form")

func (h *Handler) editState(w http.ResponseWriter, r *http.Request) {
	if h.opts.Editor == nil {
		writeError(w, http.StatusNotFound, "editing is not available")
		return
	}
	writeJSON(w, http.StatusOK, h.opts.Editor.State(r.Context()))
}

// editSave runs a change and answers with the new state; a refused change
// says why (422).
func (h *Handler) editSave(fn func(ctx context.Context, e Editor, name string, raw json.RawMessage) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.opts.Editor == nil {
			writeError(w, http.StatusNotFound, "editing is not available")
			return
		}
		var raw json.RawMessage
		if r.Method == http.MethodPut {
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&raw); err != nil {
				writeError(w, http.StatusBadRequest, errBadBody.Error())
				return
			}
		}
		name := r.PathValue("name")
		if err := fn(r.Context(), h.opts.Editor, name, raw); err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		h.log.Info("changed on the page", "path", r.URL.Path, "method", r.Method)
		writeJSON(w, http.StatusOK, h.opts.Editor.State(r.Context()))
	}
}

// mcpTimeout bounds a connection test.
const mcpTimeout = 20 * time.Second

func (h *Handler) mcpCheck(w http.ResponseWriter, r *http.Request) {
	if h.opts.Editor == nil {
		writeError(w, http.StatusNotFound, "editing is not available")
		return
	}
	var c MCPCheckRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&c); err != nil {
		writeError(w, http.StatusBadRequest, errBadBody.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), mcpTimeout)
	defer cancel()
	writeJSON(w, http.StatusOK, h.opts.Editor.CheckMCP(ctx, r.PathValue("name"), c))
}

func (h *Handler) restart(w http.ResponseWriter, r *http.Request) {
	if h.opts.Editor == nil {
		writeError(w, http.StatusNotFound, "editing is not available")
		return
	}
	h.log.Info("restart asked for on the page")
	writeJSON(w, http.StatusAccepted, map[string]bool{"restarting": true})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go h.opts.Editor.Restart()
}
