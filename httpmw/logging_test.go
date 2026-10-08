package httpmw_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/trace"

	"github.com/jedi-knights/go-platform/httpmw"
)

func TestLogging_EmitsOneStructuredLinePerRequest(t *testing.T) {
	t.Parallel()
	logger, buf := newBufLogger(t)
	h := httpmw.Logging(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	req := httptest.NewRequest(http.MethodPost, "/brew?x=1", nil)
	req.Header.Set("User-Agent", "kettle/1.0")
	req.RemoteAddr = "203.0.113.9:4711"
	h.ServeHTTP(httptest.NewRecorder(), req)

	lines := logLines(t, buf)
	if len(lines) != 1 {
		t.Fatalf("got %d log lines, want 1: %s", len(lines), buf.String())
	}
	l := lines[0]
	want := map[string]any{
		"msg": "request completed", "method": "POST", "path": "/brew",
		"status": float64(http.StatusTeapot), "remote_ip": "203.0.113.9", "user_agent": "kettle/1.0",
	}
	for k, v := range want {
		if l[k] != v {
			t.Errorf("%s = %v, want %v", k, l[k], v)
		}
	}
	if _, ok := l["duration_ms"]; !ok {
		t.Error("duration_ms missing")
	}
}

func TestLogging_RecordsStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    float64
	}{
		{"explicit header", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }, 404},
		{"write without header implies 200", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("hi")) }, 200},
		{"writes nothing is 200, never 0", func(http.ResponseWriter, *http.Request) {}, 200},
		{"first header wins in the log", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte("x"))
		}, 201},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			logger, buf := newBufLogger(t)
			serve(httpmw.Logging(logger)(tt.handler), "/", nil)
			l := findLog(logLines(t, buf), "request completed")
			if l == nil || l["status"] != tt.want {
				t.Errorf("logged status = %v, want %v (%s)", l["status"], tt.want, buf.String())
			}
		})
	}
}

func TestLogging_PassesResponseThroughUnchanged(t *testing.T) {
	t.Parallel()
	logger, _ := newBufLogger(t)
	h := httpmw.Logging(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Custom", "v")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("body"))
	}))
	w := serve(h, "/", nil)
	if w.Code != http.StatusAccepted || w.Body.String() != "body" || w.Header().Get("X-Custom") != "v" {
		t.Errorf("response altered: code=%d body=%q hdr=%v", w.Code, w.Body.String(), w.Header())
	}
}

func TestLogging_RemoteIPFormats(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, remote, want string }{
		{"ipv4 host:port", "192.0.2.1:1234", "192.0.2.1"},
		{"ipv6 host:port drops brackets", "[2001:db8::1]:443", "2001:db8::1"},
		{"no port kept as-is", "unix-peer", "unix-peer"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			logger, buf := newBufLogger(t)
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remote
			httpmw.Logging(logger)(ok).ServeHTTP(httptest.NewRecorder(), req)
			l := findLog(logLines(t, buf), "request completed")
			if l == nil || l["remote_ip"] != tt.want {
				t.Errorf("remote_ip = %v, want %q", l["remote_ip"], tt.want)
			}
		})
	}
}

// otelSpanContext returns a valid, sampled OpenTelemetry span context.
func otelSpanContext() trace.SpanContext {
	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x4b, 0xf9, 0x2f, 0x35, 0x77, 0xb3, 0x4d, 0xa6, 0xa3, 0xce, 0x92, 0x9d, 0x0e, 0x0e, 0x47, 0x36},
		SpanID:     trace.SpanID{0x00, 0xf0, 0x67, 0xaa, 0x0b, 0xa9, 0x02, 0xb7},
		TraceFlags: trace.FlagsSampled,
	})
}

func serveWithSpan(h http.Handler) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(trace.ContextWithSpanContext(req.Context(), otelSpanContext()))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

const (
	wantOTelTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	wantOTelSpanID  = "00f067aa0ba902b7"
)

// When an OTel span is active its IDs, not the X-Trace-ID UUID, are what the
// trace backend indexes, so they must be what the access log carries.
func TestLogging_PrefersOTelSpanIDsOverTraceUUID(t *testing.T) {
	t.Parallel()
	logger, buf := newBufLogger(t)

	serveWithSpan(httpmw.TraceID(httpmw.Logging(logger)(ok)))

	l := findLog(logLines(t, buf), "request completed")
	if l == nil || l["trace_id"] != wantOTelTraceID || l["span_id"] != wantOTelSpanID {
		t.Errorf("trace_id/span_id = %v/%v, want %s/%s", l["trace_id"], l["span_id"], wantOTelTraceID, wantOTelSpanID)
	}
}

func TestLogging_WithoutSpanUsesTraceUUIDAndEmptySpanID(t *testing.T) {
	t.Parallel()
	logger, buf := newBufLogger(t)

	w := serve(httpmw.TraceID(httpmw.Logging(logger)(ok)), "/", nil)

	l := findLog(logLines(t, buf), "request completed")
	if l == nil || l["trace_id"] != w.Header().Get("X-Trace-ID") || l["span_id"] != "" {
		t.Errorf("trace_id/span_id = %v/%v, want %q/empty", l["trace_id"], l["span_id"], w.Header().Get("X-Trace-ID"))
	}
}
