// Package audit emits structured agent-audit events for the jedi-knights
// portfolio. It is the data plane behind the audit event schema in
// identity-platform-go ADR-0018 and the usage-accounting pipeline in ADR-0019.
//
// # Goals
//
// The package provides three things:
//
//   - A stable [Event] envelope with the fields every consumer (audit, billing,
//     compliance) needs — actor identity (ADR-0015), resource taxonomy
//     (ADR-0019 extension to ADR-0018), action, decision, and the trace and
//     correlation identifiers required to stitch events end-to-end.
//   - An [Emitter] interface that services depend on at the composition root,
//     decoupled from the concrete sink. Implementations include a synchronous
//     JSON-to-stderr [StderrJSONSink], a discarding [NoopSink] for tests, a
//     fan-out [MultiSink], and an [AsyncSink] wrapper that adds non-blocking
//     emission with overflow-drop semantics.
//   - A package-private [NewEventID] generator that produces ULID-encoded
//     identifiers so events are globally unique and lexicographically
//     time-ordered. The ULID becomes the Lago transaction_id in the metering
//     shim, making replays and reconciliation runs idempotent.
//
// # Non-goals (yet)
//
// The durable, at-least-once sink (Postgres or NATS JetStream) specified in
// ADR-0019 is intentionally out of scope for this package. It ships in a
// follow-up release alongside the metering shim so the Postgres / NATS
// dependency does not bleed into every consumer of the audit contract.
// Likewise, the OpenTelemetry log sink lives in the planned go-platform/otel
// package, not here.
//
// # Concurrency model
//
//   - [Emitter.Emit] is safe to call concurrently from any goroutine.
//   - [StderrJSONSink] writes synchronously and serialises writes via an
//     internal mutex so concurrent emits never interleave bytes on stderr.
//   - [AsyncSink] enqueues into a fixed-size channel; when the channel is
//     full the event is dropped and an internal drop counter is incremented,
//     readable via [AsyncSink.Stats]. This is the recommended way to obtain
//     ADR-0018's "non-blocking emission with overflow drop" property.
//   - [MultiSink] fans an event out to its children sequentially. If a child
//     returns an error the remaining children are still given the event;
//     errors are joined via [errors.Join].
//
// # Stability
//
// The exported [Event] shape is the public contract. New optional fields may
// be added without bumping the package major version; renaming or removing
// fields is a breaking change and requires a SchemaVersion bump on the event.
package audit
