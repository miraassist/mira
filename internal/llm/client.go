package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/sker65/mira/internal/telemetry"
)

// Error is a non-2xx response from the LLM server.
type Error struct {
	StatusCode int
	Body       string
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
}

type Client struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
	retries int
	tel     *telemetry.Provider
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
	return &Client{
		baseURL: strings.TrimRight(opts.BaseURL, "/"),
		apiKey:  opts.APIKey,
		model:   opts.Model,
		http:    opts.HTTPClient,
		retries: opts.MaxRetries,
		tel:     opts.Telemetry,
	}
}

// Model is the configured default model name.
func (c *Client) Model() string { return c.model }

// Chat performs one chat completion and records a GenAI span for it.
func (c *Client) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
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
	resp, err := c.do(ctx, req)
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

func (c *Client) do(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}

	var lastErr error
	for attempt := 0; ; attempt++ {
		resp, err := c.once(ctx, body)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if attempt >= c.retries || !retryable(err) || ctx.Err() != nil {
			return nil, lastErr
		}
		// Linear backoff: the server is local, a long wait helps nobody.
		delay := time.Duration(attempt+1) * 250 * time.Millisecond
		select {
		case <-ctx.Done():
			return nil, lastErr
		case <-time.After(delay):
		}
	}
}

func (c *Client) once(ctx context.Context, body []byte) (*ChatResponse, error) {
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

	raw, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read llm response: %w", err)
	}
	if httpResp.StatusCode < 200 || httpResp.StatusCode > 299 {
		return nil, &Error{StatusCode: httpResp.StatusCode, Body: string(raw)}
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
		if c.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.apiKey)
		}
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
