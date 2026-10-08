package otel

import (
	"context"
	"log/slog"

	"github.com/jedi-knights/go-logging/pkg/logging"
	"go.opentelemetry.io/otel/trace"
)

// SpanContextHandler is a slog.Handler that enriches every log record
// with the OpenTelemetry trace_id and span_id from the record's
// context before delegating to an inner handler.
//
// This is the single mechanism that correlates logs to spans in
// Grafana: a log line emitted inside a span carries the same trace_id
// a developer uses to find that span in Tempo. Compose it with any
// standard slog.Handler (JSON, text, or a downstream OTLP log
// exporter when the OTel log API stabilizes).
//
// SpanContextHandler is safe for concurrent use when the inner
// handler is.
type SpanContextHandler struct {
	inner slog.Handler
}

// NewSpanContextHandler wraps inner so every record it emits carries
// the OTel span context when present. If inner is nil, the handler
// delegates to [slog.Default]'s handler.
func NewSpanContextHandler(inner slog.Handler) *SpanContextHandler {
	if inner == nil {
		inner = slog.Default().Handler()
	}
	return &SpanContextHandler{inner: inner}
}

// Enabled delegates to the inner handler.
func (h *SpanContextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle adds trace_id and span_id attributes to the record when the
// context carries a valid OTel span, then delegates to the inner
// handler.
//
// When no span is present (background goroutine, pre-request setup,
// post-response work) the record is passed through unchanged — the
// absence of trace_id in a log line is itself a signal that the work
// happened outside a request span.
func (h *SpanContextHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.inner.Handle(ctx, r)
}

// WithAttrs returns a new SpanContextHandler whose inner has the
// given attributes attached.
func (h *SpanContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &SpanContextHandler{inner: h.inner.WithAttrs(attrs)}
}

// WithGroup returns a new SpanContextHandler whose inner has the
// given group opened.
func (h *SpanContextHandler) WithGroup(name string) slog.Handler {
	return &SpanContextHandler{inner: h.inner.WithGroup(name)}
}

// initLogger builds a go-logging [logging.Logger] whose underlying
// slog pipeline is wrapped by [SpanContextHandler] — every record it
// emits carries trace_id and span_id from any OTel span on the
// context.
//
// The returned Logger is API-compatible with go-logging.New(cfg); the
// only observable difference is that trace_id and span_id now reflect
// the OTel span (not the custom UUID that go-logging and
// httpmw.TraceID previously injected independently).
func initLogger(cfg Config) logging.Logger {
	level, _ := logging.ParseLevel(cfg.LogLevel)
	base := buildBaseSlogHandler(cfg, level)
	return logging.New(logging.Config{
		Handler:      NewSpanContextHandler(base),
		ServiceName:  cfg.ServiceName,
		Environment:  cfg.Environment,
		StaticFields: cfg.LogStaticFields,
	})
}

// buildBaseSlogHandler constructs the format-specific slog.Handler
// (JSON or text) that SpanContextHandler wraps.
func buildBaseSlogHandler(cfg Config, level slog.Level) slog.Handler {
	out := cfg.LogOutput
	if out == nil {
		out = defaultLogOutput()
	}
	opts := &slog.HandlerOptions{Level: level}
	if cfg.LogFormat == "text" {
		return slog.NewTextHandler(out, opts)
	}
	// Default and "json" both land here — JSON is the fleet default so
	// downstream log collectors parse records as structured events.
	return slog.NewJSONHandler(out, opts)
}
