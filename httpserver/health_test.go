package httpserver_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jedi-knights/go-platform/httpserver"
)

func TestHealthHandler(t *testing.T) {
	t.Parallel()
	w := serve(httpserver.HealthHandler(), "/health")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := strings.TrimSpace(w.Body.String()); got != `{"status":"ok"}` {
		t.Errorf("body = %s", got)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestReadyHandler(t *testing.T) {
	t.Parallel()
	pass := func(context.Context) error { return nil }
	fail := func(context.Context) error { return errors.New("db password=hunter2 unreachable") }

	tests := []struct {
		name     string
		checks   []httpserver.ReadyCheck
		wantCode int
		wantBody string
	}{
		{"no checks is ready", nil, http.StatusOK, `{"status":"ok"}`},
		{"all pass", []httpserver.ReadyCheck{pass, pass}, http.StatusOK, `{"status":"ok"}`},
		{"one fails", []httpserver.ReadyCheck{pass, fail}, http.StatusServiceUnavailable, `{"status":"unavailable"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := serve(httpserver.ReadyHandler(tt.checks...), "/ready")
			if w.Code != tt.wantCode {
				t.Errorf("status = %d, want %d", w.Code, tt.wantCode)
			}
			if got := strings.TrimSpace(w.Body.String()); got != tt.wantBody {
				t.Errorf("body = %s, want %s", got, tt.wantBody)
			}
			if strings.Contains(w.Body.String(), "hunter2") {
				t.Error("check error text leaked to the client")
			}
		})
	}
}

func TestReadyHandler_StopsAtFirstFailure(t *testing.T) {
	t.Parallel()
	called := false
	h := httpserver.ReadyHandler(
		func(context.Context) error { return errors.New("down") },
		func(context.Context) error { called = true; return nil },
	)
	serve(h, "/ready")
	if called {
		t.Error("checks after the first failure should not run")
	}
}

func TestReadyHandler_ChecksReceiveBoundedContext(t *testing.T) {
	t.Parallel()
	var deadline time.Time
	var has bool
	h := httpserver.ReadyHandler(func(ctx context.Context) error {
		deadline, has = ctx.Deadline()
		return nil
	})
	serve(h, "/ready")
	if !has {
		t.Fatal("check context has no deadline")
	}
	if remaining := time.Until(deadline); remaining <= 0 || remaining > 3*time.Second {
		t.Errorf("deadline in %s, want within (0, 3s]", remaining)
	}
}

func TestReadyHandler_NilCheckPanics(t *testing.T) {
	t.Parallel()
	mustPanic(t, "nil check", func() { httpserver.ReadyHandler(nil) })
}

// A check that never returns on its own must be cut off by the readiness
// deadline, and the probe must see "unavailable" rather than hang.
func TestReadyHandler_HungCheckIsCutOff(t *testing.T) {
	t.Parallel()
	h := httpserver.ReadyHandler(func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	start := time.Now()
	w := serve(h, "/ready")
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Errorf("readiness took %s; the deadline is not bounding checks", elapsed)
	}
}

func TestReadyHandler_ClientDisconnectCancelsChecks(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var sawCancel bool
	h := httpserver.ReadyHandler(func(c context.Context) error {
		sawCancel = c.Err() != nil
		return c.Err()
	})
	req := httptest.NewRequest(http.MethodGet, "/ready", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if !sawCancel || w.Code != http.StatusServiceUnavailable {
		t.Errorf("sawCancel=%v code=%d, want the request context to propagate to checks", sawCancel, w.Code)
	}
}
