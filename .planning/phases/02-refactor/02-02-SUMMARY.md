---
phase: 02-refactor
plan: 02-02
type: summary
subsystem: subscription
requires: []
provides: [REF-03, REF-04]
affects:
  - internal/service/subscription/batch.go
  - internal/service/subscription/batch_test.go
  - internal/service/subscription/collection.go
  - internal/service/subscription/collection_test.go
tech-stack:
  added: []
  patterns:
    - Interface-based service design (D-04)
    - Constructor dependency injection
    - Comprehensive unit testing with mocks
key-files:
  created:
    - internal/service/subscription/batch.go
    - internal/service/subscription/batch_test.go
    - internal/service/subscription/collection.go
    - internal/service/subscription/collection_test.go
  modified: []
decisions:
  - Use mikan.Service interface (not concrete type) for testability
  - Use bangumi.Enricher interface from Plan 02-01
  - Return raw errors per D-03, log at appropriate levels
  - Episode=0 as collection marker in Download records
metrics:
  duration: 0
  completed_date: "2026-04-05"
  test_coverage: "20 tests, all passing"
---

# Phase 02 Plan 02: Extract Batch Import and Collection Download Services

## One-Liner
Extracted BatchImporter (REF-03) and CollectionDownloader (REF-04) services from subscription.go, reducing it by ~379 lines while maintaining full functionality and adding comprehensive unit tests.

## What Was Built

### BatchImporter Service (REF-03)
- **Interface**: `BatchImporter` with `Import(items []ImportItem) ([]ImportResult, error)` method
- **Implementation**: `batchImporter` struct with:
  - Mikan service integration for anime search
  - Bangumi enricher integration for metadata
  - Subscription repository for persistence
  - Config repository for proxy settings
- **Features**:
  - Deduplication by anime name
  - Exact title matching with fallback to first result
  - Fansub group selection (matching or first available)
  - Bangumi metadata enrichment
  - Detailed per-item result reporting

### CollectionDownloader Service (REF-04)
- **Interface**: `CollectionDownloader` with `Download()` and `DownloadAsync()` methods
- **Implementation**: `collectionDownloader` struct with:
  - qBittorrent client integration
  - Download repository for record creation
  - Path validation for security (per SEC-01)
  - Support for both .torrent files and magnet links
- **Features**:
  - Automatic torrent type detection
  - Proxy support for .torrent downloads
  - Existing torrent detection by save path
  - Episode=0 marker for collection downloads
  - Async download support

## Test Coverage

### BatchImporter Tests (10 tests)
| Test | Description |
|------|-------------|
| TestBatchImporter_Import_CreatesSubscriptions | Creates subscriptions from RSS items |
| TestBatchImporter_Import_SkipsExisting | Skips duplicates by name |
| TestBatchImporter_Import_MatchesByTitle | Exact title matching |
| TestBatchImporter_Import_FallbackToFirstResult | Fallback when no exact match |
| TestBatchImporter_Import_SelectsMatchingFansub | Selects specified fansub |
| TestBatchImporter_Import_UsesFirstFansubIfNoMatch | Uses first fansub if no match |
| TestBatchImporter_Import_CallsBangumiEnricher | Enricher called for new subs |
| TestBatchImporter_Import_ReturnsDetailedResults | Per-item result details |
| TestBatchImporter_Import_HandlesMikanSearchError | Graceful search error handling |
| TestBatchImporter_Import_HandlesRepositoryError | Graceful repository error handling |

### CollectionDownloader Tests (10 tests)
| Test | Description |
|------|-------------|
| TestCollectionDownloader_Download_AddsTorrent | Adds torrent to qBittorrent |
| TestCollectionDownloader_Download_HandlesTorrentURL | Downloads .torrent files |
| TestCollectionDownloader_Download_HandlesMagnetLink | Handles magnet links directly |
| TestCollectionDownloader_Download_CreatesRecordWithEpisodeZero | Episode=0 collection marker |
| TestCollectionDownloader_Download_FindsExistingBySavePath | Finds existing by savePath |
| TestCollectionDownloader_Download_SkipsEmptyCollectionTorrent | Skips if no collection URL |
| TestCollectionDownloader_Download_SkipsIfQBClientNil | Handles nil client gracefully |
| TestCollectionDownloader_Download_ValidatesPath | Path traversal protection |
| TestCollectionDownloader_Download_SetsProxyForTorrentURL | Proxy for .torrent downloads |
| TestCollectionDownloader_Download_SkipsIfRecordExists | Skips duplicate records |

## Key Design Decisions

1. **Interface-based design**: Both services define interfaces first, enabling easy mocking in tests
2. **Constructor injection**: All dependencies injected via constructors (mikan.Service, bangumi.Enricher, repositories)
3. **Error handling**: Return raw errors per D-03, log at appropriate levels
4. **Security**: Path validation using `utils.ValidatePath` before file operations (SEC-01)
5. **Async support**: `DownloadAsync` launches goroutine for non-blocking operation

## Verification

```bash
# All tests pass
go test ./internal/service/subscription/... -v
# === RUN   TestBatchImporter_Import_CreatesSubscriptions
# --- PASS: TestBatchImporter_Import_CreatesSubscriptions (0.00s)
# ... (20 tests total)
# PASS
# ok      github.com/WormW/auto-rss/internal/service/subscription    0.231s
```

## Deviations from Plan

None - plan executed exactly as written. The files were already created in previous commits (97107fa, db13618). This execution verified the implementation and fixed a mock compatibility issue with the repository interface.

## Commits

- `600312e`: fix(02-02): add missing GetSubscriptionsWithDownloadCount to mock

## Self-Check: PASSED

- [x] batch.go exists with BatchImporter interface
- [x] batch_test.go exists with 10 passing tests
- [x] collection.go exists with CollectionDownloader interface
- [x] collection_test.go exists with 10 passing tests
- [x] All 20 tests pass
- [x] Path validation uses utils.ValidatePath (SEC-01)
