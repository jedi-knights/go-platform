package container_test

import (
	"context"
	"testing"

	"github.com/jedi-knights/go-platform/container"
)

// requestID is a child-scope-only dependency to exercise scoped registration.
type requestID string

func TestScope_InheritsParentSingleton(t *testing.T) {
	t.Parallel()

	parent := container.New()
	container.Register(parent, func(ctx context.Context, _ *container.Container) (*fakeLogger, error) {
		return &fakeLogger{name: "parent"}, nil
	})

	child := parent.Scope()
	got, err := container.Resolve[*fakeLogger](context.Background(), child)
	if err != nil {
		t.Fatalf("child Resolve fell through but failed: %v", err)
	}
	if got == nil || got.name != "parent" {
		t.Fatalf("child did not inherit parent singleton: %+v", got)
	}
}

func TestScope_ChildSingletonIsSharedAcrossParentAndChildResolves(t *testing.T) {
	t.Parallel()

	parent := container.New()
	container.Register(parent, func(ctx context.Context, _ *container.Container) (*fakeLogger, error) {
		return &fakeLogger{name: "shared"}, nil
	})

	child := parent.Scope()
	fromParent, err := container.Resolve[*fakeLogger](context.Background(), parent)
	if err != nil {
		t.Fatalf("parent Resolve: %v", err)
	}
	fromChild, err := container.Resolve[*fakeLogger](context.Background(), child)
	if err != nil {
		t.Fatalf("child Resolve: %v", err)
	}
	if fromParent != fromChild {
		t.Fatalf("parent and child saw different instances for inherited singleton: %p vs %p", fromParent, fromChild)
	}
}

func TestScope_ChildRegistrationDoesNotLeakToParent(t *testing.T) {
	t.Parallel()

	parent := container.New()
	child := parent.Scope()
	container.Register(child, func(ctx context.Context, _ *container.Container) (requestID, error) {
		return requestID("req-123"), nil
	})

	got, err := container.Resolve[requestID](context.Background(), child)
	if err != nil {
		t.Fatalf("child Resolve: %v", err)
	}
	if got != requestID("req-123") {
		t.Fatalf("child got %q, want req-123", got)
	}

	if _, err := container.Resolve[requestID](context.Background(), parent); err == nil {
		t.Fatal("parent resolved child-only registration; child scope leaked")
	}
}

func TestScope_OverrideOnChildDoesNotAffectParent(t *testing.T) {
	t.Parallel()

	parent := container.New()
	container.Register(parent, func(ctx context.Context, _ *container.Container) (*fakeLogger, error) {
		return &fakeLogger{name: "parent"}, nil
	})

	child := parent.Scope()
	container.OverrideValue[*fakeLogger](child, &fakeLogger{name: "child-override"})

	childGot, err := container.Resolve[*fakeLogger](context.Background(), child)
	if err != nil {
		t.Fatalf("child Resolve: %v", err)
	}
	if childGot.name != "child-override" {
		t.Fatalf("child saw %q, want child-override", childGot.name)
	}

	parentGot, err := container.Resolve[*fakeLogger](context.Background(), parent)
	if err != nil {
		t.Fatalf("parent Resolve: %v", err)
	}
	if parentGot.name != "parent" {
		t.Fatalf("parent saw %q, want parent (child override leaked)", parentGot.name)
	}
}
