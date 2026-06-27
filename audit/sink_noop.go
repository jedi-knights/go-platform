package audit

import "context"

// NoopSink discards every event. Useful for tests and for opt-out scenarios
// (e.g., disable audit emission in a benchmark).
type NoopSink struct{}

// Compile-time assertion.
var _ Sink = (*NoopSink)(nil)

// Sink discards the event and returns nil.
func (NoopSink) Sink(_ context.Context, _ Event) error { return nil }
