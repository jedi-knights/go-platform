package container_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jedi-knights/go-platform/container"
)

func TestBootstrap_RunsAllEagerProviders(t *testing.T) {
	t.Parallel()

	var loggerCalls, repoCalls int32
	c := container.New()
	container.Register(c, func(ctx context.Context, _ *container.Container) (*fakeLogger, error) {
		atomic.AddInt32(&loggerCalls, 1)
		return &fakeLogger{name: "eager"}, nil
	})
	container.Register(c, func(ctx context.Context, c *container.Container) (*fakeRepo, error) {
		atomic.AddInt32(&repoCalls, 1)
		log, err := container.Resolve[*fakeLogger](ctx, c)
		if err != nil {
			return nil, err
		}
		return &fakeRepo{log: log}, nil
	})

	if err := c.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if got := atomic.LoadInt32(&loggerCalls); got != 1 {
		t.Fatalf("logger provider ran %d times after Bootstrap, want 1", got)
	}
	if got := atomic.LoadInt32(&repoCalls); got != 1 {
		t.Fatalf("repo provider ran %d times after Bootstrap, want 1", got)
	}
}

func TestBootstrap_SkipsLazyProviders(t *testing.T) {
	t.Parallel()

	var calls int32
	c := container.New()
	container.RegisterLazy(c, func(ctx context.Context, _ *container.Container) (*fakeLogger, error) {
		atomic.AddInt32(&calls, 1)
		return &fakeLogger{name: "lazy"}, nil
	})

	if err := c.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("lazy provider ran during Bootstrap: calls=%d", got)
	}
}

func TestBootstrap_HonorsCancelledContext(t *testing.T) {
	t.Parallel()

	c := container.New()
	container.Register(c, func(ctx context.Context, _ *container.Container) (*fakeLogger, error) {
		// Provider that respects ctx cancellation — simulates slow I/O.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
			return &fakeLogger{}, nil
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := c.Bootstrap(ctx)
	if err == nil {
		t.Fatal("expected error from canceled Bootstrap, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled in chain, got %v", err)
	}
}

func TestBootstrap_ProviderError_Propagates(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("provider exploded")
	c := container.New()
	container.Register(c, func(ctx context.Context, _ *container.Container) (*fakeLogger, error) {
		return nil, wantErr
	})

	err := c.Bootstrap(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("error chain does not wrap want: got %v", err)
	}
}

func TestReady_ChannelClosesAfterBootstrap(t *testing.T) {
	t.Parallel()

	c := container.New()
	container.Register(c, func(ctx context.Context, _ *container.Container) (*fakeLogger, error) {
		return &fakeLogger{name: "ok"}, nil
	})

	select {
	case <-c.Ready():
		t.Fatal("Ready channel closed before Bootstrap")
	default:
	}

	if err := c.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	select {
	case <-c.Ready():
		// expected — channel closes when Bootstrap completes
	case <-time.After(time.Second):
		t.Fatal("Ready channel did not close after Bootstrap")
	}
}

func TestClose_RunsClosersInReverseOrder(t *testing.T) {
	t.Parallel()

	var order []string
	c := container.New()
	c.OnClose("first", func(ctx context.Context) error {
		order = append(order, "first")
		return nil
	})
	c.OnClose("second", func(ctx context.Context) error {
		order = append(order, "second")
		return nil
	})
	c.OnClose("third", func(ctx context.Context) error {
		order = append(order, "third")
		return nil
	})

	if err := c.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	want := []string{"third", "second", "first"}
	if len(order) != len(want) {
		t.Fatalf("order len %d, want %d", len(order), len(want))
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order[%d] = %q, want %q", i, order[i], want[i])
		}
	}
}

func TestClose_JoinsCloserErrors(t *testing.T) {
	t.Parallel()

	errA := errors.New("closer a failed")
	errB := errors.New("closer b failed")

	c := container.New()
	c.OnClose("a", func(ctx context.Context) error { return errA })
	c.OnClose("b", func(ctx context.Context) error { return errB })

	err := c.Close(context.Background())
	if err == nil {
		t.Fatal("expected joined error, got nil")
	}
	if !errors.Is(err, errA) || !errors.Is(err, errB) {
		t.Fatalf("joined error missing parts: %v", err)
	}
}

func TestDone_ChannelClosesAfterClose(t *testing.T) {
	t.Parallel()

	c := container.New()
	select {
	case <-c.Done():
		t.Fatal("Done closed before Close")
	default:
	}

	if err := c.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case <-c.Done():
		// expected
	case <-time.After(time.Second):
		t.Fatal("Done did not close after Close")
	}
}

func TestClose_CancelledContextShortCircuits(t *testing.T) {
	t.Parallel()

	var ran int32
	c := container.New()
	c.OnClose("first", func(ctx context.Context) error {
		atomic.AddInt32(&ran, 1)
		return nil
	})
	c.OnClose("second", func(ctx context.Context) error {
		atomic.AddInt32(&ran, 1)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := c.Close(ctx)
	if err == nil {
		t.Fatal("expected cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if got := atomic.LoadInt32(&ran); got != 0 {
		t.Fatalf("closer ran despite canceled ctx: ran=%d", got)
	}
}

func TestResolve_CycleDetected(t *testing.T) {
	t.Parallel()

	type a struct{}
	type b struct{}

	c := container.New()
	container.RegisterLazy(c, func(ctx context.Context, c *container.Container) (*a, error) {
		_, err := container.Resolve[*b](ctx, c)
		if err != nil {
			return nil, err
		}
		return &a{}, nil
	})
	container.RegisterLazy(c, func(ctx context.Context, c *container.Container) (*b, error) {
		_, err := container.Resolve[*a](ctx, c)
		if err != nil {
			return nil, err
		}
		return &b{}, nil
	})

	_, err := container.Resolve[*a](context.Background(), c)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
}
