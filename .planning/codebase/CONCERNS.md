# Codebase Concerns

**Analysis Date:** 2026-04-05

## Tech Debt

### Unimplemented TODOs

**Disk Monitor Pause/Resume:**
- Issue: `pauseDownloads()` and `resumeDownloads()` in disk monitor are stubs
- Files: `internal/service/disk/monitor.go:520-527`
- Impact: When disk space is critical, downloads are not actually paused despite notification claiming they are
- Fix approach: Implement a flag in scheduler or config that prevents new download additions when disk is critical

**Retry Logic in Download Handler:**
- Issue: `Retry` handler only updates status to "pending" without actual retry mechanism
- Files: `internal/api/handler/download.go:146-147`
- Impact: Failed downloads cannot be manually retried through API
- Fix approach: Integrate with existing `RetryService` used by monitor

**Calendar Downloaded Status:**
- Issue: `IsDownloaded` field is hardcoded to `false`
- Files: `internal/service/calendar/calendar.go:123`
- Impact: Calendar always shows episodes as not downloaded
- Fix approach: Query download repository to check if episode exists

**Plex/Jellyfin Integration:**
- Issue: `isBeingWatched()` always returns `false`
- Files: `internal/service/disk/monitor.go:454-458`
- Impact: Disk cleanup cannot protect currently watching episodes
- Fix approach: Implement API clients for Plex/Jellyfin

### Large File Complexity

**Subscription Handler (2,345 lines):**
- Files: `internal/api/handler/subscription.go`
- Issue: Contains business logic for Bangumi enrichment, file renaming, collection downloads, batch imports
- Impact: Difficult to maintain and test; violates single responsibility principle
- Fix approach: Extract services for rename operations, batch imports, and Bangumi enrichment

**Download Monitor (959 lines):**
- Files: `internal/service/downloader/monitor.go`
- Issue: Handles status updates, renaming, notifications, retry logic, reconciliation
- Impact: Complex state management, hard to unit test
- Fix approach: Split into smaller focused components

**File Organizer (683 lines):**
- Files: `internal/service/organizer/organizer.go`
- Issue: Mixes file watching, parsing, matching, moving logic
- Impact: File operations are complex and error-prone
- Fix approach: Separate parser, matcher, and mover components

## Known Bugs

### Panic in Router Setup

- Issue: `panic(err)` on scheduler start failure
- Files: `internal/api/router/router.go:221`
- Impact: Application crashes if scheduler fails to start
- Trigger: Invalid cron expression or database connection issue during startup
- Workaround: None - application cannot start

### Race Condition in Task Manager

- Issue: `cancelFunc` may be called after task completion
- Files: `internal/service/task/manager.go:115-160`
- Impact: Potential nil pointer dereference
- Trigger: Task completes naturally while cancel is requested

### File Move Without Transaction

- Issue: File operations in organizer are not atomic with database updates
- Files: `internal/service/organizer/organizer.go:536-567`
- Impact: Files may be moved but database not updated, or vice versa
- Trigger: System crash during file move operation

## Security Considerations

### SQL Injection Risk

- Risk: Raw SQL not used, but dynamic queries in repositories could be vulnerable
- Files: `internal/repository/*.go`
- Current mitigation: Uses GORM ORM which provides parameterization
- Recommendations: Audit all `Where()` clauses for user input

### Path Traversal

- Risk: File paths constructed from user input without sanitization
- Files: `internal/api/handler/subscription.go`, `internal/service/organizer/organizer.go`
- Current mitigation: Some sanitization in `sanitizeDirectoryName()`
- Recommendations: Validate all paths against whitelist, prevent `../` sequences

### No Authentication

- Risk: API endpoints have no authentication
- Files: `internal/api/router/router.go`
- Current mitigation: None
- Impact: Anyone with network access can manage downloads and subscriptions
- Recommendations: Add JWT or session-based authentication

## Performance Bottlenecks

### Database N+1 Queries

- Problem: Subscription list loads downloading count with individual queries per subscription
- Files: `internal/api/handler/subscription.go:1041-1063`
- Cause: Loop queries download repository for each subscription
- Improvement path: Use JOIN query or cache counts

### Synchronous File Operations

- Problem: File copy in organizer blocks goroutine
- Files: `internal/service/organizer/organizer.go:570-595`
- Cause: `copyFile()` uses blocking I/O
- Improvement path: Use buffered copy or io.Copy with progress callback

### RSS Parser Timeout

- Problem: Hardcoded 30-second timeout may not be sufficient for slow RSS feeds
- Files: `internal/service/rss/parser.go:97`
- Cause: `context.WithTimeout(context.Background(), 30*time.Second)`
- Improvement path: Make timeout configurable per RSS source

### Large File List Queries

- Problem: Downloads list queries may return thousands of records
- Files: `internal/repository/download.go`
- Cause: No upper limit on `List()` queries
- Improvement path: Enforce maximum page size

## Fragile Areas

### qBittorrent Integration

- Files: `internal/service/downloader/qbittorrent.go`
- Why fragile: Depends on qBittorrent Web API which may change; no versioning check
- Safe modification: Add API version detection and compatibility layer
- Test coverage: Limited - only extract tests exist

### Bangumi API Dependency

- Files: `internal/service/bangumi/bangumi.go`
- Why fragile: External API may be unavailable or rate-limited
- Safe modification: Add circuit breaker and caching
- Test coverage: Season parsing has tests, API client does not

### File Watcher

- Files: `internal/service/organizer/organizer.go`
- Why fragile: fsnotify may miss events on high-volume changes; recursive watch may fail on deep directories
- Safe modification: Add periodic full-scan fallback
- Test coverage: None

### Transaction Management

- Files: `internal/service/scheduler/scheduler.go:453-467`
- Why fragile: Manual transaction handling with early returns may leave transactions open
- Safe modification: Use deferred rollback/commit pattern consistently

## Scaling Limits

### SQLite Concurrency

- Current capacity: Single writer, multiple readers
- Limit: Will block under high concurrent write load
- Scaling path: Migrate to PostgreSQL or add connection pooling

### Single Task Manager

- Current capacity: 1 concurrent task
- Limit: `internal/service/task/manager.go:97-100` rejects new tasks if one is running
- Scaling path: Implement task queue with worker pool

### Memory Usage for Large RSS Feeds

- Current capacity: Loads all RSS items into memory
- Limit: `internal/service/scheduler/scheduler.go:191` parses entire feed
- Scaling path: Stream parse or paginate RSS processing

## Dependencies at Risk

### gorilla/websocket

- Risk: Project maintenance status unclear
- Impact: WebSocket notifications would break
- Migration plan: Consider `nhooyr/websocket` or stdlib with gorilla patterns

### robfig/cron/v3

- Risk: Stable but v3 is last major version
- Impact: Scheduler functionality
- Migration plan: Monitor for v4 or fork if needed

### mmcdole/gofeed

- Risk: RSS parsing library may not support all feed formats
- Impact: Some RSS feeds may fail to parse
- Migration plan: Add fallback parsers or contribute fixes upstream

## Missing Critical Features

### Proper Retry Mechanism

- Problem: Download retry only marks status as pending; no actual re-download
- Files: `internal/api/handler/download.go:146`
- Blocks: Users cannot retry failed downloads manually

### Batch Collection Limit

- Problem: Recent commit added limit but implementation may not be robust
- Impact: Large batch imports may still overwhelm the system

### WebSocket Reconnection

- Problem: Frontend WebSocket client may not auto-reconnect
- Impact: Users miss notifications after connection drop

## Test Coverage Gaps

### Handler Layer

- What's not tested: Most HTTP handlers have no tests
- Files: `internal/api/handler/*.go` (except `*_test.go` files)
- Risk: API changes may break without detection
- Priority: High

### Service Layer Integration

- What's not tested: External API integrations (Bangumi, qBittorrent, Mikan)
- Files: `internal/service/bangumi/bangumi.go`, `internal/service/mikan/mikan.go`
- Risk: API changes break functionality
- Priority: Medium

### File Operations

- What's not tested: File organizer move/copy operations
- Files: `internal/service/organizer/organizer.go`
- Risk: File corruption or data loss
- Priority: High

### Concurrent Operations

- What's not tested: Race conditions in task manager, notification service
- Files: `internal/service/task/manager.go`, `internal/service/notification/*.go`
- Risk: Deadlocks or data races under load
- Priority: Medium

## Code Quality Issues

### Inconsistent Error Handling

- Issue: Some errors logged and swallowed, others returned
- Files: Throughout codebase
- Example: `internal/api/handler/subscription.go:1098-1102` logs but doesn't return some errors

### Magic Numbers

- Issue: Hardcoded values without constants
- Files: `internal/service/disk/monitor.go:26-28` (thresholds), `internal/service/organizer/organizer.go:59-60` (match score, stabilize time)

### Commented Code

- Issue: Some debug code left commented
- Files: Various locations

---

*Concerns audit: 2026-04-05*
