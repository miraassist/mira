// Package telemetry wires OpenTelemetry tracing and metrics and defines the
// attribute names the agent uses.
//
// LLM spans follow the GenAI semantic conventions; everything specific to
// Lares (prompt cache behaviour, channels, notices) lives under the "lares."
// prefix so it is obvious which attributes are ours.
package telemetry

import "go.opentelemetry.io/otel/attribute"

// GenAI semantic convention attribute keys.
const (
	AttrGenAIOperationName  = attribute.Key("gen_ai.operation.name")
	AttrGenAIProviderName   = attribute.Key("gen_ai.provider.name")
	AttrGenAIRequestModel   = attribute.Key("gen_ai.request.model")
	AttrGenAIResponseModel  = attribute.Key("gen_ai.response.model")
	AttrGenAIRequestTemp    = attribute.Key("gen_ai.request.temperature")
	AttrGenAIRequestMaxTok  = attribute.Key("gen_ai.request.max_tokens")
	AttrGenAIUsageInputTok  = attribute.Key("gen_ai.usage.input_tokens")
	AttrGenAIUsageOutputTok = attribute.Key("gen_ai.usage.output_tokens")
	AttrGenAIFinishReasons  = attribute.Key("gen_ai.response.finish_reasons")
	AttrGenAIToolName       = attribute.Key("gen_ai.tool.name")
	AttrGenAIConversationID = attribute.Key("gen_ai.conversation.id")
	AttrErrorType           = attribute.Key("error.type")
)

// Lares-specific attribute keys.
const (
	// AttrPrefixHash is the hash over prompt layers 1-5. Two requests with the
	// same value should hit the llama.cpp prompt cache; a change means the
	// cache was deliberately or accidentally broken.
	AttrPrefixHash = attribute.Key("lares.prompt.prefix_hash")
	// AttrCachedTokens is how many prompt tokens llama.cpp reused.
	AttrCachedTokens  = attribute.Key("lares.llm.cached_tokens")
	AttrPromptTokens  = attribute.Key("lares.llm.prompt_tokens")
	AttrCacheHitRatio = attribute.Key("lares.llm.cache_hit_ratio")
	AttrSlot          = attribute.Key("lares.llm.slot")
	AttrChannel       = attribute.Key("lares.channel")
	AttrToolRounds    = attribute.Key("lares.conversation.tool_rounds")
	AttrToolServer    = attribute.Key("lares.tool.server")
	AttrToolBlocked   = attribute.Key("lares.tool.blocked_reason")
	AttrNoticeID      = attribute.Key("lares.notice.id")
	AttrNoticeCount   = attribute.Key("lares.notice.count")
	AttrLayer         = attribute.Key("lares.prompt.layer")
	AttrRebuildReason = attribute.Key("lares.prompt.rebuild_reason")
)

// Metric names.
const (
	MetricTokenUsage        = "gen_ai.client.token.usage"
	MetricOperationDuration = "gen_ai.client.operation.duration"
	MetricCacheHitRatio     = "lares.llm.cache_hit_ratio"
	MetricToolCalls         = "lares.tool.calls"
	MetricPrefixRebuilds    = "lares.prompt.prefix_rebuilds"
	MetricNoticesDelivered  = "lares.notices.delivered"
)
