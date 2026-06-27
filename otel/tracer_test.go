package otel_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"

	platformotel "github.com/jedi-knights/go-platform/otel"
)

func TestInit_RequiresServiceName(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "")
	_, err := platformotel.Init(context.Background(), platformotel.Config{})
	if err == nil {
		t.Fatal("expected error when ServiceName and OTEL_SERVICE_NAME are both empty")
	}
}

func TestInit_FallsBackToEnvServiceName(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "test-service")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "") // force stdout exporter
	shutdown, err := platformotel.Init(context.Background(), platformotel.Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("expected non-nil shutdown")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown: %v", err)
	}
}

func TestInit_StdoutExporterWhenEndpointMissing(t *testing.T) {
	// Reset env vars that would otherwise route to an OTLP collector.
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	shutdown, err := platformotel.Init(context.Background(), platformotel.Config{
		ServiceName: "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	// Start and end a span; ensure it doesn't blow up under the stdout
	// exporter. The actual stdout output is incidental — the contract is
	// that the SDK does not panic.
	_, span := platformotel.StartSpan(context.Background(), "test.span")
	span.End()
}

func TestInit_OTLPGRPCExporter(t *testing.T) {
	// Point at a stub endpoint and disable TLS so the exporter
	// constructs cleanly. We don't actually start a server — the test
	// only verifies Init does not error when configured with an OTLP
	// gRPC endpoint.
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	shutdown, err := platformotel.Init(context.Background(), platformotel.Config{
		ServiceName:      "test",
		ExporterEndpoint: "http://localhost:4317",
		ExporterProtocol: "grpc",
		ExporterInsecure: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown: %v", err)
	}
}

func TestInit_OTLPHTTPExporter(t *testing.T) {
	shutdown, err := platformotel.Init(context.Background(), platformotel.Config{
		ServiceName:      "test",
		ExporterEndpoint: "http://localhost:4318",
		ExporterProtocol: "http",
		ExporterInsecure: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown: %v", err)
	}
}

func TestInit_RespectsExtraResourceAttrs(t *testing.T) {
	shutdown, err := platformotel.Init(context.Background(), platformotel.Config{
		ServiceName: "test",
		ExtraResourceAttrs: []attribute.KeyValue{
			attribute.String("deployment.region", "iad"),
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	// We can't read back the resource attrs without an in-memory
	// exporter (out of scope), but the SDK would error if the slice was
	// malformed; a successful Init proves the extra attrs were accepted.
}

func TestTracer_ReturnsGlobalTracer(t *testing.T) {
	if platformotel.Tracer() == nil {
		t.Fatal("expected non-nil tracer")
	}
}

func TestStartSpan_ReturnsContextAndSpan(t *testing.T) {
	ctx, span := platformotel.StartSpan(context.Background(), "test.span")
	defer span.End()
	if span == nil {
		t.Fatal("expected non-nil span")
	}
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
}
