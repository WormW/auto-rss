---
plan: 01-01-fix-retry-logic
phase: 01-bug
status: completed
completed_at: "2025-04-05"
---

# Summary: Fix Retry Logic

## What Was Built

Implemented complete retry functionality for `DownloadHandler.Retry` endpoint:

1. **Enhanced DownloadHandler**: Added `configRepo` field to access download path configuration
2. **Complete Retry Flow**:
   - Delete old torrent from qBittorrent (if exists)
   - Reset retry count, reason, next retry time, last error, and torrent hash
   - Save updated download state to database
   - Re-add torrent to qBittorrent with proper download path (including anime name subdirectory)
3. **Comprehensive Tests**: Added `download_retry_test.go` with tests for successful retry, missing download, and qBittorrent errors

## Key Changes

- `internal/api/handler/download.go`: Complete Retry handler implementation
- `internal/api/handler/download_retry_test.go`: New test file with retry scenarios
- `internal/api/handler/download_status_test.go`: Updated for new handler signature
- `internal/api/router/router.go`: Updated to pass configRepo to DownloadHandler

## Self-Check: PASSED

- All tasks completed
- Code committed
- Tests added and passing
