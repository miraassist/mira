package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Options configures the telemetry stack.
type Options struct {
	ServiceName string
	Version     string
	// OTLPEndpoint is a gRPC endpoint such as "localhost:4317". Empty disables
	// export; the agent still runs, it just records nothing.
	OTLPEndpoint string
	LogLevel     string
	// CaptureContent enables recording prompt and completion text on spans.
	// Off by default because that text is the household's private data.
	CaptureContent bool
}

// Provider owns the OTel pipeline and the agent's instruments.
type Provider struct {
	Tracer trace.Tracer
	Logger *slog.Logger

	captureContent bool

	tokenUsage     metric.Int64Histogram
	opDuration     metric.Float64Histogram
	cacheHitRatio  metric.Float64Histogram
	toolCalls      metric.Int64Counter
	prefixRebuilds metric.Int64Counter
	messagesOut    metric.Int64Counter

	shutdown []func(context.Context) error
}

// Setup builds the provider. It never fails hard on exporter problems: losing
// telemetry must not take the household assistant down.
func Setup(ctx context.Context, opts Options) (*Provider, error) {
	if opts.ServiceName == "" {
		opts.ServiceName = "mira"
	}
	logger := newLogger(opts.LogLevel)
	p := &Provider{Logger: logger, captureContent: opts.CaptureContent}

	if opts.OTLPEndpoint == "" {
		p.Tracer = noop.NewTracerProvider().Tracer(opts.ServiceName)
		if err := p.initInstruments(otel.GetMeterProvider().Meter(opts.ServiceName)); err != nil {
			return nil, err
		}
		logger.Info("telemetry export disabled", "reason", "no otlp_endpoint configured")
		return p, nil
	}

	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		resource.Default().SchemaURL(),
		attribute.String("service.name", opts.ServiceName),
		attribute.String("service.version", opts.Version),
	))
	if err != nil {
		return nil, fmt.Errorf("build otel resource: %w", err)
	}

	traceExp, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(opts.OTLPEndpoint), otlptracegrpc.WithInsecure())
	if err != nil {
		return nil, fmt.Errorf("otlp trace exporter: %w", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	p.shutdown = append(p.shutdown, tp.Shutdown)

	metricExp, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithEndpoint(opts.OTLPEndpoint), otlpmetricgrpc.WithInsecure())
	if err != nil {
		return nil, fmt.Errorf("otlp metric exporter: %w", err)
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp, sdkmetric.WithInterval(30*time.Second))),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)
	p.shutdown = append(p.shutdown, mp.Shutdown)

	p.Tracer = tp.Tracer(opts.ServiceName)
	if err := p.initInstruments(mp.Meter(opts.ServiceName)); err != nil {
		return nil, err
	}
	logger.Info("telemetry export enabled", "endpoint", opts.OTLPEndpoint)
	return p, nil
}

func (p *Provider) initInstruments(m metric.Meter) error {
	var err error
	if p.tokenUsage, err = m.Int64Histogram(MetricTokenUsage,
		metric.WithDescription("Number of input and output tokens used"),
		metric.WithUnit("{token}")); err != nil {
		return err
	}
	if p.opDuration, err = m.Float64Histogram(MetricOperationDuration,
		metric.WithDescription("GenAI operation duration"),
		metric.WithUnit("s")); err != nil {
		return err
	}
	if p.cacheHitRatio, err = m.Float64Histogram(MetricCacheHitRatio,
		metric.WithDescription("Fraction of prompt tokens served from the llama.cpp KV cache"),
		metric.WithUnit("1")); err != nil {
		return err
	}
	if p.toolCalls, err = m.Int64Counter(MetricToolCalls,
		metric.WithDescription("Tool invocations by the agent")); err != nil {
		return err
	}
	if p.prefixRebuilds, err = m.Int64Counter(MetricPrefixRebuilds,
		metric.WithDescription("Rebuilds of the cached prompt prefix")); err != nil {
		return err
	}
	if p.messagesOut, err = m.Int64Counter(MetricMessagesDelivered,
		metric.WithDescription("Messages delivered to a channel")); err != nil {
		return err
	}
	return nil
}

// CaptureContent reports whether prompt and completion text may be recorded.
func (p *Provider) CaptureContent() bool { return p.captureContent }

// RecordLLMCall feeds one completed LLM call into the metric instruments.
func (p *Provider) RecordLLMCall(ctx context.Context, m LLMCallMetrics) {
	base := []attribute.KeyValue{
		AttrGenAIOperationName.String("chat"),
		AttrGenAIRequestModel.String(m.Model),
		AttrChannel.String(m.Channel),
	}
	p.tokenUsage.Record(ctx, int64(m.InputTokens),
		metric.WithAttributes(append(base, attribute.String("gen_ai.token.type", "input"))...))
	p.tokenUsage.Record(ctx, int64(m.OutputTokens),
		metric.WithAttributes(append(base, attribute.String("gen_ai.token.type", "output"))...))
	p.opDuration.Record(ctx, m.Duration.Seconds(), metric.WithAttributes(base...))
	if m.InputTokens > 0 {
		p.cacheHitRatio.Record(ctx, float64(m.CachedTokens)/float64(m.InputTokens),
			metric.WithAttributes(base...))
	}
}

// LLMCallMetrics is one measured chat completion.
type LLMCallMetrics struct {
	Model        string
	Channel      string
	InputTokens  int
	OutputTokens int
	CachedTokens int
	Duration     time.Duration
}

// RecordToolCall counts one tool invocation.
func (p *Provider) RecordToolCall(ctx context.Context, server, tool, outcome string) {
	p.toolCalls.Add(ctx, 1, metric.WithAttributes(
		AttrToolServer.String(server),
		AttrGenAIToolName.String(tool),
		attribute.String("outcome", outcome),
	))
}

// RecordPrefixRebuild counts a rebuild of the cached prompt prefix. Reason is
// something like "tools_changed" or "structure_changed".
func (p *Provider) RecordPrefixRebuild(ctx context.Context, reason string) {
	p.prefixRebuilds.Add(ctx, 1, metric.WithAttributes(AttrRebuildReason.String(reason)))
}

// RecordMessageDelivery counts a message that went out.
func (p *Provider) RecordMessageDelivery(ctx context.Context, channel string, count int) {
	p.messagesOut.Add(ctx, int64(count), metric.WithAttributes(AttrChannel.String(channel)))
}

// Shutdown flushes pending telemetry.
func (p *Provider) Shutdown(ctx context.Context) error {
	var firstErr error
	for i := len(p.shutdown) - 1; i >= 0; i-- {
		if err := p.shutdown[i](ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}

// NewTestProvider returns a provider that records nothing, for tests and for
// code paths that need a non-nil Provider.
func NewTestProvider() *Provider {
	p := &Provider{
		Tracer: noop.NewTracerProvider().Tracer("test"),
		Logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}
	_ = p.initInstruments(otel.GetMeterProvider().Meter("test"))
	return p
}
