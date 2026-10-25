package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SetAuth puts an API key on a request the way OpenAI-compatible servers
// take it (Authorization: Bearer) and the way Anthropic's /models wants it
// (x-api-key with a version); servers ignore the headers they do not use.
func SetAuth(req *http.Request, key string) {
	if key == "" {
		return
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", "2023-06-01")
}

// Where a context size came from (docs/spec/modelle.md, Context size).
const (
	ContextFromModels   = "models"   // the server's /models
	ContextFromGemini   = "gemini"   // Google's own model endpoint
	ContextFromLMStudio = "lmstudio" // LM Studio's /api/v0/models, as loaded
)

// contextFields are the names servers give a model's context in /models:
// OpenRouter, Mistral, Anthropic, vLLM, LM Studio.
var contextFields = []string{"context_length", "max_context_length", "max_input_tokens", "max_model_len", "loaded_context_length"}

// contextTimeout bounds each question about a model's context.
const contextTimeout = 10 * time.Second

// ContextSize asks a model server how many tokens of context a model has:
// its /models first; then Google's own model endpoint for Gemini; then LM
// Studio's, which knows the size the model was loaded with. 0 when the
// server does not say; the caller then asks the user.
func ContextSize(ctx context.Context, client *http.Client, baseURL, apiKey, model string) (int, string) {
	if client == nil {
		client = &http.Client{Timeout: contextTimeout}
	}
	base := strings.TrimRight(baseURL, "/")
	get := func(u string, header func(*http.Request)) map[string]any {
		cctx, cancel := context.WithTimeout(ctx, contextTimeout)
		defer cancel()
		req, err := http.NewRequestWithContext(cctx, http.MethodGet, u, nil)
		if err != nil {
			return nil
		}
		header(req)
		resp, err := client.Do(req)
		if err != nil {
			return nil
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil
		}
		var out map[string]any
		if json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&out) != nil {
			return nil
		}
		return out
	}
	auth := func(r *http.Request) { SetAuth(r, apiKey) }
	if n := fromList(get(base+"/models", auth), model); n > 0 {
		return n, ContextFromModels
	}
	if u, err := url.Parse(base); err == nil && u.Host == "generativelanguage.googleapis.com" {
		m := get("https://generativelanguage.googleapis.com/v1beta/models/"+url.PathEscape(strings.TrimPrefix(model, "models/")),
			func(r *http.Request) { r.Header.Set("x-goog-api-key", apiKey) })
		if n := number(m["inputTokenLimit"]); n > 0 {
			return n, ContextFromGemini
		}
	}
	root := strings.TrimSuffix(base, "/v1")
	if list := get(root+"/api/v0/models", auth); list != nil {
		for _, m := range entries(list) {
			if m["id"] == model {
				if n := number(m["loaded_context_length"]); n > 0 {
					return n, ContextFromLMStudio
				}
			}
		}
	}
	return 0, ""
}

// fromList finds a model in a /models answer and reads its context.
func fromList(list map[string]any, model string) int {
	for _, m := range entries(list) {
		id, _ := m["id"].(string)
		if id != model && strings.TrimPrefix(id, "models/") != model {
			continue
		}
		for _, f := range contextFields {
			if n := number(m[f]); n > 0 {
				return n
			}
		}
	}
	return 0
}

func entries(list map[string]any) []map[string]any {
	data, _ := list["data"].([]any)
	out := make([]map[string]any, 0, len(data))
	for _, d := range data {
		if m, ok := d.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func number(v any) int {
	if f, ok := v.(float64); ok && f > 0 {
		return int(f)
	}
	return 0
}
