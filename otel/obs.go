package otel

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"

	"github.com/jedi-knights/go-logging/pkg/logging"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Observability bundles the three signals a service needs: a Tracer,
// a Meter, a Logger that carries the OTel span context on every
// record, and the HTTP handler that exposes the Prometheus scrape
// endpoint.
//
// Build one at the composition root via [New]. Services never
// construct these signals directly — holding one struct keeps the
// trio in sync (shared service.name resource, consistent shutdown).
type Observability struct {
	// Tracer is the package's well-known Tracer. Equivalent to
	// [Tracer] after [Init] has been called.
	Tracer trace.Tracer

	// Meter is the package's well-known Meter. Register
	// counters, histograms, and gauges against this handle at the
	// adapter boundary (not inside the use case).
	Meter metric.Meter

	// Logger emits structured records. Every record carries
	// trace_id and span_id when the call site passes a context that
	// holds an OTel span.
	Logger logging.Logger

	// PromRegistry is the Prometheus registry carrying the OTel
	// Prometheus exporter plus the Go runtime / process collectors.
	// Callers may register additional collectors here if a legacy
	// prometheus/client_golang metric needs to coexist with the
	// OTel ones (api-gateway's existing gateway_requests_total is
	// the current case).
	PromRegistry *prometheus.Registry

	// PromHandler is the http.Handler for /metrics. Mount it on the
	// metrics listener (see httputil.ServeMetrics) so Fly's hosted
	// Prometheus can scrape it.
	PromHandler http.Handler

	// Shutdown drains the tracer and meter providers in parallel
	// and is safe to call more than once. It is the single cleanup
	// hook callers defer in main.
	Shutdown func(ctx context.Context) error
}

// New wires all three OpenTelemetry signals and returns an
// [*Observability] handle. It replaces the trace-only [Init] for new
// callers — every service in the jedi-knights fleet is expected to
// go through this entry point (see the architecture repo's
// observability-strategy.md).
//
// The returned handle shares one [Resource] across signals so a
// Grafana query that filters on `service.name` joins traces,
// metrics, and logs consistently.
//
// Not safe to call concurrently with itself. Call once at the
// composition root; register [Observability.Shutdown] as a deferred
// call before the main goroutine exits.
func New(ctx context.Context, cfg Config) (*Observability, error) {
	serviceName := firstNonEmpty(cfg.ServiceName,
		os.Getenv("OTEL_SERVICE_NAME"))
	if serviceName == "" {
		return nil, fmt.Errorf("otel.New: ServiceName required (or set OTEL_SERVICE_NAME)")
	}

	res, err := buildResource(ctx, cfg, serviceName)
	if err != nil {
		return nil, fmt.Errorf("otel.New: building resource: %w", err)
	}

	tp, tracerShutdown, err := initTracerProvider(ctx, cfg, res)
	if err != nil {
		return nil, fmt.Errorf("otel.New: tracer: %w", err)
	}

	mp, reg, promHandler, err := initMeter(ctx, res)
	if err != nil {
		// Shut down the tracer we just installed so we don't leak a
		// provider on the error path.
		_ = tracerShutdown(ctx)
		return nil, fmt.Errorf("otel.New: meter: %w", err)
	}

	logger := initLogger(cfg)

	// Shutdown is guarded by sync.Once so defer-plus-explicit callers
	// (idiomatic in main.go) do not surface the SDK's "reader is
	// shutdown" / "tracer provider is shutdown" errors on the second
	// invocation. OTel's SDK does not treat Shutdown as idempotent;
	// this wrapper does.
	var once sync.Once
	var shutdownErr error
	shutdown := func(sctx context.Context) error {
		once.Do(func() {
			shutdownErr = errors.Join(
				tp.Shutdown(sctx),
				mp.Shutdown(sctx),
			)
		})
		return shutdownErr
	}

	return &Observability{
		Tracer:       tp.Tracer(InstrumentationName),
		Meter:        mp.Meter(InstrumentationName),
		Logger:       logger,
		PromRegistry: reg,
		PromHandler:  promHandler,
		Shutdown:     shutdown,
	}, nil
}
