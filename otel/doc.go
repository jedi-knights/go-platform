// Package otel provides the minimal OpenTelemetry bootstrap the Jedi
// Knights portfolio depends on for end-to-end distributed tracing. It
// pairs with go-platform/audit: audit records "did this happen and was
// it allowed", OTel records "what was the latency, what other spans
// participated in this request, and where did it fail".
//
// # Goals
//
// The package provides:
//
//   - [Init] — wires a TracerProvider with the standard OpenTelemetry
//     environment-variable contract (OTEL_EXPORTER_OTLP_ENDPOINT,
//     OTEL_SERVICE_NAME, OTEL_RESOURCE_ATTRIBUTES, etc.). When the OTLP
//     endpoint is unset the provider exports to stdout — useful for
//     local development without standing up a collector.
//   - [Tracer] — returns a Tracer scoped to the package's well-known
//     instrumentation name so consumers do not have to thread a Tracer
//     handle through their code.
//   - [StartSpan] — convenience over Tracer().Start that uses the
//     portfolio's span-naming convention.
//   - Portfolio-specific attribute keys ([AttrAgentID], [AttrToolName],
//     [AttrPolicyDecision], etc.) so spans across services use the same
//     names and downstream queries are stable.
//
// # Non-goals
//
// This package is intentionally not a generic OTel wrapper. It hides
// nothing the OTel SDK exposes; it only handles bootstrap, picks
// sensible defaults, and centralizes the attribute-key vocabulary.
// Custom span processors, samplers, and propagators are out of scope —
// services that need those use the SDK directly.
//
// # Concurrency
//
// [Init] is not safe to call concurrently with itself. Call it once at
// the composition root. After Init returns, [Tracer], [StartSpan], and
// every helper is safe to call from any goroutine.
//
// # Shutdown
//
// [Init] returns a Shutdown function that flushes buffered spans and
// shuts the provider down. Register it as a deferred call in main, or
// wire it into [container.Container.OnClose].
package otel
