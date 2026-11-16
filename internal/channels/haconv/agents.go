package haconv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/agent"
	"github.com/miraassist/mira/internal/store"
)

// Agents is what Home Assistant's mira.agent, mira.agent_result and
// mira.agent_cancel reach (docs/spec/agents.md, Calling).
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

// AgentCall is the body of mira.agent_result and mira.agent_cancel: the
// call's id.
type AgentCall struct {
	CallID string `json:"call_id"`
}

// AgentCancelled is mira.agent_cancel's answer: whether a running call was
// cancelled.
type AgentCancelled struct {
	Cancelled bool `json:"cancelled"`
}

// SkillStart is the body of mira.agent with skill: the skill, its request
// (a text, or {"input": "…"}), and the receiver.
type SkillStart struct {
	Skill    string          `json:"skill"`
	Input    json.RawMessage `json:"input"`
	Receiver string          `json:"receiver"`
}

// SkillStarted is mira.agent with skill's answer: the thread it started.
type SkillStarted struct {
	ThreadID string `json:"thread_id"`
}

// agentResult serves mira.agent_result.
func (s *Server) agentResult(w http.ResponseWriter, r *http.Request) {
	var req AgentCall
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
	var req AgentCall
	if err := decode(r, &req); err != nil || req.CallID == "" {
		writeError(w, http.StatusBadRequest, `body must be {"call_id"}`)
		return
	}
	writeJSON(w, http.StatusOK, AgentCancelled{Cancelled: s.opts.Agents.Cancel(req.CallID)})
}

// SkillRefused is why a skill's thread was not started from Home
// Assistant: the caller's error (400).
type SkillRefused struct{ Reason string }

func (e *SkillRefused) Error() string { return e.Reason }

// startSkill serves mira.agent with skill (docs/spec/remote-agents.md, F):
// the skill's thread starts as if its owner had asked by voice, with the
// input as the request. A route of its own, so an older service answers
// 404 and the integration says that Mira needs an update (decision 47).
func (s *Server) startSkill(w http.ResponseWriter, r *http.Request) {
	var req SkillStart
	if err := decode(r, &req); err != nil || req.Skill == "" {
		writeError(w, http.StatusBadRequest, `body must name a "skill"`)
		return
	}
	request, err := skillRequest(req.Input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := s.opts.StartSkill(r.Context(), req.Skill, request, req.Receiver)
	var refused *SkillRefused
	switch {
	case errors.As(err, &refused):
		s.log.Warn("mira.agent with skill refused", "skill", req.Skill, "receiver", req.Receiver, "error", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		s.log.Error("mira.agent with skill failed", "skill", req.Skill, "receiver", req.Receiver, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, SkillStarted{ThreadID: id})
}

// skillRequest is the request of mira.agent with skill: a text, or an
// object with the text in input.
func skillRequest(raw json.RawMessage) (string, error) {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		if text = strings.TrimSpace(text); text != "" {
			return text, nil
		}
	}
	var in struct {
		Input string `json:"input"`
	}
	if json.Unmarshal(raw, &in) == nil && strings.TrimSpace(in.Input) != "" {
		return strings.TrimSpace(in.Input), nil
	}
	return "", fmt.Errorf(`input must be the request: a text, or {"input": "…"}; got %q`, cut(string(raw)))
}

func cut(s string) string {
	if r := []rune(s); len(r) > 200 {
		return string(r[:200]) + "…"
	}
	return s
}
