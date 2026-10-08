// Package httpserver provides the server lifecycle shared by every
// jedi-knights Go service: an [http.Server] with the platform's standard
// timeouts and graceful shutdown, liveness and readiness handlers, and the
// Prometheus scrape-endpoint server. See ADR-0001 in jedi-knights/architecture
// (docs/adr/0001-shared-http-middleware.md).
//
// Middleware lives in [github.com/jedi-knights/go-platform/httpmw]; response
// helpers live in [github.com/jedi-knights/go-platform/httputil].
//
// # Usage
//
//	mux.Handle("GET /health", httpserver.HealthHandler())
//	handler := httpmw.Stack(logger)(mux)
//	srv := httpserver.New(":8080", handler)
//	if err := srv.Run(ctx); err != nil { ... }
//
// # Surface
//
//   - [New] / [Server.Run] / [Server.Serve] — standard timeouts (header 5s,
//     read 15s, write 15s, idle 60s), SIGINT/SIGTERM handling, and graceful
//     shutdown with a bounded drain.
//   - [HealthHandler] / [ReadyHandler] — liveness and readiness endpoints.
//   - [StartMetricsServer] — the fleet-wide Prometheus scrape listener.
package httpserver
