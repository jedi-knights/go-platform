package testutil

import (
	"context"
	"log/slog"
	"reflect"
	"testing"

	"github.com/jedi-knights/go-logging/pkg/logging"
)

// Logger is an alias for [logging.Logger]. Test helpers can use it
// interchangeably with the canonical logging interface without forcing
// callers to import go-logging directly.
type Logger = logging.Logger

// Compile-time check that noopLogger implements [logging.Logger]. The
// constructor returns the value form, so we assert against the value type.
var _ logging.Logger = noopLogger{}

// noopLogger is a Logger that discards every record.
type noopLogger struct{}

func (noopLogger) Debug(_ string, _ ...any) {}
func (noopLogger) Info(_ string, _ ...any)  {}
func (noopLogger) Warn(_ string, _ ...any)  {}
func (noopLogger) Error(_ string, _ ...any) {}

func (noopLogger) DebugContext(_ context.Context, _ string, _ ...any) {}
func (noopLogger) InfoContext(_ context.Context, _ string, _ ...any)  {}
func (noopLogger) WarnContext(_ context.Context, _ string, _ ...any)  {}
func (noopLogger) ErrorContext(_ context.Context, _ string, _ ...any) {}

func (n noopLogger) With(_ ...any) logging.Logger { return n }

// Enabled always reports false so test code that gates expensive log
// argument construction on [Logger.Enabled] skips the work entirely.
func (noopLogger) Enabled(_ context.Context, _ slog.Level) bool { return false }

// NewTestLogger returns a no-op [Logger] suitable for unit tests. Inject it
// anywhere a [logging.Logger] dependency is required to silence output
// without standing up a real logger.
func NewTestLogger() Logger {
	return noopLogger{}
}

// RequireNoError calls t.Fatal when err is a real, non-nil error.
//
// A typed nil (e.g. (*T)(nil) stored in an error interface) is treated as
// absent: the interface itself is non-nil but the underlying value is nil,
// so there is no genuine error to report. This matches the guard applied
// in [github.com/jedi-knights/go-platform/apperrors].Wrap.
func RequireNoError(t testing.TB, err error) {
	t.Helper()
	if err == nil {
		return
	}
	if v := reflect.ValueOf(err); isNilableKind(v.Kind()) && v.IsNil() {
		return
	}
	t.Fatalf("unexpected error (%T): %v", err, err)
}

// AssertEqual calls t.Errorf when expected and actual are not deeply equal.
// The first argument is the expected value; the second is the actual.
//
// Note: [reflect.DeepEqual] distinguishes nil slices from empty slices, so
// AssertEqual(t, []string{}, nil) reports inequality. Use a length-aware
// helper when that distinction is irrelevant.
func AssertEqual(t testing.TB, expected, actual any) {
	t.Helper()
	if !reflect.DeepEqual(expected, actual) {
		t.Errorf("expected %T(%v), got %T(%v)", expected, expected, actual, actual)
	}
}

// isNilableKind reports whether a [reflect.Kind] can hold a nil value, i.e.
// whether [reflect.Value.IsNil] is safe to call on a value of that kind.
// Mirrors the helper in apperrors to keep the typed-nil interface guard
// consistent across the codebase.
func isNilableKind(k reflect.Kind) bool {
	switch k {
	case reflect.Chan, reflect.Func, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return true
	default:
		return false
	}
}
