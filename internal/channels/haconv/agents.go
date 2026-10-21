package haconv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/miraassist/mira/internal/agent"
	"github.com/miraassist/mira/internal/store"
)

// Agents is what Home Assistant's mira.agent, mira.agent_result and
// mira.agent_cancel reach (docs/spec/agenten.md, Calling).
type Agents interface {
	CallByName(ctx context.Context, name string, input json.RawMessage, cc agent.CallContext) (agent.Result, error)
	Record(ctx context.Context, callID string) (store.AgentRun, error)
	Cancel(callID string) bool
}

// agentRequest is mira.agent's fields.
type agentRequest struct {
	Agent string          `json:"agent"`
	Input json.RawMessage `json:"input"`
	// Mode is sync or async; Timeout is in seconds.
	Mode      string  `json:"mode"`
	Timeout   float64 `json:"timeout"`
	OnTimeout string  `json:"on_timeout"`
	Delivery  string  `json:"delivery"`
	Receiver  string  `json:"receiver"`
}

// defaultAgentTimeout bounds a synchronous mira.agent without its own.
const defaultAgentTimeout = 60 * time.Second

// callContext is a call from an automation: synchronous with 60 seconds and
// cancelled then, unless the fields say otherwise; delivered as the event
// mira_agent_done unless as a message.
func (req agentRequest) callContext() (agent.CallContext, error) {
	cc := agent.CallContext{Origin: agent.FromAutomation}
	switch req.Mode {
	case "", "sync":
	case "async":
		cc.Mode.Async = true
	default:
		return cc, fmt.Errorf("mode is sync or async, not %q", req.Mode)
	}
	cc.Mode.Timeout = defaultAgentTimeout
	if req.Timeout > 0 {
		cc.Mode.Timeout = time.Duration(req.Timeout * float64(time.Second))
	}
	switch req.OnTimeout {
	case "", "cancel":
	case "run_on":
		cc.Mode.OnTimeout = agent.RunOn
	default:
		return cc, fmt.Errorf("on_timeout is cancel or run_on, not %q", req.OnTimeout)
	}
	switch req.Delivery {
	case "", "event":
		cc.Mode.Delivery.Route = agent.ToEvent
	case "message":
		cc.Mode.Delivery = agent.Delivery{Route: agent.ToMessage, Receiver: req.Receiver}
		if cc.Mode.Delivery.Receiver == "" {
			cc.Mode.Delivery.Receiver = store.ReceiverHouse
		}
	default:
		return cc, fmt.Errorf("delivery is event or message, not %q", req.Delivery)
	}
	return cc, nil
}

// agentResponse is a run as an automation reads it.
type agentResponse struct {
	CallID    string `json:"call_id"`
	Agent     string `json:"agent,omitempty"`
	Status    string `json:"status"`
	Output    any    `json:"output,omitempty"`
	Error     string `json:"error,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
	EndedAt   string `json:"ended_at,omitempty"`
}

// output is a run's output for the automation: the JSON it is, or its text.
func output(text string) any {
	if text == "" {
		return nil
	}
	if raw := json.RawMessage(text); json.Valid(raw) {
		return raw
	}
	return text
}

// callAgent serves mira.agent.
func (s *Server) callAgent(w http.ResponseWriter, r *http.Request) {
	var req agentRequest
	if err := decode(r, &req); err != nil || req.Agent == "" {
		writeError(w, http.StatusBadRequest, `body must name an "agent"`)
		return
	}
	cc, err := req.callContext()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input := req.Input
	if len(input) == 0 {
		input = json.RawMessage(`{}`)
	}
	res, err := s.opts.Agents.CallByName(r.Context(), req.Agent, input, cc)
	if errors.Is(err, agent.ErrUnknownAgent) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	out := agentResponse{CallID: res.CallID, Agent: req.Agent, Status: string(res.Status), Output: output(res.Text)}
	if err != nil {
		if res.Status == "" {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		out.Error = err.Error()
	}
	writeJSON(w, http.StatusOK, out)
}

// agentResult serves mira.agent_result.
func (s *Server) agentResult(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CallID string `json:"call_id"`
	}
	if err := decode(r, &req); err != nil || req.CallID == "" {
		writeError(w, http.StatusBadRequest, `body must be {"call_id"}`)
		return
	}
	rec, err := s.opts.Agents.Record(r.Context(), req.CallID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no run with that call id (a run is kept 60 minutes after its end)")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := agentResponse{CallID: rec.CallID, Agent: rec.Agent, Status: rec.Status, Output: output(rec.Output),
		Error: rec.Error, StartedAt: rec.StartedAt.Format(time.RFC3339)}
	if !rec.EndedAt.IsZero() {
		out.EndedAt = rec.EndedAt.Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, out)
}

// cancelAgent serves mira.agent_cancel.
func (s *Server) cancelAgent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CallID string `json:"call_id"`
	}
	if err := decode(r, &req); err != nil || req.CallID == "" {
		writeError(w, http.StatusBadRequest, `body must be {"call_id"}`)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"cancelled": s.opts.Agents.Cancel(req.CallID)})
}
