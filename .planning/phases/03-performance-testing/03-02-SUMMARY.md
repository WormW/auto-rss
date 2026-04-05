---
phase: 03-performance-testing
plan: 02
subsystem: rss
tags: [performance, timeout, configuration]
dependency_graph:
  requires: []
  provides: [PERF-03]
  affects: [internal/model/rss_source.go, internal/service/rss/parser.go, internal/api/handler/rss_source.go, internal/pkg/database/migration.go]
tech-stack:
  added: []
  patterns: [configurable-timeout, per-source-configuration]
key-files:
  created:
    - internal/model/rss_source_test.go
    - internal/service/rss/parser_test.go
    - internal/pkg/database/migration_test.go
  modified:
    - internal/model/rss_source.go
    - internal/service/rss/parser.go
    - internal/api/handler/rss_source.go
    - internal/pkg/database/migration.go
decisions:
  - Enforce minimum 1s timeout in parser to prevent DoS (T-03-04 mitigation)
  - Use time.Duration for timeout field with nanosecond precision in database
  - Delegate FetchAndParse to FetchAndParseWithTimeout for backward compatibility
metrics:
  duration: 25m
  completed_date: 2026-04-05
---

# Phase 03 Plan 02: Configurable RSS Timeout Summary

**One-liner:** Made RSS timeout configurable per source instead of hardcoded 30 seconds, allowing different RSS sources to have different timeout values based on their response characteristics.

## What Was Built

### 1. RSSSource Model with Timeout Field
- Added `Timeout time.Duration` field to `RSSSource` model with 30s default (30000000000 nanoseconds)
- Added `DefaultRSSTimeout()` helper function returning 30 seconds
- Added comprehensive tests for the model changes

### 2. RSS Parser with Configurable Timeout
- Added `FetchAndParseWithTimeout(rssURL string, timeout time.Duration)` method to Parser interface
- Implemented method with fallback to 30s default when timeout <= 0
- Enforced minimum 1s timeout to prevent DoS attacks (T-03-04 mitigation)
- Refactored `FetchAndParse` to delegate to `FetchAndParseWithTimeout` for backward compatibility
- Added comprehensive tests for timeout behavior

### 3. RSS Handler Integration
- Updated `FetchAnimes` handler to use `FetchAndParseWithTimeout` with source-specific timeout
- Added fallback to `DefaultRSSTimeout()` when source timeout is 0

### 4. Database Migration
- Added `MigrateRSSTimeout(db *gorm.DB)` function to set default timeout for existing RSS sources
- Migration sets 30s timeout for sources with timeout = 0 or NULL
- Added tests including idempotency verification

## Deviations from Plan

None - plan executed exactly as written.

## Threat Flags

| Flag | File | Description |
|------|------|-------------|
| threat_flag: DoS mitigation | internal/service/rss/parser.go | Minimum 1s timeout enforced to prevent indefinite hanging (T-03-04) |

## Test Coverage

| Component | Test File | Coverage |
|-----------|-----------|----------|
| Model | internal/model/rss_source_test.go | Timeout field existence, default value, TableName |
| Parser | internal/service/rss/parser_test.go | Timeout behavior, zero/negative handling, context cancellation |
| Migration | internal/pkg/database/migration_test.go | Migration logic, idempotency |

All tests pass with race detector:
```bash
go test ./internal/model/... ./internal/service/rss/... ./internal/pkg/database/... -v -race
```

## Commits

| Hash | Message |
|------|---------|
| 7dfccab | feat(03-02): add Timeout field to RSSSource model |
| 78ff62e | feat(03-02): add FetchAndParseWithTimeout to RSS parser |
| 4cdda1d | feat(03-02): update RSS handler to use per-source timeout |
| e4de846 | feat(03-02): add migration for RSS source timeout |

## Self-Check: PASSED

- [x] All created files exist
- [x] All commits exist
- [x] Code compiles without errors
- [x] All tests pass
- [x] No security regressions (minimum timeout enforced)
