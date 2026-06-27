package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jedi-knights/go-platform/audit"
)

// DefaultTable is the table name used when [WithTable] is not supplied.
const DefaultTable = "audit_events"

// DefaultWriteTimeout caps the per-event INSERT. The composition root can
// override it via [WithWriteTimeout]; the default is generous enough that
// healthy connections never trip it but tight enough that a stalled
// Postgres surfaces as a request failure within request budget.
const DefaultWriteTimeout = 2 * time.Second

// Querier is the minimum subset of [pgxpool.Pool] this sink uses. Tests
// supply a [github.com/pashagolub/pgxmock/v4] implementation directly;
// production wires a *pgxpool.Pool, which satisfies this interface.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Sink writes events to a Postgres table inside the request path. It
// implements [audit.Sink] with at-least-once semantics: a successful return
// means the row is durably persisted; an error means the row may or may not
// be persisted and the caller should treat the event as not-yet-recorded.
//
// Use [New] to construct.
type Sink struct {
	db           Querier
	table        string
	writeTimeout time.Duration
}

// Compile-time assertion that Sink satisfies the parent package's Sink
// contract.
var _ audit.Sink = (*Sink)(nil)

// New constructs a Postgres-backed durable audit sink. A nil db panics —
// composition errors should be loud at startup. Pass options to override
// defaults; see [WithTable] and [WithWriteTimeout].
//
// Call [Migrate] before the first emission to ensure the table exists.
func New(db Querier, opts ...Option) *Sink {
	if db == nil {
		panic("audit/durable: New called with nil Querier")
	}
	s := &Sink{
		db:           db,
		table:        DefaultTable,
		writeTimeout: DefaultWriteTimeout,
	}
	for _, opt := range opts {
		opt(s)
	}
	if err := validateIdentifier(s.table); err != nil {
		panic("audit/durable: invalid table name: " + err.Error())
	}
	return s
}

// Sink persists the event and returns nil on success or the underlying
// Postgres error otherwise. The write is wrapped in a per-event timeout
// so a stuck connection cannot pin the caller indefinitely.
//
// Duplicate event_ids (the ULID minted by the audit package) are silently
// ignored via ON CONFLICT DO NOTHING — at-least-once becomes exactly-once
// downstream when the metering shim also dedupes via Lago transaction_id.
func (s *Sink) Sink(ctx context.Context, event audit.Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("audit/durable: marshal event: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, s.writeTimeout)
	defer cancel()
	// #nosec G201 -- table name is validated by validateIdentifier at construction.
	query := fmt.Sprintf(
		`INSERT INTO %s (event_id, payload) VALUES ($1, $2)
		 ON CONFLICT (event_id) DO NOTHING`,
		s.table,
	)
	if _, err := s.db.Exec(ctx, query, event.EventID, payload); err != nil {
		return fmt.Errorf("audit/durable: insert event %s: %w", event.EventID, err)
	}
	return nil
}

// validateIdentifier guards against SQL injection in the table-name slot
// of the INSERT statement. Postgres identifiers are 1-63 chars, letters /
// digits / underscores, leading letter or underscore. We are strict because
// callers may load the table name from configuration.
func validateIdentifier(s string) error {
	if s == "" {
		return errors.New("identifier is empty")
	}
	if len(s) > 63 {
		return errors.New("identifier exceeds 63 characters")
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r == '_':
			// always allowed
		case r >= '0' && r <= '9':
			if i == 0 {
				return errors.New("identifier cannot start with a digit")
			}
		default:
			return fmt.Errorf("identifier contains invalid character %q at position %d", r, i)
		}
	}
	return nil
}
