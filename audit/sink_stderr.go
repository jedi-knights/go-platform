package audit

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"sync"
)

// StderrJSONSink writes one JSON line per event to a target [io.Writer]
// (defaulting to os.Stderr). Writes are serialized by an internal mutex
// so concurrent emits never interleave bytes.
//
// StderrJSONSink is synchronous: Sink returns only after the bytes are
// written. Wrap with [AsyncSink] to obtain ADR-0018's non-blocking
// emission with overflow drop.
//
// The chosen writer is fixed at construction; tests pass a [bytes.Buffer]
// (or any io.Writer) to capture output. Stderr is the default because
// the MCP servers rely on a clean stdout for JSON-RPC framing — every
// audit emission goes to stderr by convention.
type StderrJSONSink struct {
	mu sync.Mutex
	w  io.Writer
}

// Compile-time assertion.
var _ Sink = (*StderrJSONSink)(nil)

// NewStderrJSONSink returns a synchronous JSON-lines sink writing to
// os.Stderr.
func NewStderrJSONSink() *StderrJSONSink {
	return &StderrJSONSink{w: os.Stderr}
}

// NewJSONSink returns a synchronous JSON-lines sink writing to w. Used by
// tests and by services that want to direct audit events to a file or
// network stream. A nil writer panics — composition errors are loud.
func NewJSONSink(w io.Writer) *StderrJSONSink {
	if w == nil {
		panic("audit: NewJSONSink called with nil writer")
	}
	return &StderrJSONSink{w: w}
}

// Sink marshals the event to JSON and writes it followed by a newline.
// json.Marshal is called inside the lock so concurrent emits cannot
// interleave their output. Marshal failures are returned to the caller —
// the emitter validates the envelope before this point, so a marshal
// failure is a programmer error (e.g., a non-serialisable value in
// Attrs) and worth surfacing.
func (s *StderrJSONSink) Sink(ctx context.Context, event Event) error {
	_ = ctx // reserved for future cancellation; stderr writes are local and fast
	b, err := json.Marshal(event)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, werr := s.w.Write(b); werr != nil {
		return werr
	}
	_, err = s.w.Write(newline)
	return err
}

// newline is reused across writes to avoid allocating a single-byte slice
// on the hot path.
var newline = []byte{'\n'}
