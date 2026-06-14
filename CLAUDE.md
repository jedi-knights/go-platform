# go-platform — Claude context

## What this is

A single Go module holding shared utility packages for `jedi-knights` services. Each package is small, narrow in purpose, and intended to be depended on by multiple applications. The four packages are:

| Package | Purpose |
|---|---|
| `apperrors` | Structured `AppError` with `ErrorCode` and HTTP status mapping |
| `jwtutil` | Canonical `Claims`, `Sign`, `Parse` for HS256 JWTs |
| `httputil` | `WriteJSON`, `WriteError`, request ID/trace ID middleware |
| `testutil` | Shared test helpers (under review — may be skipped if `go-logging` covers the use case) |

Each package corresponds to a former `libs/<name>` directory in `ocrosby/identity-platform-go`.

## Architecture

- Single Go module: `github.com/jedi-knights/go-platform`. Packages are subpackages under the module root, not separate modules. This was a deliberate decision — independent versioning per package adds ceremony that the libs don't need at their current cadence.
- `apperrors` and `jwtutil` have zero or one external dependencies. `httputil` and `testutil` depend on `github.com/jedi-knights/go-logging`.
- The package previously named `errors` (in `libs/errors`) was renamed to `apperrors` to avoid the stdlib `errors` package collision. Every consumer that aliased it as `apperrors` simplifies.

## Conventions

- **Commit messages**: Conventional Commits with optional scope. Scope is the package name: `feat(apperrors)`, `fix(jwtutil)`, etc. Breaking changes use `!`.
- **Tests**: external test package (`package foo_test`) so we exercise the public surface. Use `t.Parallel()` everywhere except where the test mutates package-level state.
- **No new external dependencies** without a clear reason. Prefer stdlib where it suffices.
- **Each package is independently usable**: do not introduce cross-package dependencies inside the module unless absolutely necessary (e.g., `httputil` importing `apperrors` for the structured error type — that is justified).

## Versioning

While packages are being ported and the surface stabilises, releases are `v0.x.y`. The first `v1.0.0` tag will mark a commitment to API stability.

## Development

```bash
task test          # go test ./... -race -count=1
task lint          # golangci-lint run ./...
task tidy          # go mod tidy && go mod verify
```
