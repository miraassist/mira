package ui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// The setup of a skill's program on the page (docs/spec/skill-programs.md):
// tools of the skill's server only an admin calls, never a model.

// SetupInfo is one entry of a skill's setup as the page shows it.
type SetupInfo struct {
	Tool  string `json:"tool"`
	Label string `json:"label"`
	// Description is the skill's, else the tool's own.
	Description string `json:"description,omitempty"`
	// Schema is the tool's input schema, for the form; empty while the
	// server has not listed the tool (not connected, an older program).
	Schema json.RawMessage `json:"schema,omitempty"`
}

// SetupCall is what the page sends: the form's values.
type SetupCall struct {
	Args map[string]any `json:"args"`
}

// SetupResult is the tool's answer: its text (JSON as the program wrote it)
// and whether it is an error.
type SetupResult struct {
	Text    string `json:"text"`
	IsError bool   `json:"is_error,omitempty"`
}

// Setup calls a skill's setup tool; who is the Home Assistant user, for the
// log.
type Setup interface {
	Call(ctx context.Context, who, skill, tool string, args json.RawMessage) (SetupResult, error)
}

// ErrNoSetupTool: the skill has no such setup tool.
var ErrNoSetupTool = errors.New("no such setup tool")

func (h *Handler) callSetup(w http.ResponseWriter, r *http.Request) {
	if h.opts.Setup == nil {
		writeError(w, http.StatusServiceUnavailable, "no setup")
		return
	}
	var c SetupCall
	if err := readJSON(r, &c); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if c.Args == nil {
		c.Args = map[string]any{}
	}
	args, _ := json.Marshal(c.Args)
	res, err := h.opts.Setup.Call(r.Context(), r.Header.Get(HeaderHAUser), r.PathValue("skill"), r.PathValue("tool"), args)
	switch {
	case errors.Is(err, ErrNoSetupTool):
		writeError(w, http.StatusNotFound, err.Error())
	case err != nil:
		writeError(w, http.StatusBadGateway, err.Error())
	default:
		writeJSON(w, http.StatusOK, res)
	}
}
