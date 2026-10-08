package httpserver_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jedi-knights/go-platform/httpserver"
)

func metricsHandler(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) })
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }() // test cleanup; close error is not actionable
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestStartMetricsServer_RejectsNilHandler(t *testing.T) {
	t.Parallel()
	srv, err := httpserver.StartMetricsServer("", "", nil)
	if err == nil || srv != nil {
		t.Fatalf("got (%v, %v), want nil server and an error", srv, err)
	}
}

func TestStartMetricsServer_ServesHandlerAtPathOnlyAndReportsBoundAddr(t *testing.T) {
	t.Parallel()
	srv, err := httpserver.StartMetricsServer("127.0.0.1:0", "/metrics", metricsHandler("up 1"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })

	base := "http://" + srv.Server.Addr
	if code, body := get(t, base+"/metrics"); code != http.StatusOK || !strings.Contains(body, "up 1") {
		t.Errorf("GET /metrics = %d %q, want 200 containing the handler body", code, body)
	}
	if code, _ := get(t, base+"/other"); code != http.StatusNotFound {
		t.Errorf("GET /other = %d, want 404 (only the scrape path is served)", code)
	}
}

func TestStartMetricsServer_DefaultPath(t *testing.T) {
	t.Parallel()
	srv, err := httpserver.StartMetricsServer("127.0.0.1:0", "", metricsHandler("m"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })

	if code, _ := get(t, "http://"+srv.Server.Addr+httpserver.DefaultMetricsPath); code != http.StatusOK {
		t.Errorf("GET %s = %d, want 200", httpserver.DefaultMetricsPath, code)
	}
}

func TestStartMetricsServer_ListenFailureIsReturned(t *testing.T) {
	t.Parallel()
	first, err := httpserver.StartMetricsServer("127.0.0.1:0", "", metricsHandler("m"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Shutdown(context.Background()) })

	if _, err := httpserver.StartMetricsServer(first.Server.Addr, "", metricsHandler("m")); err == nil {
		t.Fatal("binding an in-use address returned no error")
	}
}

func TestStartMetricsServer_ShutdownStopsServingAndIsRepeatable(t *testing.T) {
	t.Parallel()
	srv, err := httpserver.StartMetricsServer("127.0.0.1:0", "", metricsHandler("m"))
	if err != nil {
		t.Fatal(err)
	}
	addr := srv.Server.Addr
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err := srv.Shutdown(ctx); err != nil {
		t.Errorf("second Shutdown: %v, want nil (safe to call more than once)", err)
	}
	if resp, err := http.Get("http://" + addr + httpserver.DefaultMetricsPath); err == nil {
		_ = resp.Body.Close()
		t.Error("server still accepting connections after Shutdown")
	}
}

// Every fly.<svc>.toml [metrics] section assumes these exact values.
func TestMetricsDefaults(t *testing.T) {
	t.Parallel()
	if httpserver.DefaultMetricsAddr != ":9464" || httpserver.DefaultMetricsPath != "/metrics" {
		t.Errorf("fleet-wide defaults changed: %q %q", httpserver.DefaultMetricsAddr, httpserver.DefaultMetricsPath)
	}
}

// With no address given the server binds the fleet-wide default port. The
// port may legitimately be unavailable on a developer machine, so a bind
// failure skips rather than fails.
func TestStartMetricsServer_EmptyAddrUsesFleetDefault(t *testing.T) {
	srv, err := httpserver.StartMetricsServer("", "", metricsHandler("m"))
	if err != nil {
		t.Skipf("default metrics port unavailable: %v", err)
	}
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })

	if code, _ := get(t, "http://127.0.0.1"+httpserver.DefaultMetricsAddr+httpserver.DefaultMetricsPath); code != http.StatusOK {
		t.Errorf("default endpoint = %d, want 200", code)
	}
}
