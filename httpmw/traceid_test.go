package httpmw_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jedi-knights/go-logging/pkg/logging"
	"go.opentelemetry.io/otel/trace"

	"github.com/jedi-knights/go-platform/httpmw"
)

func TestTraceID_AcceptanceMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		id       string
		wantEcho bool
	}{
		{"valid v4 variant a", "f47ac10b-58cc-4372-a567-0e02b2c3d479", true},
		{"valid v4 variant 8", "550e8400-e29b-4d0f-8716-446655440000", true},
		{"valid v4 variant b", "550e8400-e29b-4d0f-b716-446655440000", true},
		{"empty", "", false},
		{"too short", "550e8400-e29b-4d0f-a716", false},
		{"uppercase hex", "F47AC10B-58CC-4372-A567-0E02B2C3D479", false},
		{"version 1", "550e8400-e29b-1d0f-a716-446655440000", false},
		{"version 3", "550e8400-e29b-3d0f-a716-446655440000", false},
		{"invalid variant c", "550e8400-e29b-4d0f-c716-446655440000", false},
		{"sql injection", "'; DROP TABLE users; --", false},
		{"log injection", "x\nlevel=ERROR forged", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var inCtx string
			h := httpmw.TraceID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				inCtx = logging.TraceIDFromContext(r.Context())
			}))
			hdr := map[string]string{}
			if tt.id != "" {
				hdr["X-Trace-ID"] = tt.id
			}
			got := serve(h, "/", hdr).Header().Get("X-Trace-ID")

			if got == "" {
				t.Fatal("X-Trace-ID must always be set, even when input is rejected")
			}
			if got != inCtx {
				t.Errorf("response header %q != context trace id %q", got, inCtx)
			}
			if !uuidV4.MatchString(got) {
				t.Errorf("trace id %q is not a canonical UUID v4", got)
			}
			if echoed := got == tt.id; echoed != tt.wantEcho {
				t.Errorf("echoed = %v, want %v (got %q)", echoed, tt.wantEcho, got)
			}
		})
	}
}

// Generated IDs must be acceptable to the middleware itself: if the emit
// format and the accept format ever drift apart, every request would have its
// freshly generated ID replaced by another one on the next hop.
func TestTraceID_GeneratedIDsRoundTrip(t *testing.T) {
	t.Parallel()
	h := httpmw.TraceID(ok)
	seen := make(map[string]struct{})
	for range 2_000 {
		id := serve(h, "/", nil).Header().Get("X-Trace-ID")
		if !uuidV4.MatchString(id) {
			t.Fatalf("generated trace id %q is not a canonical UUID v4", id)
		}
		if echoed := serve(h, "/", map[string]string{"X-Trace-ID": id}).Header().Get("X-Trace-ID"); echoed != id {
			t.Fatalf("generated id %q was not accepted on the next hop (got %q)", id, echoed)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate generated trace id %q", id)
		}
		seen[id] = struct{}{}
	}
}

// With an OTel span active, the X-Trace-ID header must be the span's trace ID:
// that is the identifier the access log and the trace backend carry, so a
// client quoting the header can find both.
func TestTraceID_UsesActiveOTelTraceID(t *testing.T) {
	t.Parallel()
	var inCtx string
	h := httpmw.TraceID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		inCtx = logging.TraceIDFromContext(r.Context())
	}))

	w := serveWithSpan(h)

	if got := w.Header().Get("X-Trace-ID"); got != wantOTelTraceID {
		t.Errorf("X-Trace-ID = %q, want the OTel trace id %q", got, wantOTelTraceID)
	}
	if inCtx != wantOTelTraceID {
		t.Errorf("context trace id = %q, want %q", inCtx, wantOTelTraceID)
	}
}

func TestTraceID_ActiveOTelSpanOverridesInboundHeader(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Trace-ID", validUUID)
	req = req.WithContext(trace.ContextWithSpanContext(req.Context(), otelSpanContext()))
	w := httptest.NewRecorder()

	httpmw.TraceID(ok).ServeHTTP(w, req)

	if got := w.Header().Get("X-Trace-ID"); got != wantOTelTraceID {
		t.Errorf("X-Trace-ID = %q, want %q (inbound %q must not win over an active span)", got, wantOTelTraceID, validUUID)
	}
}

func TestStack_HeaderAndAccessLogAgreeUnderOTel(t *testing.T) {
	t.Parallel()
	logger, buf := newBufLogger(t)

	w := serveWithSpan(httpmw.Stack(logger)(ok))

	l := findLog(logLines(t, buf), "request completed")
	if l == nil || l["trace_id"] != w.Header().Get("X-Trace-ID") {
		t.Errorf("access log trace_id = %v, X-Trace-ID header = %q; they must match", l["trace_id"], w.Header().Get("X-Trace-ID"))
	}
}
