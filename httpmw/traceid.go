package httpmw

import (
	"context"
	"net/http"

	"github.com/jedi-knights/go-logging/pkg/logging"
	"go.opentelemetry.io/otel/trace"
)

const traceIDHeader = "X-Trace-ID"

// newTraceID returns a fresh trace ID via [logging.NewTraceID]. It panics
// when the underlying crypto/rand call fails (signaled by an empty return),
// because a broken CSPRNG makes secure trace IDs impossible and silently
// emitting a degraded value would mask the failure.
func newTraceID() string {
	id := logging.NewTraceID()
	if id == "" {
		panic("httpmw: logging.NewTraceID returned empty — crypto/rand unavailable")
	}
	return id
}

// TraceID injects a trace ID into the request context and echoes it in the
// X-Trace-ID response header.
//
// When a valid OpenTelemetry span is already on the request context (for
// example because otelhttp wraps the handler), the span's trace ID is used,
// so the header a client sees is the same ID the trace backend and the access
// log carry; any inbound X-Trace-ID is ignored in that case. Otherwise the
// inbound X-Trace-ID is reused when it is a canonical UUID v4, and a fresh UUID
// is generated when it is missing or malformed (which prevents log injection).
func TraceID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var traceID string
		if sc := trace.SpanContextFromContext(r.Context()); sc.IsValid() {
			traceID = sc.TraceID().String()
		} else if inbound := r.Header.Get(traceIDHeader); uuidPattern.MatchString(inbound) {
			traceID = inbound
		} else {
			traceID = newTraceID()
		}
		ctx := logging.WithTraceID(r.Context(), traceID)
		w.Header().Set(traceIDHeader, traceID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// traceIDsFromContext returns the trace_id and span_id to attach to a log
// record. When a valid OpenTelemetry span is on the context its IDs win, since
// that is the identifier the trace backend indexes. Otherwise it falls back to
// the UUID stored by [TraceID] (with an empty span_id) so the X-Trace-ID
// response header and the log line stay consistent for callers not yet using
// OTel.
func traceIDsFromContext(ctx context.Context) (traceID, spanID string) {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		return sc.TraceID().String(), sc.SpanID().String()
	}
	return logging.TraceIDFromContext(ctx), ""
}
