package audit

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

// AsyncSink wraps a downstream [Sink] with a fixed-size buffer and a worker
// goroutine. Sink enqueues are non-blocking: when the buffer is full the
// event is dropped and the dropped-events counter is incremented. This is
// the recommended way to obtain ADR-0018's "non-blocking emission with
// overflow drop" property for the stderr and OTel sinks.
//
// AsyncSink is intentionally not the right wrapper for the durable
// (at-least-once) sink in ADR-0019 — that sink must apply backpressure to
// the request path, not drop. Use it only with best-effort sinks.
type AsyncSink struct {
	inner   Sink
	queue   chan Event
	stop    chan struct{}
	closed  chan struct{}
	stopped atomic.Bool

	dropped  atomic.Uint64
	enqueued atomic.Uint64

	closeOnce sync.Once
}

// Compile-time assertion.
var _ Sink = (*AsyncSink)(nil)

// ErrAsyncSinkClosed is returned by [AsyncSink.Sink] after [AsyncSink.Close]
// has been called. Wrap an AsyncSink only at composition root and close
// during shutdown; producers should never see this error in steady state.
var ErrAsyncSinkClosed = errors.New("audit: async sink closed")

// NewAsyncSink wraps inner with a non-blocking buffer of bufferSize events.
// The worker goroutine starts immediately and drains the buffer until
// [AsyncSink.Close] is called. A nil inner sink or a non-positive
// bufferSize panics — composition errors are loud.
func NewAsyncSink(inner Sink, bufferSize int) *AsyncSink {
	if inner == nil {
		panic("audit: NewAsyncSink called with nil inner Sink")
	}
	if bufferSize <= 0 {
		panic("audit: NewAsyncSink called with non-positive bufferSize")
	}
	s := &AsyncSink{
		inner:  inner,
		queue:  make(chan Event, bufferSize),
		stop:   make(chan struct{}),
		closed: make(chan struct{}),
	}
	go s.run()
	return s
}

// Sink enqueues the event. Returns [ErrAsyncSinkClosed] if the sink has been
// closed; otherwise returns nil. Events that don't fit in the buffer are
// dropped and the dropped counter is incremented; the call still returns nil
// because dropping is the documented behavior.
func (s *AsyncSink) Sink(_ context.Context, event Event) error {
	if s.stopped.Load() {
		return ErrAsyncSinkClosed
	}
	select {
	case s.queue <- event:
		s.enqueued.Add(1)
		return nil
	default:
		s.dropped.Add(1)
		return nil
	}
}

// Stats is a snapshot of the sink's counters. Useful for exporting as
// process metrics.
type Stats struct {
	// Enqueued is the total number of events successfully enqueued for
	// downstream emission since the sink was created.
	Enqueued uint64
	// Dropped is the total number of events dropped because the buffer was
	// full at enqueue time.
	Dropped uint64
}

// Stats returns the current counters.
func (s *AsyncSink) Stats() Stats {
	return Stats{
		Enqueued: s.enqueued.Load(),
		Dropped:  s.dropped.Load(),
	}
}

// Close stops accepting new events, drains any buffered events through the
// inner sink, and waits for the worker to finish. Subsequent Sink calls
// return [ErrAsyncSinkClosed]. Close is safe to call multiple times.
func (s *AsyncSink) Close(ctx context.Context) error {
	s.closeOnce.Do(func() {
		s.stopped.Store(true)
		close(s.stop)
	})
	select {
	case <-s.closed:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// run drains the queue until [AsyncSink.Close] is called, then exits and
// signals via the closed channel. Sink errors are intentionally swallowed:
// async sinks wrap best-effort sinks where there is no reasonable caller to
// return the error to. Operators see drops via [AsyncSink.Stats].
func (s *AsyncSink) run() {
	defer close(s.closed)
	for {
		select {
		case event := <-s.queue:
			_ = s.inner.Sink(context.Background(), event)
		case <-s.stop:
			// Drain remaining buffered events before exiting.
			for {
				select {
				case event := <-s.queue:
					_ = s.inner.Sink(context.Background(), event)
				default:
					return
				}
			}
		}
	}
}
