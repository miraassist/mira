package a2a

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	sdk "github.com/a2aproject/a2a-go/v2/a2a"

	"github.com/miraassist/mira/internal/a2a/transport"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/retry"
	"github.com/miraassist/mira/internal/store"
)

// The calls of the remote agents (docs/spec/remote-agents.md, B to E and
// G; docs/spec/remote-agent-calls.md): a thread's field or plan is due, Mira
// sends the work and hands the result to the thread as an agent's run is
// handed to it. Each call is a row of agent_runs with the remote task in
// its column remote. A call has a transport (package transport) that sends
// and receives, and one loop that handles, in order and alone, what the
// transport reports and what Mira asks: only the loop reads and writes the
// call's state, stores it, closes the transport and tells the thread.

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

// wire is what the loop needs of its transport (transport.T; a fake in
// tests).
type wire interface {
	Send(m *sdk.Message)
	Follow(task string, attempt int)
	Get(task string)
	Cancel(task string)
	Close()
}

// call is one call: its loop and what only the loop touches.
type call struct {
	h     *Host
	id    string
	agent *Agent
	label string
	rec   store.AgentRun
	tr    wire

	in inbox

	// Only the loop's goroutine reads and writes these.
	st        state
	artifacts []*sdk.Artifact
	ended     bool
	cancelled bool
	// heard: the agent reported something for the call since it started.
	heard bool
	// inflight counts the transport's operations not yet over: the loop
	// runs until the call ended and they are (decision 13).
	inflight int
	// rank is how far the task got since Mira's last message: a state that
	// comes later but is behind it is stale (decision 37a).
	rank int
	// answer is the owner's answer on its way (decision 11).
	answer *pendingAnswer
	// follows counts the subscriptions after a stream that ended early.
	follows int

	// view is what others may read (decision 14), published by the loop.
	viewMu sync.Mutex
	view   view
}

type view struct {
	ended         bool
	state, asked  string
	label, taskID string
}

type pendingAnswer struct {
	messageID string
	reply     chan error
}

// The loop's inputs besides the transport's reports.
type (
	answerIn struct {
		text  string
		reply chan error
	}
	cancelIn struct{}
	// runIn is a step of the loop's own: its start, a take-up.
	runIn struct{ fn func() }
)

// inbox is the loop's queue: never full, so neither the transport nor Mira
// ever waits to put something in.
type inbox struct {
	mu    sync.Mutex
	items []any
	ready chan struct{}
}

func (q *inbox) put(v any) {
	q.mu.Lock()
	q.items = append(q.items, v)
	q.mu.Unlock()
	select {
	case q.ready <- struct{}{}:
	default:
	}
}

func (q *inbox) take() []any {
	q.mu.Lock()
	defer q.mu.Unlock()
	items := q.items
	q.items = nil
	return items
}

// Push is a remote agent's push as the A2A protocol defines it (a
// StreamResponse: a task, a message, a status or an artifact update); here
// only to describe the route, as an open object: the remote agent's
// protocol, not Mira's.
type Push map[string]any

func (h *Host) now() time.Time { return h.clock.Now() }

// StatusError is an answer of a remote agent with an HTTP status Mira reads
// as an error (package transport).
type StatusError = transport.StatusError

func (h *Host) policy3() retry.Policy {
	return retry.Policy{Attempts: 3, First: h.callRetry, Growth: retry.Exponential, Clock: h.clock}
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

// Send starts a call: its row, then its loop, which sends. An error when the
// agent cannot take work now (its card not read, the skill's connection not
// made) or the row cannot be written; the caller fails the field with it.
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
	way := "stream"
	if st.Push {
		way = "push"
	}
	h.log.Info("remote agent called", "agent", a.Name, "name", info.Label, "call_id", rec.CallID, "way", way,
		"endpoint", info.Endpoint)
	c := h.track(rec, a, info.Label, st, client)
	c.in.put(runIn{func() { c.send(msg) }})
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

// track makes a call with its transport (none without a client) and starts
// its loop.
func (h *Host) track(rec store.AgentRun, a *Agent, label string, st state, client transport.Client) *call {
	c := h.newCall(rec, a, label, st)
	if client != nil {
		t := transport.Open(transport.Options{Client: client, Push: st.Push, PushURL: h.pushURL(c.id), Token: st.Token,
			Base: h.base, Retry: h.policy3(), Report: func(r transport.Report) { c.in.put(r) }, Agent: a.Name,
			Call: c.id, Log: h.log}, &h.wg)
		c.tr = t
		if st.Push {
			h.router.Add(c.id, t)
		}
	}
	h.start(c)
	return c
}

// newCall makes a call without its transport and without starting it.
func (h *Host) newCall(rec store.AgentRun, a *Agent, label string, st state) *call {
	return &call{h: h, id: rec.CallID, agent: a, label: label, rec: rec, st: st, in: inbox{ready: make(chan struct{}, 1)}}
}

// start lists a call and starts its loop.
func (h *Host) start(c *call) {
	c.publish()
	h.mu.Lock()
	h.calls[c.id] = c
	h.mu.Unlock()
	h.wg.Go(c.run)
}

// run is the loop: it takes the inputs in order until the call ended and
// nothing of its transport is under way, or the service stops (the call
// then stays running in its row, G).
func (c *call) run() {
	for {
		select {
		case <-c.in.ready:
		case <-c.h.base.Done():
			return
		}
		for _, v := range c.in.take() {
			c.step(v)
		}
		if c.ended && c.inflight == 0 {
			return
		}
	}
}

// step handles one input.
func (c *call) step(v any) {
	switch v := v.(type) {
	case runIn:
		v.fn()
	case answerIn:
		c.doAnswer(v)
	case cancelIn:
		c.doCancel()
	case transport.Received:
		c.agentReported()
		c.take(v.Event)
	case transport.Answered:
		c.inflight--
		if c.answer != nil && c.answer.messageID == v.MessageID {
			c.answerPassed()
		}
		c.take(v.Event)
	case transport.SendFailed:
		c.inflight--
		c.sendFailed(v)
	case transport.StreamEnded:
		c.inflight--
		c.streamEnded(v)
	case transport.Got:
		c.inflight--
		c.gotTask(v)
	case transport.CancelDone:
		c.inflight--
		if v.Err != nil {
			c.h.log.Warn("could not cancel the remote agent's task", "agent", c.agent.Name, "call_id", c.id,
				"task_id", v.TaskID, "error", v.Err)
			return
		}
		c.h.log.Info("remote agent's task cancelled", "agent", c.agent.Name, "call_id", c.id, "task_id", v.TaskID)
	}
}

// publish keeps a copy of what others may read (decision 14).
func (c *call) publish() {
	c.viewMu.Lock()
	defer c.viewMu.Unlock()
	c.view = view{ended: c.ended, state: c.st.State, asked: c.st.Asked, label: c.label, taskID: c.st.TaskID}
}

func (c *call) read() view {
	c.viewMu.Lock()
	defer c.viewMu.Unlock()
	return c.view
}

// send sends a message of the call.
func (c *call) send(m *sdk.Message) {
	if c.tr == nil {
		c.fail(fmt.Sprintf("the remote agent %s is not ready", c.agent.Name))
		return
	}
	c.inflight++
	c.tr.Send(m)
}

// agentReported: the agent reported something for the call. An answer on
// its way counts as passed on (decision 11), before the report is taken, so
// the owner hears it before the thread gets what followed (decision 12).
func (c *call) agentReported() {
	c.heard = true
	if c.answer != nil {
		c.answerPassed()
	}
}

func (c *call) answerPassed() {
	c.h.log.Info("answer passed to the remote agent", "agent", c.agent.Name, "call_id", c.id, "task_id", c.st.TaskID)
	c.answer.reply <- nil
	c.answer = nil
}

// sendFailed: the first send failed → a failed agent, unless the agent
// already reported on it; an answer → its caller hears it (D.4), unless
// decision 11 counted it passed.
func (c *call) sendFailed(v transport.SendFailed) {
	switch {
	case c.answer != nil && c.answer.messageID == v.MessageID:
		// The trace first: the caller goes on once told.
		c.h.log.Warn("the answer did not reach the remote agent", "agent", c.agent.Name, "call_id", c.id,
			"task_id", c.st.TaskID, "error", v.Err)
		c.answer.reply <- v.Err
		c.answer = nil
	case v.MessageID == c.id && !c.ended && !c.heard:
		c.fail(fmt.Sprintf("the send failed: %v", v.Err))
	default:
		c.h.log.Info("a send to the remote agent failed after the agent reported on it; nothing changes",
			"agent", c.agent.Name, "call_id", c.id, "message_id", v.MessageID, "error", v.Err)
	}
}

// streamEnded: a stream is over. When the call ended, its task waits for an
// answer, or the service stops, nothing more; a first stream without any
// answer fails the call; else Mira subscribes to the task again (decision
// 35), up to three times, and giving up fails the call.
func (c *call) streamEnded(v transport.StreamEnded) {
	if v.Events > 0 {
		c.follows = 0
	}
	if c.ended || c.h.base.Err() != nil {
		return
	}
	var broken *transport.BrokenStream
	if v.Err != nil && v.MessageID == "" && !errors.As(v.Err, &broken) {
		c.fail(fmt.Sprintf("its task could not be followed: %v", v.Err))
		return
	}
	if v.MessageID != "" && v.Events == 0 && c.st.TaskID == "" {
		c.fail("the send failed: the stream ended without an answer")
		return
	}
	if c.st.State == string(sdk.TaskStateInputRequired) || c.st.TaskID == "" {
		return
	}
	why := v.Err
	if why == nil {
		why = errors.New("the stream ended before the task")
	}
	if c.follows >= 3 {
		c.fail(fmt.Sprintf("its task could not be followed: %v", why))
		return
	}
	if v.MessageID != "" {
		c.h.log.Warn("the remote agent's stream ended early; subscribing to the task again", "agent", c.agent.Name,
			"call_id", c.id, "task_id", c.st.TaskID, "error", why)
	}
	c.follow()
}

// follow subscribes to the task again (stream only).
func (c *call) follow() {
	if c.tr == nil {
		c.fail("the remote agent is not ready to follow its task")
		return
	}
	c.inflight++
	c.tr.Follow(c.st.TaskID, c.follows)
	c.follows++
}

// gotTask takes the answer of the one GetTask after a restart (G).
func (c *call) gotTask(v transport.Got) {
	switch {
	case c.ended || c.h.base.Err() != nil:
		return
	case errors.Is(v.Err, sdk.ErrTaskNotFound):
		c.fail("it no longer knows the task after Mira's restart")
		return
	case v.Err != nil:
		c.fail(fmt.Sprintf("its task could not be asked after the restart: %v", v.Err))
		return
	}
	c.take(v.Task)
	if !c.ended && c.st.State != string(sdk.TaskStateInputRequired) && !c.st.Push {
		c.follow()
	}
}

func (h *Host) pushURL(callID string) string {
	return strings.TrimRight(h.serverURL, "/") + PushPath + callID
}

// take takes one event of a call, once (decision 15): a state already
// taken, an artifact already there change nothing.
func (c *call) take(ev sdk.Event) {
	if ev == nil {
		return
	}
	if c.ended {
		if t, ok := ev.(*sdk.Task); ok && c.cancelled && c.st.TaskID == "" && c.tr != nil {
			// Cancelled while the send was on its way: its task is
			// cancelled now (decision 19).
			c.st.TaskID = string(t.ID)
			c.inflight++
			c.tr.Cancel(c.st.TaskID)
		}
		return
	}
	defer c.publish()
	var status *sdk.TaskStatus
	save := false
	switch e := ev.(type) {
	case *sdk.Message:
		if text := partsText(e.Parts); text != "" {
			c.end(statusDone, text, "")
		} else {
			c.fail("it answered with an empty message")
		}
		return
	case *sdk.Task:
		if string(e.ID) != c.st.TaskID {
			if c.st.TaskID != "" {
				// The answer to a question made a new task in the same
				// context (Hermes): the call follows it (decision 21).
				c.h.log.Info("remote agent goes on in a new task", "agent", c.agent.Name, "call_id", c.id,
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
			c.h.log.Warn("remote agent reported another task; left", "agent", c.agent.Name, "call_id", c.id,
				"task_id", e.TaskID)
			return
		}
		if c.st.TaskID == "" {
			c.st.TaskID, c.st.ContextID, save = string(e.TaskID), e.ContextID, true
		}
		status = &e.Status
	case *sdk.TaskArtifactUpdateEvent:
		if !c.ours(string(e.TaskID)) || e.Artifact == nil {
			c.h.log.Warn("remote agent sent an artifact of another task; left", "agent", c.agent.Name, "call_id", c.id,
				"task_id", e.TaskID)
			return
		}
		c.addArtifact(e.Artifact, e.Append)
	}
	if status == nil {
		if save {
			c.save()
		}
		return
	}
	stateNow := status.State
	text := partsTextOf(status.Message)
	if r, ok := lifecycle[stateNow]; ok {
		if r < c.rank {
			// The send's answer and the pushes come side by side: a state
			// behind the one taken since Mira's last message is stale.
			c.h.log.Debug("remote agent's stale state left", "agent", c.agent.Name, "call_id", c.id,
				"state", stateNow, "task_id", c.st.TaskID)
			if save {
				c.save()
			}
			return
		}
		c.rank = r
	}
	switch stateNow {
	case sdk.TaskStateCompleted:
		out := artifactsText(c.artifacts)
		if out == "" {
			out = text
		}
		if out == "" {
			c.fail("it completed the task without a result")
			return
		}
		c.end(statusDone, out, "")
		return
	case sdk.TaskStateInputRequired:
		if c.st.State == string(stateNow) && c.st.Asked == text {
			if save {
				c.save()
			}
			return
		}
		c.st.State, c.st.Asked = string(stateNow), text
		c.save()
		c.asked(text)
		return
	case sdk.TaskStateSubmitted, sdk.TaskStateWorking:
		if c.st.State != string(stateNow) {
			c.st.State, c.st.Asked, save = string(stateNow), "", true
		}
		if save {
			c.save()
		}
		return
	}
	// Failed, rejected, cancelled by the agent, a login asked (until step
	// 7, decision 16), or a state Mira does not know: a failed agent (C.4).
	reason := stateWord(stateNow)
	if text != "" {
		reason += ": " + text
	}
	c.failSaid(reason, text)
}

// lifecycle ranks the states a task goes through while Mira sends nothing
// new: it goes forward only (decision 37a).
var lifecycle = map[sdk.TaskState]int{
	sdk.TaskStateSubmitted:     0,
	sdk.TaskStateWorking:       1,
	sdk.TaskStateInputRequired: 2,
	sdk.TaskStateAuthRequired:  2,
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

// save keeps what the call knows in its row; only the loop writes it.
func (c *call) save() {
	raw, _ := json.Marshal(c.st)
	if err := c.h.runs.SetAgentRunRemote(context.WithoutCancel(c.h.base), c.id, raw); err != nil {
		c.h.log.Error("could not store a remote agent's task", "agent", c.agent.Name, "call_id", c.id,
			"task_id", c.st.TaskID, "error", err)
	}
}

// asked hands an agent request on (D); without a way for it the call
// fails.
func (c *call) asked(text string) {
	if c.h.ask == nil {
		c.fail("it asked back, and Mira has no way for its question: " + text)
		return
	}
	c.publish()
	c.h.log.Info("remote agent asks", "agent", c.agent.Name, "call_id", c.id, "task_id", c.st.TaskID, "question", text)
	q := Question{CallID: c.id, Call: c.rec.Call, Label: c.label, Text: text}
	if err := c.h.ask(context.WithoutCancel(c.h.base), q); err != nil {
		c.fail(fmt.Sprintf("its question could not be passed on (%v): %s", err, text))
	}
}

// fail ends a call as a failed agent (C.5): a log line with the agent, the
// task and the reason; the thread asks the user.
func (c *call) fail(reason string) { c.failSaid(reason, "") }

// failSaid is fail with the remote agent's own words (its status text),
// which the thread says to the user (decision 22).
func (c *call) failSaid(reason, said string) {
	if c.ended {
		return
	}
	c.st.Said = said
	c.save()
	c.end(statusFailed, "", fmt.Sprintf("%s: %s", c.label, reason))
}

// end ends a call once: its row, the transport, the log, its route.
func (c *call) end(status, output, errText string) {
	if c.ended {
		return
	}
	c.ended = true
	c.publish()
	if c.tr != nil {
		c.tr.Close()
	}
	h := c.h
	h.router.Remove(c.id)
	h.mu.Lock()
	delete(h.calls, c.id)
	h.mu.Unlock()
	if c.answer != nil {
		c.answer.reply <- fmt.Errorf("%s asks nothing now", c.label)
		c.answer = nil
	}
	ctx := context.WithoutCancel(h.base)
	// A remote agent's result is third-party text: Mira cannot know what it
	// read (decision 34).
	if err := h.runs.EndAgentRun(ctx, c.id, status, output, errText, status == statusDone, store.AgentUsage{}); err != nil {
		h.log.Error("could not end a remote agent's call", "agent", c.agent.Name, "call_id", c.id, "error", err)
	}
	switch status {
	case statusDone:
		h.log.Info("remote agent done", "agent", c.agent.Name, "call_id", c.id, "task_id", c.st.TaskID, "chars", len(output))
	case statusFailed:
		h.log.Warn("remote agent failed", "agent", c.agent.Name, "call_id", c.id, "task_id", c.st.TaskID, "error", errText)
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

// lookup is the open call of that id.
func (h *Host) lookup(callID string) *call {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls[callID]
}

// Cancel ends a call whose thread ended (decision 19): its loop ends it and
// sends CancelTask once; a cancel that fails is logged. False when no call
// runs under that id. It does not wait for the loop.
func (h *Host) Cancel(callID string) bool {
	c := h.lookup(callID)
	if c == nil || c.read().ended {
		return false
	}
	c.in.put(cancelIn{})
	return true
}

// doCancel is Cancel in the loop.
func (c *call) doCancel() {
	if c.ended {
		return
	}
	c.cancelled = true
	task := c.st.TaskID
	c.end(statusCancelled, "", "the thread ended")
	c.h.log.Info("remote agent's call cancelled", "agent", c.agent.Name, "call_id", c.id, "task_id", task)
	if task == "" {
		return
	}
	if c.tr == nil {
		c.h.log.Warn("could not cancel the remote agent's task: the agent is not ready", "agent", c.agent.Name,
			"call_id", c.id, "task_id", task)
		return
	}
	c.inflight++
	c.tr.Cancel(task)
}

// ServePush takes a push of a remote agent (E): the push route of the
// transports.
func (h *Host) ServePush(w http.ResponseWriter, r *http.Request) {
	if h == nil {
		http.Error(w, "unknown call or wrong token", http.StatusUnauthorized)
		return
	}
	h.router.ServeHTTP(w, r)
}

// endedCall: a call of that id ended, and the token is its own.
func (h *Host) endedCall(r *http.Request, id, token string) bool {
	if h.runs == nil || token == "" {
		return false
	}
	rec, err := h.runs.GetAgentRun(r.Context(), id)
	if err != nil || rec.Status == statusRunning || len(rec.Remote) == 0 {
		return false
	}
	var st state
	if json.Unmarshal(rec.Remote, &st) != nil || st.Token == "" {
		return false
	}
	return st.Token == token
}

// Asking is the question an agent request of a call still waits on, and
// how Mira names the agent; ok false when the call asks nothing.
func (h *Host) Asking(callID string) (label, question string, ok bool) {
	c := h.lookup(callID)
	if c == nil {
		return "", "", false
	}
	v := c.read()
	if v.ended || v.state != string(sdk.TaskStateInputRequired) {
		return "", "", false
	}
	return v.label, v.asked, true
}

// Answer sends the owner's answer to a call's agent request (D.3, decision
// 21): a new message with the task and context id, the push configuration
// again for an agent without a stream (its answer may make a new task); the
// call follows whatever task comes back. It returns once the answer counts
// as passed on (decision 11); an error when the send failed before, and the
// question stays open.
func (h *Host) Answer(ctx context.Context, callID, text string) error {
	if h == nil {
		return errors.New("no remote agents")
	}
	c := h.lookup(callID)
	if c == nil {
		return fmt.Errorf("no open call %s", callID)
	}
	reply := make(chan error, 1)
	c.in.put(answerIn{text: text, reply: reply})
	select {
	case err := <-reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-h.base.Done():
		return errors.New("the Mira service stops")
	}
}

// doAnswer is Answer in the loop.
func (c *call) doAnswer(v answerIn) {
	if c.ended || c.st.State != string(sdk.TaskStateInputRequired) {
		v.reply <- fmt.Errorf("%s asks nothing now", c.label)
		return
	}
	if c.answer != nil {
		v.reply <- fmt.Errorf("an answer to %s is already on its way", c.label)
		return
	}
	c.st.Sent++
	c.rank = 0 // Mira's new message: the task may start over
	m := sdk.NewMessage(sdk.MessageRoleUser, sdk.NewTextPart(v.text))
	m.ID = fmt.Sprintf("%s-%d", c.id, c.st.Sent)
	m.TaskID, m.ContextID = sdk.TaskID(c.st.TaskID), c.st.ContextID
	if skill := c.agent.def.Remote.A2ASkill; skill != "" {
		m.Metadata = map[string]any{skillMeta: skill}
	}
	c.save()
	c.answer = &pendingAnswer{messageID: m.ID, reply: v.reply}
	c.send(m)
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
		c := h.track(rec, &Agent{Name: rec.Agent}, rec.Agent, st, nil)
		c.in.put(runIn{func() {
			c.fail(fmt.Sprintf("its call cannot be read after the restart (%v): %q", err, bodyStart(rec.Remote)))
		}})
		return
	}
	a, ok := h.agentOf(st.Skill, st.Agent)
	if !ok {
		c := h.track(rec, &Agent{Skill: st.Skill, Name: st.Agent}, st.Label, st, nil)
		c.in.put(runIn{func() { c.fail("the remote agent is no longer in its skill") }})
		return
	}
	label := st.Label
	if info, ok := a.Info(); ok && info.Label != "" {
		label = info.Label
	}
	client := a.Client()
	var tc transport.Client
	if client != nil {
		tc = client
	}
	c := h.track(rec, a, label, st, tc)
	c.in.put(runIn{func() {
		switch {
		case st.TaskID == "":
			c.fail("Mira stopped before the remote agent answered the send")
		case c.tr == nil:
			c.fail("its task cannot be asked after the restart: " + h.why(a))
		default:
			h.log.Info("remote agent's call taken up after the restart", "agent", a.Name, "call_id", c.id,
				"task_id", st.TaskID, "state", st.State)
			c.inflight++
			c.tr.Get(st.TaskID)
		}
	}})
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
