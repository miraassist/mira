package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/miraassist/mira/internal/retry"
	"github.com/miraassist/mira/internal/telemetry"
)

// Error is a non-2xx response from the LLM server.
type Error struct {
	StatusCode int
	Body       string
	// Wait is the wait the server named (Retry-After, or Google's
	// RetryInfo.retryDelay in the body); zero for none.
	Wait time.Duration
}

// RetryAfter is the wait the server named (internal/retry keeps it).
func (e *Error) RetryAfter() time.Duration { return e.Wait }

var retryDelayRE = regexp.MustCompile(`"retryDelay"\s*:\s*"([0-9.]+)s"`)

// namedWait reads the wait a server names for a 429 or 503: the header
// Retry-After (seconds or a date), else Google's retryDelay in the body
// (docs/spec/retry.md, decision 5).
func namedWait(h http.Header, body string, now time.Time) time.Duration {
	if v := strings.TrimSpace(h.Get("Retry-After")); v != "" {
		if secs, err := strconv.ParseFloat(v, 64); err == nil && secs > 0 {
			return time.Duration(secs * float64(time.Second))
		}
		if at, err := http.ParseTime(v); err == nil && at.After(now) {
			return at.Sub(now)
		}
	}
	if m := retryDelayRE.FindStringSubmatch(body); m != nil {
		if secs, err := strconv.ParseFloat(m[1], 64); err == nil && secs > 0 {
			return time.Duration(secs * float64(time.Second))
		}
	}
	return 0
}

func (e *Error) Error() string {
	return fmt.Sprintf("llm server returned %d: %s", e.StatusCode, truncate(e.Body, 500))
}

// Retryable reports whether retrying the same request could succeed. 5xx and
// 429 are worth another attempt; a 4xx means the request itself is wrong.
func (e *Error) Retryable() bool {
	return e.StatusCode == http.StatusTooManyRequests || e.StatusCode >= 500
}

type Options struct {
	BaseURL string
	APIKey  string
	Model   string
	Timeout time.Duration
	// MaxRetries applies to connection failures only. A timeout in the middle
	// of generation is never retried: the server is still busy with the first
	// request and a retry would only queue behind it.
	MaxRetries int
	HTTPClient *http.Client
	Telemetry  *telemetry.Provider
	// Log gets a warning for every retry (docs/spec/fallbacks.md, FB-13);
	// slog's default without one.
	Log *slog.Logger
}

type Client struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
	retries int
	tel     *telemetry.Provider
	log     *slog.Logger

	// last is how the model's calls went, for Mira's page.
	mu   sync.Mutex
	last LastCall
}

// LastCall is when the model last answered and when it last failed, with
// the error.
type LastCall struct {
	OK      time.Time
	Failed  time.Time
	Problem string
}

// Last is how the model's calls went last.
func (c *Client) Last() LastCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

func (c *Client) note(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil {
		c.last.OK = time.Now()
		return
	}
	// A call the caller cancelled says nothing about the model.
	if errors.Is(err, context.Canceled) {
		return
	}
	c.last.Failed, c.last.Problem = time.Now(), err.Error()
}

func New(opts Options) *Client {
	if opts.Timeout == 0 {
		opts.Timeout = 2 * time.Minute
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: opts.Timeout}
	}
	if opts.Telemetry == nil {
		opts.Telemetry = telemetry.NewTestProvider()
	}
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	return &Client{
		baseURL: strings.TrimRight(opts.BaseURL, "/"),
		apiKey:  opts.APIKey,
		model:   opts.Model,
		http:    opts.HTTPClient,
		retries: opts.MaxRetries,
		tel:     opts.Telemetry,
		log:     opts.Log,
	}
}

// Model is the configured default model name.
func (c *Client) Model() string { return c.model }

// Chat performs one chat completion and records a GenAI span for it.
func (c *Client) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	return c.chat(ctx, req, nil)
}

// ChatStream is Chat with the answer streamed (docs/spec/streaming.md):
// onText gets each piece of the answer's text as the server sends it, and
// the response is the whole answer, tool calls assembled from their pieces,
// as Chat would have returned it.
func (c *Client) ChatStream(ctx context.Context, req ChatRequest, onText func(string)) (*ChatResponse, error) {
	req.Stream, req.StreamOptions = true, &StreamOptions{IncludeUsage: true}
	return c.chat(ctx, req, onText)
}

func (c *Client) chat(ctx context.Context, req ChatRequest, onText func(string)) (*ChatResponse, error) {
	if req.Model == "" {
		req.Model = c.model
	}

	ctx, span := c.tel.Tracer.Start(ctx, "chat "+req.Model, trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()
	span.SetAttributes(
		telemetry.AttrGenAIOperationName.String("chat"),
		telemetry.AttrGenAIProviderName.String("openai"),
		telemetry.AttrGenAIRequestModel.String(req.Model),
		telemetry.AttrChannel.String(req.Channel),
	)
	if req.PrefixHash != "" {
		span.SetAttributes(telemetry.AttrPrefixHash.String(req.PrefixHash))
	}
	if req.IDSlot != nil {
		span.SetAttributes(telemetry.AttrSlot.Int(*req.IDSlot))
	}
	if req.Temperature != nil {
		span.SetAttributes(telemetry.AttrGenAIRequestTemp.Float64(*req.Temperature))
	}
	if req.MaxTokens > 0 {
		span.SetAttributes(telemetry.AttrGenAIRequestMaxTok.Int(req.MaxTokens))
	}
	if c.tel.CaptureContent() {
		if body, err := json.Marshal(req.Messages); err == nil {
			span.SetAttributes(attribute.String("gen_ai.input.messages", truncate(string(body), 16384)))
		}
	}

	started := time.Now()
	resp, err := c.do(ctx, req, onText)
	c.note(err)
	latency := time.Since(started)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.SetAttributes(telemetry.AttrErrorType.String(errorType(err)))
		return nil, err
	}
	resp.Latency = latency

	cached := resp.CachedTokens()
	span.SetAttributes(
		telemetry.AttrGenAIResponseModel.String(resp.Model),
		telemetry.AttrGenAIUsageInputTok.Int(resp.Usage.PromptTokens),
		telemetry.AttrGenAIUsageOutputTok.Int(resp.Usage.CompletionTokens),
		telemetry.AttrPromptTokens.Int(resp.Usage.PromptTokens),
		telemetry.AttrCachedTokens.Int(cached),
		telemetry.AttrCacheHitRatio.Float64(resp.CacheHitRatio()),
		telemetry.AttrGenAIFinishReasons.StringSlice([]string{resp.FinishReason()}),
	)
	if c.tel.CaptureContent() {
		if body, err := json.Marshal(resp.Choices); err == nil {
			span.SetAttributes(attribute.String("gen_ai.output.messages", truncate(string(body), 16384)))
		}
	}
	c.tel.RecordLLMCall(ctx, telemetry.LLMCallMetrics{
		Model:        req.Model,
		Channel:      req.Channel,
		InputTokens:  resp.Usage.PromptTokens,
		OutputTokens: resp.Usage.CompletionTokens,
		CachedTokens: cached,
		Duration:     latency,
	})
	return resp, nil
}

func (c *Client) do(ctx context.Context, req ChatRequest, onText func(string)) (*ChatResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}

	var began time.Time
	var resp *ChatResponse
	err = retry.DoBy(ctx, waitsFor(c.retries+1), func(ctx context.Context) error {
		// A streamed answer is retried only while nothing of it was passed
		// on: what was said cannot be taken back.
		passed := false
		pass := onText
		if onText != nil {
			pass = func(text string) { passed = true; onText(text) }
		}
		began = time.Now()
		r, err := c.once(ctx, body, pass)
		switch {
		case err == nil:
			resp = r
			return nil
		case passed || !retryable(err):
			return retry.Fatal(err)
		}
		return err
	}, func(failed int, wait time.Duration, err error) {
		// A retry is never silent (docs/spec/fallbacks.md, FB-13).
		c.log.Warn("model call failed, trying again", "model", c.model, "host", c.host(), "attempt", failed,
			"status", errorType(err), "error", err, "attempt_ms", time.Since(began).Milliseconds(),
			"wait_ms", wait.Milliseconds())
	})
	var long *retry.WaitTooLong
	if errors.As(err, &long) {
		return nil, fmt.Errorf("model %s at %s busy: %w", c.model, c.host(), err)
	}
	return resp, err
}

// waitsFor chooses the waits by the error (docs/spec/retry.md, row 3): a
// 429 asks to slow down, so 1 s, then 2 s, unless the server named its
// wait; a 5xx or a failed connection 250 ms, then 500 ms.
func waitsFor(attempts int) func(error) retry.Policy {
	slow := retry.Policy{Attempts: attempts, First: time.Second, Max: 30 * time.Second, Growth: retry.Exponential}
	quick := retry.Policy{Attempts: attempts, First: 250 * time.Millisecond, Max: 30 * time.Second, Growth: retry.Linear}
	return func(err error) retry.Policy {
		var e *Error
		if errors.As(err, &e) && e.StatusCode == http.StatusTooManyRequests {
			return slow
		}
		return quick
	}
}

func (c *Client) once(ctx context.Context, body []byte, onText func(string)) (*ChatResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call llm: %w", err)
	}
	defer httpResp.Body.Close()

	if onText != nil && httpResp.StatusCode >= 200 && httpResp.StatusCode <= 299 {
		return readStream(httpResp.Body, onText)
	}
	raw, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read llm response: %w", err)
	}
	if httpResp.StatusCode < 200 || httpResp.StatusCode > 299 {
		e := &Error{StatusCode: httpResp.StatusCode, Body: string(raw)}
		if e.StatusCode == http.StatusTooManyRequests || e.StatusCode == http.StatusServiceUnavailable {
			e.Wait = namedWait(httpResp.Header, e.Body, time.Now())
		}
		return nil, e
	}

	var out ChatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode llm response: %w (body: %s)", err, truncate(string(raw), 300))
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("llm returned no choices")
	}
	return &out, nil
}

// host is the model server's host, for the log.
func (c *Client) host() string {
	if u, err := url.Parse(c.baseURL); err == nil && u.Host != "" {
		return u.Host
	}
	return c.baseURL
}

// Warmup sends a minimal request that carries the real cached prefix, so the
// slot holds the layer 1-5 KV state before the first user turn arrives.
func (c *Client) Warmup(ctx context.Context, req ChatRequest) error {
	// Tools stay on the request on purpose: the tool block is part of the
	// cached prefix, so a warmup without them would warm the wrong prefix.
	req.MaxTokens = 1
	_, err := c.Chat(ctx, req)
	if err != nil {
		return fmt.Errorf("warmup: %w", err)
	}
	return nil
}

// Health checks that the server answers. llama.cpp serves /health next to the
// API root; other servers do not, so /models is the fallback.
func (c *Client) Health(ctx context.Context) error {
	base := strings.TrimSuffix(c.baseURL, "/v1")
	for _, url := range []string{base + "/health", c.baseURL + "/models"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			continue
		}
		SetAuth(req, c.apiKey)
		resp, err := c.http.Do(req)
		if err != nil {
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
			return nil
		}
	}
	return fmt.Errorf("llm server at %s is not healthy", c.baseURL)
}

func retryable(err error) bool {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr.Retryable()
	}
	// Anything else that reached us is a connection-level failure. A deadline
	// that fired mid-generation is not retried, which context cancellation
	// already covers in do().
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

func errorType(err error) string {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return fmt.Sprintf("http_%d", apiErr.StatusCode)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "connection_error"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// IntPtr and BoolPtr build the optional llama.cpp fields.
func IntPtr(v int) *int    { return &v }
func BoolPtr(v bool) *bool { return &v }
