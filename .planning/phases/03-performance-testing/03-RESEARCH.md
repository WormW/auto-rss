# Phase 3: 性能与测试 - Research

**Researched:** 2025-04-05
**Domain:** Go Testing, GORM Performance, Concurrent Programming
**Confidence:** HIGH

## Summary

This phase focuses on fixing performance bottlenecks and adding comprehensive test coverage to the Auto-RSS codebase. The key challenges include:

1. **N+1 Query Fix**: The subscription list handler currently performs O(N) queries to count downloading items per subscription. This needs to be optimized to a constant number of queries using GORM Preload or JOIN-based aggregation.

2. **Pagination Safety**: List queries currently lack maximum page size limits, risking memory exhaustion with large datasets.

3. **Configurable RSS Timeout**: The RSS parser has a hardcoded 30-second timeout that should be configurable per source.

4. **Test Coverage**: Handler layer needs 60%+ coverage, Organizer needs 70%+, and Task Manager needs concurrent safety testing.

**Primary recommendation:** Use GORM's Preload with a custom counting query for N+1 fix, implement pagination limits in repository layer, add timeout field to RSSSource model, and use table-driven tests with SQLite in-memory database for handler testing.

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| PERF-01 | Fix N+1 query in subscription list (lines 1041-1063) | Use GORM Preload + Count aggregation |
| PERF-02 | Add pagination limits to List queries | Implement max page size (1000) in repository layer |
| PERF-03 | Make RSS timeout configurable per source | Add Timeout field to RSSSource model, default 30s |
| TEST-01 | Handler tests for download handler | Use httptest + gin test mode + mock repository |
| TEST-02 | Handler tests for subscription handler | Use SQLite in-memory + table-driven tests |
| TEST-03 | Bangumi service tests with httpmock | Use httptest.NewServer for API mocking |
| TEST-04 | Mikan service tests with httpmock | Use httptest.NewServer for HTML mocking |
| TEST-05 | Organizer tests with temp directories | Use t.TempDir() + os.CreateTemp for file operations |
| TEST-06 | Task manager concurrent safety tests | Use sync.WaitGroup + go test -race + atomic operations |

## Standard Stack

### Core Testing Libraries
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| testing | built-in | Go's native testing framework | Zero dependencies, full IDE support |
| testify | v1.9.0 | Assertions and test utilities | Industry standard, clean syntax |
| httptest | built-in | HTTP handler mocking | Native Go, no external deps |
| sqlite | v1.5.5 | In-memory test database | Fast, isolated, no setup |

### Supporting Libraries
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| sync/atomic | built-in | Lock-free counters | Concurrent test coordination |
| sync.WaitGroup | built-in | Goroutine synchronization | Testing concurrent operations |
| t.Parallel() | built-in | Parallel test execution | Independent test cases |

### Installation
```bash
# testify is already in go.mod
go get github.com/stretchr/testify@v1.9.0
```

**Version verification:**
- testify v1.9.0 - verified in go.mod [VERIFIED: go.mod]
- GORM v1.25.7 - verified in go.mod [VERIFIED: go.mod]
- Gin v1.10.0 - verified in go.mod [VERIFIED: go.mod]

## Architecture Patterns

### Recommended Test Structure
```
internal/
├── api/handler/
│   ├── download_test.go          # Handler tests (TEST-01)
│   ├── subscription_test.go      # Handler tests (TEST-02)
│   └── download_status_test.go   # Existing (reference)
├── service/
│   ├── bangumi/
│   │   └── bangumi_test.go       # Service tests (TEST-03)
│   ├── mikan/
│   │   └── mikan_test.go         # Service tests (TEST-04)
│   ├── organizer/
│   │   ├── parser_test.go        # Existing (reference)
│   │   ├── matcher_test.go       # Existing (reference)
│   │   ├── mover_test.go         # Existing (reference)
│   │   └── organizer_test.go     # Integration tests (TEST-05)
│   └── task/
│       └── manager_test.go       # Concurrent tests (TEST-06)
```

### Pattern 1: Table-Driven Tests
**What:** Define test cases as a slice of structs, iterate with t.Run()
**When to use:** Multiple similar test scenarios with different inputs/outputs
**Example:**
```go
// Source: Go Wiki TableDrivenTests + existing codebase patterns
func TestHandlerList(t *testing.T) {
    tests := []struct {
        name       string
        status     string
        wantStatus int
        wantCount  int
    }{
        {"all downloads", "", 200, 3},
        {"downloading only", "downloading", 200, 1},
        {"invalid status", "invalid", 200, 0},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // test implementation
        })
    }
}
```

### Pattern 2: Mock Repository Pattern
**What:** Create mock implementations of repository interfaces for testing
**When to use:** Unit testing handlers without database dependencies
**Example:**
```go
// Source: internal/service/downloader/retry_test.go (existing pattern)
type mockDownloadRepo struct {
    listFunc func(offset, limit int, status string) ([]model.Download, int64, error)
}

func (m *mockDownloadRepo) List(offset, limit int, status string) ([]model.Download, int64, error) {
    return m.listFunc(offset, limit, status)
}
// ... implement other methods
```

### Pattern 3: SQLite In-Memory Database
**What:** Use SQLite with ":memory:" or "file::memory:?cache=shared" for isolated tests
**When to use:** Integration tests requiring real database behavior
**Example:**
```go
// Source: internal/api/handler/download_status_test.go (existing pattern)
db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
if err != nil {
    t.Fatalf("failed to open test DB: %v", err)
}
if err := db.AutoMigrate(&model.Subscription{}, &model.Download{}); err != nil {
    t.Fatalf("failed to migrate test DB: %v", err)
}
```

### Pattern 4: HTTP Mock Server
**What:** Use httptest.NewServer to mock external APIs
**When to use:** Testing services that call external HTTP APIs
**Example:**
```go
// Source: Go documentation + web search findings
server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)
    w.Write([]byte(`{"id": 123, "name": "Test Anime"}`))
}))
defer server.Close()

// Use server.URL in your service
```

### Pattern 5: Concurrent Test with Race Detection
**What:** Use sync.WaitGroup, goroutines, and go test -race
**When to use:** Testing thread-safe operations
**Example:**
```go
// Source: Go testing best practices 2025
func TestTaskManager_ConcurrentAccess(t *testing.T) {
    manager := task.GetManager()
    var wg sync.WaitGroup
    
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            manager.IsRunning()
        }()
    }
    
    wg.Wait()
}
```

### Anti-Patterns to Avoid
- **Don't** use real external APIs in tests (flaky, slow)
- **Don't** share database connections between tests (isolation issues)
- **Don't** write tests that depend on execution order
- **Don't** use sleep-based synchronization in concurrent tests
- **Don't** test private functions directly (test public API behavior)

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| HTTP mocking | Custom transport | `httptest.NewServer` | Standard, battle-tested, clean |
| Database mocking | Mock SQL driver | SQLite in-memory | Real SQL behavior, fast enough |
| Test assertions | Custom helpers | `testify/assert` | Rich assertions, good error messages |
| JSON comparison | string comparison | `assert.JSONEq` | Handles key ordering, formatting |
| Temp file cleanup | manual os.Remove | `t.TempDir()` | Auto-cleanup even on panic |
| Concurrent test sync | time.Sleep | `sync.WaitGroup` | Deterministic, race-detector friendly |

**Key insight:** Go's standard library plus testify covers 95% of testing needs. Custom test frameworks add complexity without value.

## Common Pitfalls

### Pitfall 1: N+1 Query in Subscription List
**What goes wrong:** The current code (subscription.go:1041-1063) loops through subscriptions and queries downloads for each one.
**Why it happens:** Simple implementation without considering query efficiency.
**How to avoid:** Use GORM Preload with a custom counting query or JOIN-based aggregation.
**Warning signs:** Slow API response with many subscriptions, database CPU spikes.

**Solution pattern:**
```go
// Instead of looping queries, use a single aggregation query
type SubscriptionWithCount struct {
    model.Subscription
    DownloadingCount int64 `gorm:"column:downloading_count"`
}

var results []SubscriptionWithCount
db.Model(&model.Subscription{}).
    Select("subscriptions.*, COUNT(CASE WHEN downloads.status = 'downloading' THEN 1 END) as downloading_count").
    Joins("LEFT JOIN downloads ON downloads.subscription_id = subscriptions.id").
    Group("subscriptions.id").
    Find(&results)
```

### Pitfall 2: Unbounded Pagination
**What goes wrong:** List queries accept any page size, risking OOM with large datasets.
**Why it happens:** No validation on limit parameter.
**How to avoid:** Enforce maximum page size (1000) in repository layer.
**Warning signs:** API timeouts, memory errors in logs.

**Solution pattern:**
```go
const MaxPageSize = 1000

func (r *repository) List(offset, limit int) {
    if limit > MaxPageSize {
        limit = MaxPageSize
    }
    // ... rest of query
}
```

### Pitfall 3: Race Conditions in Task Manager
**What goes wrong:** Concurrent access to shared state without proper synchronization.
**Why it happens:** Missing locks or incorrect lock ordering.
**How to avoid:** Use sync.RWMutex consistently, run tests with -race flag.
**Warning signs:** Intermittent test failures, data corruption.

### Pitfall 4: Test Pollution
**What goes wrong:** Tests share state and fail when run in different orders.
**Why it happens:** Global state, shared database connections, file system state.
**How to avoid:** Use t.TempDir(), in-memory databases, reset global state in cleanup.

### Pitfall 5: Hardcoded Timeouts
**What goes wrong:** RSS parser has hardcoded 30s timeout, not suitable for all sources.
**Why it happens:** One-size-fits-all approach doesn't account for slow sources.
**How to avoid:** Make timeout configurable per source with sensible defaults.

## Code Examples

### N+1 Query Fix (PERF-01)
```go
// Source: GORM documentation + web research
// File: internal/repository/subscription.go

// GetSubscriptionsWithDownloadCount returns all subscriptions with downloading counts in a single query
func (r *subscriptionRepository) GetSubscriptionsWithDownloadCount() ([]SubscriptionWithStats, error) {
    var results []SubscriptionWithStats
    
    err := r.db.Model(&model.Subscription{}).
        Select(
            "subscriptions.*",
            "COUNT(CASE WHEN downloads.status = ? THEN 1 END) as downloading_count",
            "downloading",
        ).
        Joins("LEFT JOIN downloads ON downloads.subscription_id = subscriptions.id AND downloads.status = ?", "downloading").
        Group("subscriptions.id").
        Find(&results).Error
    
    return results, err
}

type SubscriptionWithStats struct {
    model.Subscription
    DownloadingCount int64 `gorm:"column:downloading_count" json:"downloading_count"`
}
```

### Pagination Limit (PERF-02)
```go
// Source: Existing handler pattern + safety enhancement
// File: internal/repository/download.go

const MaxPageSize = 1000
const DefaultPageSize = 20

func (r *downloadRepository) List(offset, limit int, status string) ([]model.Download, int64, error) {
    // Enforce pagination limits
    if limit <= 0 {
        limit = DefaultPageSize
    }
    if limit > MaxPageSize {
        limit = MaxPageSize
    }
    if offset < 0 {
        offset = 0
    }
    
    // ... rest of query
}
```

### Configurable RSS Timeout (PERF-03)
```go
// Source: Existing model pattern
// File: internal/model/rss_source.go

type RSSSource struct {
    ID          uint          `json:"id" gorm:"primaryKey"`
    Name        string        `json:"name" gorm:"not null;index"`
    BaseURL     string        `json:"base_url" gorm:"not null"`
    Description string        `json:"description" gorm:"type:text"`
    Enabled     bool          `json:"enabled" gorm:"default:true;index"`
    Timeout     time.Duration `json:"timeout" gorm:"default:30000000000"` // 30s in nanoseconds
    CreatedAt   time.Time     `json:"created_at"`
    UpdatedAt   time.Time     `json:"updated_at"`
}

// File: internal/service/rss/parser.go
func (p *parser) FetchAndParseWithTimeout(rssURL string, timeout time.Duration) ([]RSSItem, error) {
    if timeout == 0 {
        timeout = 30 * time.Second // default
    }
    
    ctx, cancel := context.WithTimeout(context.Background(), timeout)
    defer cancel()
    
    // ... use ctx for HTTP request
}
```

### Handler Test with Mock Repository (TEST-01)
```go
// Source: Existing test patterns + httptest best practices
// File: internal/api/handler/download_test.go

func TestDownloadHandler_List(t *testing.T) {
    tests := []struct {
        name           string
        query          string
        mockList       func() ([]model.Download, int64, error)
        wantStatus     int
        wantCount      int
        wantTotal      int64
    }{
        {
            name:   "success with default pagination",
            query:  "",
            mockList: func() ([]model.Download, int64, error) {
                return []model.Download{
                    {ID: 1, Title: "Test 1", Status: "downloading"},
                    {ID: 2, Title: "Test 2", Status: "completed"},
                }, 2, nil
            },
            wantStatus: 200,
            wantCount:  2,
            wantTotal:  2,
        },
        {
            name:   "filter by status",
            query:  "?status=downloading",
            mockList: func() ([]model.Download, int64, error) {
                return []model.Download{
                    {ID: 1, Title: "Test 1", Status: "downloading"},
                }, 1, nil
            },
            wantStatus: 200,
            wantCount:  1,
            wantTotal:  1,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Setup mock
            mockRepo := &mockDownloadRepo{listFunc: tt.mockList}
            handler := NewDownloadHandler(mockRepo, nil, nil)
            
            // Setup gin
            gin.SetMode(gin.TestMode)
            r := gin.New()
            r.GET("/downloads", handler.List)
            
            // Create request
            req := httptest.NewRequest("GET", "/downloads"+tt.query, nil)
            w := httptest.NewRecorder()
            
            // Execute
            r.ServeHTTP(w, req)
            
            // Assert
            assert.Equal(t, tt.wantStatus, w.Code)
            
            var resp struct {
                Code int `json:"code"`
                Data struct {
                    List  []model.Download `json:"list"`
                    Total int64            `json:"total"`
                } `json:"data"`
            }
            err := json.Unmarshal(w.Body.Bytes(), &resp)
            require.NoError(t, err)
            assert.Equal(t, tt.wantCount, len(resp.Data.List))
            assert.Equal(t, tt.wantTotal, resp.Data.Total)
        })
    }
}
```

### Concurrent Test for Task Manager (TEST-06)
```go
// Source: Go concurrent testing best practices 2025
// File: internal/service/task/manager_test.go

func TestManager_ConcurrentAccess(t *testing.T) {
    // Reset singleton for test isolation
    instance = nil
    once = sync.Once{}
    
    manager := GetManager()
    
    t.Run("concurrent status checks", func(t *testing.T) {
        var wg sync.WaitGroup
        
        for i := 0; i < 100; i++ {
            wg.Add(1)
            go func() {
                defer wg.Done()
                _ = manager.IsRunning()
                _ = manager.GetCurrentTask()
                _ = manager.GetTaskHistory()
            }()
        }
        
        wg.Wait()
    })
    
    t.Run("concurrent start and cancel", func(t *testing.T) {
        var wg sync.WaitGroup
        
        for i := 0; i < 50; i++ {
            wg.Add(1)
            go func(id int) {
                defer wg.Done()
                
                taskName := fmt.Sprintf("test-task-%d", id)
                _, _ = manager.StartTask(TaskTypeCollect, uint(id), taskName, func(ctx context.Context, t *Task) error {
                    time.Sleep(10 * time.Millisecond)
                    return nil
                })
            }(i)
        }
        
        // Concurrent cancels
        for i := 0; i < 50; i++ {
            wg.Add(1)
            go func() {
                defer wg.Done()
                _ = manager.CancelTask()
            }()
        }
        
        wg.Wait()
    })
}
```

### Organizer Test with Temp Directory (TEST-05)
```go
// Source: Go testing best practices + existing parser_test.go patterns
// File: internal/service/organizer/organizer_test.go

func TestFileOrganizer_OrganizeFile(t *testing.T) {
    // Create temp directories
    watchDir := t.TempDir()
    destDir := t.TempDir()
    
    // Setup in-memory database
    db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
    require.NoError(t, err)
    require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}))
    
    // Create test file
    testFile := filepath.Join(watchDir, "[Group] Anime Title - 01 [1080p].mkv")
    require.NoError(t, os.WriteFile(testFile, []byte("test content"), 0644))
    
    // Setup organizer with mocks
    downloadRepo := repository.NewDownloadRepository(db)
    subRepo := repository.NewSubscriptionRepository(db)
    
    organizer, err := NewFileOrganizer(watchDir, destDir, subRepo, downloadRepo, db, nil, "")
    require.NoError(t, err)
    
    // Create subscription
    sub := &model.Subscription{Name: "Anime Title", Season: 1}
    require.NoError(t, subRepo.Create(sub))
    
    // Test organize
    err = organizer.organizeFile(testFile)
    require.NoError(t, err)
    
    // Verify file was moved
    expectedPath := filepath.Join(destDir, "Anime Title", "Season 1", "Anime Title S01E01.mkv")
    _, err = os.Stat(expectedPath)
    assert.NoError(t, err, "file should be moved to destination")
    
    // Verify download record updated
    downloads, err := downloadRepo.ListBySubscriptionID(sub.ID)
    require.NoError(t, err)
    assert.Len(t, downloads, 1)
    assert.Equal(t, model.DownloadStatusCompleted, downloads[0].Status)
    assert.Equal(t, expectedPath, downloads[0].FilePath)
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| GORM v1 | GORM v2 | 2020 | Better performance, cleaner API |
| govendor | Go modules | 2019 | Better dependency management |
| testify v1.8 | testify v1.9 | 2024 | New assertions, bug fixes |
| Go 1.21 | Go 1.23 | 2024 | Built-in slices, maps packages |

**Deprecated/outdated:**
- `github.com/golang/mock`: Use `go.uber.org/mock` or hand-written mocks instead
- `goconvey`: Complex, hard to maintain; prefer standard testing

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | SQLite in-memory is fast enough for handler tests | Standard Stack | If too slow, tests may timeout in CI |
| A2 | GORM Preload works correctly with SQLite | N+1 Query Fix | May need raw SQL fallback |
| A3 | 1000 is appropriate max page size | Pagination Limit | May need tuning based on production data |
| A4 | 30s default timeout is suitable for most RSS sources | RSS Timeout | Some sources may need longer/shorter |

## Open Questions

1. **RSS Source Timeout Migration**
   - What we know: RSSSource model needs Timeout field
   - What's unclear: Should existing sources get default timeout or preserve behavior?
   - Recommendation: Migration sets default 30s for existing sources

2. **Test Database Strategy**
   - What we know: SQLite in-memory works for unit tests
   - What's unclear: Should we use testcontainers for integration tests?
   - Recommendation: SQLite for unit tests, consider testcontainers for CI integration tests

3. **Coverage Measurement Baseline**
   - What we know: Current coverage is unknown
   - What's unclear: What's the current baseline before improvements?
   - Recommendation: Run `go test -cover` before starting to establish baseline

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go | All tests | ✓ | 1.23.0 | — |
| SQLite | Repository tests | ✓ | 3.x (via mattn/go-sqlite3) | — |
| testify | Assertions | ✓ | v1.9.0 | Use t.Errorf |
| Race detector | Concurrent tests | ✓ | Built-in | Manual review |

**Missing dependencies with no fallback:** None

**Missing dependencies with fallback:** None

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go testing + testify |
| Config file | go.mod (dependencies) |
| Quick run command | `go test -v ./internal/api/handler/...` |
| Full suite command | `go test -v -race -coverprofile=coverage.out ./...` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| PERF-01 | N+1 query eliminated | integration | `go test -v ./internal/repository/...` | ❌ Wave 0 |
| PERF-02 | Pagination limits enforced | unit | `go test -v ./internal/repository/...` | ❌ Wave 0 |
| PERF-03 | RSS timeout configurable | unit | `go test -v ./internal/service/rss/...` | ❌ Wave 0 |
| TEST-01 | Download handler coverage | unit | `go test -v ./internal/api/handler/... -run TestDownload` | ❌ Wave 0 |
| TEST-02 | Subscription handler coverage | unit | `go test -v ./internal/api/handler/... -run TestSubscription` | ❌ Wave 0 |
| TEST-03 | Bangumi service with httpmock | unit | `go test -v ./internal/service/bangumi/...` | ❌ Wave 0 |
| TEST-04 | Mikan service with httpmock | unit | `go test -v ./internal/service/mikan/...` | ❌ Wave 0 |
| TEST-05 | Organizer file operations | integration | `go test -v ./internal/service/organizer/...` | ❌ Wave 0 |
| TEST-06 | Task manager concurrency | unit | `go test -v -race ./internal/service/task/...` | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go test -v ./path/to/package -run TestName`
- **Per wave merge:** `go test -v -race ./...`
- **Phase gate:** Full suite green before `/gsd-verify-work`

### Wave 0 Gaps
- [ ] `internal/api/handler/download_test.go` — covers TEST-01
- [ ] `internal/api/handler/subscription_test.go` — covers TEST-02
- [ ] `internal/service/bangumi/bangumi_test.go` — covers TEST-03
- [ ] `internal/service/mikan/mikan_test.go` — covers TEST-04
- [ ] `internal/service/organizer/organizer_test.go` — covers TEST-05
- [ ] `internal/service/task/manager_test.go` — covers TEST-06
- [ ] `internal/repository/subscription_nplus1_test.go` — covers PERF-01

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | No | N/A |
| V3 Session Management | No | N/A |
| V4 Access Control | No | N/A |
| V5 Input Validation | Yes | Pagination limits prevent DoS |
| V6 Cryptography | No | N/A |
| V7 Error Handling | Yes | Test error paths, don't leak internals |

### Known Threat Patterns for Go Testing

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Resource exhaustion via large page sizes | Denial of Service | Max page size limit (1000) |
| SQL injection in test data | Tampering | Use parameterized queries (GORM) |
| Test data pollution | Information Disclosure | Isolated in-memory databases |
| Race conditions | Elevation of Privilege | Race detector, proper locking |

## Sources

### Primary (HIGH confidence)
- [Go Wiki: TableDrivenTests](https://go.dev/wiki/TableDrivenTests) - Official Go testing patterns
- [GORM Preload Documentation](https://gorm.io/docs/preload.html) - Official GORM documentation
- [Gin Testing Documentation](https://gin-gonic.com/en/docs/testing/) - Official Gin testing guide
- Existing codebase patterns in `internal/api/handler/download_status_test.go`
- Existing codebase patterns in `internal/service/downloader/retry_test.go`
- Existing codebase patterns in `internal/service/organizer/parser_test.go`

### Secondary (MEDIUM confidence)
- [GORM Preload vs Joins Performance](https://goldlapel.com/how-to/gorm-preload-vs-joins-postgres) - Performance comparison
- [Advanced GORM Techniques](https://leapcell.io/blog/advanced-gorm-techniques-for-efficient-data-handling) - Advanced patterns
- [Go Unit Testing Best Practices 2025](https://www.glukhov.org/post/2025/11/unit-tests-in-go/) - Modern Go testing
- [Testing Go Gin with httptest](https://blog.marcnuri.com/go-testing-gin-gonic-with-httptest) - Handler testing

### Tertiary (LOW confidence)
- GitHub issues referenced in search results (GORM issues #7497, #6988, #6834)
- Web search findings for Go 1.24 concurrent testing features

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - Verified against go.mod and existing tests
- Architecture: HIGH - Based on existing codebase patterns
- Pitfalls: HIGH - Derived from actual code review of subscription.go N+1 issue
- GORM patterns: MEDIUM - Based on documentation and community reports

**Research date:** 2025-04-05
**Valid until:** 2025-07-05 (90 days for stable Go ecosystem)
