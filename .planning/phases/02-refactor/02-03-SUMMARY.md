---
phase: 02-refactor
plan: 03
subsystem: downloader
completed_date: 2026-04-05
duration: 45min
tags: [refactor, service-extraction, tdd]
dependency_graph:
  requires: [02-01, 02-02]
  provides: [downloader-services]
  affects: [internal/service/downloader]
tech-stack:
  added: []
  patterns: [interface-based-design, constructor-injection, tdd]
key-files:
  created:
    - internal/service/downloader/status_sync.go
    - internal/service/downloader/status_sync_test.go
    - internal/service/downloader/completion_handler.go
    - internal/service/downloader/completion_handler_test.go
  modified:
    - internal/service/downloader/retry.go
    - internal/service/downloader/retry_test.go
decisions:
  - "StatusSync interface with Sync, UpdateStatus, Reconcile methods per D-04"
  - "CompletionHandler interface with HandleComplete method per D-04"
  - "Extended RetryService with ProcessRetries method per REF-07"
  - "Nil DB handling in completion handler for testability"
  - "Helper functions (mapQBStateToStatus, isTorrentComplete) kept as package-level functions"
metrics:
  duration: 45min
  tasks_completed: 3
  files_created: 4
  files_modified: 2
  tests_added: 25+
  lines_extracted: ~257 (from monitor.go)
---

# Phase 02 Plan 03: Extract Status Sync, Completion Handler, and Extend Retry Service

## Summary

Extracted three services from `monitor.go` (959 lines) to reduce its size and improve maintainability:

1. **StatusSync service** (REF-05): Handles download status synchronization from qBittorrent
2. **CompletionHandler service** (REF-06): Handles download completion processing (notifications, renaming, stats)
3. **Extended RetryService** (REF-07): Added `ProcessRetries` method for batch retry processing

## One-Liner

Extracted 257 lines from monitor.go into three focused services with comprehensive unit tests, following TDD and interface-based design patterns.

## What Was Built

### StatusSync Service

**File:** `internal/service/downloader/status_sync.go`

```go
type StatusSync interface {
    Sync(torrents []*TorrentInfo) error
    UpdateStatus(download *model.Download, torrent *TorrentInfo) (bool, error)
    Reconcile(torrents []*TorrentInfo, downloadingTasks, stalledTasks []model.Download) (reconciled int, skipped int, err error)
}
```

**Key Features:**
- Maps qBittorrent states to internal status (downloading, completed, stalled, failed)
- Detects completion via progress >= 99.99% or downloaded size
- Reconciles missing torrents with grace period handling
- Sends notifications for failed downloads during reconciliation

### CompletionHandler Service

**File:** `internal/service/downloader/completion_handler.go`

```go
type CompletionHandler interface {
    HandleComplete(download *model.Download, torrent *TorrentInfo, subscription *model.Subscription) error
}
```

**Key Features:**
- Sends download completion notifications
- Handles file renaming for single episodes and collections
- Updates subscription stats (CurrentEpisode, LastDownloadAt)
- Gracefully handles rename errors without failing completion
- Supports nil notification service for testing

### Extended RetryService

**File:** `internal/service/downloader/retry.go` (modified)

Added `ProcessRetries` method:

```go
func (s *RetryService) ProcessRetries(limit int) (processed int, err error)
```

**Key Features:**
- Processes failed downloads ready for retry
- Respects limit parameter for batch control
- Double-checks ShouldRetry before processing
- Logs retry decisions appropriately

## Test Coverage

### StatusSync Tests
- `TestStatusSync_UpdateStatus` - Status change detection
- `TestStatusSync_UpdateStatus_SetsErrorMessage` - Error handling
- `TestStatusSync_Sync` - Batch sync operation
- `TestStatusSync_Sync_RepositoryError` - Error propagation
- `TestStatusSync_Reconcile` - Missing torrent reconciliation
- `TestStatusSync_Reconcile_SendsNotification` - Notification on failure
- `TestStatusSync_Reconcile_RepositoryError` - Error handling

### CompletionHandler Tests
- `TestCompletionHandler_HandleComplete_SendsNotification` - Notification sending
- `TestCompletionHandler_HandleComplete_UpdatesSubscriptionStats` - Stats update
- `TestCompletionHandler_HandleComplete_WithRenameEnabled` - File renaming
- `TestCompletionHandler_HandleComplete_CollectionRename` - Collection handling
- `TestCompletionHandler_HandleComplete_NoNotificationService` - Nil handling
- `TestCompletionHandler_HandleComplete_RenameError` - Error resilience

### RetryService Tests
- `TestRetryService_ProcessRetries` - Batch processing
- `TestRetryService_ProcessRetries_RepositoryError` - Error handling
- `TestRetryService_ProcessRetries_Limit` - Limit respect

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed test DB nil pointer handling**
- **Found during:** Task 2 testing
- **Issue:** Tests using `&gorm.DB{}` caused nil pointer dereference in Transaction
- **Fix:** Added nil DB check in `updateSubscriptionStats` to skip DB operations in test mode
- **Files modified:** `completion_handler.go`

**2. [Rule 2 - Missing Critical Functionality] Added helper functions**
- **Found during:** Task 2 implementation
- **Issue:** Needed `isConflictError`, `containsSubstring`, `lastIndexOf` helpers
- **Fix:** Added helper functions for conflict detection and string manipulation
- **Files modified:** `completion_handler.go`

## Verification

```bash
# All StatusSync tests pass
go test ./internal/service/downloader/... -v -run StatusSync

# All CompletionHandler tests pass
go test ./internal/service/downloader/... -v -run Completion

# All RetryService tests pass
go test ./internal/service/downloader/... -v -run Retry

# All downloader tests pass
go test ./internal/service/downloader/... -v
```

## Acceptance Criteria Verification

| Criteria | Status | Evidence |
|----------|--------|----------|
| StatusSync interface exists | PASS | `grep -n "type StatusSync interface" internal/service/downloader/status_sync.go` |
| NewStatusSync constructor | PASS | `grep -n "func NewStatusSync" internal/service/downloader/status_sync.go` |
| CompletionHandler interface | PASS | `grep -n "type CompletionHandler interface" internal/service/downloader/completion_handler.go` |
| NewCompletionHandler constructor | PASS | `grep -n "func NewCompletionHandler" internal/service/downloader/completion_handler.go` |
| ProcessRetries method | PASS | `grep -n "func (s \*RetryService) ProcessRetries" internal/service/downloader/retry.go` |
| All tests pass | PASS | `go test ./internal/service/downloader/... -v` exits 0 |

## Commits

| Hash | Message |
|------|---------|
| ef40c51 | feat(02-03): create StatusSync service with Sync, UpdateStatus, and Reconcile methods |
| 4a90c47 | feat(02-03): create CompletionHandler service with HandleComplete method |
| 13aa4ea | feat(02-03): extend RetryService with ProcessRetries method |

## Lines Extracted from monitor.go

| Function(s) | Lines | Destination |
|-------------|-------|-------------|
| updateDownloadStatus + mapQBStateToStatus | ~50 | status_sync.go |
| handleDownloadComplete + sendFailedNotification | ~100 | completion_handler.go |
| reconcileMissingDownloadingTasks | ~64 | status_sync.go |
| processFailedRetries | ~43 | retry.go (ProcessRetries) |
| **Total** | **~257** | **3 services** |

## Next Steps

The extracted services are ready for integration into `DownloadMonitor`. The next plan (02-04) will focus on extracting the remaining components from monitor.go and updating it to use these new services.

---
*Summary created: 2026-04-05*
