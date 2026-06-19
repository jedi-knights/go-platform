package container

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
)

// Provider constructs a value of T. The container is passed in so the provider
// can resolve its own dependencies via [Resolve]. The provider must respect
// the supplied [context.Context].
type Provider[T any] func(ctx context.Context, c *Container) (T, error)

// entry is the type-erased interface that backs each registration.
// Concrete entries are *typedEntry[T]; the indirection through any is the
// price of having generic free functions over a non-generic map.
type entry interface {
	resolve(ctx context.Context, c *Container) (any, error)
	eager() bool
}

type typedEntry[T any] struct {
	once     sync.Once
	provider Provider[T]
	value    T
	err      error
	isEager  bool
}

func (e *typedEntry[T]) resolve(ctx context.Context, c *Container) (any, error) {
	e.once.Do(func() {
		e.value, e.err = e.provider(ctx, c)
	})
	return e.value, e.err
}

func (e *typedEntry[T]) eager() bool { return e.isEager }

type closerEntry struct {
	name string
	fn   func(ctx context.Context) error
}

// Container holds a registry of typed providers and a set of shutdown hooks.
// The zero value is not usable; construct one with [New].
type Container struct {
	parent *Container

	mu       sync.RWMutex
	services map[reflect.Type]entry
	closers  []closerEntry

	readyOnce sync.Once
	ready     chan struct{}
	doneOnce  sync.Once
	done      chan struct{}
}

// New returns a new root container.
func New() *Container {
	return &Container{
		services: make(map[reflect.Type]entry),
		ready:    make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Scope returns a child container that inherits the parent's registrations
// but holds its own. Resolves walk from the child up to the root; the first
// container in the chain that has a registration for T provides it.
func (c *Container) Scope() *Container {
	return &Container{
		parent:   c,
		services: make(map[reflect.Type]entry),
		ready:    make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Ready returns a channel that is closed when [Container.Bootstrap] completes
// successfully. Callers can compose it with [select] to await startup.
func (c *Container) Ready() <-chan struct{} { return c.ready }

// Done returns a channel that is closed when [Container.Close] finishes.
// Callers can compose it with [select] to await shutdown.
func (c *Container) Done() <-chan struct{} { return c.done }

// Register adds an eager singleton provider. The provider runs during
// [Container.Bootstrap]. It is a programming error to register the same type
// twice on the same container; doing so panics.
func Register[T any](c *Container, provider Provider[T]) {
	register(c, provider, true)
}

// RegisterLazy adds a lazy singleton provider. The provider does not run
// until the first [Resolve] for T.
func RegisterLazy[T any](c *Container, provider Provider[T]) {
	register(c, provider, false)
}

func register[T any](c *Container, provider Provider[T], eager bool) {
	if provider == nil {
		panic("container: nil provider")
	}
	key := reflect.TypeFor[T]()
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.services[key]; exists {
		panic(fmt.Sprintf("container: %s already registered", key))
	}
	c.services[key] = &typedEntry[T]{provider: provider, isEager: eager}
}

// OverrideValue replaces (or creates) the registration for T with a fixed
// instance. Intended for test seams. Replacing a registration resets the
// singleton cache for T on this container.
func OverrideValue[T any](c *Container, value T) {
	key := reflect.TypeFor[T]()
	e := &typedEntry[T]{
		provider: func(context.Context, *Container) (T, error) { return value, nil },
	}
	c.mu.Lock()
	c.services[key] = e
	c.mu.Unlock()
}

// Resolve fetches a value of type T. The walk starts at c and falls through
// to parent containers (see [Container.Scope]) until a registration is found.
// The provider runs at most once per registration; subsequent calls return
// the cached value.
//
// Cycle detection: the in-progress resolution stack is tracked through the
// returned context. If the same T is being resolved further up the stack,
// Resolve returns an error rather than recursing.
func Resolve[T any](ctx context.Context, c *Container) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	key := reflect.TypeFor[T]()

	ctx, err := pushBuilding(ctx, key)
	if err != nil {
		return zero, err
	}

	e := findEntry(c, key)
	if e == nil {
		return zero, fmt.Errorf("container: %s not registered", key)
	}
	te, ok := e.(*typedEntry[T])
	if !ok {
		// This is structurally impossible if Register is the only writer:
		// the map key is the reflect.Type for T, so the value must be *typedEntry[T].
		return zero, fmt.Errorf("container: internal type mismatch for %s", key)
	}
	v, perr := te.resolve(ctx, c)
	if perr != nil {
		return zero, fmt.Errorf("container: resolving %s: %w", key, perr)
	}
	// A provider may legitimately return (nil, nil) for an interface type T
	// to signal "this optional dependency is not configured". The any
	// returned from resolve is then a nil any (no dynamic type, no value),
	// and v.(T) on a nil any reports !ok regardless of T. Short-circuit so
	// nil interface registrations resolve to the zero value of T without a
	// spurious type-assertion error.
	if v == nil {
		return zero, nil
	}
	out, ok := v.(T)
	if !ok {
		return zero, fmt.Errorf("container: stored value for %s is not %s", key, key)
	}
	return out, nil
}

// MustResolve is the panic-on-error variant of [Resolve]. Use at the
// composition root only.
func MustResolve[T any](ctx context.Context, c *Container) T {
	v, err := Resolve[T](ctx, c)
	if err != nil {
		panic(err)
	}
	return v
}

// findEntry walks from c up through parents looking for a registration for key.
// Returns nil if not found anywhere.
func findEntry(c *Container, key reflect.Type) entry {
	for cur := c; cur != nil; cur = cur.parent {
		cur.mu.RLock()
		e, ok := cur.services[key]
		cur.mu.RUnlock()
		if ok {
			return e
		}
	}
	return nil
}

// Bootstrap eagerly resolves every non-lazy registration on this container.
// It does NOT descend into parents; bootstrap your root container, then any
// child scopes that need pre-warming. Returns the first provider error, or
// the context error if ctx is canceled mid-bootstrap.
func (c *Container) Bootstrap(ctx context.Context) error {
	c.mu.RLock()
	eagerEntries := make([]entry, 0, len(c.services))
	eagerKeys := make([]reflect.Type, 0, len(c.services))
	for k, e := range c.services {
		if e.eager() {
			eagerKeys = append(eagerKeys, k)
			eagerEntries = append(eagerEntries, e)
		}
	}
	c.mu.RUnlock()

	for i, e := range eagerEntries {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Push the key onto the building stack for cycle detection while the
		// provider runs. Bootstrap calls Resolve transitively via the provider's
		// dependencies, and each Resolve pushes its own key.
		ctxBuild, err := pushBuilding(ctx, eagerKeys[i])
		if err != nil {
			return err
		}
		if _, err := e.resolve(ctxBuild, c); err != nil {
			return fmt.Errorf("container: bootstrap %s: %w", eagerKeys[i], err)
		}
	}
	c.readyOnce.Do(func() { close(c.ready) })
	return nil
}

// OnClose registers a shutdown hook. Hooks run in reverse registration order
// (LIFO) during [Container.Close]. name appears in error messages and is for
// human consumption only.
func (c *Container) OnClose(name string, fn func(ctx context.Context) error) {
	if fn == nil {
		panic("container: nil close func")
	}
	c.mu.Lock()
	c.closers = append(c.closers, closerEntry{name: name, fn: fn})
	c.mu.Unlock()
}

// Close runs every registered closer in reverse registration order. If ctx
// is canceled before a closer runs, the remaining closers are skipped and
// the cancellation error is included in the joined return value.
//
// Errors from individual closers are joined via [errors.Join]; the caller can
// inspect them with [errors.Is]/[errors.As].
//
// Close is safe to call exactly once; subsequent calls are no-ops on the
// closer list but [Container.Done] remains closed.
func (c *Container) Close(ctx context.Context) error {
	c.mu.Lock()
	closers := c.closers
	c.closers = nil
	c.mu.Unlock()

	var errs []error
	for i := len(closers) - 1; i >= 0; i-- {
		if err := ctx.Err(); err != nil {
			errs = append(errs, fmt.Errorf("container: close canceled before %s: %w", closers[i].name, err))
			break
		}
		if err := closers[i].fn(ctx); err != nil {
			errs = append(errs, fmt.Errorf("container: close %s: %w", closers[i].name, err))
		}
	}
	c.doneOnce.Do(func() { close(c.done) })
	return errors.Join(errs...)
}

// --- cycle detection -------------------------------------------------------

// buildingKey is the context key for the in-progress resolution stack.
// Using an unexported, zero-sized type avoids collisions with other ctx values.
type buildingKey struct{}

// pushBuilding adds key to the resolution stack carried by ctx. Returns an
// error (and the original ctx) when key already appears in the stack, which
// indicates a registration cycle.
func pushBuilding(ctx context.Context, key reflect.Type) (context.Context, error) {
	prev, _ := ctx.Value(buildingKey{}).([]reflect.Type)
	for _, p := range prev {
		if p == key {
			return ctx, fmt.Errorf("container: cycle detected: %s", formatCycle(append(prev, key)))
		}
	}
	// Copy-on-push so concurrent sibling resolves don't observe each other's
	// stacks. Slices appended in place can alias on shared backing arrays;
	// allocating a fresh slice avoids that.
	next := make([]reflect.Type, len(prev), len(prev)+1)
	copy(next, prev)
	next = append(next, key)
	return context.WithValue(ctx, buildingKey{}, next), nil
}

func formatCycle(path []reflect.Type) string {
	parts := make([]string, 0, len(path))
	for _, p := range path {
		parts = append(parts, p.String())
	}
	return strings.Join(parts, " -> ")
}
