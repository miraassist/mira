// Package telemetry wires OpenTelemetry tracing and metrics and defines the
// attribute names the agent uses.
//
// LLM spans follow the GenAI semantic conventions; everything specific to
// Mira (prompt cache behaviour, channels, notices) lives under the "mira."
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

// Mira-specific attribute keys.
const (
	// AttrPrefixHash is the hash over prompt layers 1-5. Two requests with the
	// same value should hit the llama.cpp prompt cache; a change means the
	// cache was deliberately or accidentally broken.
	AttrPrefixHash = attribute.Key("mira.prompt.prefix_hash")
	// AttrCachedTokens is how many prompt tokens llama.cpp reused.
	AttrCachedTokens  = attribute.Key("mira.llm.cached_tokens")
	AttrPromptTokens  = attribute.Key("mira.llm.prompt_tokens")
	AttrCacheHitRatio = attribute.Key("mira.llm.cache_hit_ratio")
	AttrSlot          = attribute.Key("mira.llm.slot")
	AttrChannel       = attribute.Key("mira.channel")
	AttrToolRounds    = attribute.Key("mira.conversation.tool_rounds")
	AttrToolServer    = attribute.Key("mira.tool.server")
	AttrToolBlocked   = attribute.Key("mira.tool.blocked_reason")
	AttrNoticeID      = attribute.Key("mira.notice.id")
	AttrNoticeCount   = attribute.Key("mira.notice.count")
	AttrLayer         = attribute.Key("mira.prompt.layer")
	AttrRebuildReason = attribute.Key("mira.prompt.rebuild_reason")
)

// Metric names.
const (
	MetricTokenUsage        = "gen_ai.client.token.usage"
	MetricOperationDuration = "gen_ai.client.operation.duration"
	MetricCacheHitRatio     = "mira.llm.cache_hit_ratio"
	MetricToolCalls         = "mira.tool.calls"
	MetricPrefixRebuilds    = "mira.prompt.prefix_rebuilds"
	MetricNoticesDelivered  = "mira.notices.delivered"
)
