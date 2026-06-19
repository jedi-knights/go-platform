// Package testutil provides small, stable test helpers shared across
// jedi-knights services.
//
// Surface is deliberately narrow — three helpers, no external dependencies
// beyond [github.com/jedi-knights/go-logging] for the no-op test logger:
//
//   - [NewTestLogger] — a [logging.Logger] that discards all output. Inject
//     this in unit tests to silence log noise without standing up a real
//     logger.
//   - [RequireNoError] — `t.Fatal` if `err` is a real error. The typed-nil
//     interface hazard (`(*T)(nil)` stored in an `error` interface) is
//     treated as absent, matching the guard in
//     [github.com/jedi-knights/go-platform/apperrors].Wrap.
//   - [AssertEqual] — `reflect.DeepEqual`-based equality check. Note that
//     `reflect.DeepEqual` distinguishes nil slices from empty slices.
//
// Service-specific helpers must NOT live here; keep them next to the tests
// that use them. This package should stay small.
package testutil
