package httputil

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jedi-knights/go-logging/pkg/logging"
)

// Logger is an alias for [logging.Logger]. It keeps middleware signatures
// readable without forcing callers to import the logging package twice.
type Logger = logging.Logger

const traceIDHeader = "X-Trace-ID"

// uuidPattern matches a canonical RFC 4122 v4 UUID. Validating before reuse
// prevents log-injection via crafted X-Trace-ID headers.
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// newTraceID returns a fresh trace ID via [logging.NewTraceID]. It panics
// when the underlying crypto/rand call fails (signaled by an empty return),
// because a broken CSPRNG makes secure trace IDs impossible and silently
// emitting a degraded value would mask the failure.
func newTraceID() string {
	id := logging.NewTraceID()
	if id == "" {
		panic("httputil: logging.NewTraceID returned empty — crypto/rand unavailable")
	}
	return id
}

// TraceIDMiddleware injects a trace ID into the request context. It reads
// X-Trace-ID from the inbound request header when the value is a canonical
// UUID v4; otherwise it generates a fresh one. The selected ID is echoed in
// the X-Trace-ID response header so downstream consumers and clients see it.
func TraceIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceID := r.Header.Get(traceIDHeader)
		if !uuidPattern.MatchString(traceID) {
			// Reject missing, malformed, or potentially injected trace IDs.
			traceID = newTraceID()
		}
		ctx := logging.WithTraceID(r.Context(), traceID)
		w.Header().Set(traceIDHeader, traceID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// responseWriter wraps [http.ResponseWriter] to capture the status code and
// track whether the response header has been committed. LoggingMiddleware
// uses the captured status; RecoveryMiddleware uses wroteHeader to avoid
// emitting a second response after a panic.
type responseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.wroteHeader = true
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		// Explicitly call WriteHeader(200) through the wrapper so that both
		// the wrapper state (status, wroteHeader) and the underlying
		// ResponseWriter are committed via a single canonical path.
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

// LoggingMiddleware returns middleware that emits a structured log line
// per request.
//
// It reads trace_id and request_id from the request context, so it must be
// placed INSIDE TraceIDMiddleware (and any request-ID middleware) in the
// chain:
//
//	TraceIDMiddleware → RecoveryMiddleware → LoggingMiddleware → handler
//
// Fields logged on every request: method, path, status, duration_ms,
// trace_id, request_id, remote_ip, user_agent.
func LoggingMiddleware(logger Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w, status: 0}

			// Read correlation IDs set by upstream middleware. Both default
			// to "" when not present; the log line still records the absence.
			ctx := r.Context()
			traceID := logging.TraceIDFromContext(ctx)
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
				"request_id", requestID,
				"remote_ip", remoteIP,
				"user_agent", r.UserAgent(),
			)
			l.Info("request completed")
		})
	}
}

// remoteIP extracts the IP address from a "host:port" RemoteAddr string.
func remoteIP(remoteAddr string) string {
	if idx := strings.LastIndex(remoteAddr, ":"); idx != -1 {
		return remoteAddr[:idx]
	}
	return remoteAddr
}

// RecoveryMiddleware returns middleware that recovers from handler panics
// and logs them. It wraps the [http.ResponseWriter] so it can detect whether
// a partial response was already committed before the panic; when it was,
// it skips writing a 500 to avoid emitting conflicting headers or a double
// body.
func RecoveryMiddleware(logger Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rw := &responseWriter{ResponseWriter: w}
			defer func() {
				if rec := recover(); rec != nil {
					ctx := r.Context()
					traceID := logging.TraceIDFromContext(ctx)
					logger.With("trace_id", traceID, "panic", fmt.Sprintf("%v", rec)).
						Error("recovered from panic")
					if !rw.wroteHeader {
						http.Error(rw, "internal server error", http.StatusInternalServerError)
					}
				}
			}()
			next.ServeHTTP(rw, r)
		})
	}
}
