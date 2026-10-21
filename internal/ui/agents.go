package ui

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/miraassist/mira/internal/agent"
	"github.com/miraassist/mira/internal/store"
)

// The run view (docs/spec/agenten.md, Run view): what the agent runtime
// stores, read-only, for admins. Only asynchronous runs (and those running
// on after their timeout) are stored, each until 60 minutes after its end.

// keepRuns is how long an ended run is shown, as long as the runtime keeps it.
const keepRuns = 60 * time.Minute

// Run is a stored run in the list.
type Run struct {
	CallID           string `json:"call_id"`
	ParentID         string `json:"parent_id,omitempty"`
	Agent            string `json:"agent"`
	Status           string `json:"status"`
	Started          string `json:"started"`
	Ended            string `json:"ended,omitempty"`
	DurationMS       int64  `json:"duration_ms"`
	Model            string `json:"model,omitempty"`
	ModelCalls       int    `json:"model_calls"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	Route            string `json:"route,omitempty"`
}

// RunDetail is one run with what it got and did.
type RunDetail struct {
	Run
	Input   string    `json:"input"`
	Output  string    `json:"output,omitempty"`
	Error   string    `json:"error,omitempty"`
	Foreign bool      `json:"foreign"`
	Call    RunCall   `json:"call"`
	Steps   []RunStep `json:"steps"`
}

// RunCall is a run's call context as the view shows it.
type RunCall struct {
	Origin   string `json:"origin"`
	Person   string `json:"person,omitempty"`
	Place    string `json:"place,omitempty"`
	Rights   string `json:"rights"`
	Depth    int    `json:"depth"`
	Mode     string `json:"mode"`
	Delivery string `json:"delivery,omitempty"`
	Thread   string `json:"thread,omitempty"`
	Into     string `json:"into,omitempty"`
}

// RunStep is one stored step: a model answer, a tool marked as started, a
// tool result, a change of what the guard knows.
type RunStep struct {
	Seq  int             `json:"seq"`
	Kind string          `json:"kind"`
	Body json.RawMessage `json:"body"`
}

func runOf(rec store.AgentRun, cc agent.CallContext) Run {
	r := Run{CallID: rec.CallID, ParentID: rec.ParentID, Agent: rec.Agent, Status: rec.Status,
		Started: rec.StartedAt.In(time.Local).Format("15:04:05"), Model: rec.Usage.Model,
		ModelCalls: rec.Usage.ModelCalls, PromptTokens: rec.Usage.PromptTokens,
		CompletionTokens: rec.Usage.CompletionTokens, Route: string(cc.Mode.Delivery.Route)}
	end := time.Now()
	if !rec.EndedAt.IsZero() {
		end = rec.EndedAt
		r.Ended = rec.EndedAt.In(time.Local).Format("15:04:05")
	}
	r.DurationMS = end.Sub(rec.StartedAt).Milliseconds()
	return r
}

func callOf(rec store.AgentRun) agent.CallContext {
	var cc agent.CallContext
	_ = json.Unmarshal(rec.Call, &cc)
	return cc
}

// runs lists the stored runs: running ones first, then newest first.
func (h *Handler) runs(w http.ResponseWriter, r *http.Request) {
	list, err := h.opts.Store.RecentAgentRuns(r.Context(), time.Now().Add(-keepRuns))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := []Run{}
	for _, rec := range list {
		out = append(out, runOf(rec, callOf(rec)))
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": out})
}

// run is one stored run with its call context and its steps in order.
func (h *Handler) run(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec, err := h.opts.Store.GetAgentRun(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such run (a run is kept 60 minutes after its end)")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	steps, err := h.opts.Store.AgentSteps(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cc := callOf(rec)
	d := RunDetail{Run: runOf(rec, cc), Input: rec.Input, Output: rec.Output, Error: rec.Error, Foreign: rec.Foreign,
		Call: RunCall{Origin: string(cc.Origin), Person: cc.Person, Place: cc.Place, Rights: cc.Rights.String(),
			Depth: cc.Depth, Mode: cc.Mode.String(), Delivery: string(cc.Mode.Delivery.Route),
			Thread: cc.Mode.Delivery.Thread, Into: cc.Mode.Delivery.Into},
		Steps: []RunStep{}}
	for _, st := range steps {
		d.Steps = append(d.Steps, RunStep{Seq: st.Seq, Kind: st.Kind, Body: st.Body})
	}
	writeJSON(w, http.StatusOK, d)
}
