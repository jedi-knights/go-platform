package httpmw_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/jedi-knights/go-platform/httpmw"
)

// The defect this package exists to prevent: access logs must carry the same
// trace and request IDs the client sees in response headers.
func TestStack_AccessLogCarriesTraceAndRequestID(t *testing.T) {
	t.Parallel()
	logger, buf := newBufLogger(t)

	w := serve(httpmw.Stack(logger)(ok), "/things", nil)

	traceID, requestID := w.Header().Get("X-Trace-ID"), w.Header().Get("X-Request-ID")
	if traceID == "" || requestID == "" {
		t.Fatalf("response headers: X-Trace-ID=%q X-Request-ID=%q, want both set", traceID, requestID)
	}
	out := buf.String()
	if !strings.Contains(out, "request completed") {
		t.Fatalf("no access log line in %q", out)
	}
	if !strings.Contains(out, traceID) {
		t.Errorf("access log missing trace id %q: %s", traceID, out)
	}
	if !strings.Contains(out, requestID) {
		t.Errorf("access log missing request id %q: %s", requestID, out)
	}
}

func TestStack_PanicLogCarriesTraceIDAndClientGets500(t *testing.T) {
	t.Parallel()
	logger, buf := newBufLogger(t)
	boom := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("kaboom") })

	w := serve(httpmw.Stack(logger)(boom), "/things", nil)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	l := findLog(logLines(t, buf), "recovered from panic")
	if l == nil {
		t.Fatalf("panic not logged: %s", buf.String())
	}
	if l["trace_id"] != w.Header().Get("X-Trace-ID") || l["request_id"] != w.Header().Get("X-Request-ID") {
		t.Errorf("panic log ids = (%v, %v), want response header ids (%q, %q)",
			l["trace_id"], l["request_id"], w.Header().Get("X-Trace-ID"), w.Header().Get("X-Request-ID"))
	}
}

func TestStack_HealthIsNotAccessLoggedByDefault(t *testing.T) {
	t.Parallel()
	logger, buf := newBufLogger(t)
	h := httpmw.Stack(logger)(ok)

	w := serve(h, "/health", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if strings.Contains(buf.String(), "request completed") {
		t.Errorf("/health should not be access-logged: %s", buf.String())
	}
	// Correlation IDs still apply to skipped paths.
	if w.Header().Get("X-Trace-ID") == "" || w.Header().Get("X-Request-ID") == "" {
		t.Error("skipped path must still receive trace and request IDs")
	}
}

func TestStack_SkippedPathStillRecoversFromPanic(t *testing.T) {
	t.Parallel()
	logger, _ := newBufLogger(t)
	boom := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("kaboom") })

	w := serve(httpmw.Stack(logger)(boom), "/health", nil)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestStack_WithSkipLogPaths(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		opts       []httpmw.StackOption
		path       string
		wantLogged bool
	}{
		{"custom path skipped", []httpmw.StackOption{httpmw.WithSkipLogPaths("/ping")}, "/ping", false},
		{"custom option replaces default", []httpmw.StackOption{httpmw.WithSkipLogPaths("/ping")}, "/health", true},
		{"no args disables skipping", []httpmw.StackOption{httpmw.WithSkipLogPaths()}, "/health", true},
		{"match is exact, not prefix", nil, "/health/deep", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			logger, buf := newBufLogger(t)
			serve(httpmw.Stack(logger, tt.opts...)(ok), tt.path, nil)
			if got := strings.Contains(buf.String(), "request completed"); got != tt.wantLogged {
				t.Errorf("logged = %v, want %v (%s)", got, tt.wantLogged, buf.String())
			}
		})
	}
}

func TestStack_ReusesValidInboundIDs(t *testing.T) {
	t.Parallel()
	logger, _ := newBufLogger(t)

	w := serve(httpmw.Stack(logger)(ok), "/x",
		map[string]string{"X-Trace-ID": validUUID, "X-Request-ID": validUUID})

	if got := w.Header().Get("X-Trace-ID"); got != validUUID {
		t.Errorf("X-Trace-ID = %q, want %q", got, validUUID)
	}
	if got := w.Header().Get("X-Request-ID"); got != validUUID {
		t.Errorf("X-Request-ID = %q, want %q", got, validUUID)
	}
}

func TestStack_NilArgumentsPanic(t *testing.T) {
	t.Parallel()
	logger, _ := newBufLogger(t)
	mustPanic(t, "nil logger", func() { httpmw.Stack(nil) })
	mustPanic(t, "nil handler", func() { httpmw.Stack(logger)(nil) })
}
