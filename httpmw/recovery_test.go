package httpmw_test

import (
	"net/http"
	"testing"

	"github.com/jedi-knights/go-platform/httpmw"
)

func TestRecovery_PanicBecomes500AndIsLogged(t *testing.T) {
	t.Parallel()
	logger, buf := newBufLogger(t)
	h := httpmw.Recovery(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("kaboom") }))

	w := serve(h, "/", nil)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	l := findLog(logLines(t, buf), "recovered from panic")
	if l == nil || l["level"] != "ERROR" || l["panic"] != "kaboom" {
		t.Errorf("panic not logged as ERROR with its value: %s", buf.String())
	}
}

func TestRecovery_NonStringPanicValuesAreLogged(t *testing.T) {
	t.Parallel()
	for name, v := range map[string]any{"error": http.ErrAbortHandler, "int": 42} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			logger, buf := newBufLogger(t)
			h := httpmw.Recovery(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(v) }))
			if w := serve(h, "/", nil); w.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want 500", w.Code)
			}
			if findLog(logLines(t, buf), "recovered from panic") == nil {
				t.Errorf("panic not logged: %s", buf.String())
			}
		})
	}
}

func TestRecovery_DoesNotOverwriteCommittedResponse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		handler  http.HandlerFunc
		wantCode int
		wantBody string
	}{
		{"panic after Write keeps implicit 200", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("partial"))
			panic("late")
		}, http.StatusOK, "partial"},
		{"panic after WriteHeader keeps that status", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			panic("late")
		}, http.StatusAccepted, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			logger, buf := newBufLogger(t)
			w := serve(httpmw.Recovery(logger)(tt.handler), "/", nil)
			if w.Code != tt.wantCode || w.Body.String() != tt.wantBody {
				t.Errorf("code=%d body=%q, want %d %q", w.Code, w.Body.String(), tt.wantCode, tt.wantBody)
			}
			if findLog(logLines(t, buf), "recovered from panic") == nil {
				t.Error("panic must still be logged when the response was already committed")
			}
		})
	}
}

func TestRecovery_PassesThroughNormalRequests(t *testing.T) {
	t.Parallel()
	logger, buf := newBufLogger(t)
	h := httpmw.Recovery(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("made"))
	}))
	w := serve(h, "/", nil)
	if w.Code != http.StatusCreated || w.Body.String() != "made" {
		t.Errorf("code=%d body=%q", w.Code, w.Body.String())
	}
	if buf.Len() != 0 {
		t.Errorf("Recovery must not log when nothing panicked: %s", buf.String())
	}
}

func TestRecovery_PanicLogPrefersOTelSpanIDs(t *testing.T) {
	t.Parallel()
	logger, buf := newBufLogger(t)
	h := httpmw.Recovery(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("kaboom") }))

	w := serveWithSpan(httpmw.TraceID(h))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	l := findLog(logLines(t, buf), "recovered from panic")
	if l == nil || l["trace_id"] != wantOTelTraceID || l["span_id"] != wantOTelSpanID {
		t.Errorf("panic log trace_id/span_id = %v/%v, want %s/%s", l["trace_id"], l["span_id"], wantOTelTraceID, wantOTelSpanID)
	}
}
