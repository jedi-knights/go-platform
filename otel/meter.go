package otel

import (
	"context"
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

// Meter returns the package's well-known Meter from the global
// provider. Mirrors [Tracer] — use this instead of holding a Meter
// handle so every call site shares one instrumentation name.
func Meter() metric.Meter {
	return otel.Meter(InstrumentationName)
}

// initMeter wires a MeterProvider with an OpenTelemetry Prometheus
// exporter. Metrics are exposed via the returned http.Handler on a
// caller-mounted endpoint (conventionally /metrics on :9464) and
// scraped by Fly's hosted Prometheus.
//
// The returned registry carries both the OTel-sourced metrics and the
// standard Go runtime / process collectors so a single scrape surfaces
// everything an operator needs for liveness and resource questions.
//
// Shared with the tracer provider: the resource argument carries the
// same service.name / service.version / deployment.environment set so
// a Grafana query that filters on service.name joins metrics, traces,
// and logs for the same service.
func initMeter(_ context.Context, res *resource.Resource) (*sdkmetric.MeterProvider, *prometheus.Registry, http.Handler, error) {
	reg := prometheus.NewRegistry()

	// Register Go runtime + process collectors on the same registry so
	// go_goroutines, go_memstats_*, process_cpu_seconds_total, etc.
	// land in the same scrape as the OTel-sourced metrics.
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	exporter, err := otelprom.New(otelprom.WithRegisterer(reg))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("otel.initMeter: prometheus exporter: %w", err)
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(exporter),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	// runtime.Start registers go.opentelemetry.io/contrib's standard
	// Go runtime metrics (runtime.go.gc.pause.ns, runtime.go.mem.*,
	// etc.) against the global MeterProvider we just installed.
	// Failure here is unusual but not fatal — the manual Go collectors
	// above still produce the critical metrics, so we return the
	// provider and surface the error so callers can log it.
	if err := runtime.Start(runtime.WithMeterProvider(mp)); err != nil {
		return mp, reg, nil, fmt.Errorf("otel.initMeter: runtime instrumentation: %w", err)
	}

	handler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		Registry:          reg,
		EnableOpenMetrics: true,
	})

	return mp, reg, handler, nil
}
