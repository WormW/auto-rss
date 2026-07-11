# Reduce Health Diagnostics Disk Pressure Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove implicit media-disk access from normal diagnostics and eliminate routine SQLite maintenance caused by health probes and per-log cleanup.

**Architecture:** Keep the existing diagnostics API schema but derive file counters from recorded database paths. Centralize request-log exclusions in the HTTP logger middleware, switch container liveness to `/live`, and add a concurrency-safe hourly cleanup gate inside `DBWriter`.

**Tech Stack:** Go, Gin, GORM, SQLite, testify, Docker Compose.

---

## File Map

- Modify `internal/api/handler/subscription_diagnostics.go`: replace physical file existence checks with recorded-path checks and update summaries.
- Modify `internal/api/handler/subscription_diagnostics_test.go`: cover database-only file diagnostics semantics.
- Modify `web/src/views/Subscriptions.vue`: change the metric label from physical files to recorded paths.
- Modify `internal/api/middleware/logger.go`: skip routine probe request logs.
- Create `internal/api/middleware/logger_test.go`: test excluded and included paths.
- Modify `internal/pkg/logger/db_writer.go`: throttle cleanup with a concurrency-safe time gate and only schedule it after a successful insert.
- Create `internal/pkg/logger/db_writer_test.go`: test cleanup gate timing and concurrency.
- Modify `Dockerfile` and `docker-compose.yml`: use `/live` for liveness probes.

### Task 1: Make Subscription Diagnostics Database-Only

**Files:**
- Modify: `internal/api/handler/subscription_diagnostics_test.go`
- Modify: `internal/api/handler/subscription_diagnostics.go`
- Modify: `web/src/views/Subscriptions.vue`

- [ ] **Step 1: Write the failing integration test**

Add a completed download whose `FilePath` is a deliberately nonexistent absolute path. Request diagnostics and assert `CompletedWithFile == 1`, `CompletedMissingFile == 0`, and the file check text describes a recorded path rather than a confirmed file.

```go
require.NoError(t, downloadRepo.Create(&model.Download{
    SubscriptionID: sub.ID,
    Title:          "Test Anime 03",
    Episode:        3,
    FilePath:       "/path-that-must-not-be-statted/test-anime-03.mkv",
    Status:         model.DownloadStatusCompleted,
}))

require.Equal(t, 1, resp.Data.Files.CompletedWithFile)
require.Equal(t, 0, resp.Data.Files.CompletedMissingFile)
require.Contains(t, fileCheck.Summary, "记录")
```

- [ ] **Step 2: Run the focused test and verify RED**

Run: `go test ./internal/api/handler -run TestSubscriptionDiagnosticsHandler_GetAggregatesHealth -count=1`

Expected: FAIL because the current implementation calls `os.Stat` and treats the nonexistent path as missing.

- [ ] **Step 3: Implement the database-only path check**

Replace `downloadHasExistingFile` with a helper that only checks whether `RenamedPath` or `FilePath` is non-empty. Update summaries and details so they say paths are recorded and direct users to **Scan local files** for physical verification.

```go
func downloadHasRecordedFilePath(download model.Download) bool {
    return strings.TrimSpace(download.RenamedPath) != "" || strings.TrimSpace(download.FilePath) != ""
}
```

- [ ] **Step 4: Update the frontend metric wording**

Change the diagnostics metric label from `本地文件` to `已记录路径` and change the secondary label from `缺路径` to `未记录路径`.

- [ ] **Step 5: Run focused tests and verify GREEN**

Run: `go test ./internal/api/handler -run 'TestSubscriptionDiagnosticsHandler_(GetAggregatesHealth|RetryFailedResetsRetryableDownloads)' -count=1`

Expected: PASS.

### Task 2: Stop Probe Requests From Persisting Logs

**Files:**
- Create: `internal/api/middleware/logger_test.go`
- Modify: `internal/api/middleware/logger.go`
- Modify: `Dockerfile`
- Modify: `docker-compose.yml`

- [ ] **Step 1: Write the failing path-exclusion test**

Add a table test for a wished-for `shouldSkipRequestLog` helper.

```go
func TestShouldSkipRequestLog(t *testing.T) {
    tests := map[string]bool{
        "/health": true,
        "/api/v1/health": true,
        "/ready": true,
        "/live": true,
        "/api/v1/logs": true,
        "/api/v1/logs/clear": true,
        "/api/v1/subscriptions": false,
    }
    for path, expected := range tests {
        require.Equal(t, expected, shouldSkipRequestLog(path), path)
    }
}
```

- [ ] **Step 2: Run the focused test and verify RED**

Run: `go test ./internal/api/middleware -run TestShouldSkipRequestLog -count=1`

Expected: build FAIL because `shouldSkipRequestLog` does not exist.

- [ ] **Step 3: Implement and use the exclusion helper**

Add the helper and call it after `c.Next()` before emitting `logger.Info`.

```go
func shouldSkipRequestLog(path string) bool {
    switch path {
    case "/health", "/api/v1/health", "/ready", "/live", "/api/v1/logs", "/api/v1/logs/clear":
        return true
    default:
        return false
    }
}
```

- [ ] **Step 4: Switch container probes to `/live`**

Update both healthcheck commands from `http://localhost:7892/health` to `http://localhost:7892/live`.

- [ ] **Step 5: Run middleware tests and verify GREEN**

Run: `go test ./internal/api/middleware/... -count=1`

Expected: PASS.

### Task 3: Throttle Database Log Cleanup

**Files:**
- Create: `internal/pkg/logger/db_writer_test.go`
- Modify: `internal/pkg/logger/db_writer.go`

- [ ] **Step 1: Write failing cleanup-gate tests**

Test a wished-for `reserveCleanup` method with a configurable interval and clock. Verify the first call succeeds, a second call within the hour is rejected, a call after the hour succeeds, and concurrent calls at one instant produce exactly one winner.

```go
writer := &DBWriter{cleanupInterval: time.Hour}
now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
require.True(t, writer.reserveCleanup(now))
require.False(t, writer.reserveCleanup(now.Add(30*time.Minute)))
require.True(t, writer.reserveCleanup(now.Add(time.Hour)))
```

- [ ] **Step 2: Run logger tests and verify RED**

Run: `go test ./internal/pkg/logger -run 'TestDBWriterReserveCleanup' -count=1`

Expected: build FAIL because cleanup gate fields and `reserveCleanup` do not exist.

- [ ] **Step 3: Implement the concurrency-safe gate**

Add `sync.Mutex`, `lastCleanup`, and `cleanupInterval` to `DBWriter`. Default the interval to one hour in `NewDBWriter`.

```go
func (w *DBWriter) reserveCleanup(now time.Time) bool {
    w.cleanupMu.Lock()
    defer w.cleanupMu.Unlock()
    if !w.lastCleanup.IsZero() && now.Sub(w.lastCleanup) < w.cleanupInterval {
        return false
    }
    w.lastCleanup = now
    return true
}
```

- [ ] **Step 4: Gate cleanup after successful insertion**

In the existing asynchronous goroutine, return immediately when `Create` fails. Run cleanup only when `reserveCleanup(time.Now())` succeeds.

```go
go func() {
    if err := w.db.Create(log).Error; err != nil {
        return
    }
    if w.reserveCleanup(time.Now()) {
        w.cleanupOldLogs()
    }
}()
```

- [ ] **Step 5: Run logger tests and verify GREEN**

Run: `go test ./internal/pkg/logger -count=1`

Expected: PASS, including the concurrent single-winner assertion.

### Task 4: Format and Verify the Complete Change

**Files:**
- Modify only files listed above through formatting.

- [ ] **Step 1: Format Go files**

Run: `gofmt -w internal/api/handler/subscription_diagnostics.go internal/api/handler/subscription_diagnostics_test.go internal/api/middleware/logger.go internal/api/middleware/logger_test.go internal/pkg/logger/db_writer.go internal/pkg/logger/db_writer_test.go`

Expected: command exits 0.

- [ ] **Step 2: Run focused regression tests**

Run: `go test ./internal/api/handler ./internal/api/middleware/... ./internal/pkg/logger -count=1`

Expected: PASS.

- [ ] **Step 3: Run the complete Go test suite**

Run: `go test ./... -count=1`

Expected: PASS.

- [ ] **Step 4: Verify the frontend build**

Run: `npm run build --prefix web`

Expected: Vite production build exits 0.

- [ ] **Step 5: Inspect the final diff**

Run: `git diff --check && git status --short && git diff --stat`

Expected: no whitespace errors; only the plan and intended implementation files are changed.

