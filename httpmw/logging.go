package httpmw

import (
	"net"
	"net/http"
	"time"

	"github.com/jedi-knights/go-logging/pkg/logging"
)

// Logger is an alias for [logging.Logger]. It keeps middleware signatures
// readable without forcing callers to import the logging package twice.
type Logger = logging.Logger

// Logging returns middleware that emits a structured log line
// per request.
//
// It reads trace_id and request_id from the request context, so it must be
// placed INSIDE TraceID and [RequestID] in the
// chain:
//
//	TraceID → Recovery → Logging → handler
//
// Fields logged on every request: method, path, status, duration_ms,
// trace_id, span_id, request_id, remote_ip, user_agent.
func Logging(logger Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w, status: 0}

			// Read correlation IDs set by upstream middleware. Both default
			// to "" when not present; the log line still records the absence.
			ctx := r.Context()
			traceID, spanID := traceIDsFromContext(ctx)
			requestID := logging.RequestIDFromContext(ctx)

			remoteIP := remoteIP(r.RemoteAddr)

			next.ServeHTTP(rw, r)
			if !rw.wroteHeader {
				// A handler that wrote nothing defaults to 200 on the wire
				// (per net/http). Record that here so the log line never
				// reports a synthetic 0.
				rw.status = http.StatusOK
			}

			duration := time.Since(start)
			l := logger.With(
				"method", r.Method,
				"path", r.URL.Path,
				"status", rw.status,
				"duration_ms", duration.Milliseconds(),
				"trace_id", traceID,
				"span_id", spanID,
				"request_id", requestID,
				"remote_ip", remoteIP,
				"user_agent", r.UserAgent(),
			)
			l.Info("request completed")
		})
	}
}

// remoteIP extracts the host from a "host:port" RemoteAddr, handling IPv6
// ("[::1]:80" → "::1"). A value that is not host:port (for example a unix
// socket peer) is returned unchanged.
func remoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}
