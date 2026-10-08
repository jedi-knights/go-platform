package httpmw

import "net/http"

// defaultSkipLogPaths are exempt from the access-log line. Platform health
// checks poll /health constantly and would otherwise dominate log volume.
var defaultSkipLogPaths = []string{"/health"}

type stackConfig struct {
	skipLogPaths map[string]struct{}
}

// StackOption configures [Stack].
type StackOption func(*stackConfig)

// WithSkipLogPaths replaces the default set of paths (just "/health") whose
// requests do not emit an access-log line. Matching is exact on
// [http.Request.URL].Path. Request IDs, trace IDs, and panic recovery still
// apply to skipped paths. Calling it with no arguments disables skipping.
func WithSkipLogPaths(paths ...string) StackOption {
	return func(c *stackConfig) {
		c.skipLogPaths = make(map[string]struct{}, len(paths))
		for _, p := range paths {
			c.skipLogPaths[p] = struct{}{}
		}
	}
}

// Stack returns middleware that applies, outermost first:
//
//	RequestID → TraceID → Recovery → Logging → next
//
// TraceID and RequestID sit outside Recovery and Logging so both can read the
// IDs from the request context. Wrap Stack's result with any outer layer a
// service needs (for example otelhttp).
func Stack(logger Logger, opts ...StackOption) func(http.Handler) http.Handler {
	if logger == nil {
		panic("httpmw: Stack requires a non-nil logger")
	}
	cfg := stackConfig{skipLogPaths: make(map[string]struct{}, len(defaultSkipLogPaths))}
	for _, p := range defaultSkipLogPaths {
		cfg.skipLogPaths[p] = struct{}{}
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	recovery := Recovery(logger)
	logMW := Logging(logger)

	return func(next http.Handler) http.Handler {
		if next == nil {
			panic("httpmw: Stack applied to a nil handler")
		}
		logged := recovery(logMW(next))
		quiet := recovery(next)
		// O(1) per request: one map lookup selects the pre-built chain.
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, skip := cfg.skipLogPaths[r.URL.Path]; skip {
				quiet.ServeHTTP(w, r)
				return
			}
			logged.ServeHTTP(w, r)
		})
		return RequestID(TraceID(inner))
	}
}
