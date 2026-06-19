package httputil

import "testing"

// TestNewTraceID_MatchesUUIDPattern guards against drift between go-logging's
// NewTraceID output and the local uuidPattern used by TraceIDMiddleware. If
// go-logging ever changes its emit format (e.g. drops a hyphen, switches to
// uppercase hex), TraceIDMiddleware would silently start replacing every
// generated ID with another generated ID — wasting entropy and masking the
// upstream change. Running many iterations exercises every nibble position.
func TestNewTraceID_MatchesUUIDPattern(t *testing.T) {
	const iterations = 10_000
	for range iterations {
		id := newTraceID()
		if len(id) != 36 {
			t.Fatalf("newTraceID() returned %q with length %d, want 36", id, len(id))
		}
		if !uuidPattern.MatchString(id) {
			t.Fatalf("newTraceID() returned %q which does not match uuidPattern", id)
		}
	}
}
