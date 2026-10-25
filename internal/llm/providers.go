package llm

import "strings"

// Provider is a common cloud provider with an OpenAI-compatible API: the
// user picks it by name and gives only a key (docs/spec/modelle.md,
// Providers). The integration's setup and Mira's page both take this list.
type Provider struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
}

// Providers are the cloud providers Mira knows, in the order offered.
var Providers = []Provider{
	{"openai", "OpenAI", "https://api.openai.com/v1"},
	{"gemini", "Google Gemini", "https://generativelanguage.googleapis.com/v1beta/openai"},
	{"openrouter", "OpenRouter", "https://openrouter.ai/api/v1"},
	{"mistral", "Mistral", "https://api.mistral.ai/v1"},
	{"anthropic", "Anthropic", "https://api.anthropic.com/v1"},
}

// ProviderOf names the provider whose address an endpoint is; "" for a
// server of one's own.
func ProviderOf(endpoint string) string {
	e := strings.TrimRight(endpoint, "/")
	for _, p := range Providers {
		if e == p.BaseURL {
			return p.Key
		}
	}
	return ""
}
