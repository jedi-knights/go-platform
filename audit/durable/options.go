package durable

import "time"

// Option configures a [Sink] at construction. Use functional options to
// keep the [New] signature stable as new knobs are added.
type Option func(*Sink)

// WithTable overrides the default audit_events table name. The supplied
// name is validated as a Postgres identifier at construction; an invalid
// name panics.
//
// Useful when:
//   - Multiple services share a database and need a per-service table.
//   - A test wants to write to a scratch table.
func WithTable(name string) Option {
	return func(s *Sink) {
		s.table = name
	}
}

// WithWriteTimeout overrides the default per-event INSERT timeout
// ([DefaultWriteTimeout]). A non-positive timeout panics — composition
// errors are loud.
func WithWriteTimeout(d time.Duration) Option {
	return func(s *Sink) {
		if d <= 0 {
			panic("audit/durable: WithWriteTimeout requires a positive duration")
		}
		s.writeTimeout = d
	}
}
