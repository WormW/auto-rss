# Coding Conventions

**Analysis Date:** 2026-04-05

## Language & Tooling

**Language:** Go 1.23.0

**Required Tools:**
- `go fmt` - Standard Go formatting (enforced via `make fmt`)
- `golangci-lint` - Linting (via `make lint`)

## Naming Patterns

### Files
- **Go files:** snake_case for test files (e.g., `download_status_test.go`), camelCase for regular files (e.g., `retry.go`)
- **Test files:** Always suffixed with `_test.go`, co-located with source files
- **Packages:** Lowercase, no underscores (e.g., `repository`, `downloader`, `scheduler`)

### Types
- **Structs:** PascalCase, descriptive names (e.g., `DownloadRepository`, `RetryService`, `SubscriptionHandler`)
- **Interfaces:** PascalCase with `-er` suffix for single-method interfaces (e.g., `QBittorrentClient`)
- **Constants:** PascalCase for exported, camelCase for unexported
  ```go
  const RetryIntervalBase = 1 * time.Minute
  const retryExponent = 2.0
  ```

### Functions & Methods
- **Exported:** PascalCase (e.g., `NewDownloadRepository`, `CalculateNextRetryTime`)
- **Unexported:** camelCase (e.g., `isRetryableError`, `containsIgnoreCase`)
- **Constructor pattern:** `New{TypeName}` for constructors
- **Handler methods:** Use gin context parameter named `c`
  ```go
  func (h *DownloadHandler) List(c *gin.Context)
  ```

### Variables
- **Local:** camelCase, short names for short scopes
- **Struct fields:** PascalCase for exported, camelCase for unexported
- **Test variables:** `tt` for table test struct, `tc` for test case

## Code Style

### Formatting
- Use `go fmt` for all code formatting
- Line length: No strict limit, but keep readable
- Imports: Grouped with stdlib first, then third-party, then internal

### Import Organization
Standard import grouping pattern observed:
```go
import (
    // Standard library
    "context"
    "fmt"
    "net/http"
    "strconv"
    "time"

    // Third-party
    "github.com/gin-gonic/gin"
    "github.com/stretchr/testify/assert"
    "gorm.io/driver/sqlite"
    "gorm.io/gorm"

    // Internal packages
    "github.com/WormW/auto-rss/internal/model"
    "github.com/WormW/auto-rss/internal/pkg/logger"
    "github.com/WormW/auto-rss/internal/repository"
)
```

### Error Handling
**Pattern:** Explicit error checking with immediate return
```go
if err != nil {
    logger.Error("Failed to create subscription", "error", err.Error())
    c.JSON(http.StatusInternalServerError, gin.H{
        "code":    500,
        "message": "Failed to create subscription",
    })
    return
}
```

**Error wrapping:** Use `fmt.Errorf` with `%w` verb for error wrapping
```go
return fmt.Errorf("failed to update download for retry: %w", err)
```

**HTTP error responses:** Always include code and message in JSON response
```go
c.JSON(http.StatusBadRequest, gin.H{
    "code":    400,
    "message": "Invalid request body",
})
```

## Logging

**Framework:** Uber Zap via internal wrapper (`internal/pkg/logger`)

**Patterns:**
- Use structured logging with key-value pairs
- Log levels: `Debug`, `Info`, `Warn`, `Error`, `Fatal`
- Always include context (IDs, names) in log entries

```go
logger.Info("Creating new subscription",
    "name", subscription.Name,
    "rss_url", subscription.RssURL,
    "client_ip", c.ClientIP())

logger.Error("Failed to create subscription",
    "name", subscription.Name,
    "error", err.Error())
```

**When to log:**
- INFO: Handler entry points, service operations, state changes
- DEBUG: Detailed flow information, proxy settings
- WARN: Non-fatal errors, retries, missing optional data
- ERROR: Operation failures with context

## Comments

**Language:** Mixed Chinese and English (Chinese for business logic, English for technical patterns)

**Patterns:**
- Package comments describe purpose
- Function comments for exported functions
- Inline comments for complex logic

```go
// DownloadRepository 下载仓储接口
type DownloadRepository interface {
    // CreateInTx 在事务中创建下载任务
    CreateInTx(tx *gorm.DB, download *model.Download) error
}
```

## Architecture Patterns

### Repository Pattern
All data access uses repository interfaces:
```go
// Location: internal/repository/*.go
type DownloadRepository interface {
    Create(download *model.Download) error
    GetByID(id uint) (*model.Download, error)
    // ...
}
```

### Handler Pattern
HTTP handlers use dependency injection:
```go
// Location: internal/api/handler/*.go
type SubscriptionHandler struct {
    repo           repository.SubscriptionRepository
    downloadRepo   repository.DownloadRepository
    configRepo     repository.ConfigRepository
    // ...
}
```

### Service Pattern
Business logic in service layer:
```go
// Location: internal/service/*/
type RetryService struct {
    downloadRepo repository.DownloadRepository
}
```

## Function Design

### Size
- Keep functions focused and under 100 lines when possible
- Large handlers (like `doCollectEpisodes`) are acceptable for complex workflows

### Parameters
- Use struct pointers for complex inputs
- Pass repositories as interfaces
- Use context for cancellation in long-running operations

### Return Values
- Return `(result, error)` pattern
- Return early on errors

## Model Design

**Location:** `internal/model/*.go`

**Patterns:**
- GORM models with JSON and GORM tags
- TableName method for explicit table naming
- Use pointers for optional time fields

```go
type Download struct {
    ID             uint       `json:"id" gorm:"primaryKey"`
    Title          string     `json:"title" gorm:"not null"`
    Status         string     `json:"status" gorm:"default:pending;index"`
    DownloadedAt   *time.Time `json:"downloaded_at"`
}

func (Download) TableName() string {
    return "downloads"
}
```

## Constants & Configuration

**Location:** Near usage or in dedicated config files

**Patterns:**
- Time durations as typed constants
- Status strings as string constants (not iota enums)

```go
const (
    RetryIntervalBase = 1 * time.Minute
    RetryIntervalMax  = 60 * time.Minute
    RetryExponent     = 2.0
)
```

## Testing Conventions

See TESTING.md for detailed testing patterns.

---

*Convention analysis: 2026-04-05*
