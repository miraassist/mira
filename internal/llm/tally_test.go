package llm

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// Calls under a tallied context add up; others count nowhere.
func TestTally(t *testing.T) {
	Count(context.Background(), "x", time.Second, &ChatResponse{}) // no tally: nothing happens
	ctx, tally := WithTally(context.Background())
	Count(ctx, "smart", 1200*time.Millisecond, &ChatResponse{Usage: Usage{PromptTokens: 1000, CompletionTokens: 50}})
	Count(ctx, "smart", 300*time.Millisecond, &ChatResponse{Usage: Usage{PromptTokens: 200, CompletionTokens: 5}})
	got := fmt.Sprint(tally.Attrs())
	want := "[model smart model_calls 2 model_ms 1500 prompt_tokens 1200 cached_tokens 0 completion_tokens 55]"
	if got != want {
		t.Errorf("tally = %s, want %s", got, want)
	}
}
