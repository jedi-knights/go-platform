// Package httputil provides HTTP response helpers and middleware shared
// across jedi-knights services.
//
// # Surface
//
//   - [WriteJSON] / [WriteError] — buffer-before-headers JSON response helpers.
//     Encoding failures result in 500 Internal Server Error, never a 200 with
//     a truncated body.
//   - [HTTPStatus] — maps an [apperrors.AppError] code to its HTTP status.
//   - [TraceIDMiddleware] — injects a UUID v4 trace ID into request context
//     via [logging.WithTraceID] and echoes it in the X-Trace-ID response header.
//     Invalid or missing inbound IDs are replaced with a freshly generated one
//     to prevent log-injection via crafted headers.
//   - [LoggingMiddleware] — emits a structured log line per request with
//     method, path, status, duration, trace_id, request_id, remote_ip, and
//     user_agent.
//   - [RecoveryMiddleware] — recovers from handler panics, logs them, and
//     writes a 500 only when no response has been committed yet.
//
// # Middleware order
//
// The canonical chain is:
//
//	TraceIDMiddleware → RecoveryMiddleware → LoggingMiddleware → (auth) → handler
//
// [TraceIDMiddleware] must run first so every later layer has a trace ID in
// context. [RecoveryMiddleware] must wrap [LoggingMiddleware] so panics are
// caught before the logging middleware tries to record a status.
//
// # WriteJSON invariant
//
// [WriteJSON] always encodes into a [bytes.Buffer] before touching the
// [http.ResponseWriter]. This is intentional and must be preserved: a stream
// encode that fails mid-write commits a 200 OK header with a truncated body,
// which is worse than a 500 because clients cannot distinguish it from a
// successful response.
package httputil
