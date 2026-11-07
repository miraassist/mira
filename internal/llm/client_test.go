package llm_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/testsupport/fakellm"
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

// Thinking tokens: OpenAI's reasoning_tokens where sent, else what Gemini's
// total counts beyond prompt and completion (seen 2026-11-06: 20 + 2, total
// 172), else none.
func TestThinkingTokens(t *testing.T) {
	for _, c := range []struct {
		usage string
		want  int
	}{
		{`{"prompt_tokens":20,"completion_tokens":2,"total_tokens":172}`, 150},
		{`{"prompt_tokens":20,"completion_tokens":30,"total_tokens":50,"completion_tokens_details":{"reasoning_tokens":12}}`, 12},
		{`{"prompt_tokens":20,"completion_tokens":2,"total_tokens":22}`, 0},
		{`{"prompt_tokens":20,"completion_tokens":2}`, 0},
	} {
		body := `{"id":"x","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":` + c.usage + `}`
		if got := decodeResponse(t, body).Usage.ThinkingTokens(); got != c.want {
			t.Errorf("%s: thinking tokens = %d, want %d", c.usage, got, c.want)
		}
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

// Gemini's thought signature must survive a round trip unchanged.
func TestToolCallKeepsExtraContent(t *testing.T) {
	in := `{"id":"c1","type":"function","function":{"name":"x","arguments":"{}"},"extra_content":{"google":{"thought_signature":"abc="}}}`
	var call llm.ToolCall
	if err := json.Unmarshal([]byte(in), &call); err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(call)
	if !strings.Contains(string(out), `"extra_content":{"google":{"thought_signature":"abc="}}`) {
		t.Errorf("lost: %s", out)
	}
	plain, _ := json.Marshal(llm.ToolCall{ID: "c2", Type: "function"})
	if strings.Contains(string(plain), "extra_content") {
		t.Errorf("empty extra_content sent: %s", plain)
	}
}

// The client keeps when its model last answered and last failed, for Mira's
// page; a call the caller cancelled is no failure of the model.
func TestLastCall(t *testing.T) {
	dead := llm.New(llm.Options{BaseURL: "http://127.0.0.1:1/v1", Model: "test", MaxRetries: 0,
		HTTPClient: &http.Client{Timeout: 200 * time.Millisecond}})
	dead.Chat(context.Background(), llm.ChatRequest{Messages: []llm.Message{{Role: "user", Content: "x"}}})
	if l := dead.Last(); l.Failed.IsZero() || l.Problem == "" || !l.OK.IsZero() {
		t.Errorf("after a failure: %+v", l)
	}
	srv := fakellm.New(fakellm.Step{Content: "ja"})
	defer srv.Close()
	c := llm.New(llm.Options{BaseURL: srv.BaseURL(), Model: "test"})
	if _, err := c.Chat(context.Background(), llm.ChatRequest{Messages: []llm.Message{{Role: "user", Content: "x"}}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Chat(ctx, llm.ChatRequest{Messages: []llm.Message{{Role: "user", Content: "x"}}})
	if l := c.Last(); l.OK.IsZero() || !l.Failed.IsZero() {
		t.Errorf("after an answer and a cancelled call: %+v", l)
	}
}

// ST-01: a streamed answer comes out in pieces, in order, and its tool call
// is the same as the whole answer would have had (docs/spec/streaming.md).
func TestST01_AStreamedAnswerAndItsToolCallsAreAssembled(t *testing.T) {
	call := fakellm.ToolCall("c1", "ha__GetLiveContext", `{"area":"Küche","domain":"light"}`)
	srv := fakellm.New(fakellm.Step{Content: "Ich schaue in der Küche nach. ", ToolCalls: []llm.ToolCall{call}})
	defer srv.Close()
	c := llm.New(llm.Options{BaseURL: srv.BaseURL(), Model: "test"})

	var pieces []string
	resp, err := c.ChatStream(context.Background(), llm.ChatRequest{Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}},
		func(text string) { pieces = append(pieces, text) })
	if err != nil {
		t.Fatal(err)
	}
	if len(pieces) < 2 || strings.Join(pieces, "") != "Ich schaue in der Küche nach. " {
		t.Fatalf("pieces = %q", pieces)
	}
	msg := resp.Message()
	if msg.Content != "Ich schaue in der Küche nach. " || len(msg.ToolCalls) != 1 {
		t.Fatalf("message = %+v", msg)
	}
	if got := msg.ToolCalls[0]; got.ID != "c1" || got.Function.Name != call.Function.Name || got.Function.Arguments != call.Function.Arguments {
		t.Fatalf("tool call = %+v", got)
	}
	if resp.FinishReason() != "tool_calls" || resp.Usage.PromptTokens == 0 || resp.CachedTokens() != 0 {
		t.Fatalf("finish %q, usage %+v", resp.FinishReason(), resp.Usage)
	}
	if !srv.Last().Request.Stream || srv.Last().Request.StreamOptions == nil || !srv.Last().Request.StreamOptions.IncludeUsage {
		t.Fatalf("request = %s", srv.Last().RawBody)
	}
}

// Gemini sends a tool call whole, without index, with its thought signature,
// and the usage so far in every chunk (tried 2026-11-03).
func TestStreamTakesGeminisToolCallsWithoutIndex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, ev := range []string{
			`{"choices":[{"delta":{"role":"assistant","tool_calls":[{"extra_content":{"google":{"thought_signature":"sig"}},"function":{"arguments":"{\"name\":\"Küche\"}","name":"get_state"},"id":"call_1","type":"function"},{"function":{"arguments":"{}","name":"get_time"},"id":"call_2","type":"function"}]},"index":0}],"usage":{"completion_tokens":19,"prompt_tokens":57,"total_tokens":76}}`,
			`{"choices":[{"delta":{"role":"assistant"},"finish_reason":"stop","index":0}],"usage":{"completion_tokens":20,"prompt_tokens":57,"total_tokens":77}}`,
		} {
			fmt.Fprintf(w, "data: %s\n\n", ev)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	c := llm.New(llm.Options{BaseURL: srv.URL, Model: "gemini"})
	resp, err := c.ChatStream(context.Background(), llm.ChatRequest{}, func(string) { t.Error("no text expected") })
	if err != nil {
		t.Fatal(err)
	}
	calls := resp.Message().ToolCalls
	if len(calls) != 2 || calls[0].Function.Name != "get_state" || calls[1].ID != "call_2" ||
		string(calls[0].ExtraContent) != `{"google":{"thought_signature":"sig"}}` {
		t.Fatalf("calls = %+v", calls)
	}
	if resp.Usage.CompletionTokens != 20 {
		t.Fatalf("usage = %+v", resp.Usage)
	}
}

// A stream that breaks off after some text is an error, and it is not
// retried: what was passed on cannot be taken back.
func TestStreamBrokenOffIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Es war einmal \"}}]}\n\n")
	}))
	defer srv.Close()
	c := llm.New(llm.Options{BaseURL: srv.URL, Model: "test", MaxRetries: 2})
	var got string
	_, err := c.ChatStream(context.Background(), llm.ChatRequest{}, func(text string) { got += text })
	if err == nil || got != "Es war einmal " || calls.Load() != 1 {
		t.Fatalf("err %v, got %q, calls %d", err, got, calls.Load())
	}
}

func TestFB13_a_retried_model_call_is_in_the_log(t *testing.T) {
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error":"overloaded"}`))
			return
		}
		w.Write([]byte(`{"id":"x","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`))
	}))
	defer srv.Close()

	var logged bytes.Buffer
	c := llm.New(llm.Options{BaseURL: srv.URL + "/v1", Model: "gemini-x", MaxRetries: 2,
		Log: slog.New(slog.NewTextHandler(&logged, nil))})
	resp, err := c.Chat(context.Background(), llm.ChatRequest{Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}})
	if err != nil || resp.Choices[0].Message.Content != "ok" {
		t.Fatalf("answer %+v, error %v", resp, err)
	}
	line := logged.String()
	host := strings.TrimPrefix(srv.URL, "http://")
	for _, want := range []string{"level=WARN", "model=gemini-x", "host=" + host, "attempt=1", "status=http_503", "overloaded", "attempt_ms=", "wait_ms=250"} {
		if !strings.Contains(line, want) {
			t.Errorf("log lacks %q: %s", want, line)
		}
	}
	if n := strings.Count(line, "\n"); n != 1 {
		t.Errorf("want one line for one retry, got %d: %s", n, line)
	}
}

// Why a model failed (docs/spec/models.md, When a model fails).
func TestCause(t *testing.T) {
	for err, want := range map[error]string{
		&llm.Error{StatusCode: 401, Body: "bad"}:                                                             llm.CauseKey,
		&llm.Error{StatusCode: 403, Body: "forbidden"}:                                                       llm.CauseKey,
		&llm.Error{StatusCode: 400, Body: `{"error":{"message":"Missing or invalid Authorization header"}}`}: llm.CauseKey,
		&llm.Error{StatusCode: 400, Body: "API key not valid"}:                                               llm.CauseKey,
		&llm.Error{StatusCode: 400, Body: "context too long"}:                                                "",
		&llm.Error{StatusCode: 500, Body: "oops"}:                                                            "",
		fmt.Errorf("post: %w", &net.OpError{Op: "dial", Err: errors.New("connection refused")}):              llm.CauseUnreachable,
		context.DeadlineExceeded:                                                                             llm.CauseUnreachable,
	} {
		if got := llm.Cause(err); got != want {
			t.Errorf("Cause(%v) = %q, want %q", err, got, want)
		}
	}
}
