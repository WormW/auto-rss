# Reduce Health Diagnostics Disk Pressure

## Context

Auto-RSS currently creates disk activity through two independent health-related paths:

1. Opening subscription diagnostics loads every download for the subscription and calls `os.Stat` for the recorded media paths of every completed download. On HDD and NAS storage, these random metadata reads can wake disks and scale linearly with download history.
2. Docker calls `/health` every 30 seconds. The global request logger persists each probe to SQLite, and every persisted log immediately runs log-count and retention cleanup queries.

The recent scan-pressure changes constrain explicit filesystem scans, but do not change either path above.

## Goals

- Make normal subscription diagnostics cheap enough to open without walking recorded media paths.
- Keep explicit filesystem verification available as a user-triggered operation.
- Stop routine container probes from generating SQLite writes.
- Avoid running database log-maintenance queries for every log entry.
- Preserve the existing diagnostics response shape where practical.

## Non-Goals

- Redesigning recovery scans or the manual subscription folder scanner.
- Adding background filesystem indexing or a new persistent file-status cache.
- Changing download status reconciliation semantics.
- Removing detailed `/health` checks for operators who request that endpoint directly.

## Design

### Lightweight Subscription Diagnostics

The diagnostics endpoint will continue to load subscription download records and calculate status counts, retryability, missing episode numbers, RSS reachability, qBittorrent state, and disk capacity.

It will no longer call `os.Stat` for each completed download's `renamed_path` or `file_path`. File diagnostics will report database-backed information only:

- `completed_with_file` counts completed records that contain at least one recorded file path.
- `completed_missing_file` counts completed records without either recorded path.
- `missing_renamed` keeps its existing meaning.
- The summary text will explicitly say that paths are recorded, rather than claiming that files were confirmed on disk.

The existing **Scan local files** action remains the explicit deep-verification path. It opens the current scoped scanner UI, where the user chooses a subscription directory and starts the scan deliberately.

This preserves the response schema and frontend layout while removing the expensive implicit media-disk access.

### Probe Logging

The HTTP logger middleware will skip `/health`, `/api/v1/health`, `/ready`, and `/live`. These endpoints remain visible through metrics and direct responses, but routine probes will no longer create database log rows.

Dockerfile and Compose health checks will use `/live`. Liveness only verifies that the process can serve HTTP; the richer `/health` endpoint remains available for operational diagnostics without making qBittorrent, SQLite, or download storage part of every 30-second container liveness decision.

### Log Cleanup Throttling

`DBWriter` will retain asynchronous log persistence but throttle cleanup to at most once per hour per writer instance. The first successful log write may trigger cleanup; subsequent writes within the interval only insert the log row.

The throttle will be concurrency-safe because multiple log writes can complete concurrently. Cleanup failures will not prevent future attempts after the interval.

This is intentionally an in-process throttle rather than a new scheduler or database migration.

## Error Handling

- A failed log insert will not trigger cleanup.
- Cleanup remains best-effort and does not affect request handling.
- The explicit scanner continues to surface filesystem errors through its existing API behavior.
- Direct `/health` calls retain the current degraded and unhealthy responses.

## Testing

- Add a subscription diagnostics regression test using recorded paths whose parent directory is not searchable. The endpoint must treat the paths as recorded without depending on filesystem access.
- Add middleware tests proving probe paths are not logged while a normal API path is logged.
- Add `DBWriter` tests proving cleanup is throttled under repeated writes and can run again after the interval.
- Run focused package tests, then `go test ./...`.

## Compatibility

The diagnostics JSON fields remain present. The semantic change is that `completed_with_file` means "completed records with a recorded path" rather than "files confirmed by a synchronous stat call." User-facing wording will be updated to avoid presenting it as physical verification.

