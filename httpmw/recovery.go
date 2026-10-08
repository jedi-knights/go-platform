package httpmw

import (
	"fmt"
	"net/http"

	"github.com/jedi-knights/go-logging/pkg/logging"
)

// Recovery returns middleware that recovers from handler panics
// and logs them. It wraps the [http.ResponseWriter] so it can detect whether
// a partial response was already committed before the panic; when it was,
// it skips writing a 500 to avoid emitting conflicting headers or a double
// body.
func Recovery(logger Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rw := &responseWriter{ResponseWriter: w}
			defer func() {
				if rec := recover(); rec != nil {
					ctx := r.Context()
					traceID, spanID := traceIDsFromContext(ctx)
					logger.With(
						"trace_id", traceID,
						"span_id", spanID,
						"request_id", logging.RequestIDFromContext(ctx),
						"panic", fmt.Sprintf("%v", rec),
					).
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
