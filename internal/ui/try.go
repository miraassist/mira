package ui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/miraassist/mira/internal/startools"
)

// ToolInfo is one of a skill's Starlark tools as the page shows it.
type ToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"`
	// Writes: the tool is under tools.writes; the page asks before it runs.
	Writes bool `json:"writes,omitempty"`
}

// TryCall is the arguments of a try.
type TryCall struct {
	Args map[string]any `json:"args"`
}

// TryResult is what a try shows (docs/spec/api-tools.md, Seeing what went
// over the wire): the answer or the error with its backtrace, and every
// request and print in order.
type TryResult struct {
	Text      string            `json:"text,omitempty"`
	Error     string            `json:"error,omitempty"`
	Backtrace string            `json:"backtrace,omitempty"`
	Events    []startools.Event `json:"events"`
}

// Try runs a skill's Starlark tool for an admin.
type Try interface {
	Try(ctx context.Context, who, skill, tool string, args json.RawMessage) (TryResult, error)
}

// ErrNoTool is a skill or tool that is not there.
var ErrNoTool = errors.New("no such tool")

func (h *Handler) tryTool(w http.ResponseWriter, r *http.Request) {
	if h.opts.Try == nil {
		writeError(w, http.StatusServiceUnavailable, "no tools")
		return
	}
	var c TryCall
	if err := readJSON(r, &c); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Empty fields of the form are arguments not given.
	args := map[string]any{}
	for k, v := range c.Args {
		if s, ok := v.(string); ok && s == "" {
			continue
		}
		args[k] = v
	}
	raw, _ := json.Marshal(args)
	res, err := h.opts.Try.Try(r.Context(), r.Header.Get(HeaderHAUser), r.PathValue("skill"), r.PathValue("tool"), raw)
	switch {
	case errors.Is(err, ErrNoTool):
		writeError(w, http.StatusNotFound, err.Error())
	case err != nil:
		writeError(w, http.StatusBadGateway, err.Error())
	default:
		if res.Events == nil {
			res.Events = []startools.Event{}
		}
		writeJSON(w, http.StatusOK, res)
	}
}
