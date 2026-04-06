# Phase 2: 代码重构 - Research

**Researched:** 2026-04-05
**Domain:** Go service refactoring, code extraction, dependency injection
**Confidence:** HIGH

## Summary

This phase involves splitting three bloated files (`subscription.go` 2345 lines, `monitor.go` 959 lines, `organizer.go` 755 lines) into 10 focused services. The goal is to reduce each file to under 800/500/400 lines respectively while maintaining full backward compatibility.

**Primary recommendation:** Use vertical functional decomposition - extract cohesive functionality into standalone services with clear interfaces, following the existing `RetryService` pattern from `downloader/retry.go`.

## User Constraints (from CONTEXT.md)

### Locked Decisions
- **D-01:** Functional vertical splitting strategy
  - `bangumi/enrich.go` - Bangumi metadata enrichment (from subscription.go)
  - `renamer/service.go` - File renaming service (from subscription.go and monitor.go)
  - `subscription/batch.go` - Batch import service (from subscription.go)
  - `subscription/collection.go` - Collection download service (from subscription.go)
  - `downloader/status_sync.go` - Status sync component (from monitor.go)
  - `downloader/completion_handler.go` - Completion handler (from monitor.go)
  - `downloader/retry_service.go` - Retry service (from monitor.go)
  - `organizer/parser.go` - Filename parser (from organizer.go)
  - `organizer/matcher.go` - Episode matcher (from organizer.go)
  - `organizer/mover.go` - File mover (from organizer.go)

- **D-02:** Strict backward compatibility - handler signatures unchanged, API responses unchanged, zero breaking changes
- **D-03:** Simple raw error handling + logging - no custom error types
- **D-04:** Each service defines a main interface for mock testing and dependency injection

### Claude's Discretion
- Log message content (keep consistent with existing style)
- Interface method naming (follow Go conventions)
- Dependency injection approach (constructor vs setter)

### Deferred Ideas (OUT OF SCOPE)
- API design optimization (requires frontend coordination)
- Error code system introduction
- Async message communication between services

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| REF-01 | Extract Bangumi enrichment service | Lines 241-396 in subscription.go contain `enrichWithBangumi` and `enrichWithBangumiInternal` |
| REF-02 | Extract renamer service | Lines 1814-2018 (`doReorganizeFiles`), 2143-2354 (`doRenameFiles`) use rename logic; monitor.go lines 441-701 also have rename functions |
| REF-03 | Extract batch import service | Lines 1553-1759 contain `BatchImportFromRSS` with Mikan search and subscription creation |
| REF-04 | Extract collection download service | Lines 399-555 contain `downloadCollectionTorrent` function |
| REF-05 | Extract status sync component | Lines 310-358 in monitor.go contain `updateDownloadStatus` |
| REF-06 | Extract completion handler | Lines 361-439 contain `handleDownloadComplete` |
| REF-07 | Extract retry service | Lines 884-925 contain `processFailedRetries` - enhance existing `RetryService` |
| REF-08 | Extract parser component | Already exists at `organizer/parser.go` - verify coverage |
| REF-09 | Extract matcher component | Lines 407-502 contain `findMatchingSubscription` - needs extraction |
| REF-10 | Extract mover component | Lines 609-667 contain `moveFile` and `copyFile` - needs extraction |

## Code Structure Analysis

### subscription.go (2345 lines) - Target: < 800 lines

**Current Structure:**
```
Lines 1-80:   Imports and SubscriptionHandler struct + constructor
Lines 81-238: Helper functions (normalizeSubscriptionNameAndSeason, parseSeasonToken, romanToInt, chineseNumeralToInt)
Lines 241-396: Bangumi enrichment (enrichWithBangumi, enrichWithBangumiInternal) [REF-01]
Lines 399-555: Collection torrent download (downloadCollectionTorrent) [REF-04]
Lines 557-629: Create handler
Lines 631-789: Update handler
Lines 791-814: Delete handler
Lines 816-865: Toggle handler
Lines 867-962: EnrichBangumi handler
Lines 964-1009: DownloadCollection handler
Lines 1011-1036: GetByID handler
Lines 1038-1081: List handler
Lines 1083-1131: CollectEpisodes handler (async task launcher)
Lines 1133-1536: doCollectEpisodes (core collection logic) [REF-03 partially]
Lines 1538-1759: BatchImportFromRSS + helpers [REF-03]
Lines 1761-1812: ReorganizeFiles handler (async task launcher)
Lines 1814-2018: doReorganizeFiles (file reorganization via qBittorrent) [REF-02]
Lines 2020-2028: isVideoFile helper
Lines 2030-2089: moveFile, cleanEmptyDirs helpers
Lines 2091-2141: RenameFiles handler (async task launcher)
Lines 2143-2354: doRenameFiles (batch rename via qBittorrent) [REF-02]
```

**Extraction Targets:**
| Function(s) | Lines | Target Service | Estimated Savings |
|-------------|-------|----------------|-------------------|
| enrichWithBangumi* | 156 | bangumi/enrich.go | ~150 lines |
| downloadCollectionTorrent | 157 | subscription/collection.go | ~150 lines |
| doCollectEpisodes (core logic) | 404 | subscription/batch.go | ~350 lines |
| BatchImportFromRSS | 222 | subscription/batch.go | ~200 lines |
| doReorganizeFiles | 205 | renamer/service.go | ~180 lines |
| doRenameFiles | 212 | renamer/service.go | ~190 lines |
| moveFile, cleanEmptyDirs, isVideoFile | 69 | organizer/mover.go | ~60 lines |
| **Total** | | | **~1280 lines** |

**Remaining in subscription.go after extraction:** ~1065 lines (need further reduction to < 800)

**Additional consolidation needed:**
- Helper functions (normalizeSubscriptionNameAndSeason, etc.) could move to `pkg/utils/season.go`
- Handler methods can be simplified by delegating to services

### monitor.go (959 lines) - Target: < 500 lines

**Current Structure:**
```
Lines 1-67:   Imports, constants, DownloadMonitor struct, constructor
Lines 69-77:  NotificationService interface and setter
Lines 79-110: Start/Stop methods
Lines 112-277: processPendingDownloads [REF-05 partially]
Lines 279-308: checkDownloads (orchestrator)
Lines 310-358: updateDownloadStatus [REF-05]
Lines 361-439: handleDownloadComplete [REF-06]
Lines 441-533: renameFile [REF-02]
Lines 535-701: renameCollectionFiles [REF-02]
Lines 703-734: extractEpisodeFromFilename (helper)
Lines 736-767: updateSubscriptionStats
Lines 769-808: mapQBStateToStatus, isTorrentComplete, shouldSkipReconcileByGracePeriod
Lines 818-881: reconcileMissingDownloadingTasks [REF-05]
Lines 883-925: processFailedRetries [REF-07]
Lines 927-959: sendFailedNotification
```

**Extraction Targets:**
| Function(s) | Lines | Target Service | Estimated Savings |
|-------------|-------|----------------|-------------------|
| updateDownloadStatus + mapQBStateToStatus | ~50 | downloader/status_sync.go | ~45 lines |
| handleDownloadComplete + sendFailedNotification | ~100 | downloader/completion_handler.go | ~90 lines |
| renameFile + renameCollectionFiles + extractEpisodeFromFilename | ~260 | renamer/service.go | ~240 lines |
| processFailedRetries | ~43 | downloader/retry_service.go (extend existing) | ~35 lines |
| reconcileMissingDownloadingTasks | ~64 | downloader/status_sync.go | ~55 lines |
| **Total** | | | **~465 lines** |

**Remaining in monitor.go after extraction:** ~494 lines (meets < 500 target)

### organizer.go (755 lines) - Target: < 400 lines

**Current Structure:**
```
Lines 1-71:   Imports, FileOrganizer struct, constructor
Lines 73-94:  Start method
Lines 96-122: addWatchRecursively
Lines 124-131: Stop method
Lines 133-137: TriggerScan
Lines 139-178: watchLoop
Lines 180-207: scanExistingFiles
Lines 209-255: handleNewFile
Lines 257-404: organizeFile (main orchestration)
Lines 407-502: findMatchingSubscription [REF-09]
Lines 504-543: generateNewPath
Lines 545-558: sanitizeDirectoryName
Lines 560-598: isSimilarDirectoryName
Lines 600-606: abs helper
Lines 608-640: moveFile [REF-10]
Lines 642-667: copyFile [REF-10]
Lines 669-681: isVideoFile
Lines 683-715: isFileReady
Lines 717-755: isAlreadyOrganized
```

**Extraction Targets:**
| Function(s) | Lines | Target Service | Estimated Savings |
|-------------|-------|----------------|-------------------|
| findMatchingSubscription | ~96 | organizer/matcher.go | ~90 lines |
| moveFile + copyFile | ~60 | organizer/mover.go | ~55 lines |
| sanitizeDirectoryName + isSimilarDirectoryName + abs | ~60 | organizer/matcher.go (helpers) or pkg/utils | ~50 lines |
| generateNewPath (partial) | ~40 | organizer/matcher.go | ~35 lines |
| **Total** | | | **~230 lines** |

**Remaining in organizer.go after extraction:** ~525 lines (need additional ~125 lines reduction)

**Additional extraction:**
- `isVideoFile`, `isFileReady`, `isAlreadyOrganized` could move to `pkg/utils/file.go`
- Consider extracting `watchLoop` and `scanExistingFiles` to a `watcher` component

## Extraction Strategy

### Service Dependencies Graph

```
subscription/handler.go
├── bangumi/enrich.go (BangumiEnricher)
├── subscription/collection.go (CollectionDownloader)
├── subscription/batch.go (BatchImporter)
├── renamer/service.go (RenamerService)
└── organizer/mover.go (FileMover)

downloader/monitor.go
├── downloader/status_sync.go (StatusSync)
├── downloader/completion_handler.go (CompletionHandler)
├── downloader/retry_service.go (RetryService) - already exists
└── renamer/service.go (RenamerService)

organizer/organizer.go
├── organizer/parser.go (FileNameParser) - already exists
├── organizer/matcher.go (SubscriptionMatcher)
├── organizer/mover.go (FileMover)
└── renamer/service.go (RenamerService)
```

### Interface Definitions

Based on existing patterns (RetryService, RenameService), here are the proposed interfaces:

#### REF-01: BangumiEnricher
```go
// BangumiEnricher 自动获取并填充 Bangumi 元数据
type BangumiEnricher interface {
    // Enrich 为订阅填充 Bangumi 数据
    // force: 是否强制刷新（即使已有 Bangumi ID）
    Enrich(subscription *model.Subscription, force bool) error
}

// NewBangumiEnricher 创建 Bangumi 富化服务
// Dependencies: bangumi.BangumiService, bangumi.ImageService, configRepo
func NewBangumiEnricher(
    bangumiService *bangumi.BangumiService,
    imageService *bangumi.ImageService,
    configRepo repository.ConfigRepository,
) BangumiEnricher
```

#### REF-02: RenamerService (extends existing)
```go
// Existing in downloader/renamer.go - needs extension:

type RenamerService interface {
    // GenerateFileName 生成新文件名（已存在）
    GenerateFileName(ctx *RenameContext) string
    
    // RenameViaQBittorrent 通过 qBittorrent API 重命名单个文件
    RenameViaQBittorrent(client QBittorrentClient, hash string, oldName, newName string) error
    
    // MoveViaQBittorrent 通过 qBittorrent API 移动种子位置
    MoveViaQBittorrent(client QBittorrentClient, hash string, newLocation string) error
    
    // RenameCollection 批量重命名合集种子中的所有视频文件
    RenameCollection(client QBittorrentClient, hash string, subscription *model.Subscription) (int, error)
    
    // ReorganizeSubscriptionFiles 重新组织订阅的所有已完成下载文件
    ReorganizeSubscriptionFiles(ctx context.Context, subscription *model.Subscription, downloads []model.Download) error
}
```

#### REF-03: BatchImporter
```go
// BatchImporter 批量导入订阅服务
type BatchImporter interface {
    // Import 从 RSS 项目批量导入订阅
    // 返回导入结果列表，每个项目对应一个结果
    Import(items []RSSAnimeImportItem) ([]ImportResult, error)
}

// ImportResult 单个导入项的结果
type ImportResult struct {
    Title        string              `json:"title"`
    Success      bool                `json:"success"`
    Message      string              `json:"message"`
    Skipped      bool                `json:"skipped"`
    Subscription *model.Subscription `json:"subscription,omitempty"`
}

// Dependencies: mikan.MikanService, BangumiEnricher, SubscriptionRepository
```

#### REF-04: CollectionDownloader
```go
// CollectionDownloader 合集种子下载服务
type CollectionDownloader interface {
    // Download 下载合集种子并创建下载记录
    // 返回创建的下载记录（如果成功）
    Download(subscription *model.Subscription) (*model.Download, error)
    
    // DownloadAsync 异步下载合集（内部调用 Download）
    DownloadAsync(subscription *model.Subscription)
}

// Dependencies: QBittorrentClient, DownloadRepository, ConfigRepository
```

#### REF-05: StatusSync
```go
// StatusSync 下载状态同步服务
type StatusSync interface {
    // Sync 同步 qBittorrent 状态到数据库
    // 处理所有活跃种子的状态更新
    Sync(torrents []*TorrentInfo) error
    
    // UpdateStatus 更新单个下载任务的状态
    UpdateStatus(download *model.Download, torrent *TorrentInfo) error
    
    // Reconcile 对账：标记 qBittorrent 中已消失的任务为失败
    Reconcile(torrents []*TorrentInfo) error
}

// Dependencies: DownloadRepository, NotificationService
```

#### REF-06: CompletionHandler
```go
// CompletionHandler 下载完成处理服务
type CompletionHandler interface {
    // HandleComplete 处理下载完成事件
    // 包括：发送通知、执行重命名、更新订阅统计
    HandleComplete(download *model.Download, torrent *TorrentInfo) error
}

// Dependencies: SubscriptionRepository, DownloadRepository, NotificationService, RenamerService
```

#### REF-07: RetryService (extends existing)
```go
// Existing in downloader/retry.go - needs extension:

type RetryService interface {
    // 已有方法：
    // - CalculateNextRetryTime(retryCount int) time.Time
    // - ShouldRetry(download *model.Download) (bool, string)
    // - PrepareRetry(download *model.Download, reason string) error
    // - MarkFailed(download *model.Download, err error, reason string) error
    
    // 新增方法：
    // ProcessRetries 处理所有待重试的失败任务
    ProcessRetries(limit int) error
}
```

#### REF-09: SubscriptionMatcher
```go
// SubscriptionMatcher 订阅匹配服务
type SubscriptionMatcher interface {
    // Match 根据文件名信息查找最匹配的订阅
    // 返回匹配的订阅和匹配分数（0-1）
    Match(info *FileNameInfo) (*model.Subscription, float64)
    
    // MatchWithBangumi 使用 Bangumi API 辅助匹配
    MatchWithBangumi(info *FileNameInfo, bangumiSubject *bangumi.Subject) (*model.Subscription, float64)
    
    // SetMinMatchScore 设置最小匹配分数阈值
    SetMinMatchScore(score float64)
}

// Dependencies: SubscriptionRepository, FileNameParser, BangumiService (optional)
```

#### REF-10: FileMover
```go
// FileMover 文件移动服务
type FileMover interface {
    // Move 移动文件（支持跨文件系统）
    Move(src, dest string) error
    
    // Copy 复制文件
    Copy(src, dest string) error
    
    // MoveWithFallback 移动文件，如果目标存在则添加时间戳后缀
    MoveWithFallback(src, dest string) (string, error)
    
    // CleanEmptyDirs 递归清理空目录
    CleanEmptyDirs(dir string)
    
    // IsVideoFile 检查是否是视频文件
    IsVideoFile(filePath string) bool
    
    // IsFileReady 检查文件是否已完全写入
    IsFileReady(filePath string) bool
}
```

## Architecture Patterns

### Recommended Project Structure After Refactoring

```
internal/
├── api/handler/
│   └── subscription.go          # < 800 lines - thin handlers only
├── service/
│   ├── bangumi/
│   │   ├── bangumi.go           # existing
│   │   ├── enrich.go            # NEW: REF-01
│   │   └── image.go             # existing
│   ├── downloader/
│   │   ├── monitor.go           # < 500 lines - orchestration only
│   │   ├── renamer.go           # existing - extend for REF-02
│   │   ├── retry.go             # existing - extend for REF-07
│   │   ├── status_sync.go       # NEW: REF-05
│   │   └── completion_handler.go # NEW: REF-06
│   ├── organizer/
│   │   ├── organizer.go         # < 400 lines - orchestration only
│   │   ├── parser.go            # existing - REF-08
│   │   ├── matcher.go           # NEW: REF-09
│   │   └── mover.go             # NEW: REF-10
│   ├── renamer/
│   │   └── service.go           # NEW: REF-02 (alternative location)
│   └── subscription/
│       ├── batch.go             # NEW: REF-03
│       └── collection.go        # NEW: REF-04
```

### Pattern: Constructor Injection (Following Existing Code)

```go
// Example from existing retry.go:
func NewRetryService(downloadRepo repository.DownloadRepository) *RetryService {
    return &RetryService{
        downloadRepo: downloadRepo,
    }
}

// New services should follow same pattern:
func NewStatusSync(
    downloadRepo repository.DownloadRepository,
    notificationSvc downloader.NotificationService,
) *StatusSyncService {
    return &StatusSyncService{
        downloadRepo: downloadRepo,
        notificationSvc: notificationSvc,
    }
}
```

### Pattern: Interface Segregation

Each service should have a focused interface:
- **Good:** `BangumiEnricher` with single `Enrich` method
- **Bad:** `SubscriptionService` with 10+ unrelated methods

### Pattern: Error Handling (Per D-03)

```go
// Return raw errors, log at appropriate level
func (s *Service) DoSomething() error {
    result, err := s.dep.Operation()
    if err != nil {
        logger.Error("Operation failed", "error", err.Error())
        return err  // Return raw error
    }
    // ...
    return nil
}
```

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| File moving with cross-device support | Custom copy+delete logic | `FileMover` service with `io.Copy` | Already exists in organizer.go, just extract |
| String similarity for title matching | Custom algorithm | `FileNameParser.MatchTitle` with Levenshtein | Already implemented in parser.go |
| Retry backoff calculation | Simple linear backoff | `RetryService.CalculateNextRetryTime` with exponential backoff | Already implemented |
| Template variable substitution | `strings.Replace` chain | `RenamerService.GenerateFileName` | Already implemented with validation |
| QBittorrent file operations | Direct API calls scattered | Centralized via `RenamerService` | Consistent error handling, easier to mock |

## Common Pitfalls

### Pitfall 1: Breaking API Compatibility
**What goes wrong:** Changing handler signatures or response formats during refactoring
**Why it happens:** Extracting services sometimes leads to "cleaning up" APIs
**How to avoid:** 
- Verify all handler methods maintain exact same gin.Context usage
- Run API tests before and after each extraction
- Document all API contracts in handler comments

### Pitfall 2: Circular Dependencies
**What goes wrong:** Services depend on each other in a cycle
**Why it happens:** `CompletionHandler` needs `RenamerService`, but `RenamerService` might need notifications
**How to avoid:**
- Use interface types, not concrete types
- Pass `NotificationService` interface, not concrete handler
- Dependency graph should be DAG

### Pitfall 3: State Synchronization Issues
**What goes wrong:** Moving code that accesses shared state (maps, caches) without proper synchronization
**Why it happens:** `organizer.go` has `processing` map with mutex protection
**How to avoid:**
- Keep mutex-protected state in the original struct
- Or extract to thread-safe service with its own locking
- Document thread-safety requirements in interface comments

### Pitfall 4: Missing Context Cancellation Checks
**What goes wrong:** Long-running operations in new services don't check for context cancellation
**Why it happens:** `doCollectEpisodes` has cancellation checks that might be missed during extraction
**How to avoid:**
- Pass `context.Context` as first parameter to all long-running methods
- Document cancellation requirements
- Add tests for cancellation behavior

### Pitfall 5: Repository Interface Bloat
**What goes wrong:** Adding methods to repository interfaces for single-service use
**Why it happens:** New services need specialized queries
**How to avoid:**
- Keep repository methods generic and reusable
- Services can filter results in memory for complex cases
- Or create service-specific query objects

## Code Examples

### Example: Extracting BangumiEnricher (REF-01)

**Before (in subscription.go):**
```go
func (h *SubscriptionHandler) enrichWithBangumi(subscription *model.Subscription) {
    h.enrichWithBangumiInternal(subscription, false)
}

func (h *SubscriptionHandler) enrichWithBangumiInternal(subscription *model.Subscription, force bool) {
    // 156 lines of logic...
}
```

**After (bangumi/enrich.go):**
```go
package bangumi

type Enricher struct {
    bangumiService *BangumiService
    imageService   *ImageService
    configRepo     repository.ConfigRepository
}

func NewEnricher(bg *BangumiService, img *ImageService, cfg repository.ConfigRepository) *Enricher {
    return &Enricher{bangumiService: bg, imageService: img, configRepo: cfg}
}

func (e *Enricher) Enrich(subscription *model.Subscription, force bool) error {
    // Extracted 156 lines...
}
```

**Handler becomes:**
```go
type SubscriptionHandler struct {
    // ... other fields
    bangumiEnricher *bangumi.Enricher  // Replace bangumiService and imageService
}

func (h *SubscriptionHandler) Create(c *gin.Context) {
    // ...
    h.bangumiEnricher.Enrich(&subscription, false)
    // ...
}
```

### Example: Testing with Mocked Interfaces

```go
// Mock for testing
type mockBangumiEnricher struct {
    enrichFunc func(sub *model.Subscription, force bool) error
}

func (m *mockBangumiEnricher) Enrich(sub *model.Subscription, force bool) error {
    return m.enrichFunc(sub, force)
}

// Test usage
func TestCreateSubscription(t *testing.T) {
    handler := &SubscriptionHandler{
        bangumiEnricher: &mockBangumiEnricher{
            enrichFunc: func(sub *model.Subscription, force bool) error {
                sub.BangumiID = 12345
                return nil
            },
        },
    }
    // Test handler behavior...
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Monolithic handlers | Service decomposition | This phase | Better testability, maintainability |
| Direct QBittorrent calls | Via RenamerService | This phase | Consistent error handling, mockable |
| Inline retry logic | RetryService with exponential backoff | Already exists | Reusable, configurable |
| File operations scattered | FileMover service | This phase | Centralized path validation |

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Existing `RetryService` pattern in `downloader/retry.go` is the canonical pattern to follow | Standard Stack | Services may have inconsistent initialization patterns |
| A2 | `QBittorrentClient` interface is already defined and stable | Interface Definitions | New services may need interface updates |
| A3 | `FileNameParser` in `organizer/parser.go` covers all parsing needs | REF-08 | May need to extend parser for edge cases |
| A4 | All async tasks use `task.GetManager()` pattern | Code Examples | New services may need task manager injection |

## Open Questions

1. **RenamerService Location**
   - What we know: Both `downloader/` and `renamer/` directories exist
   - What's unclear: Should REF-02 use `downloader/renamer.go` or create `renamer/service.go`?
   - Recommendation: Extend existing `downloader/renamer.go` to avoid confusion

2. **QBittorrentClient Interface Scope**
   - What we know: Used across multiple services
   - What's unclear: Whether to extract interface to separate package to avoid import cycles
   - Recommendation: Keep in downloader package, use interface type in other packages

3. **Task Manager Access**
   - What we know: Currently uses global `task.GetManager()`
   - What's unclear: Whether to inject TaskManager into services
   - Recommendation: Keep global for now, inject if testing becomes difficult

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go compiler | All | ✓ | 1.21+ | — |
| go mod | Dependencies | ✓ | — | — |
| Existing tests | Validation | ✓ | — | Manual testing |

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go testing + testify |
| Config file | none |
| Quick run command | `go test ./internal/service/... -short` |
| Full suite command | `go test ./...` |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| REF-01 | Bangumi enrichment | unit | `go test ./internal/service/bangumi/...` | ❌ Wave 0 |
| REF-02 | File renaming | unit | `go test ./internal/service/downloader/...` | ✅ (partial) |
| REF-03 | Batch import | unit | `go test ./internal/service/subscription/...` | ❌ Wave 0 |
| REF-04 | Collection download | unit | `go test ./internal/service/subscription/...` | ❌ Wave 0 |
| REF-05 | Status sync | unit | `go test ./internal/service/downloader/...` | ❌ Wave 0 |
| REF-06 | Completion handling | unit | `go test ./internal/service/downloader/...` | ❌ Wave 0 |
| REF-07 | Retry processing | unit | `go test ./internal/service/downloader/...` | ✅ (partial) |
| REF-08 | Filename parsing | unit | `go test ./internal/service/organizer/...` | ✅ |
| REF-09 | Subscription matching | unit | `go test ./internal/service/organizer/...` | ❌ Wave 0 |
| REF-10 | File moving | unit | `go test ./internal/service/organizer/...` | ❌ Wave 0 |

### Wave 0 Gaps
- [ ] `internal/service/bangumi/enrich_test.go` - covers REF-01
- [ ] `internal/service/subscription/batch_test.go` - covers REF-03
- [ ] `internal/service/subscription/collection_test.go` - covers REF-04
- [ ] `internal/service/downloader/status_sync_test.go` - covers REF-05
- [ ] `internal/service/downloader/completion_handler_test.go` - covers REF-06
- [ ] `internal/service/organizer/matcher_test.go` - covers REF-09
- [ ] `internal/service/organizer/mover_test.go` - covers REF-10

### Integration Testing Strategy
- Use `httptest` for handler-level tests
- Mock external services (Bangumi API, qBittorrent) using interfaces
- Use temporary directories for file operation tests

## Sources

### Primary (HIGH confidence)
- `internal/api/handler/subscription.go` - Full code analysis of extraction targets
- `internal/service/downloader/monitor.go` - Full code analysis of extraction targets
- `internal/service/organizer/organizer.go` - Full code analysis of extraction targets
- `internal/service/downloader/retry.go` - Canonical service pattern reference
- `internal/service/organizer/parser.go` - Existing extracted service reference
- `internal/service/downloader/renamer.go` - Existing service to extend

### Secondary (MEDIUM confidence)
- `internal/repository/download.go` - Repository interface patterns
- `internal/model/download.go` - Model definitions

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - Based on existing code patterns
- Architecture: HIGH - Clear extraction targets identified
- Pitfalls: MEDIUM - Based on common Go refactoring patterns

**Research date:** 2026-04-05
**Valid until:** 2026-05-05 (30 days for stable Go codebase)
