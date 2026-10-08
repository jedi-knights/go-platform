package httpmw

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/jedi-knights/go-platform/httputil"
)

// readyTimeout bounds the total time ReadyHandler spends running checks, so a
// hung dependency cannot hold probe connections open indefinitely.
const readyTimeout = 2 * time.Second

// writeStatus writes the {"status": value} body shared by the liveness and
// readiness handlers.
func writeStatus(w http.ResponseWriter, code int, value string) {
	httputil.WriteJSON(w, code, map[string]string{"status": value})
}

// HealthHandler returns a liveness handler that responds 200 {"status":"ok"}.
func HealthHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeStatus(w, http.StatusOK, "ok")
	})
}

// ReadyCheck reports whether one dependency is ready. It must honor ctx
// cancellation; the context carries a deadline of two seconds.
type ReadyCheck func(ctx context.Context) error

// ReadyHandler returns a readiness handler. It runs checks in order and stops
// at the first failure, responding 503 {"status":"unavailable"}. The failing
// check's error text is deliberately not returned to the caller. With no
// checks it behaves like [HealthHandler].
func ReadyHandler(checks ...ReadyCheck) http.Handler {
	for i, c := range checks {
		if c == nil {
			panic(fmt.Sprintf("httpmw: ReadyHandler check %d is nil", i))
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
		defer cancel()
		for _, check := range checks {
			if err := check(ctx); err != nil {
				writeStatus(w, http.StatusServiceUnavailable, "unavailable")
				return
			}
		}
		writeStatus(w, http.StatusOK, "ok")
	})
}
