# Testing Patterns

**Analysis Date:** 2026-04-05

## Test Framework

**Runner:** Go's built-in `testing` package

**Assertion Library:** `github.com/stretchr/testify` (assert package)

**Run Commands:**
```bash
# Run all tests
make test
# Or directly:
go test -v ./...

# Run specific package tests
go test -v ./internal/repository/

# Run specific test function
go test -v ./internal/repository/ -run TestDownloadRepository_StalledDownloadingFilter
```

## Test File Organization

**Location:** Co-located with source files (same directory)

**Naming:** `{source_file}_test.go`

**Examples:**
- `internal/repository/download.go` -> `internal/repository/download_status_test.go`
- `internal/service/downloader/retry.go` -> `internal/service/downloader/retry_test.go`
- `internal/api/handler/subscription.go` -> `internal/api/handler/subscription_name_normalize_test.go`

## Test Structure

### Table-Driven Tests
All tests use table-driven pattern:

```go
func TestCalculateNextRetryTime(t *testing.T) {
    svc := &RetryService{}

    tests := []struct {
        retryCount int
        minMinutes int64
        maxMinutes int64
    }{
        {0, 1, 2},   // 第0次重试：1分钟后
        {1, 2, 3},   // 第1次重试：2分钟后
        {2, 4, 5},   // 第2次重试：4分钟后
    }

    for _, tt := range tests {
        t.Run(fmt.Sprintf("retry_%d", tt.retryCount), func(t *testing.T) {
            nextTime := svc.CalculateNextRetryTime(tt.retryCount)
            diff := time.Until(nextTime).Minutes()

            if diff < float64(tt.minMinutes)-0.5 || diff > float64(tt.maxMinutes)+0.5 {
                t.Errorf("CalculateNextRetryTime(%d) = %v, expected between %d and %d minutes",
                    tt.retryCount, diff, tt.minMinutes, tt.maxMinutes)
            }
        })
    }
}
```

### Named Test Cases
Use descriptive names for test cases:
```go
tests := []struct {
    name     string
    state    string
    want     string
}{
    {name: "real downloading", state: "downloading", want: "downloading"},
    {name: "stalled downloading", state: "stalledDL", want: "stalled"},
    {name: "error failed", state: "error", want: "failed"},
}

for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        got := mapQBStateToStatus(tt.state)
        if got != tt.want {
            t.Fatalf("mapQBStateToStatus(%q) = %q, want %q", tt.state, got, tt.want)
        }
    })
}
```

### Testify Assertions
When using testify, prefer `assert.Equal`:
```go
import "github.com/stretchr/testify/assert"

func TestExtractInfoHashFromTorrentURL(t *testing.T) {
    result := ExtractInfoHashFromTorrentURL(tt.url)
    assert.Equal(t, tt.expected, result)
}
```

## Database Testing

### In-Memory SQLite
All repository tests use SQLite in-memory database:

```go
func setupTestDB(t *testing.T) *gorm.DB {
    db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
        Logger: logger.Default.LogMode(logger.Silent),
    })
    if err != nil {
        t.Fatalf("Failed to connect to test database: %v", err)
    }

    err = db.AutoMigrate(&model.Subscription{}, &model.Download{})
    if err != nil {
        t.Fatalf("Failed to migrate test database: %v", err)
    }

    return db
}
```

### Test Data Seeding
Pattern for creating test data:
```go
// Create test subscription
testSub := model.Subscription{Name: "test-sub", RssURL: "https://example.com/rss"}
if err := db.Create(&testSub).Error; err != nil {
    t.Fatalf("failed to create test subscription: %v", err)
}

// Seed test data
seed := []model.Download{
    {SubscriptionID: testSub.ID, Title: "active download", Status: "downloading", TorrentURL: "magnet:1", TorrentHash: "hash-dl1"},
    {SubscriptionID: testSub.ID, Title: "stalled download", Status: "stalled", TorrentURL: "magnet:2", TorrentHash: "hash-st1"},
}
for i := range seed {
    if err := repo.Create(&seed[i]); err != nil {
        t.Fatalf("failed to create seed download %d: %v", i, err)
    }
}
```

## Mocking

**Approach:** No mocking framework used. Tests use real implementations with test databases.

**External Services:** Tests avoid external dependencies by:
- Using in-memory SQLite instead of real database
- Testing pure functions with input/output validation
- Not testing external API calls (qBittorrent, Bangumi, etc.)

**Interface-based testing:** Repositories are tested through their implementations, allowing integration-style testing.

## Test Types

### Unit Tests
**Scope:** Individual functions and methods
**Location:** Throughout codebase
**Pattern:**
```go
func TestExtractSeasonFromName(t *testing.T) {
    tests := []struct {
        name     string
        nameCN   string
        expected int
    }{
        {name: "Jian Lai 2", nameCN: "剑来 第二季", expected: 2},
        {name: "Sword of Coming Season 2", nameCN: "", expected: 2},
    }

    for _, tt := range tests {
        got := extractSeasonFromName(tt.name, tt.nameCN)
        if got != tt.expected {
            t.Fatalf("extractSeasonFromName(%q, %q) = %d, want %d", tt.name, tt.nameCN, got, tt.expected)
        }
    }
}
```

### Repository Tests
**Scope:** Database operations with real SQLite
**Examples:**
- `internal/repository/download_status_test.go`
- `internal/repository/status_filter_test.go`

### Handler Tests
**Scope:** HTTP handlers with Gin test context
**Pattern:**
```go
func TestDownloadStatusAlignment(t *testing.T) {
    // Setup test DB
    db, _ := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
    db.AutoMigrate(&model.Subscription{}, &model.Download{})

    downloadRepo := repository.NewDownloadRepository(db)
    handler := NewDownloadHandler(downloadRepo, nil)

    // Setup gin test context
    r := gin.Default()
    r.GET("/api/downloads", handler.List)
    req, _ := http.NewRequest("GET", "/api/downloads?status=stalled", nil)
    w := httptest.NewRecorder()
    r.ServeHTTP(w, req)

    // Validate response
    if w.Code != http.StatusOK {
        t.Fatalf("expected status 200, got %d", w.Code)
    }
}
```

### Regression Tests
**Location:** `internal/service/rss/parser_regression_test.go`

**Purpose:** Ensure episode/fansub extraction continues to work for known patterns

```go
func TestExtractEpisodeRegressionCases(t *testing.T) {
    cases := []struct {
        name    string
        title   string
        episode int
        fansub  string
    }{
        {name: "ani standard dash pattern", title: "[ANi] 某番剧 - 12 [1080P][Baha][WEB-DL]", episode: 12, fansub: "ANi"},
        {name: "chinese episode format", title: "[LoliHouse] 某番剧 第03集 [WebRip 1080p]", episode: 3, fansub: "LoliHouse"},
        // ... more cases
    }

    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            episode := p.ExtractEpisode(tc.title)
            if episode != tc.episode {
                t.Fatalf("ExtractEpisode(%q) = %d, want %d", tc.title, episode, tc.episode)
            }
        })
    }
}
```

## Common Patterns

### Async Testing
No special async testing patterns. Long-running operations are tested through:
- Unit tests for individual functions
- Integration testing via manual verification

### Error Testing
Test both success and error cases:
```go
tests := []struct {
    name     string
    template string
    wantErr  bool
}{
    {name: "有效模板", template: "${title}/Season ${season}", wantErr: false},
    {name: "无变量", template: "固定文件名.mp4", wantErr: true},
    {name: "括号不匹配", template: "${title/Season ${season", wantErr: true},
}
```

### Time-Based Testing
Use fixed time references or allow tolerance:
```go
now := time.Date(2026, 2, 25, 14, 30, 0, 0, time.FixedZone("CST", 8*3600))
withinGrace := model.Download{UpdatedAt: now.Add(-5 * time.Minute)}

// Or allow tolerance
diff := time.Until(nextTime).Minutes()
if diff < float64(tt.minMinutes)-0.5 || diff > float64(tt.maxMinutes)+0.5 {
    t.Errorf(...)
}
```

## Test Coverage

**Current Status:** Tests exist for critical business logic:
- Repository filtering and status alignment
- Episode/fansub extraction from RSS titles
- Language detection
- Retry logic
- File renaming templates
- Hash extraction from URLs

**Gaps:**
- No E2E tests
- Limited handler integration tests
- No external service mocking (qBittorrent, Bangumi API)

## Running Tests

```bash
# All tests
make test

# With coverage
go test -cover ./...

# Coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Verbose output
go test -v ./...

# Specific package
go test -v ./internal/service/downloader/
```

---

*Testing analysis: 2026-04-05*
