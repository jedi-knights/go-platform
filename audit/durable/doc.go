// Package durable provides a Postgres-backed at-least-once [audit.Sink].
//
// It is the implementation of the "durable" sink specified in
// identity-platform-go ADR-0019: every accepted event is persisted to a
// Postgres table inside the request path so subsequent billing / metering
// has zero gaps. The companion best-effort sinks (StderrJSONSink, AsyncSink,
// the planned OTel log sink) live in the parent audit package and use
// different durability semantics by design.
//
// # Backend choice
//
// Postgres is the first cut. NATS JetStream is a documented alternative in
// ADR-0019 and will land as a sibling package without changing the
// [audit.Sink] contract. Choose Postgres when:
//
//   - The service already runs Postgres (most identity-platform services do).
//   - Operational simplicity matters more than ingest throughput.
//   - The metering shim wants to poll or use LISTEN/NOTIFY without a broker.
//
// # Schema
//
// One table, owned by this package. Apply with [Migrate] at service
// startup before any [Sink.Sink] call.
//
//	CREATE TABLE IF NOT EXISTS audit_events (
//	    event_id    TEXT        PRIMARY KEY,
//	    payload     JSONB       NOT NULL,
//	    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
//	    consumed_at TIMESTAMPTZ
//	);
//	CREATE INDEX IF NOT EXISTS audit_events_unconsumed
//	    ON audit_events (created_at)
//	    WHERE consumed_at IS NULL;
//
// The metering shim sets consumed_at on successful Lago push; the partial
// index lets the shim find new events with no full-table scan.
//
// # Idempotency
//
// Inserts use ON CONFLICT (event_id) DO NOTHING. A duplicate event_id is
// silently ignored — the caller may retry without producing duplicate
// audit rows. Combined with the ULID event_id minted by the audit package,
// this gives end-to-end exactly-once semantics from emitter through
// metering shim through Lago.
//
// # Concurrency
//
// [Sink.Sink] is safe to call concurrently. Connection management is
// delegated to the supplied [pgxpool.Pool], which is itself concurrent-safe.
//
// # Failure semantics
//
// Sink returns the underlying database error when the INSERT fails. Wrap
// this sink in a [audit.MultiSink] together with the best-effort
// StderrJSONSink and decide per-event-type whether the durable error
// fails the request (paid events) or is swallowed (free / best-effort
// events). The per-event-type policy lives in the composition root,
// not in this package.
package durable
