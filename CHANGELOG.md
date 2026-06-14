# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Initial repository skeleton: go.mod (Go 1.26), README, CHANGELOG, LICENSE (MIT), Taskfile, golangci-lint v2 config, GitHub Actions CI (lint + test + build).
- `apperrors` package — structured application errors with `ErrorCode`, `AppError`, HTTP status mapping. Ported from `ocrosby/identity-platform-go/libs/errors`.

### Planned

- `jwtutil` — canonical Claims, Sign, Parse. Ported from `libs/jwtutil`.
- `httputil` — `WriteJSON`, `WriteError`, trace ID middleware. Ported from `libs/httputil` after `jedi-knights/go-logging` tags `v2.0.0`.
- `testutil` — shared test helpers. Ported from `libs/testutil` after `go-logging v2.0.0`. May be skipped if `go-logging`'s `New(Config{Output: &buf})` already covers the use case.
