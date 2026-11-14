package a2a

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	sdk "github.com/a2aproject/a2a-go/v2/a2a"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/retry"
	"github.com/miraassist/mira/internal/store"
)

// The calls of the remote agents (docs/spec/remote-agents.md, B to E and
// G): a thread's field or plan is due, Mira sends the work, follows the
// stream (or takes the pushes of an agent without one) and hands the result
// to the thread as an agent's run is handed to it. Each call is a row of
// agent_runs with the remote task in its column remote.

// PushPath is where the remote agents push, followed by the call id
// (decision 13).
const PushPath = "/v1/a2a/push/"

// The statuses of a call's row, as the agent runtime's.
const (
	statusRunning   = "running"
	statusDone      = "done"
	statusFailed    = "failed"
	statusCancelled = "cancelled"
)

// Runs keeps the calls (package store).
type Runs interface {
	PutAgentRun(ctx context.Context, r store.AgentRun) error
	EndAgentRun(ctx context.Context, callID, status, output, errText string, foreign bool, u store.AgentUsage) error
	GetAgentRun(ctx context.Context, callID string) (store.AgentRun, error)
	RunningAgentRuns(ctx context.Context) ([]store.AgentRun, error)
	SetAgentRunRemote(ctx context.Context, callID string, remote json.RawMessage) error
}

// Request is a field or a plan due for a remote agent.
type Request struct {
	CallID string
	// Agent is the remote agent's name in its skill.
	Agent string
	// Input is the target's input with its placeholders filled.
	Input json.RawMessage
	// Call is the call context, kept with the row and handed back with the
	// result.
	Call json.RawMessage
	// Deadline is the row's deadline: the thread's expiry. A call has no
	// time limit of its own; it ends with its thread (decision 19, 33).
	Deadline time.Time
}

// Question is a remote agent asking back (input-required, D).
type Question struct {
	CallID string
	// Call is the call context, as the request gave it.
	Call json.RawMessage
	// Label names the agent to the user (decision 22).
	Label string
	Text  string
}

// state is what a call knows of its remote task, kept in its row.
type state struct {
	Skill     string `json:"skill"`
	Agent     string `json:"agent"`
	TaskID    string `json:"task_id,omitempty"`
	ContextID string `json:"context_id,omitempty"`
	// Push: the call takes pushes (an agent without a stream); Token is
	// the call's push token (decision 14).
	Push  bool   `json:"push,omitempty"`
	Token string `json:"token,omitempty"`
	// State is the task's state taken last.
	State string `json:"state,omitempty"`
	// Asked is the question of an agent request still open.
	Asked string `json:"asked,omitempty"`
	// Sent counts the messages sent, for their ids.
	Sent int `json:"sent,omitempty"`
	// Label names the agent to the user (decision 22); Said is what it
	// said when the call failed (its status text).
	Label string `json:"label,omitempty"`
	Said  string `json:"said,omitempty"`
}

// call is one call in memory.
type call struct {
	id    string
	agent *Agent
	label string
	rec   store.AgentRun

	mu        sync.Mutex
	st        state
	artifacts []*sdk.Artifact
	ended     bool
	cancelled bool
	// stop ends what follows the call now: its stream, its send.
	ctx  context.Context
	stop context.CancelFunc
	// queue are pushes not yet taken, in order.
	queue    []sdk.Event
	draining bool
}

// StatusError is an answer of a remote agent with an HTTP status Mira reads
// as an error.
type StatusError struct {
	URL    string
	Status int
	// Body is the start of the answer.
	Body string
	// Wait is the wait the agent named (Retry-After).
	Wait time.Duration
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s: %d %s, the answer begins %q", e.URL, e.Status, http.StatusText(e.Status), e.Body)
}

// statusTransport turns an answer of the remote agent that is no A2A
// answer into a StatusError, so the error says what came: a 408, a 429 or
// a 5xx always, another 4xx unless it carries a JSON-RPC error, which the
// SDK reads itself.
type statusTransport struct{ next http.RoundTripper }

func (t statusTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := t.next.RoundTrip(r)
	if err != nil || resp.StatusCode < 400 {
		return resp, err
	}
	always := resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests ||
		resp.StatusCode >= 500
	if !always && strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		return resp, nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	e := &StatusError{URL: r.URL.Redacted(), Status: resp.StatusCode, Body: bodyStart(body)}
	if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && secs > 0 {
		e.Wait = time.Duration(secs) * time.Second
	}
	return nil, e
}

// notReached says that a send did not reach the remote agent, so sending
// it again cannot make the work twice (decision 26): the connection was
// refused, no route, the name unknown, or the agent answered 429 or 503.
func notReached(err error) bool {
	var se *StatusError
	if errors.As(err, &se) {
		return se.Status == http.StatusTooManyRequests || se.Status == http.StatusServiceUnavailable
	}
	var dns *net.DNSError
	return errors.As(err, &dns) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ENETUNREACH)
}

// classify marks an error of a call for internal/retry: an A2A error in
// the answer and a 4xx are fatal, a lost connection, a timeout, a 408, 429
// and 5xx retryable (Retries); a wait the agent named is kept.
func classify(err error) error {
	var ae *sdk.Error
	var se *StatusError
	switch {
	case errors.As(err, &ae):
		return retry.Fatal(err)
	case errors.As(err, &se) && se.Wait > 0:
		return retry.After(err, se.Wait)
	case errors.As(err, &se) && se.Status != http.StatusRequestTimeout && se.Status != http.StatusTooManyRequests &&
		se.Status < 500:
		return retry.Fatal(err)
	}
	return retry.Transient(err)
}

// classifySend is classify for a send: tried again only when it did not
// reach the agent (decision 26).
func classifySend(err error) error {
	if !notReached(err) {
		return retry.Fatal(err)
	}
	return classify(err)
}

func (h *Host) policy3() retry.Policy {
	return retry.Policy{Attempts: 3, First: h.callRetry, Growth: retry.Exponential}
}

func randomToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand does not fail on supported systems
	}
	return hex.EncodeToString(b)
}

// Remote reports whether a name is a remote agent of the skills.
func (h *Host) Remote(name string) bool {
	if h == nil {
		return false
	}
	_, ok := h.Agent(name)
	return ok
}

// Send starts a call: its row, then the send in the background. An error
// when the agent cannot take work now (its card not read, the skill's
// connection not made) or the row cannot be written; the caller fails the
// field with it.
func (h *Host) Send(ctx context.Context, req Request) error {
	a, ok := h.Agent(req.Agent)
	if !ok {
		return fmt.Errorf("no remote agent %s", req.Agent)
	}
	info, ready := a.Info()
	client := a.Client()
	if !ready || client == nil {
		return fmt.Errorf("the remote agent %s at %s is not ready: %s", a.Name, a.def.Remote.URL, h.why(a))
	}
	if info.Scheme == SchemeOAuth2 && !h.conns.Connected(a.Skill) {
		return fmt.Errorf("the remote agent %s needs the connection of the skill %s, which is not made", info.Label, a.Skill)
	}
	if h.runs == nil {
		return errors.New("no store for the calls of remote agents")
	}
	msg, text, err := firstMessage(req.CallID, a.def.Remote.A2ASkill, req.Input)
	if err != nil {
		return err
	}
	st := state{Skill: a.Skill, Agent: a.Name, Label: info.Label, Push: info.Push && !info.Stream, Sent: 1}
	if st.Push {
		st.Token = randomToken()
	}
	raw, _ := json.Marshal(st)
	now := h.now()
	deadline := req.Deadline
	if deadline.IsZero() || deadline.Before(now) {
		deadline = now
	}
	rec := store.AgentRun{CallID: req.CallID, Agent: a.Name, Status: statusRunning, Call: req.Call, Input: text,
		StartedAt: now, Deadline: deadline, Remote: raw}
	if err := h.runs.PutAgentRun(ctx, rec); err != nil {
		return fmt.Errorf("could not store the call of %s: %w", a.Name, err)
	}
	c := h.track(rec, a, info.Label, st)
	way := "stream"
	if st.Push {
		way = "push"
	}
	h.log.Info("remote agent called", "agent", a.Name, "name", info.Label, "call_id", c.id, "way", way,
		"endpoint", info.Endpoint)
	h.wg.Go(func() { h.deliverSend(c, msg) })
	return nil
}

// why names why an agent is not ready.
func (h *Host) why(a *Agent) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case a.problem != nil:
		return a.problem.Text(i18n.EN)
	case a.err != nil:
		return "its card cannot be read: " + a.err.Error()
	}
	return "its card was not read yet"
}

// track keeps a call in memory, with what ends it.
func (h *Host) track(rec store.AgentRun, a *Agent, label string, st state) *call {
	c := &call{id: rec.CallID, agent: a, label: label, rec: rec, st: st}
	c.ctx, c.stop = context.WithCancel(h.base)
	h.mu.Lock()
	h.calls[c.id] = c
	h.mu.Unlock()
	return c
}

// firstMessage is the work as one A2A message (decision 10): the input's
// "input" as a text part, the input's other fields as a data part beside
// it; the card's skill in the metadata (decision 27). Its id is the call
// id, so a send tried again keeps it. text is what the row shows.
func firstMessage(callID, skill string, input json.RawMessage) (*sdk.Message, string, error) {
	var in map[string]any
	if len(input) > 0 {
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, "", fmt.Errorf("the input of the remote agent is no JSON object (%v): %q", err, bodyStart(input))
		}
	}
	var parts []*sdk.Part
	text, _ := in["input"].(string)
	if strings.TrimSpace(text) != "" {
		parts = append(parts, sdk.NewTextPart(text))
	}
	rest := map[string]any{}
	for k, v := range in {
		if k != "input" {
			rest[k] = v
		}
	}
	if len(rest) > 0 {
		parts = append(parts, sdk.NewDataPart(rest))
		raw, _ := json.Marshal(rest)
		text = strings.TrimSpace(text + "\n" + string(raw))
	}
	if len(parts) == 0 {
		return nil, "", errors.New("the input of the remote agent is empty")
	}
	m := sdk.NewMessage(sdk.MessageRoleUser, parts...)
	m.ID = callID
	if skill != "" {
		m.Metadata = map[string]any{skillMeta: skill}
	}
	return m, text, nil
}

// skillMeta is the key of a message's metadata naming the card's skill
// (decision 27).
const skillMeta = "skill"

// deliverSend sends a message of a call and follows what comes back: the
// stream, or for a push agent the answer of the send.
func (h *Host) deliverSend(c *call, m *sdk.Message) {
	if err := h.sendMessage(c, m, nil); err != nil {
		h.fail(c, fmt.Sprintf("the send failed: %v", err))
	}
}

// sendMessage sends m and takes what comes back; an error when the send did
// not get through (after its retries) or broke off where nothing can follow
// it. With a stream it returns once the stream ends. reached, when set, is
// told once the agent took the message (the answer of the send, the
// stream's first event).
func (h *Host) sendMessage(c *call, m *sdk.Message, reached func()) error {
	client := c.agent.Client()
	if client == nil {
		return fmt.Errorf("the remote agent %s is not ready", c.agent.Name)
	}
	if reached == nil {
		reached = func() {}
	}
	c.mu.Lock()
	push, token, task := c.st.Push, c.st.Token, c.st.TaskID
	c.mu.Unlock()
	ctx := c.ctx
	p := h.policy3()
	warn := retry.Warn(h.log, "sending to the remote agent", p, "agent", c.agent.Name, "call_id", c.id)
	if push {
		req := &sdk.SendMessageRequest{Message: m, Config: &sdk.SendMessageConfig{ReturnImmediately: true,
			PushConfig: &sdk.PushConfig{ID: c.id, URL: h.pushURL(c.id), Token: token}}}
		var res sdk.SendMessageResult
		err := retry.Do(ctx, p, func(ctx context.Context) error {
			var err error
			res, err = client.SendMessage(ctx, req)
			if err != nil {
				return classifySend(err)
			}
			return nil
		}, warn)
		if err != nil {
			return err
		}
		reached()
		h.take(c, res)
		return nil
	}
	req := &sdk.SendMessageRequest{Message: m}
	var got int
	err := retry.Do(ctx, p, func(ctx context.Context) error {
		n, err := h.consume(c, client.SendStreamingMessage(ctx, req), reached)
		got += n
		switch {
		case err == nil:
			return nil
		case n > 0:
			// The send reached the agent; what broke is the stream.
			return retry.Fatal(&brokenStream{err})
		}
		return classifySend(err)
	}, warn)
	var broken *brokenStream
	switch {
	case ctx.Err() != nil:
		return nil
	case err != nil && !errors.As(err, &broken):
		return err
	}
	if got == 0 && task == "" {
		return errors.New("the stream ended without an answer")
	}
	h.afterStream(c, err)
	return nil
}

// brokenStream is a stream that broke off after the send reached the agent.
type brokenStream struct{ err error }

func (b *brokenStream) Error() string { return "the stream broke off: " + b.err.Error() }
func (b *brokenStream) Unwrap() error { return b.err }

// afterStream: a stream ended. When the task waits for an answer (the
// stream closes at input-required) or the call ended, nothing more; else
// Mira subscribes to the task again, with the retries of a read.
func (h *Host) afterStream(c *call, why error) {
	c.mu.Lock()
	open := !c.ended && c.st.State != string(sdk.TaskStateInputRequired) && c.st.TaskID != ""
	task := c.st.TaskID
	c.mu.Unlock()
	if !open || c.ctx.Err() != nil {
		return
	}
	if why == nil {
		why = errors.New("the stream ended before the task")
	}
	h.log.Warn("the remote agent's stream ended early; subscribing to the task again", "agent", c.agent.Name,
		"call_id", c.id, "task_id", task, "error", why)
	h.subscribe(c)
}

// subscribe follows the task with SubscribeToTask until it ends or waits
// for an answer, tried again as a read (Retries); giving up fails the call.
func (h *Host) subscribe(c *call) {
	client := c.agent.Client()
	if client == nil {
		h.fail(c, "the remote agent is not ready to follow its task")
		return
	}
	p := h.policy3()
	err := retry.Do(c.ctx, p, func(ctx context.Context) error {
		c.mu.Lock()
		task := c.st.TaskID
		c.mu.Unlock()
		_, err := h.consume(c, client.SubscribeToTask(ctx, &sdk.SubscribeToTaskRequest{ID: sdk.TaskID(task)}), nil)
		if err != nil {
			return classify(err)
		}
		c.mu.Lock()
		open := !c.ended && c.st.State != string(sdk.TaskStateInputRequired)
		c.mu.Unlock()
		if open && ctx.Err() == nil {
			return retry.Transient(errors.New("the stream ended before the task"))
		}
		return nil
	}, retry.Warn(h.log, "following the remote agent's task", p, "agent", c.agent.Name, "call_id", c.id))
	if err != nil && c.ctx.Err() == nil {
		h.fail(c, fmt.Sprintf("its task could not be followed: %v", err))
	}
}

// consume takes a stream's events in order; it stops when the call ended or
// waits for an answer. first, when set, is told at the first event.
func (h *Host) consume(c *call, events iter.Seq2[sdk.Event, error], first func()) (int, error) {
	n := 0
	for ev, err := range events {
		if err != nil {
			return n, err
		}
		if n == 0 && first != nil {
			first()
		}
		n++
		h.take(c, ev)
		c.mu.Lock()
		stop := c.ended || c.st.State == string(sdk.TaskStateInputRequired)
		c.mu.Unlock()
		if stop {
			return n, nil
		}
	}
	return n, nil
}

func (h *Host) pushURL(callID string) string {
	return strings.TrimRight(h.serverURL, "/") + PushPath + callID
}

// take takes one event of a call, once (decision 15): a state already
// taken, an artifact already there change nothing.
func (h *Host) take(c *call, ev sdk.Event) {
	c.mu.Lock()
	if c.ended {
		task, cancel := "", false
		if t, ok := ev.(*sdk.Task); ok && c.cancelled && c.st.TaskID == "" {
			// Cancelled while the send was on its way: its task is
			// cancelled now (decision 19).
			c.st.TaskID, task, cancel = string(t.ID), string(t.ID), true
		}
		c.mu.Unlock()
		if cancel {
			h.cancelTask(c, task)
		}
		return
	}
	var status *sdk.TaskStatus
	save := false
	switch e := ev.(type) {
	case *sdk.Message:
		c.mu.Unlock()
		if text := partsText(e.Parts); text != "" {
			h.end(c, statusDone, text, "")
		} else {
			h.fail(c, "it answered with an empty message")
		}
		return
	case *sdk.Task:
		if string(e.ID) != c.st.TaskID {
			if c.st.TaskID != "" {
				// The answer to a question made a new task in the same
				// context (Hermes): the call follows it (decision 21).
				h.log.Info("remote agent goes on in a new task", "agent", c.agent.Name, "call_id", c.id,
					"task_id", c.st.TaskID, "new_task_id", e.ID)
			}
			c.st.TaskID, c.st.ContextID, c.artifacts = string(e.ID), e.ContextID, nil
			save = true
		}
		if len(e.Artifacts) > 0 {
			c.artifacts = slices.Clone(e.Artifacts)
		}
		status = &e.Status
	case *sdk.TaskStatusUpdateEvent:
		if !c.ours(string(e.TaskID)) {
			c.mu.Unlock()
			h.log.Warn("remote agent reported another task; left", "agent", c.agent.Name, "call_id", c.id,
				"task_id", e.TaskID)
			return
		}
		if c.st.TaskID == "" {
			c.st.TaskID, c.st.ContextID, save = string(e.TaskID), e.ContextID, true
		}
		status = &e.Status
	case *sdk.TaskArtifactUpdateEvent:
		if !c.ours(string(e.TaskID)) || e.Artifact == nil {
			c.mu.Unlock()
			h.log.Warn("remote agent sent an artifact of another task; left", "agent", c.agent.Name, "call_id", c.id,
				"task_id", e.TaskID)
			return
		}
		c.addArtifact(e.Artifact, e.Append)
	}
	if status == nil {
		snap := c.st
		c.mu.Unlock()
		if save {
			h.save(c, snap)
		}
		return
	}
	stateNow := status.State
	text := partsTextOf(status.Message)
	switch stateNow {
	case sdk.TaskStateCompleted:
		out := artifactsText(c.artifacts)
		c.mu.Unlock()
		if out == "" {
			out = text
		}
		if out == "" {
			h.fail(c, "it completed the task without a result")
			return
		}
		h.end(c, statusDone, out, "")
		return
	case sdk.TaskStateInputRequired:
		if c.st.State == string(stateNow) && c.st.Asked == text {
			c.mu.Unlock()
			return
		}
		c.st.State, c.st.Asked = string(stateNow), text
		snap := c.st
		c.mu.Unlock()
		h.save(c, snap)
		h.asked(c, text)
		return
	case sdk.TaskStateSubmitted, sdk.TaskStateWorking:
		if c.st.State != string(stateNow) {
			c.st.State, c.st.Asked, save = string(stateNow), "", true
		}
		snap := c.st
		c.mu.Unlock()
		if save {
			h.save(c, snap)
		}
		return
	}
	c.mu.Unlock()
	// Failed, rejected, cancelled by the agent, a login asked (until step
	// 7, decision 16), or a state Mira does not know: a failed agent (C.4).
	reason := stateWord(stateNow)
	if text != "" {
		reason += ": " + text
	}
	h.failSaid(c, reason, text)
}

// ours: an event of the call's task, or the first one naming it.
func (c *call) ours(task string) bool { return c.st.TaskID == "" || task == c.st.TaskID }

// addArtifact keeps an artifact by its id: a repeated one replaces itself,
// a chunk with append adds its parts.
func (c *call) addArtifact(a *sdk.Artifact, appendParts bool) {
	for i, have := range c.artifacts {
		if have.ID != a.ID {
			continue
		}
		if appendParts {
			joined := *have
			joined.Parts = append(slices.Clone(have.Parts), a.Parts...)
			c.artifacts[i] = &joined
			return
		}
		c.artifacts[i] = a
		return
	}
	c.artifacts = append(c.artifacts, a)
}

// stateWord names a final state in the log and the thread.
func stateWord(s sdk.TaskState) string {
	switch s {
	case sdk.TaskStateFailed:
		return "failed"
	case sdk.TaskStateRejected:
		return "rejected"
	case sdk.TaskStateCanceled:
		return "cancelled by the remote agent"
	case sdk.TaskStateAuthRequired:
		return "needs a login"
	}
	return "an unknown state " + s.String()
}

// partsText joins a message's text parts; a data part as JSON; a file by
// its name (decision 11).
func partsText(parts sdk.ContentParts) string {
	var out []string
	for _, p := range parts {
		if p == nil {
			continue
		}
		switch v := p.Content.(type) {
		case sdk.Text:
			if s := strings.TrimSpace(string(v)); s != "" {
				out = append(out, s)
			}
		case sdk.Data:
			if raw, err := json.Marshal(v.Value); err == nil {
				out = append(out, string(raw))
			}
		case sdk.Raw, sdk.URL:
			name := p.Filename
			if name == "" {
				name = "ohne Namen"
			}
			out = append(out, "[Datei: "+name+"]")
		}
	}
	return strings.Join(out, "\n")
}

func partsTextOf(m *sdk.Message) string {
	if m == nil {
		return ""
	}
	return partsText(m.Parts)
}

// artifactsText is the task's result: its artifacts' parts, in order.
func artifactsText(artifacts []*sdk.Artifact) string {
	var out []string
	for _, a := range artifacts {
		if a == nil {
			continue
		}
		if s := partsText(a.Parts); s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, "\n\n")
}

// save keeps what the call knows in its row.
func (h *Host) save(c *call, st state) {
	raw, _ := json.Marshal(st)
	if err := h.runs.SetAgentRunRemote(context.WithoutCancel(c.ctx), c.id, raw); err != nil {
		h.log.Error("could not store a remote agent's task", "agent", c.agent.Name, "call_id", c.id,
			"task_id", st.TaskID, "error", err)
	}
}

// asked hands an agent request on (D); without a way for it the call
// fails.
func (h *Host) asked(c *call, text string) {
	if h.ask == nil {
		h.fail(c, "it asked back, and Mira has no way for its question: "+text)
		return
	}
	h.log.Info("remote agent asks", "agent", c.agent.Name, "call_id", c.id, "task_id", c.taskID(), "question", text)
	if err := h.ask(context.WithoutCancel(c.ctx), Question{CallID: c.id, Call: c.rec.Call, Label: c.label, Text: text}); err != nil {
		h.fail(c, fmt.Sprintf("its question could not be passed on (%v): %s", err, text))
	}
}

func (c *call) taskID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.st.TaskID
}

// fail ends a call as a failed agent (C.5): a log line with the agent, the
// task and the reason; the thread asks the user.
func (h *Host) fail(c *call, reason string) { h.failSaid(c, reason, "") }

// failSaid is fail with the remote agent's own words (its status text),
// which the thread says to the user (decision 22).
func (h *Host) failSaid(c *call, reason, said string) {
	c.mu.Lock()
	c.st.Said = said
	snap := c.st
	c.mu.Unlock()
	h.save(c, snap)
	h.end(c, statusFailed, "", fmt.Sprintf("%s: %s", c.label, reason))
}

// Failure is, for a remote agent's call that failed, how Mira names the
// agent to the user and what it said about it (decision 22); ok is false
// for a run that is no remote agent's call.
func Failure(rec store.AgentRun) (label, said string, ok bool) {
	if len(rec.Remote) == 0 {
		return "", "", false
	}
	var st state
	if err := json.Unmarshal(rec.Remote, &st); err != nil {
		return "", "", false
	}
	label = st.Label
	if label == "" {
		label = st.Agent
	}
	return label, st.Said, true
}

// Failure is Failure for the calls of h's remote agents, and names also an
// agent whose call could not start (its card not read, decision 22): its
// card's name when it was read, else the skill's name for it, else its key.
func (h *Host) Failure(rec store.AgentRun) (label, said string, ok bool) {
	if label, said, ok := Failure(rec); ok || h == nil {
		return label, said, ok
	}
	a, found := h.Agent(rec.Agent)
	if !found {
		return "", "", false
	}
	info, _ := a.Info()
	label = info.Label
	if label == "" {
		label = strings.TrimSpace(a.def.Name)
	}
	if label == "" {
		label = a.Name
	}
	return label, "", true
}

// end ends a call once: its row, the log, its route.
func (h *Host) end(c *call, status, output, errText string) {
	c.mu.Lock()
	if c.ended {
		c.mu.Unlock()
		return
	}
	c.ended = true
	task := c.st.TaskID
	c.mu.Unlock()
	c.stop()
	h.mu.Lock()
	delete(h.calls, c.id)
	h.mu.Unlock()
	ctx := context.Background()
	// A remote agent's result is third-party text: Mira cannot know what it
	// read (decision 34).
	if err := h.runs.EndAgentRun(ctx, c.id, status, output, errText, status == statusDone, store.AgentUsage{}); err != nil {
		h.log.Error("could not end a remote agent's call", "agent", c.agent.Name, "call_id", c.id, "error", err)
	}
	switch status {
	case statusDone:
		h.log.Info("remote agent done", "agent", c.agent.Name, "call_id", c.id, "task_id", task, "chars", len(output))
	case statusFailed:
		h.log.Warn("remote agent failed", "agent", c.agent.Name, "call_id", c.id, "task_id", task, "error", errText)
	}
	if h.deliver == nil || status == statusCancelled {
		return
	}
	rec, err := h.runs.GetAgentRun(ctx, c.id)
	if err != nil {
		h.log.Error("could not read a remote agent's call to deliver it", "call_id", c.id, "error", err)
		rec = c.rec
		rec.Status, rec.Output, rec.Error = status, output, errText
	}
	h.deliver(ctx, rec)
}

// Cancel ends a call whose thread ended (decision 19): Mira sends
// CancelTask once; a cancel that fails is logged. False when no call runs
// under that id.
func (h *Host) Cancel(callID string) bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	c := h.calls[callID]
	h.mu.Unlock()
	if c == nil {
		return false
	}
	c.mu.Lock()
	if c.ended {
		c.mu.Unlock()
		return false
	}
	c.cancelled = true
	task := c.st.TaskID
	c.mu.Unlock()
	h.end(c, statusCancelled, "", "the thread ended")
	h.log.Info("remote agent's call cancelled", "agent", c.agent.Name, "call_id", c.id, "task_id", task)
	if task != "" {
		h.wg.Go(func() { h.cancelTask(c, task) })
	}
	return true
}

// cancelTask sends CancelTask once.
func (h *Host) cancelTask(c *call, task string) {
	client := c.agent.Client()
	if client == nil {
		h.log.Warn("could not cancel the remote agent's task: the agent is not ready", "agent", c.agent.Name,
			"call_id", c.id, "task_id", task)
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(h.base), 30*time.Second)
	defer cancel()
	if _, err := client.CancelTask(ctx, &sdk.CancelTaskRequest{ID: sdk.TaskID(task)}); err != nil {
		h.log.Warn("could not cancel the remote agent's task", "agent", c.agent.Name, "call_id", c.id,
			"task_id", task, "error", err)
		return
	}
	h.log.Info("remote agent's task cancelled", "agent", c.agent.Name, "call_id", c.id, "task_id", task)
}

// ServePush takes a push of a remote agent (E): the call by its id, the
// token from A2A-Notification-Token or Authorization: Bearer (decision 14).
// It answers at once and hands the event on; it never blocks.
func (h *Host) ServePush(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("call")
	token := r.Header.Get("A2A-Notification-Token")
	if token == "" {
		token, _ = strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	var c *call
	if h != nil {
		h.mu.Lock()
		c = h.calls[id]
		h.mu.Unlock()
	}
	if c == nil {
		if h != nil && h.endedCall(r.Context(), id, token) {
			h.log.Info("push for a remote agent's call that has ended; nothing to do", "call_id", id, "from", r.RemoteAddr)
			w.WriteHeader(http.StatusOK)
			return
		}
		h.logger().Warn("push refused: no such call", "call_id", id, "from", r.RemoteAddr)
		http.Error(w, "unknown call or wrong token", http.StatusUnauthorized)
		return
	}
	c.mu.Lock()
	want := c.st.Token
	c.mu.Unlock()
	if want == "" || subtle.ConstantTimeCompare([]byte(token), []byte(want)) != 1 {
		h.log.Warn("push refused: wrong token", "call_id", id, "agent", c.agent.Name, "from", r.RemoteAddr)
		http.Error(w, "unknown call or wrong token", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		h.log.Warn("push broke off", "call_id", id, "agent", c.agent.Name, "from", r.RemoteAddr, "error", err)
		http.Error(w, "the body could not be read", http.StatusBadRequest)
		return
	}
	var sr sdk.StreamResponse
	if err := json.Unmarshal(body, &sr); err != nil || sr.Event == nil {
		h.log.Warn("push not readable", "call_id", id, "agent", c.agent.Name, "from", r.RemoteAddr, "error", err,
			"body", bodyStart(body))
		http.Error(w, "the body is no A2A event", http.StatusBadRequest)
		return
	}
	h.enqueue(c, sr.Event)
	w.WriteHeader(http.StatusOK)
}

func (h *Host) logger() interface {
	Warn(msg string, args ...any)
} {
	if h == nil {
		return nopLog{}
	}
	return h.log
}

type nopLog struct{}

func (nopLog) Warn(string, ...any) {}

// endedCall: a call of that id ended, and the token is its own.
func (h *Host) endedCall(ctx context.Context, id, token string) bool {
	if h.runs == nil || token == "" {
		return false
	}
	rec, err := h.runs.GetAgentRun(ctx, id)
	if err != nil || rec.Status == statusRunning || len(rec.Remote) == 0 {
		return false
	}
	var st state
	if json.Unmarshal(rec.Remote, &st) != nil || st.Token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(st.Token)) == 1
}

// enqueue hands a push to the call's queue, taken in order by one goroutine.
func (h *Host) enqueue(c *call, ev sdk.Event) {
	c.mu.Lock()
	c.queue = append(c.queue, ev)
	start := !c.draining
	c.draining = true
	c.mu.Unlock()
	if start {
		h.wg.Go(func() { h.drain(c) })
	}
}

func (h *Host) drain(c *call) {
	for {
		c.mu.Lock()
		if len(c.queue) == 0 {
			c.draining = false
			c.mu.Unlock()
			return
		}
		ev := c.queue[0]
		c.queue = c.queue[1:]
		c.mu.Unlock()
		h.take(c, ev)
	}
}

func (h *Host) now() time.Time {
	if h.clock != nil {
		return h.clock()
	}
	return time.Now()
}

// Asking is the question an agent request of a call still waits on, and
// how Mira names the agent; ok false when the call asks nothing.
func (h *Host) Asking(callID string) (label, question string, ok bool) {
	if h == nil {
		return "", "", false
	}
	h.mu.Lock()
	c := h.calls[callID]
	h.mu.Unlock()
	if c == nil {
		return "", "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ended || c.st.State != string(sdk.TaskStateInputRequired) {
		return "", "", false
	}
	return c.label, c.st.Asked, true
}

// Answer sends the owner's answer to a call's agent request (D.3, decision
// 21): a new message with the task and context id, the push configuration
// again for an agent without a stream (its answer may make a new task); the
// call follows whatever task comes back. It returns once the agent took the
// answer; an error when it did not (after the retries of a send), and the
// question stays open.
func (h *Host) Answer(ctx context.Context, callID, text string) error {
	if h == nil {
		return errors.New("no remote agents")
	}
	h.mu.Lock()
	c := h.calls[callID]
	h.mu.Unlock()
	if c == nil {
		return fmt.Errorf("no open call %s", callID)
	}
	c.mu.Lock()
	if c.ended || c.st.State != string(sdk.TaskStateInputRequired) {
		c.mu.Unlock()
		return fmt.Errorf("%s asks nothing now", c.label)
	}
	c.st.Sent++
	m := sdk.NewMessage(sdk.MessageRoleUser, sdk.NewTextPart(text))
	m.ID = fmt.Sprintf("%s-%d", c.id, c.st.Sent)
	m.TaskID, m.ContextID = sdk.TaskID(c.st.TaskID), c.st.ContextID
	snap := c.st
	c.mu.Unlock()
	if skill := c.agent.def.Remote.A2ASkill; skill != "" {
		m.Metadata = map[string]any{skillMeta: skill}
	}
	h.save(c, snap)
	result := make(chan error, 1)
	var once sync.Once
	tell := func(err error) { once.Do(func() { result <- err }) }
	h.wg.Go(func() {
		err := h.sendMessage(c, m, func() { tell(nil) })
		if err != nil {
			tell(err)
			h.log.Warn("the answer did not reach the remote agent", "agent", c.agent.Name, "call_id", c.id,
				"task_id", snap.TaskID, "error", err)
			return
		}
		tell(nil)
	})
	select {
	case err := <-result:
		if err == nil {
			h.log.Info("answer passed to the remote agent", "agent", c.agent.Name, "call_id", c.id, "task_id", snap.TaskID)
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Resume takes up the calls that were open when Mira stopped (G, decision
// 20), once the cards were read: it asks each task once with GetTask and
// takes the state reached meanwhile; a task still working is followed again
// (SubscribeToTask) where Mira streams, or waits for its pushes. A call
// without a task, of an agent no longer there, or whose task the agent no
// longer knows (TaskNotFound) is a failed agent. It does not wait.
func (h *Host) Resume(ctx context.Context) {
	if h == nil || h.runs == nil {
		return
	}
	h.wg.Go(func() {
		select {
		case <-h.read:
		case <-ctx.Done():
			return
		case <-h.base.Done():
			return
		}
		runs, err := h.runs.RunningAgentRuns(ctx)
		if err != nil {
			h.log.Error("could not read the open calls of remote agents; they stay open until their threads end", "error", err)
			return
		}
		for _, rec := range runs {
			if len(rec.Remote) == 0 {
				continue
			}
			h.takeUp(rec)
		}
	})
}

// takeUp takes up one open call after a restart.
func (h *Host) takeUp(rec store.AgentRun) {
	var st state
	if err := json.Unmarshal(rec.Remote, &st); err != nil {
		st = state{Agent: rec.Agent}
		c := h.track(rec, &Agent{Name: rec.Agent}, rec.Agent, st)
		h.fail(c, fmt.Sprintf("its call cannot be read after the restart (%v): %q", err, bodyStart(rec.Remote)))
		return
	}
	a, ok := h.agentOf(st.Skill, st.Agent)
	if !ok {
		c := h.track(rec, &Agent{Skill: st.Skill, Name: st.Agent}, st.Label, st)
		h.fail(c, "the remote agent is no longer in its skill")
		return
	}
	label := st.Label
	if info, ok := a.Info(); ok && info.Label != "" {
		label = info.Label
	}
	c := h.track(rec, a, label, st)
	if st.TaskID == "" {
		h.fail(c, "Mira stopped before the remote agent answered the send")
		return
	}
	client := a.Client()
	if client == nil {
		h.fail(c, "its task cannot be asked after the restart: "+h.why(a))
		return
	}
	h.log.Info("remote agent's call taken up after the restart", "agent", a.Name, "call_id", c.id, "task_id", st.TaskID,
		"state", st.State)
	h.wg.Go(func() {
		p := h.policy3()
		var task *sdk.Task
		err := retry.Do(c.ctx, p, func(ctx context.Context) error {
			var err error
			task, err = client.GetTask(ctx, &sdk.GetTaskRequest{ID: sdk.TaskID(st.TaskID)})
			if err != nil {
				return classify(err)
			}
			return nil
		}, retry.Warn(h.log, "asking the remote agent's task", p, "agent", a.Name, "call_id", c.id))
		switch {
		case c.ctx.Err() != nil:
			return
		case errors.Is(err, sdk.ErrTaskNotFound):
			h.fail(c, "it no longer knows the task after Mira's restart")
			return
		case err != nil:
			h.fail(c, fmt.Sprintf("its task could not be asked after the restart: %v", err))
			return
		}
		h.take(c, task)
		c.mu.Lock()
		follow := !c.ended && c.st.State != string(sdk.TaskStateInputRequired) && !c.st.Push
		c.mu.Unlock()
		if follow {
			h.subscribe(c)
		}
	})
}

// agentOf is the remote agent of a skill by its name.
func (h *Host) agentOf(skill, name string) (*Agent, bool) {
	for _, a := range h.agents {
		if a.Name == name && (skill == "" || a.Skill == skill) {
			return a, true
		}
	}
	return nil, false
}
