package durable_test

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v4"

	"github.com/jedi-knights/go-platform/audit"
	"github.com/jedi-knights/go-platform/audit/durable"
)

func newMock(t *testing.T) pgxmock.PgxPoolIface {
	t.Helper()
	m, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock.NewPool: %v", err)
	}
	t.Cleanup(m.Close)
	return m
}

func validEvent(t *testing.T) audit.Event {
	t.Helper()
	return audit.Event{
		SchemaVersion: audit.SchemaVersion,
		EventID:       audit.NewEventID(),
		EventType:     "tool_invoked",
		Timestamp:     time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC),
		Service:       "jk-mcp-nwsl",
		ActorType:     audit.ActorTypeAgent,
		ActorID:       "agent-claude-test",
		Resource:      "tool:get_standings",
		ResourceKind:  audit.ResourceKindTool,
		ResourceID:    "get_standings",
		Action:        "invoke",
		Decision:      audit.DecisionAllow,
	}
}

func TestNew_NilDBPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic")
		}
	}()
	_ = durable.New(nil)
}

func TestNew_InvalidTableNamePanics(t *testing.T) {
	tests := []struct {
		name  string
		table string
	}{
		{"empty", ""},
		{"leading digit", "1bad"},
		{"semicolon", "events; DROP TABLE x"},
		{"hyphen", "audit-events"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Fatal("expected panic on invalid table name")
				}
			}()
			_ = durable.New(newMock(t), durable.WithTable(tt.table))
		})
	}
}

func TestNew_ValidTableNamesAccepted(t *testing.T) {
	tests := []string{
		"audit_events",
		"AuditEvents",
		"_private",
		"events_v2",
	}
	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			s := durable.New(newMock(t), durable.WithTable(name))
			if s == nil {
				t.Fatal("expected non-nil sink")
			}
		})
	}
}

func TestSink_InsertHappyPath(t *testing.T) {
	mock := newMock(t)
	s := durable.New(mock)

	event := validEvent(t)
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO audit_events")).
		WithArgs(event.EventID, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	if err := s.Sink(context.Background(), event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations not met: %v", err)
	}
}

func TestSink_OnConflictDoesNotErr(t *testing.T) {
	mock := newMock(t)
	s := durable.New(mock)

	event := validEvent(t)
	// ON CONFLICT DO NOTHING — INSERT returns 0 rows affected but no error.
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO audit_events")).
		WithArgs(event.EventID, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 0))

	if err := s.Sink(context.Background(), event); err != nil {
		t.Fatalf("unexpected error on duplicate: %v", err)
	}
}

func TestSink_PropagatesDatabaseError(t *testing.T) {
	mock := newMock(t)
	s := durable.New(mock)

	dbErr := errors.New("connection refused")
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO audit_events")).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(dbErr)

	err := s.Sink(context.Background(), validEvent(t))
	if !errors.Is(err, dbErr) {
		t.Errorf("expected wrapped db error, got %v", err)
	}
}

func TestSink_RespectsCustomTableName(t *testing.T) {
	mock := newMock(t)
	s := durable.New(mock, durable.WithTable("events_v2"))

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO events_v2")).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	if err := s.Sink(context.Background(), validEvent(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSink_AppliesWriteTimeout(t *testing.T) {
	mock := newMock(t)
	s := durable.New(mock, durable.WithWriteTimeout(50*time.Millisecond))

	// pgxmock's WillDelayFor causes Exec to block; we expect the context
	// deadline to fire and return a deadline-exceeded error.
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO audit_events")).
		WillReturnResult(pgxmock.NewResult("INSERT", 1)).
		WillDelayFor(500 * time.Millisecond)

	start := time.Now()
	err := s.Sink(context.Background(), validEvent(t))
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed > 300*time.Millisecond {
		t.Errorf("expected fast timeout, took %v", elapsed)
	}
}

func TestWithWriteTimeout_NonPositivePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic")
		}
	}()
	_ = durable.New(newMock(t), durable.WithWriteTimeout(0))
}

func TestMigrate_CreatesTableAndIndex(t *testing.T) {
	mock := newMock(t)
	s := durable.New(mock)

	mock.ExpectExec(regexp.QuoteMeta("CREATE TABLE IF NOT EXISTS audit_events")).
		WillReturnResult(pgxmock.NewResult("CREATE TABLE", 0))
	mock.ExpectExec(regexp.QuoteMeta("CREATE INDEX IF NOT EXISTS audit_events_unconsumed")).
		WillReturnResult(pgxmock.NewResult("CREATE INDEX", 0))

	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations not met: %v", err)
	}
}

func TestMigrate_PropagatesError(t *testing.T) {
	mock := newMock(t)
	s := durable.New(mock)

	dbErr := errors.New("permission denied")
	mock.ExpectExec(regexp.QuoteMeta("CREATE TABLE IF NOT EXISTS audit_events")).
		WillReturnError(dbErr)

	err := s.Migrate(context.Background())
	if !errors.Is(err, dbErr) {
		t.Errorf("expected wrapped db error, got %v", err)
	}
}

func TestMigrate_RespectsCustomTable(t *testing.T) {
	mock := newMock(t)
	s := durable.New(mock, durable.WithTable("events_v2"))

	mock.ExpectExec(regexp.QuoteMeta("CREATE TABLE IF NOT EXISTS events_v2")).
		WillReturnResult(pgxmock.NewResult("CREATE TABLE", 0))
	mock.ExpectExec(regexp.QuoteMeta("CREATE INDEX IF NOT EXISTS events_v2_unconsumed")).
		WillReturnResult(pgxmock.NewResult("CREATE INDEX", 0))

	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
}

// Compile-time check: *durable.Sink can be used through the audit.Sink
// interface; verified at runtime by passing one to audit.NewMultiSink.
func TestSink_ComposesWithAuditPackage(t *testing.T) {
	mock := newMock(t)
	durableSink := durable.New(mock)
	combined := audit.NewMultiSink(durableSink, audit.NoopSink{})

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO audit_events")).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	e := audit.New(combined)
	if err := e.Emit(context.Background(), validEvent(t)); err != nil {
		t.Fatalf("emit failed: %v", err)
	}
}
