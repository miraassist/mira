// Package llm talks to an OpenAI-compatible chat completions endpoint.
//
// The target is llama.cpp's server, whose prompt cache is a core concern of
// this agent: requests carry an explicit slot id and cache_prompt, and the
// server's timings block is read back to see how much of the prompt was
// actually reused. Both extensions are optional so the same client works
// against LM Studio, vLLM or the OpenAI API.
package llm

import (
	"encoding/json"
	"time"
)

// Role values for chat messages.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// Message is one chat message. Content is always serialised, including as an
// empty string on assistant messages that only carry tool calls: some servers
// reject a missing content field.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
	// ExtraContent is passed back exactly as the server sent it. Gemini puts
	// its thought signature here and refuses the next round of a turn when a
	// call comes back without it.
	ExtraContent json.RawMessage `json:"extra_content,omitempty"`
}

type FunctionCall struct {
	Name string `json:"name"`
	// Arguments is a JSON object encoded as a string, per the OpenAI shape.
	Arguments string `json:"arguments"`
}

// Tool is a function the model may call. Parameters stays a RawMessage so the
// prompt package can canonicalise the schema once and keep the exact bytes
// stable across requests, which is what the prompt cache depends on.
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ChatRequest is one completion request.
type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []Tool    `json:"tools,omitempty"`
	ToolChoice  any       `json:"tool_choice,omitempty"`
	Temperature *float64  `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Stop        []string  `json:"stop,omitempty"`
	// ResponseFormat requests structured output, used by the notice evaluator.
	ResponseFormat any `json:"response_format,omitempty"`
	// ReasoningEffort limits the thinking of models that think by default.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`

	// IDSlot pins the request to a llama.cpp slot so its KV cache is reused.
	// Nil leaves the choice to the server.
	IDSlot *int `json:"id_slot,omitempty"`
	// CachePrompt asks llama.cpp to keep the prompt in the slot's KV cache.
	CachePrompt *bool `json:"cache_prompt,omitempty"`

	// Channel is not sent to the server; it labels telemetry.
	Channel string `json:"-"`
	// PrefixHash is not sent either; it is the layer 1-5 hash for telemetry.
	PrefixHash string `json:"-"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	// PromptTokensDetails is the OpenAI-standard place for cached tokens.
	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
}

// Timings is llama.cpp's per-request timing block.
type Timings struct {
	// PromptN is the number of prompt tokens actually evaluated. Tokens served
	// from the KV cache are not counted here, which is how cache hits are
	// derived when the server does not report CacheN.
	PromptN      int     `json:"prompt_n"`
	PromptMS     float64 `json:"prompt_ms"`
	PredictedN   int     `json:"predicted_n"`
	PredictedMS  float64 `json:"predicted_ms"`
	PromptPerSec float64 `json:"prompt_per_second"`
	PredPerSec   float64 `json:"predicted_per_second"`
	// CacheN is reported by newer llama.cpp builds: tokens reused from cache.
	CacheN *int `json:"cache_n,omitempty"`
}

type ChatResponse struct {
	ID      string   `json:"id"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
	Timings *Timings `json:"timings,omitempty"`

	// Latency is measured by the client, not returned by the server.
	Latency time.Duration `json:"-"`
}

// Message returns the first choice's message.
func (r *ChatResponse) Message() Message {
	if len(r.Choices) == 0 {
		return Message{}
	}
	return r.Choices[0].Message
}

func (r *ChatResponse) FinishReason() string {
	if len(r.Choices) == 0 {
		return ""
	}
	return r.Choices[0].FinishReason
}

// CachedTokens reports how many prompt tokens the server reused.
//
// Three sources, in order of trust: llama.cpp's explicit cache_n, the OpenAI
// prompt_tokens_details.cached_tokens, and finally the difference between the
// prompt size and the tokens llama.cpp says it evaluated.
func (r *ChatResponse) CachedTokens() int {
	if r.Timings != nil && r.Timings.CacheN != nil {
		return *r.Timings.CacheN
	}
	if d := r.Usage.PromptTokensDetails; d != nil {
		return d.CachedTokens
	}
	if r.Timings != nil && r.Usage.PromptTokens > 0 {
		if n := r.Usage.PromptTokens - r.Timings.PromptN; n > 0 {
			return n
		}
	}
	return 0
}

// CacheHitRatio is the fraction of the prompt that did not have to be
// re-evaluated. It is the number the cache invariants are judged by.
func (r *ChatResponse) CacheHitRatio() float64 {
	if r.Usage.PromptTokens == 0 {
		return 0
	}
	return float64(r.CachedTokens()) / float64(r.Usage.PromptTokens)
}
