package httpmw

import (
	"net/http"
	"regexp"

	"github.com/jedi-knights/go-logging/pkg/logging"
)

const requestIDHeader = "X-Request-ID"

// uuidPattern matches a canonical RFC 4122 v4 UUID. Inbound trace and request
// IDs are only reused when they match, which prevents log injection via
// crafted X-Trace-ID / X-Request-ID headers.
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// RequestID stores a request ID in the request context via
// [logging.WithRequestID] and echoes it in the X-Request-ID response header.
// An inbound X-Request-ID is reused only when it is a canonical UUID v4;
// otherwise a fresh one is generated.
//
// It panics if the system CSPRNG is unavailable: a degraded ID would mask the
// failure.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if !uuidPattern.MatchString(id) {
			id = newTraceID()
		}
		w.Header().Set(requestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(logging.WithRequestID(r.Context(), id)))
	})
}
