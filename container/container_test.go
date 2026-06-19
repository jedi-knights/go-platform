package container_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jedi-knights/go-platform/container"
)

// fakeLogger is a stand-in for a logger to exercise pointer-typed singletons.
type fakeLogger struct{ name string }

// optionalFetcher is a stand-in for a service's "optional dependency"
// interface — e.g. a permissions fetcher that resolves to nil when its
// upstream URL is unconfigured.
type optionalFetcher interface {
	Fetch() string
}

// fakeRepo depends on a logger; exercises the dep-graph case.
type fakeRepo struct{ log *fakeLogger }

func TestResolve_ReturnsRegisteredSingleton(t *testing.T) {
	t.Parallel()

	c := container.New()
	container.Register(c, func(ctx context.Context, _ *container.Container) (*fakeLogger, error) {
		return &fakeLogger{name: "root"}, nil
	})

	ctx := context.Background()
	got, err := container.Resolve[*fakeLogger](ctx, c)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got == nil || got.name != "root" {
		t.Fatalf("got %+v, want fakeLogger{name:\"root\"}", got)
	}
}

func TestResolve_SingletonReturnsSameInstance(t *testing.T) {
	t.Parallel()

	c := container.New()
	container.Register(c, func(ctx context.Context, _ *container.Container) (*fakeLogger, error) {
		return &fakeLogger{name: "once"}, nil
	})

	ctx := context.Background()
	first, err := container.Resolve[*fakeLogger](ctx, c)
	if err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	second, err := container.Resolve[*fakeLogger](ctx, c)
	if err != nil {
		t.Fatalf("second Resolve: %v", err)
	}
	if first != second {
		t.Fatalf("singleton broken: %p != %p", first, second)
	}
}

func TestResolve_UnregisteredTypeReturnsError(t *testing.T) {
	t.Parallel()

	c := container.New()
	ctx := context.Background()
	_, err := container.Resolve[*fakeLogger](ctx, c)
	if err == nil {
		t.Fatal("expected error for unregistered type, got nil")
	}
}

func TestRegister_DoubleRegisterPanics(t *testing.T) {
	t.Parallel()

	c := container.New()
	container.Register(c, func(ctx context.Context, _ *container.Container) (*fakeLogger, error) {
		return &fakeLogger{name: "first"}, nil
	})

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on double Register, got none")
		}
	}()
	container.Register(c, func(ctx context.Context, _ *container.Container) (*fakeLogger, error) {
		return &fakeLogger{name: "second"}, nil
	})
}

func TestRegister_NilProviderPanics(t *testing.T) {
	t.Parallel()

	c := container.New()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil provider, got none")
		}
	}()
	container.Register[*fakeLogger](c, nil)
}

func TestResolve_LazyProviderRunsOnFirstResolve(t *testing.T) {
	t.Parallel()

	var calls int32
	c := container.New()
	container.RegisterLazy(c, func(ctx context.Context, _ *container.Container) (*fakeLogger, error) {
		atomic.AddInt32(&calls, 1)
		return &fakeLogger{name: "lazy"}, nil
	})

	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("lazy provider ran before Resolve: calls=%d", got)
	}

	ctx := context.Background()
	if _, err := container.Resolve[*fakeLogger](ctx, c); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, err := container.Resolve[*fakeLogger](ctx, c); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("lazy provider ran %d times, want 1", got)
	}
}

func TestResolve_ProviderError_Propagates(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("boom")
	c := container.New()
	container.Register(c, func(ctx context.Context, _ *container.Container) (*fakeLogger, error) {
		return nil, wantErr
	})

	ctx := context.Background()
	_, err := container.Resolve[*fakeLogger](ctx, c)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("error chain does not wrap want: got %v", err)
	}
}

func TestResolve_DependentService(t *testing.T) {
	t.Parallel()

	c := container.New()
	container.Register(c, func(ctx context.Context, _ *container.Container) (*fakeLogger, error) {
		return &fakeLogger{name: "shared"}, nil
	})
	container.Register(c, func(ctx context.Context, c *container.Container) (*fakeRepo, error) {
		log, err := container.Resolve[*fakeLogger](ctx, c)
		if err != nil {
			return nil, err
		}
		return &fakeRepo{log: log}, nil
	})

	ctx := context.Background()
	repo, err := container.Resolve[*fakeRepo](ctx, c)
	if err != nil {
		t.Fatalf("Resolve repo: %v", err)
	}
	if repo.log == nil || repo.log.name != "shared" {
		t.Fatalf("repo did not receive shared logger: %+v", repo)
	}
}

func TestMustResolve_PanicsOnUnregistered(t *testing.T) {
	t.Parallel()

	c := container.New()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic, got none")
		}
	}()
	container.MustResolve[*fakeLogger](context.Background(), c)
}

func TestOverrideValue_ReplacesRegistration(t *testing.T) {
	t.Parallel()

	c := container.New()
	container.Register(c, func(ctx context.Context, _ *container.Container) (*fakeLogger, error) {
		return &fakeLogger{name: "real"}, nil
	})
	stub := &fakeLogger{name: "stub"}
	container.OverrideValue[*fakeLogger](c, stub)

	got, err := container.Resolve[*fakeLogger](context.Background(), c)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != stub {
		t.Fatalf("override ignored: got %+v, want %+v", got, stub)
	}
}

func TestResolve_ConcurrentCallsRunProviderExactlyOnce(t *testing.T) {
	t.Parallel()

	const goroutines = 64
	var calls int32

	c := container.New()
	container.Register(c, func(ctx context.Context, _ *container.Container) (*fakeLogger, error) {
		atomic.AddInt32(&calls, 1)
		return &fakeLogger{name: "shared"}, nil
	})

	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan *fakeLogger, goroutines)
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			got, err := container.Resolve[*fakeLogger](context.Background(), c)
			if err != nil {
				t.Errorf("Resolve: %v", err)
				return
			}
			results <- got
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("provider ran %d times under contention, want 1", got)
	}

	var first *fakeLogger
	for r := range results {
		if first == nil {
			first = r
			continue
		}
		if r != first {
			t.Fatalf("singleton broken under contention: %p != %p", r, first)
		}
	}
}

// TestResolve_NilInterfaceRegistration covers the legitimate "not
// configured" pattern: a provider returns (nil, nil) for an interface
// type to indicate that the optional dependency is absent. Resolve must
// return a nil interface — not panic on a failed type assertion against
// the empty any.
func TestResolve_NilInterfaceRegistration(t *testing.T) {
	t.Parallel()

	c := container.New()
	container.Register(c, func(context.Context, *container.Container) (optionalFetcher, error) {
		return nil, nil
	})

	ctx := context.Background()
	if err := c.Bootstrap(ctx); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	got, err := container.Resolve[optionalFetcher](ctx, c)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil interface, got %#v", got)
	}
}

// TestMustResolve_NilInterfaceRegistration mirrors the above via
// MustResolve to confirm the panic-on-error variant also tolerates nil
// interface registrations.
func TestMustResolve_NilInterfaceRegistration(t *testing.T) {
	t.Parallel()

	c := container.New()
	container.Register(c, func(context.Context, *container.Container) (optionalFetcher, error) {
		return nil, nil
	})

	ctx := context.Background()
	if err := c.Bootstrap(ctx); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("MustResolve panicked on nil interface registration: %v", r)
		}
	}()
	got := container.MustResolve[optionalFetcher](ctx, c)
	if got != nil {
		t.Fatalf("expected nil interface, got %#v", got)
	}
}
