package httpserver_test

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/jedi-knights/go-platform/httpserver"
)

func TestNew_DefaultTimeouts(t *testing.T) {
	t.Parallel()
	hs := httpserver.New(":0", ok).HTTPServer()
	if hs.ReadHeaderTimeout != 5*time.Second || hs.ReadTimeout != 15*time.Second ||
		hs.WriteTimeout != 15*time.Second || hs.IdleTimeout != 60*time.Second {
		t.Errorf("timeouts = header %s read %s write %s idle %s",
			hs.ReadHeaderTimeout, hs.ReadTimeout, hs.WriteTimeout, hs.IdleTimeout)
	}
}

func TestNew_Options(t *testing.T) {
	t.Parallel()
	hs := httpserver.New(":0", ok,
		httpserver.WithReadHeaderTimeout(1*time.Second),
		httpserver.WithReadTimeout(2*time.Second),
		httpserver.WithWriteTimeout(0),
		httpserver.WithIdleTimeout(4*time.Second),
	).HTTPServer()
	if hs.ReadHeaderTimeout != time.Second || hs.ReadTimeout != 2*time.Second ||
		hs.WriteTimeout != 0 || hs.IdleTimeout != 4*time.Second {
		t.Errorf("options not applied: %+v", hs)
	}
}

func TestNew_InvalidArgumentsPanic(t *testing.T) {
	t.Parallel()
	mustPanic(t, "nil handler", func() { httpserver.New(":0", nil) })
	mustPanic(t, "negative read timeout", func() { httpserver.New(":0", ok, httpserver.WithReadTimeout(-1)) })
	mustPanic(t, "zero shutdown timeout", func() { httpserver.New(":0", ok, httpserver.WithShutdownTimeout(0)) })
}

func TestServer_ServesThenShutsDownCleanly(t *testing.T) {
	t.Parallel()
	ln := listen(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- httpserver.New("", ok).Serve(ctx, ln) }()

	resp, err := http.Get("http://" + ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve = %v, want nil after clean shutdown", err)
		}
	case <-time.After(testWait):
		t.Fatal("Serve did not return after cancel")
	}
}

func TestServer_ShutdownWaitsForInFlightRequest(t *testing.T) {
	t.Parallel()
	ln := listen(t)
	started, release := make(chan struct{}), make(chan struct{})
	slow := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusAccepted)
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- httpserver.New("", slow).Serve(ctx, ln) }()

	respc := make(chan int, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String())
		if err != nil {
			respc <- -1
			return
		}
		_ = resp.Body.Close()
		respc <- resp.StatusCode
	}()
	<-started
	cancel()

	select {
	case err := <-done:
		t.Fatalf("Serve returned (%v) while a request was still in flight", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	if code := <-respc; code != http.StatusAccepted {
		t.Errorf("in-flight response = %d, want 202", code)
	}
	if err := <-done; err != nil {
		t.Errorf("Serve = %v, want nil", err)
	}
}

func TestServer_ShutdownTimeoutReturnsDeadlineExceeded(t *testing.T) {
	t.Parallel()
	ln := listen(t)
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	stuck := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(started)
		<-release
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- httpserver.New("", stuck, httpserver.WithShutdownTimeout(50*time.Millisecond)).Serve(ctx, ln)
	}()
	go func() { _, _ = http.Get("http://" + ln.Addr().String()) }() //nolint:bodyclose // test: request is aborted when the server force-closes
	<-started
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Serve = %v, want context.DeadlineExceeded", err)
		}
	case <-time.After(testWait):
		t.Fatal("Serve did not return after shutdown timeout")
	}
}

func TestServer_ServeFailureIsReturned(t *testing.T) {
	t.Parallel()
	ln := listen(t)
	_ = ln.Close() // serving on a closed listener must fail, not hang
	err := httpserver.New("", ok).Serve(context.Background(), ln)
	if err == nil {
		t.Fatal("Serve on a closed listener returned nil")
	}
}

func TestServer_RunReportsListenError(t *testing.T) {
	t.Parallel()
	busy := listen(t)
	defer func() { _ = busy.Close() }() // test cleanup; close error is not actionable
	err := httpserver.New(busy.Addr().String(), ok).Run(context.Background())
	if err == nil {
		t.Fatal("Run on an in-use address returned nil")
	}
}

func TestServer_RunServesOnAddressAndStopsOnCancel(t *testing.T) {
	t.Parallel()
	probe := listen(t)
	addr := probe.Addr().String()
	_ = probe.Close() // free the port so Run can bind it; a rebind race would fail the test loudly

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- httpserver.New(addr, ok).Run(ctx) }()

	var resp *http.Response
	var err error
	for deadline := time.Now().Add(testWait); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if resp, err = http.Get("http://" + addr); err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("server never became reachable on %s: %v", addr, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run = %v, want nil after cancel", err)
	}
}

// A client that connects but never sends headers must be dropped once the
// header timeout elapses (slowloris protection).
func TestServer_ReadHeaderTimeoutDropsSilentClients(t *testing.T) {
	t.Parallel()
	ln := listen(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = httpserver.New("", ok, httpserver.WithReadHeaderTimeout(100*time.Millisecond)).Serve(ctx, ln)
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }() // test cleanup; close error is not actionable
	_ = conn.SetReadDeadline(time.Now().Add(testWait))
	if _, err := bufio.NewReader(conn).ReadByte(); err == nil {
		t.Fatal("expected the server to close or reject the silent connection")
	} else if ne, isNet := err.(net.Error); isNet && ne.Timeout() { //nolint:errorlint // test: distinguishing our own read deadline from a server-side close
		t.Fatal("server kept a header-less connection open past ReadHeaderTimeout")
	}
}

func TestServer_WriteTimeoutAbortsSlowHandlers(t *testing.T) {
	t.Parallel()
	ln := listen(t)
	slow := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte("too late"))
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = httpserver.New("", slow, httpserver.WithWriteTimeout(50*time.Millisecond)).Serve(ctx, ln) }()

	resp, err := http.Get("http://" + ln.Addr().String())
	if err == nil {
		_ = resp.Body.Close()
		t.Fatalf("expected the connection to be dropped, got status %d", resp.StatusCode)
	}
}
