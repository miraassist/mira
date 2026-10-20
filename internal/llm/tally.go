package llm

import (
	"context"
	"sync"
	"time"
)

// Tally adds up the model calls made under a context: how many, how long,
// and the tokens. Work that is not a conversation (reading a mail) logs it
// as one line.
type Tally struct {
	mu                                sync.Mutex
	Model                             string
	Calls, Prompt, Cached, Completion int
	Took                              time.Duration
}

type tallyKey struct{}

// WithTally returns a context whose model calls count into the returned
// tally.
func WithTally(ctx context.Context) (context.Context, *Tally) {
	t := &Tally{}
	return context.WithValue(ctx, tallyKey{}, t), t
}

// Count adds one call to the context's tally, if it has one.
func Count(ctx context.Context, model string, took time.Duration, out *ChatResponse) {
	t, ok := ctx.Value(tallyKey{}).(*Tally)
	if !ok || out == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Model = model
	t.Calls++
	t.Took += took
	t.Prompt += out.Usage.PromptTokens
	t.Cached += out.CachedTokens()
	t.Completion += out.Usage.CompletionTokens
}

// Attrs are the tally as log attributes.
func (t *Tally) Attrs() []any {
	t.mu.Lock()
	defer t.mu.Unlock()
	return []any{"model", t.Model, "model_calls", t.Calls, "model_ms", t.Took.Milliseconds(),
		"prompt_tokens", t.Prompt, "cached_tokens", t.Cached, "completion_tokens", t.Completion}
}
