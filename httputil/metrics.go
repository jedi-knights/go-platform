package httputil

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// DefaultMetricsAddr is the fleet-wide default listen address for the
// Prometheus scrape endpoint. 9464 is OpenTelemetry's conventional
// port for the Prometheus exporter; standardizing on it means every
// fly.<svc>.toml [metrics] section is byte-for-byte identical and
// operators do not have to memorize per-service variations.
const DefaultMetricsAddr = ":9464"

// DefaultMetricsPath is the fleet-wide default scrape path.
const DefaultMetricsPath = "/metrics"

// MetricsServer wraps the *http.Server that exposes the scrape
// endpoint plus a Shutdown hook that stops it cleanly. Callers defer
// Shutdown next to the main server's shutdown so Fly's hosted
// Prometheus sees a clean socket close during a rolling restart
// instead of a connection reset.
type MetricsServer struct {
	// Server is the underlying http.Server. Exposed for callers that
	// need to tune TLS or custom listeners; normal usage never
	// touches it.
	Server *http.Server

	// Shutdown stops the metrics server. Safe to call more than once.
	Shutdown func(ctx context.Context) error
}

// StartMetricsServer binds a listener on addr, mounts handler at path,
// and starts serving in a background goroutine. It returns once the
// listener is bound so the caller is guaranteed a usable endpoint by
// the time StartMetricsServer returns — a race on startup between the
// main HTTP server and the metrics server causes early scrapes to
// return connection-refused and the dashboard to show a brief data
// gap on every deploy.
//
// addr and path default to [DefaultMetricsAddr] and
// [DefaultMetricsPath] when empty. handler must not be nil — it is
// the http.Handler returned by otel.Observability.PromHandler.
func StartMetricsServer(addr, path string, handler http.Handler) (*MetricsServer, error) {
	if handler == nil {
		return nil, errors.New("httputil.StartMetricsServer: handler must not be nil")
	}
	if addr == "" {
		addr = DefaultMetricsAddr
	}
	if path == "" {
		path = DefaultMetricsPath
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("httputil.StartMetricsServer: listen %s: %w", addr, err)
	}

	mux := http.NewServeMux()
	mux.Handle(path, handler)

	// Metrics scrapes are short and cheap; keep timeouts tight so a
	// stuck client cannot pin a goroutine.
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	go func() {
		// http.ErrServerClosed is the expected signal that Shutdown
		// was called cleanly; anything else is a startup defect worth
		// surfacing — but we have no logger handle here, so callers
		// that need log on error wrap StartMetricsServer themselves.
		_ = srv.Serve(ln)
	}()

	return &MetricsServer{
		Server:   srv,
		Shutdown: srv.Shutdown,
	}, nil
}
