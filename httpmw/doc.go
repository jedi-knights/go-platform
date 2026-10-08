// Package httpmw provides the HTTP middleware, health handlers, and server
// lifecycle shared by every jedi-knights Go service. See ADR-0001 in
// jedi-knights/architecture (docs/adr/0001-shared-http-middleware.md).
//
// All shared HTTP middleware lives here; [httputil] holds only response
// helpers ([httputil.WriteJSON], [httputil.WriteError]).
//
// # Tier 1 surface
//
//   - [Stack] — composes RequestID → TraceID → Recovery → Logging in the one
//     correct order. Prefer it to wiring the pieces by hand: the ordering is
//     the thing services repeatedly got wrong.
//   - [RequestID] — sets X-Request-ID and the context request ID.
//   - [TraceID] — injects a UUID v4 trace ID into the context via
//     [logging.WithTraceID] and echoes it in X-Trace-ID. Invalid or missing
//     inbound IDs are replaced, preventing log injection.
//   - [Logging] — one structured access-log line per request: method, path,
//     status, duration_ms, trace_id, request_id, remote_ip, user_agent.
//   - [Recovery] — recovers handler panics, logs them, and writes a 500 only
//     when no response has been committed yet.
//   - [HealthHandler] / [ReadyHandler] — liveness and readiness endpoints.
//   - [NewServer] — an [http.Server] with the platform's standard timeouts,
//     SIGINT/SIGTERM handling, and graceful shutdown.
//
// # Usage
//
//	handler := httpmw.Stack(logger)(mux)
//	handler = otelhttp.NewHandler(handler, "my-service") // optional, per service
//	srv := httpmw.NewServer(":8080", handler)
//	if err := srv.Run(ctx); err != nil { ... }
//
// # Why ordering matters
//
// [Logging] and [Recovery] read the
// trace ID from the request context before calling the next handler, so
// TraceID must be outermost. Wiring them in the opposite order silently logs
// an empty trace_id. [Stack] makes that mistake impossible.
package httpmw
