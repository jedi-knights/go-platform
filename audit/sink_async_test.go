package audit_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jedi-knights/go-platform/audit"
)

type blockingSink struct {
	mu      sync.Mutex
	release chan struct{}
	count   int
}

func newBlockingSink() *blockingSink {
	return &blockingSink{release: make(chan struct{})}
}

func (b *blockingSink) Sink(ctx context.Context, _ audit.Event) error {
	select {
	case <-b.release:
		b.mu.Lock()
		b.count++
		b.mu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *blockingSink) Count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.count
}

func TestAsyncSink_NonBlockingAndDropsOnOverflow(t *testing.T) {
	inner := newBlockingSink()
	async := audit.NewAsyncSink(inner, 4)
	t.Cleanup(func() {
		close(inner.release)
		_ = async.Close(context.Background())
	})

	e := audit.New(async)

	// Send more events than the buffer can hold. The blocking inner sink
	// holds the worker so the buffer fills, but Emit must return
	// immediately for every call.
	const totalEvents = 50
	deadline := time.Now().Add(2 * time.Second)
	for i := 0; i < totalEvents; i++ {
		start := time.Now()
		if err := e.Emit(context.Background(), audit.Event{
			EventType: "tool_invoked",
			Service:   "jk-mcp-nwsl",
			ActorType: audit.ActorTypeAgent,
			ActorID:   "agent-claude",
			Resource:  "tool:get_standings",
			Action:    "invoke",
		}); err != nil {
			t.Fatalf("emit returned error on call %d: %v", i, err)
		}
		if time.Since(start) > 100*time.Millisecond {
			t.Fatalf("emit blocked for %v on call %d", time.Since(start), i)
		}
		if time.Now().After(deadline) {
			t.Fatal("test exceeded deadline")
		}
	}

	stats := async.Stats()
	if stats.Enqueued+stats.Dropped != totalEvents {
		t.Errorf("expected enqueued + dropped = %d, got enqueued=%d dropped=%d",
			totalEvents, stats.Enqueued, stats.Dropped)
	}
	if stats.Dropped == 0 {
		t.Errorf("expected at least one drop with a blocking sink and tiny buffer")
	}
}

func TestAsyncSink_DrainsOnClose(t *testing.T) {
	inner := &captureSink{}
	async := audit.NewAsyncSink(inner, 16)

	e := audit.New(async)
	const n = 10
	for i := 0; i < n; i++ {
		if err := e.Emit(context.Background(), audit.Event{
			EventType: "tool_invoked",
			Service:   "jk-mcp-nwsl",
			ActorType: audit.ActorTypeAgent,
			ActorID:   "agent-claude",
			Resource:  "tool:get_standings",
			Action:    "invoke",
		}); err != nil {
			t.Fatalf("emit error: %v", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := async.Close(ctx); err != nil {
		t.Fatalf("close error: %v", err)
	}
	if len(inner.events) != n {
		t.Errorf("expected all %d events drained, got %d", n, len(inner.events))
	}
}

func TestAsyncSink_AfterCloseReturnsError(t *testing.T) {
	inner := &captureSink{}
	async := audit.NewAsyncSink(inner, 4)
	if err := async.Close(context.Background()); err != nil {
		t.Fatalf("close error: %v", err)
	}
	err := async.Sink(context.Background(), audit.Event{})
	if !errors.Is(err, audit.ErrAsyncSinkClosed) {
		t.Errorf("expected ErrAsyncSinkClosed, got %v", err)
	}
}

func TestAsyncSink_DoubleClose(t *testing.T) {
	async := audit.NewAsyncSink(&captureSink{}, 4)
	if err := async.Close(context.Background()); err != nil {
		t.Fatalf("first close error: %v", err)
	}
	if err := async.Close(context.Background()); err != nil {
		t.Errorf("second close should be a no-op, got %v", err)
	}
}

func TestNewAsyncSink_NilInnerPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic")
		}
	}()
	_ = audit.NewAsyncSink(nil, 4)
}

func TestNewAsyncSink_ZeroBufferPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic")
		}
	}()
	_ = audit.NewAsyncSink(&captureSink{}, 0)
}
