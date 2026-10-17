// Package trace records what happened in a turn, for the evals
// (docs/spec/evals.md): tool calls, threads, Home Assistant actions and
// model rounds with their time. It is on only in eval mode
// (server.eval); otherwise nothing is recorded.
package trace

import (
	"context"
	"sync"
	"time"
)

// Event kinds.
const (
	Tool    = "tool"    // a tool call; Name is the tool, Detail its arguments
	Blocked = "blocked" // a tool call the guard refused
	Thread  = "thread"  // a thread changed; Name is its target, Detail the state
	Action  = "action"  // a Home Assistant action Mira called; Name is the action
	Round   = "round"   // one model call; Ms and Tokens
)

type Event struct {
	Kind   string `json:"kind"`
	Name   string `json:"name,omitempty"`
	Detail string `json:"detail,omitempty"`
	Ms     int64  `json:"ms,omitempty"`
	Tokens int    `json:"tokens,omitempty"`
}

type Recorder struct {
	mu     sync.Mutex
	events []Event
}

type key struct{}

// With starts recording for a turn.
func With(ctx context.Context) (context.Context, *Recorder) {
	r := &Recorder{}
	return context.WithValue(ctx, key{}, r), r
}

// Add records an event when ctx records; otherwise it does nothing.
func Add(ctx context.Context, kind, name, detail string) {
	add(ctx, Event{Kind: kind, Name: name, Detail: shorten(detail)})
}

// AddRound records a model call.
func AddRound(ctx context.Context, model string, d time.Duration, tokens int) {
	add(ctx, Event{Kind: Round, Name: model, Ms: d.Milliseconds(), Tokens: tokens})
}

func add(ctx context.Context, e Event) {
	r, _ := ctx.Value(key{}).(*Recorder)
	if r == nil {
		return
	}
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
}

// Events returns what was recorded.
func (r *Recorder) Events() []Event {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Event(nil), r.events...)
}

func shorten(s string) string {
	if r := []rune(s); len(r) > 300 {
		return string(r[:300]) + "…"
	}
	return s
}
