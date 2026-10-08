package httpmw_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jedi-knights/go-logging/pkg/logging"

	"github.com/jedi-knights/go-platform/httpmw"
)

func TestRequestID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		inbound   string
		wantReuse bool
	}{
		{"valid uuid v4 reused", validUUID, true},
		{"missing replaced", "", false},
		{"not a uuid replaced", "abc", false},
		{"log injection replaced", "x\nlevel=ERROR forged", false},
		{"uuid v1 replaced", "f47ac10b-58cc-1372-a567-0e02b2c3d479", false},
		{"uppercase replaced", "F47AC10B-58CC-4372-A567-0E02B2C3D479", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var inContext string
			h := httpmw.RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				inContext = logging.RequestIDFromContext(r.Context())
			}))
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.inbound != "" {
				req.Header.Set("X-Request-ID", tt.inbound)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			got := w.Header().Get("X-Request-ID")
			if got == "" {
				t.Fatal("X-Request-ID response header not set")
			}
			if got != inContext {
				t.Errorf("header %q != context %q", got, inContext)
			}
			if reused := got == tt.inbound; reused != tt.wantReuse {
				t.Errorf("reused = %v, want %v (got %q)", reused, tt.wantReuse, got)
			}
		})
	}
}

func TestRequestID_GeneratesDistinctIDs(t *testing.T) {
	t.Parallel()
	h := httpmw.RequestID(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	a, b := httptest.NewRecorder(), httptest.NewRecorder()
	h.ServeHTTP(a, httptest.NewRequest(http.MethodGet, "/", nil))
	h.ServeHTTP(b, httptest.NewRequest(http.MethodGet, "/", nil))
	if a.Header().Get("X-Request-ID") == b.Header().Get("X-Request-ID") {
		t.Error("two requests received the same generated request ID")
	}
}
