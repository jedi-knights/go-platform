package otel

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
	"go.opentelemetry.io/otel/trace"
)

// InstrumentationName is the well-known Tracer name every consumer of
// this package uses. Per OpenTelemetry conventions, the instrumentation
// name identifies the *library* doing the instrumenting (this package),
// not the service being instrumented (which lives on the Resource).
const InstrumentationName = "github.com/jedi-knights/go-platform/otel"

// Config carries the inputs to [Init]. Every field is optional; missing
// values fall back to OpenTelemetry standard environment variables.
//
// The standard env-var contract is documented at
// https://opentelemetry.io/docs/languages/sdk-configuration/general/.
type Config struct {
	// ServiceName is recorded as the service.name resource attribute.
	// When empty, OTEL_SERVICE_NAME is used; if that is also empty Init
	// returns an error — every span needs a service name.
	ServiceName string

	// ServiceVersion is recorded as the service.version resource
	// attribute. When empty, OTEL_SERVICE_VERSION is used.
	ServiceVersion string

	// Environment is recorded as the deployment.environment.name
	// resource attribute. When empty, the OTEL_DEPLOYMENT_ENVIRONMENT
	// or OTEL_DEPLOYMENT_ENVIRONMENT_NAME env var is used.
	Environment string

	// ExporterEndpoint is the OTLP endpoint. When empty,
	// OTEL_EXPORTER_OTLP_ENDPOINT is used. When that is also empty, the
	// stdout exporter is wired so spans are visible during local
	// development without a collector.
	ExporterEndpoint string

	// ExporterProtocol is "grpc" (default) or "http". When empty,
	// OTEL_EXPORTER_OTLP_PROTOCOL is used; missing falls back to grpc.
	ExporterProtocol string

	// ExporterInsecure disables TLS on the OTLP gRPC endpoint. When
	// false the exporter dials over TLS. Matches the standard
	// OTEL_EXPORTER_OTLP_INSECURE env var when ExporterEndpoint is
	// empty (i.e., env-driven config).
	ExporterInsecure bool

	// SamplerRatio sets the head-based parent-based + ratio sampler.
	// 0 disables tracing entirely; 1 (default when zero) samples every
	// root span. Values between 0 and 1 sample that fraction.
	SamplerRatio float64

	// ExtraResourceAttrs are appended to the standard set
	// (service.name, service.version, deployment.environment.name).
	// Use this for region, instance id, or any other static label.
	ExtraResourceAttrs []attribute.KeyValue
}

// Shutdown flushes buffered spans and tears down the TracerProvider.
// Idempotent — calling twice is safe.
type Shutdown func(context.Context) error

// Init wires the global TracerProvider and propagator. Call exactly
// once at the composition root, defer the returned Shutdown until main
// exits.
//
// The propagator is set to the W3C TraceContext + Baggage composite, so
// every consumer can pull / push trace context across HTTP and gRPC
// without further configuration.
func Init(ctx context.Context, cfg Config) (Shutdown, error) {
	serviceName := firstNonEmpty(cfg.ServiceName,
		os.Getenv("OTEL_SERVICE_NAME"))
	if serviceName == "" {
		return nil, fmt.Errorf("otel.Init: ServiceName required (or set OTEL_SERVICE_NAME)")
	}
	res, err := buildResource(ctx, cfg, serviceName)
	if err != nil {
		return nil, fmt.Errorf("otel.Init: building resource: %w", err)
	}

	exporter, err := buildExporter(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("otel.Init: building exporter: %w", err)
	}

	sampler := buildSampler(cfg.SamplerRatio)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter,
			sdktrace.WithBatchTimeout(5*time.Second)),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}

// Tracer returns the package's well-known Tracer from the global
// provider. Use this instead of holding a Tracer handle directly so
// every call site shares one instrumentation name.
func Tracer() trace.Tracer {
	return otel.Tracer(InstrumentationName)
}

// StartSpan is a convenience over Tracer().Start that respects the
// portfolio's naming convention: short.snake_case names anchored on
// the action being recorded (e.g. "token.issue", "tool.invoke").
//
// Returns the child context (which carries the span) and a function
// the caller defers to end the span.
func StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return Tracer().Start(ctx, name, opts...)
}

// buildResource composes the standard service / deployment attributes
// from Config and the OTel env-var contract, then layers any extras the
// caller supplied.
func buildResource(ctx context.Context, cfg Config, serviceName string) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{
		semconv.ServiceName(serviceName),
	}
	if v := firstNonEmpty(cfg.ServiceVersion, os.Getenv("OTEL_SERVICE_VERSION")); v != "" {
		attrs = append(attrs, semconv.ServiceVersion(v))
	}
	if env := firstNonEmpty(cfg.Environment,
		os.Getenv("OTEL_DEPLOYMENT_ENVIRONMENT_NAME"),
		os.Getenv("OTEL_DEPLOYMENT_ENVIRONMENT")); env != "" {
		attrs = append(attrs, semconv.DeploymentEnvironmentName(env))
	}
	attrs = append(attrs, cfg.ExtraResourceAttrs...)

	return resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(attrs...),
	)
}

// buildExporter selects between OTLP gRPC, OTLP HTTP, and stdout based
// on configuration. When no endpoint is configured (neither Config nor
// env var) the stdout exporter is wired so spans are visible during
// development without a collector.
func buildExporter(ctx context.Context, cfg Config) (sdktrace.SpanExporter, error) {
	endpoint := firstNonEmpty(cfg.ExporterEndpoint,
		os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"),
		os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	if endpoint == "" {
		return stdouttrace.New(stdouttrace.WithPrettyPrint())
	}
	protocol := strings.ToLower(firstNonEmpty(cfg.ExporterProtocol,
		os.Getenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL"),
		os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL"),
		"grpc"))
	insecure := cfg.ExporterInsecure ||
		strings.EqualFold(os.Getenv("OTEL_EXPORTER_OTLP_INSECURE"), "true")

	switch protocol {
	case "http", "http/protobuf":
		opts := []otlptracehttp.Option{otlptracehttp.WithEndpointURL(endpoint)}
		if insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		return otlptrace.New(ctx, otlptracehttp.NewClient(opts...))
	default:
		opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpointURL(endpoint)}
		if insecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		return otlptrace.New(ctx, otlptracegrpc.NewClient(opts...))
	}
}

// buildSampler returns the parent-based + ratio sampler. Ratio 0 is a
// "never sample" sampler — useful for shutting tracing off without
// removing the Init call. Negative or > 1 ratios are clamped because
// the SDK panics otherwise; the cost of a permissive normalisation is
// lower than the cost of a startup crash from a bad env var.
func buildSampler(ratio float64) sdktrace.Sampler {
	switch {
	case ratio <= 0:
		// Default when no sampler ratio is set: sample everything. The
		// SDK's default head-based sampler is ParentBased(AlwaysOn), and
		// this package matches that for predictability.
		if ratio == 0 {
			return sdktrace.ParentBased(sdktrace.AlwaysSample())
		}
		return sdktrace.ParentBased(sdktrace.NeverSample())
	case ratio >= 1:
		return sdktrace.ParentBased(sdktrace.AlwaysSample())
	default:
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))
	}
}

// firstNonEmpty returns the first non-empty string in values, or "".
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
