package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sker65/mira/internal/llm"
	"github.com/sker65/mira/internal/testsupport/fakellm"
)

func TestChatSendsSlotAndCachePrompt(t *testing.T) {
	srv := fakellm.New(fakellm.Step{Content: "Das Licht ist an."})
	defer srv.Close()
	c := llm.New(llm.Options{BaseURL: srv.BaseURL(), Model: "test"})

	resp, err := c.Chat(context.Background(), llm.ChatRequest{
		Messages:    []llm.Message{{Role: llm.RoleUser, Content: "Licht an"}},
		IDSlot:      llm.IntPtr(1),
		CachePrompt: llm.BoolPtr(true),
		Channel:     "telegram",
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got := resp.Message().Content; got != "Das Licht ist an." {
		t.Errorf("content = %q", got)
	}

	last := srv.Last()
	if !last.HasSlot || last.Slot != 1 {
		t.Errorf("id_slot not forwarded: hasSlot=%v slot=%d", last.HasSlot, last.Slot)
	}
	var sent map[string]any
	json.Unmarshal(last.RawBody, &sent)
	if sent["cache_prompt"] != true {
		t.Errorf("cache_prompt not forwarded: %v", sent["cache_prompt"])
	}
	if _, ok := sent["channel"]; ok {
		t.Error("the internal Channel field leaked into the wire request")
	}
}

func TestOptionalFieldsAreOmittedWhenPinningIsOff(t *testing.T) {
	srv := fakellm.New(fakellm.Step{Content: "ok"})
	defer srv.Close()
	c := llm.New(llm.Options{BaseURL: srv.BaseURL(), Model: "test"})

	if _, err := c.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	var sent map[string]any
	json.Unmarshal(srv.Last().RawBody, &sent)
	for _, field := range []string{"id_slot", "cache_prompt"} {
		if _, ok := sent[field]; ok {
			t.Errorf("%s must be absent when unset, a strict server rejects it", field)
		}
	}
}

func TestCachedTokensFromLlamaCppTimings(t *testing.T) {
	body := `{"id":"x","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],
	          "usage":{"prompt_tokens":1000,"completion_tokens":5,"total_tokens":1005},
	          "timings":{"prompt_n":200,"predicted_n":5}}`
	resp := decodeResponse(t, body)
	if got := resp.CachedTokens(); got != 800 {
		t.Errorf("cached tokens = %d, want 800 (prompt_tokens minus evaluated)", got)
	}
	if got := resp.CacheHitRatio(); got < 0.79 || got > 0.81 {
		t.Errorf("hit ratio = %v, want ~0.8", got)
	}
}

func TestCachedTokensPrefersExplicitCacheN(t *testing.T) {
	body := `{"id":"x","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],
	          "usage":{"prompt_tokens":1000},"timings":{"prompt_n":200,"cache_n":750}}`
	if got := decodeResponse(t, body).CachedTokens(); got != 750 {
		t.Errorf("cached tokens = %d, want the explicit cache_n of 750", got)
	}
}

func TestCachedTokensFromOpenAIDetails(t *testing.T) {
	body := `{"id":"x","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],
	          "usage":{"prompt_tokens":1000,"prompt_tokens_details":{"cached_tokens":640}}}`
	if got := decodeResponse(t, body).CachedTokens(); got != 640 {
		t.Errorf("cached tokens = %d, want 640", got)
	}
}

func decodeResponse(t *testing.T, body string) *llm.ChatResponse {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	defer srv.Close()
	c := llm.New(llm.Options{BaseURL: srv.URL + "/v1", Model: "m"})
	resp, err := c.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	return resp
}

func TestRetriesConnectionFailuresButNotClientErrors(t *testing.T) {
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error":"busy"}`))
			return
		}
		w.Write([]byte(`{"id":"x","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`))
	}))
	defer srv.Close()

	c := llm.New(llm.Options{BaseURL: srv.URL + "/v1", Model: "m", MaxRetries: 3})
	if _, err := c.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	}); err != nil {
		t.Fatalf("Chat should have recovered after retries: %v", err)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}

	attempts = 0
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		http.Error(w, `{"error":"bad tool schema"}`, http.StatusBadRequest)
	}))
	defer bad.Close()
	c2 := llm.New(llm.Options{BaseURL: bad.URL + "/v1", Model: "m", MaxRetries: 3})
	_, err := c2.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("want an error for 400")
	}
	if attempts != 1 {
		t.Errorf("a 400 was retried %d times; it is the request that is wrong", attempts-1)
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should name the status: %v", err)
	}
}

func TestTimeoutMidGenerationIsNotRetried(t *testing.T) {
	var attempts int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	c := llm.New(llm.Options{BaseURL: srv.URL + "/v1", Model: "m", MaxRetries: 3,
		HTTPClient: &http.Client{Timeout: 150 * time.Millisecond}})
	if _, err := c.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	}); err == nil {
		t.Fatal("want a timeout error")
	}
	if n := atomic.LoadInt32(&attempts); n != 1 {
		t.Errorf("a generation timeout was retried %d times; the server is still busy", n-1)
	}
}

func TestFakeServerSimulatesPromptCachePerSlot(t *testing.T) {
	srv := fakellm.New()
	defer srv.Close()
	c := llm.New(llm.Options{BaseURL: srv.BaseURL(), Model: "test"})
	ctx := context.Background()

	stable := llm.Message{Role: llm.RoleSystem, Content: strings.Repeat("stabiler prefix ", 50)}

	first, err := c.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{stable, {Role: llm.RoleUser, Content: "erste frage"}},
		IDSlot:   llm.IntPtr(0),
	})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if first.CachedTokens() != 0 {
		t.Errorf("a cold slot reported %d cached tokens", first.CachedTokens())
	}

	second, err := c.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{stable, {Role: llm.RoleUser, Content: "zweite frage"}},
		IDSlot:   llm.IntPtr(0),
	})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second.CacheHitRatio() < 0.9 {
		t.Errorf("same prefix should mostly hit the cache, ratio = %v", second.CacheHitRatio())
	}

	// A different slot has its own cache and must start cold.
	other, err := c.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{stable, {Role: llm.RoleUser, Content: "dritte frage"}},
		IDSlot:   llm.IntPtr(1),
	})
	if err != nil {
		t.Fatalf("other slot: %v", err)
	}
	if other.CachedTokens() != 0 {
		t.Errorf("slot 1 saw %d cached tokens from slot 0's prompt", other.CachedTokens())
	}

	// Changing the stable prefix must cost the cache.
	changed, err := c.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "ein anderer prefix"},
			{Role: llm.RoleUser, Content: "vierte frage"},
		},
		IDSlot: llm.IntPtr(0),
	})
	if err != nil {
		t.Fatalf("changed prefix: %v", err)
	}
	if changed.CacheHitRatio() > 0.2 {
		t.Errorf("a changed prefix still reported ratio %v", changed.CacheHitRatio())
	}
}

func TestHealth(t *testing.T) {
	srv := fakellm.New()
	defer srv.Close()
	c := llm.New(llm.Options{BaseURL: srv.BaseURL(), Model: "test"})
	if err := c.Health(context.Background()); err != nil {
		t.Errorf("Health: %v", err)
	}

	dead := llm.New(llm.Options{BaseURL: "http://127.0.0.1:1/v1", Model: "test",
		HTTPClient: &http.Client{Timeout: 200 * time.Millisecond}})
	if err := dead.Health(context.Background()); err == nil {
		t.Error("Health should fail against a dead server")
	}
}
