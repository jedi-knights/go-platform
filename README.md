# go-platform

Shared Go libraries for jedi-knights services: structured errors, HTTP utilities, JWT helpers, and test utilities.

![CI](https://github.com/jedi-knights/go-platform/actions/workflows/ci.yml/badge.svg)
[![Go Reference](https://pkg.go.dev/badge/github.com/jedi-knights/go-platform.svg)](https://pkg.go.dev/github.com/jedi-knights/go-platform)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

## Table of contents

- [Overview](#overview)
- [Packages](#packages)
- [Requirements](#requirements)
- [Installation](#installation)
- [Usage](#usage)
- [Configuration](#configuration)
- [Development](#development)
- [Contributing](#contributing)
- [License](#license)

## Overview

`go-platform` is a single Go module holding shared utility packages used by multiple `jedi-knights` services. Each package is small, has a narrow purpose, and is designed to be imported independently. The module pairs with [`jedi-knights/go-logging`](https://github.com/jedi-knights/go-logging) for structured logging.

## Packages

| Package | Purpose | Status |
|---|---|---|
| [`apperrors`](apperrors/) | Structured `AppError` with `ErrorCode` and HTTP status mapping | ✅ Available |
| [`jwtutil`](jwtutil/) | Canonical `Claims`, `Sign`, `Parse` for HS256 JWTs | Planned |
| [`httputil`](httputil/) | `WriteJSON`, `WriteError`, request and trace ID middleware | Planned (blocked on `go-logging` v2.0.0 tag) |
| [`testutil`](testutil/) | Shared test helpers | Under review |
| [`audit`](audit/) | Agent audit event schema + pluggable sinks (see ADR-0018 in identity-platform-go) | ✅ Available |
| [`audit/durable`](audit/durable/) | Postgres-backed at-least-once durable sink (ADR-0019) | ✅ Available |
| [`otel`](otel/) | Minimal OpenTelemetry bootstrap for end-to-end tracing | ✅ Available |

## Requirements

- Go 1.26 or later

## Installation

```bash
go get github.com/jedi-knights/go-platform@latest
```

Import individual packages:

```go
import (
    "github.com/jedi-knights/go-platform/apperrors"
    "github.com/jedi-knights/go-platform/jwtutil"
    "github.com/jedi-knights/go-platform/httputil"
)
```

## Usage

### apperrors

```go
import "github.com/jedi-knights/go-platform/apperrors"

err := apperrors.New(apperrors.ErrCodeNotFound, "user not found")
status := apperrors.HTTPStatus(err) // 404
```

See [`apperrors/`](apperrors/) for the full reference.

## Agentic posture roadmap

Two new packages are planned to support the portfolio-wide agentic-enterprise
posture documented in
[jedi-knights/architecture/docs/agentic-posture.md](https://github.com/jedi-knights/architecture/blob/main/docs/agentic-posture.md):

- **`audit`** — structured agent-audit events with a stable cross-service
  schema (see ADR-0018 in `identity-platform-go`). Pluggable sinks (stderr
  JSON, OTel log, append-only file). Non-blocking emission so the audit hot
  path can't fail the underlying operation.
- **`otel`** — minimal OpenTelemetry bootstrap (span helpers, context
  propagation, env-based exporter selection) so every service emits traces
  with consistent `agent_id`, `tool_name`, `policy_decision` attributes.

Both packages release independently via the existing semantic-release pipeline.
The `jwtutil.Claims` type is the entry point for the new `actor_type` /
`agent_id` claims described in ADR-0015 of `identity-platform-go` — added
additively, no breaking change.

## Configuration

This module has no module-level configuration. Each package documents its own options where applicable.

## Development

```bash
# Run tests with race detector
go test ./... -race -count=1

# Lint
golangci-lint run ./...

# Tidy and verify dependencies
go mod tidy && go mod verify
```

The repository uses [Task](https://taskfile.dev/) — see `Taskfile.yml` for the canonical task list.

## Contributing

Issues and pull requests welcome. Conventional Commits required (see [CLAUDE.md](CLAUDE.md) for scopes).

## License

[MIT](LICENSE)
