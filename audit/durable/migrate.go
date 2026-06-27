package durable

import (
	"context"
	"fmt"
)

// Migrate creates the audit_events table and its partial index if they do
// not exist. Idempotent; safe to call on every service start.
//
// The table name is the package default unless [WithTable] was used on the
// Sink; pass the same Sink to ensure the migration targets the same table.
// Migrate is a method on Sink rather than a free function so the table name
// is always the one the Sink will write to.
func (s *Sink) Migrate(ctx context.Context) error {
	createTable := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			event_id    TEXT        PRIMARY KEY,
			payload     JSONB       NOT NULL,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
			consumed_at TIMESTAMPTZ
		)`, s.table)
	if _, err := s.db.Exec(ctx, createTable); err != nil {
		return fmt.Errorf("audit/durable: create table %s: %w", s.table, err)
	}
	createIndex := fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS %s_unconsumed
			ON %s (created_at)
			WHERE consumed_at IS NULL`, s.table, s.table)
	if _, err := s.db.Exec(ctx, createIndex); err != nil {
		return fmt.Errorf("audit/durable: create index on %s: %w", s.table, err)
	}
	return nil
}
