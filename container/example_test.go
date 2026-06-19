package container_test

import (
	"context"
	"fmt"

	"github.com/jedi-knights/go-platform/container"
)

// A logger stand-in for the example; in real services this would be
// *slog.Logger from github.com/jedi-knights/go-logging.
type logger struct{ name string }

func (l *logger) Info(msg string) { fmt.Printf("[%s] %s\n", l.name, msg) }

type userRepo struct{ log *logger }

func newUserRepo(log *logger) *userRepo { return &userRepo{log: log} }

func (r *userRepo) Find(id string) string {
	r.log.Info("finding user " + id)
	return "user-" + id
}

// Example demonstrates the canonical wiring pattern: register a logger
// once at startup, then have downstream services resolve it from the
// container so every layer shares the same configured instance.
func Example() {
	ctx := context.Background()
	c := container.New()

	container.Register(c, func(ctx context.Context, _ *container.Container) (*logger, error) {
		return &logger{name: "platform"}, nil
	})

	container.Register(c, func(ctx context.Context, c *container.Container) (*userRepo, error) {
		log, err := container.Resolve[*logger](ctx, c)
		if err != nil {
			return nil, err
		}
		return newUserRepo(log), nil
	})

	if err := c.Bootstrap(ctx); err != nil {
		panic(err)
	}

	repo := container.MustResolve[*userRepo](ctx, c)
	fmt.Println(repo.Find("42"))

	// Output:
	// [platform] finding user 42
	// user-42
}

// ExampleContainer_Scope shows request-scoped resolution: a per-request
// logger (with a trace ID baked in) is registered on a child scope while
// shared singletons still resolve through the parent.
func ExampleContainer_Scope() {
	ctx := context.Background()
	root := container.New()
	container.Register(root, func(ctx context.Context, _ *container.Container) (*logger, error) {
		return &logger{name: "root"}, nil
	})
	if err := root.Bootstrap(ctx); err != nil {
		panic(err)
	}

	scope := root.Scope()
	container.OverrideValue[*logger](scope, &logger{name: "request-trace-abc"})

	container.MustResolve[*logger](ctx, scope).Info("handling request")
	container.MustResolve[*logger](ctx, root).Info("background job")

	// Output:
	// [request-trace-abc] handling request
	// [root] background job
}
