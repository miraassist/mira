// Package agent is Mira's agent building block (docs/spec/agenten.md). An
// agent is a model with a context profile, tools, rights, an input and an
// output; it is callable like a tool. Spec is its static description,
// CallContext what comes with each call, and Runtime runs a call.
//
// The testable rules are the scenarios in docs/spec/agenten.feature; each
// test here carries the scenario's number in its name (TestAG07_…).
package agent

import (
	"encoding/json"
	"errors"
	"time"
)

// Profile is what the model sees (docs/spec/agenten.md, Context profiles).
type Profile int

const (
	// Isolated: only the agent's instructions as the system message and the
	// input; no house, no tools, no memory.
	Isolated Profile = iota
	// Background: the background prefix with the house and the tools the
	// rights allow, then the instructions and the input as the turn.
	Background
	// Conversation: everything, with the session's history. The
	// conversation agents run in package conversation.
	Conversation
)

func (p Profile) String() string {
	switch p {
	case Background:
		return "background"
	case Conversation:
		return "conversation"
	}
	return "isolated"
}

// ToolKind says which tools an agent can call.
type ToolKind int

const (
	NoTools ToolKind = iota
	ReadOnlyTools
	AllTools
	ListedTools
)

// Tools are the tools an agent can call: none, the read-only ones, all, or
// the ones named (qualified names as the model sees them).
type Tools struct {
	Kind  ToolKind
	Names []string
}

// Limits bound a run. Zero values take the defaults.
type Limits struct {
	// MaxRounds bounds the tool rounds; default the model's max_tool_rounds,
	// else 3.
	MaxRounds int
	// AttemptTimeout bounds one model call; an attempt that takes longer is
	// tried again (Retries). Zero means no bound of its own.
	AttemptTimeout time.Duration
	// Retries is how often a failed or hanging model call is tried again.
	Retries int
}

// Spec is an agent's static description, the same on every call.
type Spec struct {
	Name string
	// Description says when to call it, for a calling model.
	Description string
	// Input is the JSON schema of the input; nil for plain text.
	Input json.RawMessage
	// Output is the JSON schema of the answer; nil for text.
	Output json.RawMessage
	// Role is the agent's role in routing; without an entry the model is
	// main.
	Role string
	// Model names a model directly (agents from YAML); it wins over Role.
	Model   string
	Profile Profile
	// Instructions are the system message (isolated) or the instructions
	// before the input (background).
	Instructions string
	Tools        Tools
	// Rights are the most the agent needs.
	Rights Rights
	Limits Limits
}

// Origin is who calls.
type Origin string

// The origins of docs/spec/agenten.md, The call context: Home Assistant
// (an automation, Assist, Telegram, IMAP), Mira's timers, the Mira page,
// another agent, and Mira's own code (the notifier, say).
const (
	FromAutomation Origin = "automation"
	FromAssist     Origin = "assist"
	FromTelegram   Origin = "telegram"
	FromIMAP       Origin = "imap"
	FromSchedule   Origin = "schedule"
	FromPage       Origin = "page"
	FromAgent      Origin = "agent"
	FromCode       Origin = "code"
)

// OnTimeout is what a synchronous call does at its timeout.
type OnTimeout int

const (
	// Cancel ends the run; the caller gets that the time is up.
	Cancel OnTimeout = iota
	// RunOn lets the run go on and delivers its result asynchronously.
	RunOn
)

// Mode is how the caller waits, and where an asynchronous result goes.
type Mode struct {
	Async bool
	// Timeout bounds a synchronous call; zero means none.
	Timeout   time.Duration
	OnTimeout OnTimeout
	Delivery  Delivery
}

// Route is how a finished asynchronous run reaches its caller
// (docs/spec/agenten.md, Delivery).
type Route string

const (
	// ToEvent: Home Assistant fires mira_agent_done.
	ToEvent Route = "event"
	// ToMessage: a message to a person or the house, worded by
	// notify.compose.
	ToMessage Route = "message"
)

// Delivery is the route; for a message also its receiver (a person's key or
// "house") and the room it is for, if any.
type Delivery struct {
	Route    Route
	Receiver string
	Area     string
}

func (m Mode) String() string {
	if m.Async {
		return "async"
	}
	return "sync"
}

// CallContext is what comes with each call. A call made inside a run takes
// its parent's context from ctx, narrower, never wider.
type CallContext struct {
	CallID, ParentID string
	Origin           Origin
	// Person, Place (an area id) and Lang, as far as known.
	Person, Place, Lang string
	// SessionKey and DeviceID tie tool calls to a conversation and a
	// satellite, as tools.Invocation does.
	SessionKey, DeviceID string
	Rights               Rights
	Depth                int
	// Foreign: third-party text is already involved in the call.
	Foreign bool
	Mode    Mode
}

// Status is how a run ended.
type Status string

const (
	Done      Status = "done"
	Failed    Status = "failed"
	Expired   Status = "expired"
	Cancelled Status = "cancelled"
)

// Result is what a run gives back.
type Result struct {
	CallID string
	Status Status
	// Text is the answer; with an output schema it is the checked JSON.
	Text string
	// Foreign: the run read third-party text; for its caller it counts as
	// if it had read it itself.
	Foreign bool
	// Model, its calls, their time and tokens.
	Model                    string
	ModelCalls               int
	ModelTime                time.Duration
	PromptTokens, Completion int
}

// JSON is the answer as JSON, for agents with an output schema.
func (r Result) JSON() json.RawMessage { return json.RawMessage(r.Text) }

var (
	// ErrDepth: a call beyond the allowed depth.
	ErrDepth = errors.New("the allowed depth of agent calls is reached")
	// ErrTimeout: a synchronous call that was not done by its timeout.
	ErrTimeout = errors.New("the time for the agent is up")
	// ErrOutput: an answer that does not follow the output schema.
	ErrOutput = errors.New("the answer does not follow the output schema")
	// ErrProfile: a profile Call cannot build (the conversation).
	ErrProfile = errors.New("this context profile runs elsewhere")
	// ErrNoAnswer: the model gave no answer.
	ErrNoAnswer = errors.New("the model gave no answer")
)
