// Package container provides a stdlib-only, concurrency-aware dependency
// injection container for jedi-knights services. It is intended for use at
// the composition root (typically main and internal/container/container.go);
// business code should still receive its dependencies via constructor parameters.
//
// # Concurrency model
//
//   - [Register] and [RegisterLazy] are safe to call concurrently with each other
//     but should typically run during a single startup phase before [Container.Bootstrap].
//   - [Resolve] is safe to call from any number of goroutines. Each registered
//     service is constructed exactly once via [sync.Once]; concurrent first-resolves
//     do not run the provider twice.
//   - Providers receive a [context.Context] so I/O-bound construction respects
//     cancellation and deadlines.
//   - [Container.Bootstrap] eagerly resolves every non-lazy registration. It
//     honors the passed [context.Context]. Bootstrap is sequential; cross-service
//     parallelism is a deliberate non-goal for the first cut.
//   - [Container.Ready] returns a channel closed after [Container.Bootstrap]
//     completes successfully; [Container.Done] returns a channel closed after
//     [Container.Close] finishes. Compose them with [select] for graceful startup
//     and shutdown.
//   - Closers registered via [Container.OnClose] run in reverse registration order
//     (LIFO) during [Container.Close]. Errors are joined via [errors.Join].
//   - Cycle detection is goroutine-local: the resolution stack is tracked in the
//     [context.Context] value chain so two goroutines resolving overlapping graphs
//     do not false-positive each other.
//
// # Scope
//
// [Container.Scope] creates a child container that shares the parent's
// singletons but holds its own registrations. The canonical use is per-request
// scoping: a request-scoped logger (with a trace ID baked in) can be registered
// on the scope without leaking to other requests, while shared singletons such
// as a *sql.DB still resolve through the parent.
//
// # Override
//
// [OverrideValue] replaces a registration with a fixed instance. It is intended
// for test seams. Overriding during a live Resolve race is undefined; perform
// overrides before any Resolve, or on a fresh child Scope.
package container
