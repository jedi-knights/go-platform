package otel_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	platformotel "github.com/jedi-knights/go-platform/otel"
)

// newForTest builds an Observability with the stdout trace exporter
// silenced (writes to io.Discard) and a buffer-backed logger. Call
// t.Cleanup for shutdown so no deferred error is dropped.
func newForTest(t *testing.T, cfg platformotel.Config) (*platformotel.Observability, *bytes.Buffer) {
	t.Helper()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "") // force stdout trace fallback
	buf := &bytes.Buffer{}
	if cfg.ServiceName == "" {
		cfg.ServiceName = "test-service"
	}
	if cfg.LogFormat == "" {
		cfg.LogFormat = "json"
	}
	if cfg.LogOutput == nil {
		cfg.LogOutput = buf
	}
	obs, err := platformotel.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = obs.Shutdown(ctx)
	})
	return obs, buf
}

func TestNew_ServiceNameRequired(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "")
	if _, err := platformotel.New(context.Background(), platformotel.Config{}); err == nil {
		t.Fatal("expected error when ServiceName is unset")
	} else if !strings.Contains(err.Error(), "ServiceName") {
		t.Fatalf("expected ServiceName in error, got %q", err)
	}
}

func TestNew_ReturnsUsableSignals(t *testing.T) {
	obs, _ := newForTest(t, platformotel.Config{
		ServiceName:    "test-service",
		ServiceVersion: "0.0.1",
		Environment:    "test",
	})

	if obs.Tracer == nil {
		t.Error("Tracer is nil")
	}
	if obs.Meter == nil {
		t.Error("Meter is nil")
	}
	if obs.Logger == nil {
		t.Error("Logger is nil")
	}
	if obs.PromRegistry == nil {
		t.Error("PromRegistry is nil")
	}
	if obs.PromHandler == nil {
		t.Error("PromHandler is nil")
	}
}

func TestNew_PromHandlerServes(t *testing.T) {
	obs, _ := newForTest(t, platformotel.Config{LogOutput: io.Discard})

	srv := httptest.NewServer(obs.PromHandler)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	// The Go collector always publishes go_goroutines; its presence
	// is a round-trip proof that the scrape endpoint is wired to the
	// registry carrying the runtime collectors.
	if !strings.Contains(string(body), "go_goroutines") {
		t.Errorf("expected go_goroutines in /metrics output; body:\n%s", body)
	}
}

func TestNew_LoggerCarriesSpanContext(t *testing.T) {
	obs, buf := newForTest(t, platformotel.Config{})

	// Build a context carrying a known-valid span. Using the SDK
	// directly keeps the test independent of the tracer provider the
	// New call installed.
	tracer := sdktrace.NewTracerProvider().Tracer("test")
	ctx, span := tracer.Start(context.Background(), "op")
	wantTraceID := span.SpanContext().TraceID().String()
	wantSpanID := span.SpanContext().SpanID().String()

	obs.Logger.InfoContext(ctx, "hello")
	span.End()

	line := lastLine(buf.String())
	if line == "" {
		t.Fatal("expected a log record, got empty buffer")
	}
	rec := map[string]any{}
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("unmarshal log record: %v (line=%q)", err, line)
	}
	if got := rec["trace_id"]; got != wantTraceID {
		t.Errorf("trace_id = %v, want %s", got, wantTraceID)
	}
	if got := rec["span_id"]; got != wantSpanID {
		t.Errorf("span_id = %v, want %s", got, wantSpanID)
	}
}

func TestNew_LoggerSkipsSpanIDWhenNoSpan(t *testing.T) {
	obs, buf := newForTest(t, platformotel.Config{})

	obs.Logger.Info("hello")
	line := lastLine(buf.String())
	rec := map[string]any{}
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := rec["trace_id"]; ok {
		t.Error("trace_id should be absent when no span is on context")
	}
	if _, ok := rec["span_id"]; ok {
		t.Error("span_id should be absent when no span is on context")
	}
}

func TestNew_ShutdownIsIdempotent(t *testing.T) {
	obs, err := platformotel.New(context.Background(), platformotel.Config{
		ServiceName: "test-service",
		LogOutput:   io.Discard,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := obs.Shutdown(ctx); err != nil {
		t.Errorf("first Shutdown: %v", err)
	}
	if err := obs.Shutdown(ctx); err != nil {
		t.Errorf("second Shutdown: %v", err)
	}
}

func TestSpanContextHandler_PassesThroughWithoutSpan(t *testing.T) {
	buf := &bytes.Buffer{}
	inner := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := platformotel.NewSpanContextHandler(inner)
	logger := slog.New(h)

	logger.Info("plain", "k", "v")
	rec := map[string]any{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &rec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rec["k"] != "v" {
		t.Errorf("expected k=v in record, got %v", rec)
	}
}

func TestSpanContextHandler_InjectsIDsFromSpan(t *testing.T) {
	buf := &bytes.Buffer{}
	inner := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := platformotel.NewSpanContextHandler(inner)
	logger := slog.New(h)

	tracer := sdktrace.NewTracerProvider().Tracer("test")
	ctx, span := tracer.Start(context.Background(), "op")
	defer span.End()

	logger.InfoContext(ctx, "with-span")

	rec := map[string]any{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &rec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rec["trace_id"] != span.SpanContext().TraceID().String() {
		t.Errorf("trace_id mismatch: got %v want %s", rec["trace_id"], span.SpanContext().TraceID())
	}
	if rec["span_id"] != span.SpanContext().SpanID().String() {
		t.Errorf("span_id mismatch: got %v want %s", rec["span_id"], span.SpanContext().SpanID())
	}
}

// Compile-time check that SpanContextHandler satisfies slog.Handler.
var _ slog.Handler = (*platformotel.SpanContextHandler)(nil)

func lastLine(s string) string {
	s = strings.TrimRight(s, "\n")
	if idx := strings.LastIndex(s, "\n"); idx != -1 {
		return s[idx+1:]
	}
	return s
}
