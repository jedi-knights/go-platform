package httpserver

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"
)

// Default server timeouts, identical to the values the services previously
// copy-pasted into each main.go.
const (
	defaultReadHeaderTimeout = 5 * time.Second
	defaultReadTimeout       = 15 * time.Second
	defaultWriteTimeout      = 15 * time.Second
	defaultIdleTimeout       = 60 * time.Second
	defaultShutdownTimeout   = 30 * time.Second
)

// Server wraps an [http.Server] with the platform's standard timeouts and
// graceful-shutdown lifecycle.
type Server struct {
	srv             *http.Server
	shutdownTimeout time.Duration
}

// Option configures [New].
type Option func(*Server)

// WithReadHeaderTimeout overrides the header read timeout (default 5s).
// Zero disables it; negative values panic.
func WithReadHeaderTimeout(d time.Duration) Option {
	return func(s *Server) { s.srv.ReadHeaderTimeout = nonNegative("ReadHeaderTimeout", d) }
}

// WithReadTimeout overrides the full request read timeout (default 15s).
func WithReadTimeout(d time.Duration) Option {
	return func(s *Server) { s.srv.ReadTimeout = nonNegative("ReadTimeout", d) }
}

// WithWriteTimeout overrides the response write timeout (default 15s). Raise
// or zero it for streaming endpoints.
func WithWriteTimeout(d time.Duration) Option {
	return func(s *Server) { s.srv.WriteTimeout = nonNegative("WriteTimeout", d) }
}

// WithIdleTimeout overrides the keep-alive idle timeout (default 60s).
func WithIdleTimeout(d time.Duration) Option {
	return func(s *Server) { s.srv.IdleTimeout = nonNegative("IdleTimeout", d) }
}

// WithShutdownTimeout overrides how long graceful shutdown waits for in-flight
// requests (default 30s). It must be positive.
func WithShutdownTimeout(d time.Duration) Option {
	return func(s *Server) {
		if d <= 0 {
			panic(fmt.Sprintf("httpserver: shutdown timeout must be positive, got %s", d))
		}
		s.shutdownTimeout = d
	}
}

func nonNegative(name string, d time.Duration) time.Duration {
	if d < 0 {
		panic(fmt.Sprintf("httpserver: %s must not be negative, got %s", name, d))
	}
	return d
}

// New builds a Server for addr serving handler. It panics when handler
// is nil, since a server with no handler would serve 404s forever.
func New(addr string, handler http.Handler, opts ...Option) *Server {
	if handler == nil {
		panic("httpserver: New requires a non-nil handler")
	}
	s := &Server{
		srv: &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: defaultReadHeaderTimeout,
			ReadTimeout:       defaultReadTimeout,
			WriteTimeout:      defaultWriteTimeout,
			IdleTimeout:       defaultIdleTimeout,
		},
		shutdownTimeout: defaultShutdownTimeout,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// HTTPServer returns the underlying [http.Server] for configuration this
// package does not expose (TLS, BaseContext, ErrorLog). Mutate it only before
// calling [Server.Run] or [Server.Serve].
func (s *Server) HTTPServer() *http.Server { return s.srv }

// Run listens on the configured address and serves until ctx is canceled or
// the process receives SIGINT or SIGTERM, then shuts down gracefully. See
// [Server.Serve] for the return contract.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.srv.Addr)
	if err != nil {
		return fmt.Errorf("httpserver: listen %s: %w", s.srv.Addr, err)
	}
	return s.Serve(ctx, ln)
}

// Serve serves on ln until ctx is canceled or SIGINT/SIGTERM arrives, then
// calls [http.Server.Shutdown] with the shutdown timeout. It returns nil after
// a clean shutdown, the serve error if the server failed, or the shutdown
// error (for example context.DeadlineExceeded) if in-flight requests did not
// finish in time.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	sigCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() { serveErr <- s.srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		// Serve returns only on failure here: ErrServerClosed requires
		// Shutdown, which has not been called yet.
		return fmt.Errorf("httpserver: serve: %w", err)
	case <-sigCtx.Done():
	}

	// WithoutCancel: the shutdown budget must not be cut short by the very
	// cancellation that triggered shutdown.
	shutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.shutdownTimeout)
	defer cancel()
	if err := s.srv.Shutdown(shutCtx); err != nil {
		_ = s.srv.Close() // best-effort: release connections after a failed graceful drain
		return fmt.Errorf("httpserver: shutdown: %w", err)
	}
	<-serveErr // always http.ErrServerClosed after a successful Shutdown; waiting ensures Serve has fully returned
	return nil
}
