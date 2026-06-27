package audit

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Emitter is what services depend on at the composition root. It accepts a
// validated [Event] and hands it to one or more sinks. Implementations must
// be safe for concurrent use.
//
// Emit defaults [Event.SchemaVersion] to [SchemaVersion], [Event.EventID] to
// a fresh ULID, and [Event.Timestamp] to time.Now() when any of those fields
// are zero on entry. Callers that need deterministic IDs (tests, replay
// pipelines) must populate the fields before calling Emit.
type Emitter interface {
	// Emit validates the event, fills defaults, and dispatches it. It returns
	// an error wrapping [ErrInvalidEvent] when validation fails; sink errors
	// are returned as-is and may be joined when there are multiple sinks.
	Emit(ctx context.Context, event Event) error
}

// Sink is the leaf interface implemented by each transport. It has the same
// shape as [Emitter] but the contract differs: a Sink receives an event whose
// envelope has already been validated and defaulted by the parent emitter.
// Sinks are responsible for transporting bytes; they do not validate fields.
type Sink interface {
	// Sink writes the event to a transport. Implementations must be safe
	// for concurrent use.
	Sink(ctx context.Context, event Event) error
}

// emitter is the default Emitter implementation. It validates, defaults,
// then fans out to the wrapped Sink.
type emitter struct {
	sink Sink
}

// New returns an Emitter that writes every event to sink. Pass a
// [MultiSink] when more than one sink is required; pass an [AsyncSink]
// wrapper when non-blocking emission is required.
//
// A nil sink panics — composition errors should be loud at startup, not
// silent at request time.
func New(sink Sink) Emitter {
	if sink == nil {
		panic("audit: New called with nil Sink")
	}
	return &emitter{sink: sink}
}

// Emit validates the event, fills defaults, and dispatches.
func (e *emitter) Emit(ctx context.Context, event Event) error {
	if event.SchemaVersion == "" {
		event.SchemaVersion = SchemaVersion
	}
	if event.EventID == "" {
		event.EventID = NewEventID()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if err := event.Validate(); err != nil {
		return err
	}
	return e.sink.Sink(ctx, event)
}

// MultiSink fans an event out to every child sink. Errors are collected
// via [errors.Join] so a single sink failure does not prevent the others
// from receiving the event.
type MultiSink struct {
	sinks []Sink
}

// Compile-time assertion that MultiSink satisfies Sink.
var _ Sink = (*MultiSink)(nil)

// NewMultiSink returns a Sink that fans events out to every sink in order.
// A nil entry in sinks panics at construction time. An empty sinks slice
// returns a sink that always succeeds (useful for tests).
func NewMultiSink(sinks ...Sink) *MultiSink {
	for i, s := range sinks {
		if s == nil {
			panic(fmt.Sprintf("audit: NewMultiSink: sinks[%d] is nil", i))
		}
	}
	// Copy so a caller cannot mutate our slice after construction.
	dup := make([]Sink, len(sinks))
	copy(dup, sinks)
	return &MultiSink{sinks: dup}
}

// Sink fans the event out. Every child receives the event even if earlier
// children returned an error.
func (m *MultiSink) Sink(ctx context.Context, event Event) error {
	var errs []error
	for _, s := range m.sinks {
		if err := s.Sink(ctx, event); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

