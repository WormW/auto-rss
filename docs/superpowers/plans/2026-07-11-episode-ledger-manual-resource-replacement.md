# 剧集台账与人工资源替换实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 建立以订阅和相对集数为唯一身份的剧集台账，停止所有自动同集替换，并提供 RSS 换源基线、人工状态管理和可恢复的人工资源替换流程。

**Architecture:** 新增 `episode` 领域服务，统一拥有剧集状态转换、RSS 条目决策、候选去重和进度刷新；调度器、下载完成回调和 handler 通过窄接口调用该服务。下载仓储在所有删除入口维护“解绑但保留拥有状态”的底层不变量，覆盖磁盘清理等旁路。人工替换使用持久化阶段机协调数据库、qBittorrent 和文件系统，先暂存新文件并保留旧资源，成功切换后才清理旧任务和文件。

**Tech Stack:** Go 1.22、Gin、GORM、gormigrate、SQLite、Vue 3、TypeScript、Naive UI、Node test runner。

---

## 文件映射

新增文件：

- `internal/model/subscription_episode.go`：剧集状态、资源候选和替换阶段模型及常量。
- `internal/repository/episode.go`：台账和候选的查询、原子状态更新、进度聚合。
- `internal/repository/episode_test.go`：唯一性、回填、解绑和候选幂等测试。
- `internal/service/episode/service.go`：剧集状态机、RSS 决策和订阅汇总刷新。
- `internal/service/episode/service_test.go`：状态转换、候选决策和并发防重测试。
- `internal/service/episode/replacement.go`：人工替换阶段机和崩溃恢复。
- `internal/service/episode/replacement_downloader.go`：创建替换下载、等待 qB 完成并产出暂存文件。
- `internal/service/episode/replacement_test.go`：暂存、回滚、切换和清理失败测试。
- `internal/api/handler/episode.go`：剧集列表、批量状态和候选处理 API。
- `internal/api/handler/episode_test.go`：API 请求和错误映射测试。
- `web/src/api/episode.ts`：剧集与候选 API 类型和调用。
- `web/src/components/EpisodeManagerDrawer.vue`：剧集管理抽屉和资源比较对话框。
- `web/src/utils/episode-ledger.ts`：前端状态标签、批量选择和候选展示纯函数。
- `web/tests/episode-ledger.test.ts`：前端纯函数测试。

修改文件：

- `internal/model/subscription.go`：增加 `RSSBaselinePending`，保持现有原始集号兼容字段。
- `internal/model/download.go`：增加下载用途和候选关联，区分普通下载与人工替换任务。
- `internal/pkg/database/migration.go`、`internal/pkg/database/database.go`、`internal/pkg/database/migration_test.go`：建表、索引和历史回填。
- `internal/service/scheduler/scheduler.go`、`internal/service/scheduler/smart_fetch.go` 及测试：改为台账决策，移除自动替换。
- `internal/api/handler/subscription.go` 及测试：换源基线、预览和手动采集使用台账。
- `internal/service/downloader/monitor.go`、`internal/service/downloader/completion_handler.go` 及测试：下载成功和失败驱动台账状态。
- `internal/service/organizer/organizer.go` 及测试：整理成功后完成台账。
- `internal/api/handler/download.go` 及测试：删除记录只解绑台账，不改变拥有状态。
- `internal/repository/download.go` 及测试：所有删除入口原子解绑台账，避免磁盘清理和监控旁路。
- `internal/repository/subscription.go` 及测试：删除订阅时级联删除候选和台账。
- `internal/service/disk/monitor_test.go`：验证自动清理删除下载记录后仍保留已下载台账。
- `internal/api/router/router.go`、`internal/api/router/router_test.go`：注入服务和注册路由。
- `internal/service/backup/backup.go`、`internal/service/backup/backup_test.go`：导出和恢复剧集台账与候选，剥离运行时 ID 和路径。
- `web/src/api/index.ts`、`web/src/views/Subscriptions.vue`、`web/package.json`：接入剧集管理入口和前端测试。
- `README.md`、`docs/API.md`、`docs/LANGUAGE_FEATURE.md`：更新用户行为和 API 文档。

## Task 1: 定义剧集台账模型和迁移

**Files:**
- Create: `internal/model/subscription_episode.go`
- Modify: `internal/model/subscription.go`
- Modify: `internal/model/download.go`
- Modify: `internal/pkg/database/migration.go`
- Modify: `internal/pkg/database/database.go`
- Test: `internal/pkg/database/migration_test.go`

- [ ] **Step 1: Write failing migration tests**

在 `internal/pkg/database/migration_test.go` 增加测试，先创建旧结构和代表性下载记录，再运行迁移：

```go
func TestRunMigrationsBackfillsEpisodeLedgerWithoutInferringGaps(t *testing.T) {
    db := openMigrationTestDB(t)
    require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}))

    sub := model.Subscription{Name: "Offset Anime", RssURL: "https://old.test/rss", EpisodeOffset: 170, CurrentEpisode: 173}
    require.NoError(t, db.Create(&sub).Error)
    require.NoError(t, db.Create(&model.Download{
        SubscriptionID: sub.ID,
        Episode: 171,
        Title: "Offset Anime - 171",
        TorrentURL: "https://old.test/171.torrent",
        TorrentHash: "hash-171",
        Status: model.DownloadStatusCompleted,
        RenamedPath: "/media/Offset Anime S01E01.mkv",
    }).Error)
    require.NoError(t, db.Create(&model.Download{
        SubscriptionID: sub.ID,
        Episode: 173,
        Title: "Offset Anime - 173",
        TorrentURL: "https://old.test/173.torrent",
        TorrentHash: "hash-173",
        Status: model.DownloadStatusDownloading,
    }).Error)

    require.NoError(t, RunMigrations(db))

    var episodes []model.SubscriptionEpisode
    require.NoError(t, db.Order("episode").Find(&episodes).Error)
    require.Len(t, episodes, 2)
    assert.Equal(t, 1, episodes[0].Episode)
    assert.Equal(t, model.EpisodeStatusDownloaded, episodes[0].Status)
    assert.Equal(t, 3, episodes[1].Episode)
    assert.Equal(t, model.EpisodeStatusDownloading, episodes[1].Status)
}

func TestRunMigrationsMergesSameEpisodeResourcesDeterministically(t *testing.T) {
    db := openMigrationTestDB(t)
    require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}))
    sub := model.Subscription{Name: "Merge Anime", RssURL: "https://merge.test/rss"}
    require.NoError(t, db.Create(&sub).Error)
    require.NoError(t, db.Create(&model.Download{
        SubscriptionID: sub.ID, Episode: 1, Title: "E01 pending",
        TorrentURL: "https://merge.test/pending", TorrentHash: "pending-hash",
        Status: model.DownloadStatusDownloading,
    }).Error)
    completed := model.Download{
        SubscriptionID: sub.ID, Episode: 1, Title: "E01 completed",
        TorrentURL: "https://merge.test/completed", TorrentHash: "completed-hash",
        Status: model.DownloadStatusCompleted, RenamedPath: "/media/E01.mkv",
    }
    require.NoError(t, db.Create(&completed).Error)

    require.NoError(t, RunMigrations(db))
    var episodes []model.SubscriptionEpisode
    require.NoError(t, db.Find(&episodes).Error)
    require.Len(t, episodes, 1)
    assert.Equal(t, model.EpisodeStatusDownloaded, episodes[0].Status)
    assert.Equal(t, completed.ID, *episodes[0].ActiveDownloadID)
    assert.Equal(t, "completed-hash", episodes[0].ActiveTorrentHash)
    assert.Equal(t, "https://merge.test/completed", episodes[0].ActiveTorrentURL)
}

func TestRunMigrationsCreatesMissingRowsWithoutMarkingCurrentRangeDownloaded(t *testing.T) {
    db := openMigrationTestDB(t)
    require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}))
    sub := model.Subscription{
        Name: "Known Range", RssURL: "https://range.test/rss",
        CurrentEpisode: 3, LatestEpisode: 3, TotalEpisodes: 3,
    }
    require.NoError(t, db.Create(&sub).Error)

    require.NoError(t, RunMigrations(db))

    var episodes []model.SubscriptionEpisode
    require.NoError(t, db.Order("episode").Find(&episodes).Error)
    require.Len(t, episodes, 3)
    for index, ledger := range episodes {
        assert.Equal(t, index+1, ledger.Episode)
        assert.Equal(t, model.EpisodeStatusMissing, ledger.Status)
    }
}

func TestRunMigrationsEpisodeLedgerConstraintsAreIdempotent(t *testing.T) {
    db := openMigrationTestDB(t)
    require.NoError(t, RunMigrations(db))
    require.NoError(t, RunMigrations(db))
    first := model.SubscriptionEpisode{SubscriptionID: 1, Episode: 1, Status: model.EpisodeStatusMissing, StatusSource: "test"}
    require.NoError(t, db.Create(&first).Error)
    duplicate := model.SubscriptionEpisode{SubscriptionID: 1, Episode: 1, Status: model.EpisodeStatusMissing, StatusSource: "test"}
    require.Error(t, db.Create(&duplicate).Error)
}

func TestRunMigrationsMarksExistingRSSSubscriptionsForSafeBaseline(t *testing.T) {
    db := openMigrationTestDB(t)
    require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}))
    sub := model.Subscription{Name: "Existing", RssURL: "https://existing.test/rss"}
    require.NoError(t, db.Create(&sub).Error)
    require.NoError(t, RunMigrations(db))
    require.NoError(t, db.First(&sub, sub.ID).Error)
    assert.True(t, sub.RSSBaselinePending)
}
```

- [ ] **Step 2: Run migration tests and verify RED**

Run: `go test ./internal/pkg/database -run 'TestRunMigrations(BackfillsEpisodeLedger|MergesSameEpisodeResources|CreatesMissingRows|EpisodeLedgerConstraints|MarksExistingRSS)' -count=1`

Expected: FAIL，提示 `model.SubscriptionEpisode`、状态常量或新表不存在。

- [ ] **Step 3: Add model types and constants**

在 `internal/model/subscription_episode.go` 定义：

```go
package model

import "time"

const (
    EpisodeStatusMissing          = "missing"
    EpisodeStatusDownloading      = "downloading"
    EpisodeStatusDownloaded       = "downloaded"
    EpisodeStatusMarkedDownloaded = "marked_downloaded"
    EpisodeStatusIgnored          = "ignored"

    CandidateStatusPending               = "pending"
    CandidateStatusKeptExisting          = "kept_existing"
    CandidateStatusReplacing             = "replacing"
    CandidateStatusAccepted              = "accepted"
    CandidateStatusAcceptedCleanupFailed = "accepted_cleanup_failed"
    CandidateStatusFailed                = "failed"

    DownloadPurposeNormal      = "normal"
    DownloadPurposeReplacement = "replacement"

    EpisodeStatusSourceAutomatic = "automatic"
    EpisodeStatusSourceUser      = "user"
    EpisodeStatusSourceMigration = "migration"
)

type SubscriptionEpisode struct {
    ID                 uint       `json:"id" gorm:"primaryKey"`
    SubscriptionID     uint       `json:"subscription_id" gorm:"uniqueIndex:idx_subscription_episode,priority:1;index"`
    Episode            int        `json:"episode" gorm:"uniqueIndex:idx_subscription_episode,priority:2"`
    Status             string     `json:"status" gorm:"size:32;not null;index"`
    ActiveDownloadID   *uint      `json:"active_download_id" gorm:"index"`
    ActiveTorrentHash  string     `json:"active_torrent_hash" gorm:"size:128"`
    ActiveTorrentURL   string     `json:"active_torrent_url" gorm:"type:text"`
    ActiveTitle        string     `json:"active_title" gorm:"type:text"`
    StatusSource       string     `json:"status_source" gorm:"size:32;not null"`
    DownloadedAt       *time.Time `json:"downloaded_at"`
    CreatedAt          time.Time  `json:"created_at"`
    UpdatedAt          time.Time  `json:"updated_at"`
}

type EpisodeResource struct {
    Hash  string
    URL   string
    Title string
}

type EpisodeResourceCandidate struct {
    ID                    uint       `json:"id" gorm:"primaryKey"`
    SubscriptionEpisodeID uint       `json:"subscription_episode_id" gorm:"uniqueIndex:idx_episode_candidate_resource,priority:1;index;not null"`
    ResourceKey           string     `json:"resource_key" gorm:"uniqueIndex:idx_episode_candidate_resource,priority:2;size:512"`
    TorrentHash           string     `json:"torrent_hash" gorm:"size:128"`
    TorrentURL            string     `json:"torrent_url" gorm:"type:text"`
    Title                 string     `json:"title" gorm:"type:text"`
    Fansub                string     `json:"fansub" gorm:"size:100"`
    Language              string     `json:"language" gorm:"size:16"`
    PubTime               *time.Time `json:"pub_time"`
    SourceRSSURL          string     `json:"source_rss_url" gorm:"type:text"`
    Status                string     `json:"status" gorm:"size:40;not null;index"`
    FailureReason         string     `json:"failure_reason" gorm:"type:text"`
    StagedPath            string     `json:"staged_path" gorm:"type:text"`
    OldResourcePath       string     `json:"old_resource_path" gorm:"type:text"`
    RollbackPath          string     `json:"rollback_path" gorm:"type:text"`
    FinalPath             string     `json:"final_path" gorm:"type:text"`
    ReplacementStage      string     `json:"replacement_stage" gorm:"size:40;index"`
    ReplacementDownloadID *uint      `json:"replacement_download_id" gorm:"index"`
    CreatedAt             time.Time  `json:"created_at"`
    UpdatedAt             time.Time  `json:"updated_at"`
}

func (SubscriptionEpisode) TableName() string { return "subscription_episodes" }
func (EpisodeResourceCandidate) TableName() string { return "episode_resource_candidates" }
```

给 `Subscription` 增加：

```go
RSSBaselinePending bool `json:"rss_baseline_pending" gorm:"default:false;index"`
```

给 `Download` 增加：

```go
Purpose                string `json:"purpose" gorm:"size:20;default:normal;index"`
ReplacementCandidateID *uint  `json:"replacement_candidate_id" gorm:"index"`
```

普通 RSS、手动采集和合集下载显式写 `Purpose: model.DownloadPurposeNormal`；人工替换写 `DownloadPurposeReplacement`。

- [ ] **Step 4: Add idempotent migration and backfill**

在 `RunMigrations` 末尾增加迁移 ID `202607110001`：

```go
{
    ID: "202607110001",
    Migrate: func(tx *gorm.DB) error {
        if err := tx.AutoMigrate(
            &model.Subscription{},
            &model.Download{},
            &model.SubscriptionEpisode{},
            &model.EpisodeResourceCandidate{},
        ); err != nil {
            return err
        }
        return backfillSubscriptionEpisodes(tx)
    },
}
```

同步更新 `database.Migrate`、初始迁移和 `ResetMigrations` 的模型列表，使全新数据库、测试数据库和开发重置都包含两张新表。

实现 `backfillSubscriptionEpisodes`：按 `subscription_id, episode` 读取 `episode > 0` 的下载；用 `sub.RelativeEpisode(download.Episode)` 转换；按 `completed > organizing/downloading/pending/stalled > failed` 选择台账状态；同等级优先 `renamed_path != ''`，再按 `updated_at DESC`；使用 `Where("subscription_id = ? AND episode = ?", sub.ID, relativeEpisode).FirstOrCreate(&ledger)` 保证可重复运行。

随后按已知范围补 `missing`：优先使用 `TotalEpisodes`，否则使用 `sub.RelativeEpisode(LatestEpisode)`；只为不存在的集数创建 missing。`CurrentEpisode` 绝不能用于标记 downloaded，也不能作为范围上限覆盖更可靠的 total/latest。

迁移最后把 `rss_url != ''` 且非 calendar-only 的现有订阅设为 `rss_baseline_pending=true`，使升级后的首次抓取只做安全对账和水位线重建。

- [ ] **Step 5: Run migration tests and verify GREEN**

Run: `go test ./internal/pkg/database -count=1`

Expected: PASS。

- [ ] **Step 6: Commit model and migration**

```bash
git add internal/model/subscription_episode.go internal/model/subscription.go internal/model/download.go internal/pkg/database/migration.go internal/pkg/database/database.go internal/pkg/database/migration_test.go
git commit -m "Add episode ledger schema and migration"
```

## Task 2: 实现剧集仓储和状态服务

**Files:**
- Create: `internal/repository/episode.go`
- Create: `internal/repository/episode_test.go`
- Modify: `internal/repository/subscription.go`
- Create: `internal/repository/subscription_delete_test.go`
- Create: `internal/service/episode/service.go`
- Create: `internal/service/episode/service_test.go`

- [ ] **Step 1: Write failing repository tests**

覆盖以下真实行为：

```go
func TestEpisodeRepositoryClaimMissingCreatesSingleDownloadingEpisode(t *testing.T) {
    repo, _ := setupEpisodeRepository(t)
    first, claimed, err := repo.ClaimForDownload(1, 2, model.EpisodeResource{Hash: "a", URL: "https://x/a", Title: "E02"})
    require.NoError(t, err)
    assert.True(t, claimed)

    second, claimed, err := repo.ClaimForDownload(1, 2, model.EpisodeResource{Hash: "b", URL: "https://x/b", Title: "E02 v2"})
    require.NoError(t, err)
    assert.False(t, claimed)
    assert.Equal(t, first.ID, second.ID)
}

func TestEpisodeRepositoryDetachDownloadKeepsDownloadedStateAndResourceIdentity(t *testing.T) {
    repo, db := setupEpisodeRepository(t)
    download := model.Download{SubscriptionID: 1, Episode: 1, Title: "E01", TorrentURL: "https://x/e01", TorrentHash: "hash-e01", Status: model.DownloadStatusCompleted}
    require.NoError(t, db.Create(&download).Error)
    ledger := model.SubscriptionEpisode{
        SubscriptionID: 1, Episode: 1, Status: model.EpisodeStatusDownloaded,
        ActiveDownloadID: &download.ID, ActiveTorrentHash: download.TorrentHash,
        ActiveTorrentURL: download.TorrentURL, ActiveTitle: download.Title, StatusSource: "automatic",
    }
    require.NoError(t, db.Create(&ledger).Error)

    require.NoError(t, repo.DetachDownload(download.ID))
    got, err := repo.GetBySubscriptionAndEpisode(1, 1)
    require.NoError(t, err)
    assert.Nil(t, got.ActiveDownloadID)
    assert.Equal(t, model.EpisodeStatusDownloaded, got.Status)
    assert.Equal(t, "hash-e01", got.ActiveTorrentHash)
    assert.Equal(t, "https://x/e01", got.ActiveTorrentURL)
    assert.Equal(t, "E01", got.ActiveTitle)
}

func TestDeleteSubscriptionRemovesEpisodeLedgerAndCandidates(t *testing.T) {
    subscriptionRepo, episodeRepo, db := setupSubscriptionEpisodeRepositories(t)
    sub := model.Subscription{Name: "Delete Anime", RssURL: "https://delete.test/rss"}
    require.NoError(t, subscriptionRepo.Create(&sub))
    ledger, err := episodeRepo.ObserveEpisode(sub.ID, 1)
    require.NoError(t, err)
    _, _, err = episodeRepo.UpsertCandidate(ledger.ID, &model.EpisodeResourceCandidate{ResourceKey: "hash:x", TorrentHash: "x", Status: model.CandidateStatusPending})
    require.NoError(t, err)

    require.NoError(t, subscriptionRepo.Delete(sub.ID))

    var episodeCount, candidateCount int64
    require.NoError(t, db.Model(&model.SubscriptionEpisode{}).Count(&episodeCount).Error)
    require.NoError(t, db.Model(&model.EpisodeResourceCandidate{}).Count(&candidateCount).Error)
    assert.Zero(t, episodeCount)
    assert.Zero(t, candidateCount)
}

func TestUpsertCandidateDoesNotReopenKeptExistingResource(t *testing.T) {
    repo, db := setupEpisodeRepository(t)
    ledger := model.SubscriptionEpisode{SubscriptionID: 1, Episode: 1, Status: model.EpisodeStatusDownloaded, StatusSource: "migration"}
    require.NoError(t, db.Create(&ledger).Error)
    candidate := model.EpisodeResourceCandidate{
        SubscriptionEpisodeID: ledger.ID, ResourceKey: "hash:new", TorrentHash: "new",
        TorrentURL: "https://x/new", Status: model.CandidateStatusKeptExisting,
    }
    require.NoError(t, db.Create(&candidate).Error)

    got, created, err := repo.UpsertCandidate(ledger.ID, &model.EpisodeResourceCandidate{
        ResourceKey: "hash:new", TorrentHash: "new", TorrentURL: "https://x/new", Status: model.CandidateStatusPending,
    })
    require.NoError(t, err)
    assert.False(t, created)
    assert.Equal(t, model.CandidateStatusKeptExisting, got.Status)
}
```

- [ ] **Step 2: Run repository tests and verify RED**

Run: `go test ./internal/repository -run Episode -count=1`

Expected: FAIL，提示 episode repository 不存在。

- [ ] **Step 3: Implement repository interface**

在 `internal/repository/episode.go` 定义集中接口：

```go
type EpisodeRepository interface {
    ListBySubscription(subscriptionID uint) ([]model.SubscriptionEpisode, error)
    ListWithCandidateCounts(subscriptionID uint) ([]EpisodeWithCandidateCount, error)
    GetBySubscriptionAndEpisode(subscriptionID uint, episode int) (*model.SubscriptionEpisode, error)
    ClaimForDownload(subscriptionID uint, episode int, resource model.EpisodeResource) (*model.SubscriptionEpisode, bool, error)
    AttachDownload(episodeID, downloadID uint) error
    MarkDownloaded(episodeID, downloadID uint, resource model.EpisodeResource, at time.Time) error
    MarkDownloadedInTx(tx *gorm.DB, episodeID, downloadID uint, resource model.EpisodeResource, at time.Time) error
    MarkMissingIfActiveDownload(downloadID uint) error
    DetachDownload(downloadID uint) error
    SetStatus(subscriptionID uint, episodes []int, status, source string) error
    UpsertCandidate(episodeID uint, candidate *model.EpisodeResourceCandidate) (*model.EpisodeResourceCandidate, bool, error)
    ListCandidates(episodeID uint) ([]model.EpisodeResourceCandidate, error)
    UpdateCandidate(candidate *model.EpisodeResourceCandidate) error
    ObserveEpisode(subscriptionID uint, episode int) (*model.SubscriptionEpisode, error)
    EnsureRange(subscriptionID uint, totalEpisodes int) error
    RefreshSubscriptionProgress(subscriptionID uint) error
}

type EpisodeWithCandidateCount struct {
    model.SubscriptionEpisode
    ActionRequiredCandidateCount int64 `json:"action_required_candidate_count" gorm:"column:action_required_candidate_count"`
}
```

新增 `model.EpisodeResource` 纯值类型，避免 service 依赖 RSS 包。`ClaimForDownload` 在事务中创建或读取唯一台账，只允许无记录或 `missing` 原子变为 `downloading`；SQLite 使用条件更新和唯一约束处理并发，不依赖 `FOR UPDATE`。

`ListWithCandidateCounts` 使用一次 `LEFT JOIN` 和条件计数返回 `pending`、`failed`、`accepted_cleanup_failed` 的待处理数量，禁止 handler 对每一集执行单独 count 查询。

同时把 `SubscriptionRepository.Delete` 和 `BatchDelete` 改成事务：当台账表存在时，先按 subscription IDs 删除 `episode_resource_candidates`，再删除 `subscription_episodes`，最后删除 subscriptions。使用 `Migrator().HasTable` 兼容旧结构测试，不依赖 SQLite 默认未开启的外键级联。

- [ ] **Step 4: Write failing service tests**

```go
func TestServiceEvaluateRSSCreatesCandidateForDownloadedEpisode(t *testing.T) {
    svc, _ := setupEpisodeService(t)
    seedDownloadedEpisode(t, svc, 1, 4, model.EpisodeResource{Hash: "old", URL: "https://x/old"})

    decision, err := svc.EvaluateRSSItem(context.Background(), &model.Subscription{ID: 1}, episode.RSSResource{
        OriginalEpisode: 4,
        Resource: model.EpisodeResource{Hash: "new", URL: "https://x/new", Title: "E04 v2"},
    }, false)

    require.NoError(t, err)
    assert.Equal(t, episode.DecisionCandidate, decision.Action)
    assert.NotZero(t, decision.CandidateID)
}

func TestServiceProgressUsesContinuousOwnedEpisodes(t *testing.T) {
    svc, db := setupEpisodeService(t)
    sub := model.Subscription{Name: "Progress Anime", RssURL: "https://x/rss", EpisodeOffset: 170}
    require.NoError(t, db.Create(&sub).Error)
    require.NoError(t, db.Create(&[]model.SubscriptionEpisode{
        {SubscriptionID: sub.ID, Episode: 1, Status: model.EpisodeStatusDownloaded, StatusSource: "migration"},
        {SubscriptionID: sub.ID, Episode: 2, Status: model.EpisodeStatusMarkedDownloaded, StatusSource: "user"},
        {SubscriptionID: sub.ID, Episode: 4, Status: model.EpisodeStatusDownloaded, StatusSource: "migration"},
    }).Error)

    require.NoError(t, svc.RefreshSubscriptionProgress(sub.ID))
    require.NoError(t, db.First(&sub, sub.ID).Error)
    assert.Equal(t, 172, sub.CurrentEpisode)
}

func TestResourceKeyPrefersCaseInsensitiveHashThenNormalizedURL(t *testing.T) {
    assert.Equal(t, "hash:abcdef", episode.ResourceKey(model.EpisodeResource{Hash: "ABCDEF", URL: "https://ignored.test"}))
    assert.Equal(t, "url:https://x/e01", episode.ResourceKey(model.EpisodeResource{URL: "  https://x/e01  "}))
    assert.Empty(t, episode.ResourceKey(model.EpisodeResource{}))
}
```

- [ ] **Step 5: Run service tests and verify RED**

Run: `go test ./internal/service/episode -count=1`

Expected: FAIL，提示 service 和 decision 类型不存在。

- [ ] **Step 6: Implement state service and resource comparison**

在 `internal/service/episode/service.go` 定义：

```go
const (
    DecisionDownload  = "download"
    DecisionSkip      = "skip"
    DecisionCandidate = "candidate"
    DecisionIgnored   = "ignored"
    DecisionBaseline  = "baseline_missing"
)

type RSSResource struct {
    OriginalEpisode int
    Resource        model.EpisodeResource
    Fansub          string
    Language        string
    PubTime         time.Time
    SourceRSSURL    string
}

type RSSDecision struct {
    Action      string
    EpisodeID   uint
    CandidateID uint
    Reason      string
}
```

`Service` 对外提供与调用方一致的窄方法：

```go
func (s *Service) ObserveRSSItem(sub *model.Subscription, originalEpisode int) (*model.SubscriptionEpisode, error)
func (s *Service) EvaluateRSSItem(ctx context.Context, sub *model.Subscription, item RSSResource, baseline bool) (RSSDecision, error)
func (s *Service) PreviewRSSItem(sub *model.Subscription, item RSSResource) (RSSDecision, error)
func (s *Service) MarkDownloadCompleted(download *model.Download, sub *model.Subscription, completedAt time.Time) error
func (s *Service) MarkDownloadCompletedInTx(tx *gorm.DB, download *model.Download, sub *model.Subscription, completedAt time.Time) error
func (s *Service) MarkDownloadFailed(downloadID uint) error
func (s *Service) DetachDownload(downloadID uint) error
func (s *Service) EnsureRange(subscriptionID uint, totalEpisodes int) error
func (s *Service) RefreshSubscriptionProgress(subscriptionID uint) error
```

实现规则：

- 相对集数小于等于 0 返回 `skip`。
- `ignored` 返回 `ignored`，不建候选。
- 无记录或 `missing`：普通同步返回 `download`；基线同步只创建/保持 `missing` 并返回 `baseline_missing`。
- `downloading`、`downloaded`、`marked_downloaded`：资源相同返回 `skip`，不同或当前资源身份为空返回幂等候选。
- `ResourceKey` 优先返回小写 hash；无 hash 时使用 trim 后 URL；两者都为空时不创建候选，返回 `skip` 并记录 `resource_identity_missing`，避免不可幂等的候选污染数据库。
- `ObserveRSSItem` 为所有合法相对集数建立 `missing` 台账，使过滤掉的资源仍能贡献“已知最新集数”，但不创建候选或下载。
- `EnsureRange` 幂等创建 `1..totalEpisodes` 的 missing 行，不覆盖现有状态；totalEpisodes 减小时不删除超出范围的历史台账。
- `RefreshSubscriptionProgress` 只把连续的 `downloaded`、`marked_downloaded`、`ignored` 计入完成进度；`CurrentEpisode` 在连续进度为 0 时写 0，否则加 `EpisodeOffset`；`LatestEpisode` 使用台账最大相对集数，在最大值为 0 时写 0，否则加偏移。刷新后按现有 `IsCompleted` 语义设置首次 `CompletedAt`，未完结时清除陈旧值，使手动标记立即反映在智能拉取和 UI 中。

- [ ] **Step 7: Run repository and service tests**

Run: `go test ./internal/repository ./internal/service/episode -count=1`

Expected: PASS。

- [ ] **Step 8: Commit repository and service**

```bash
git add internal/model/subscription_episode.go internal/repository/episode.go internal/repository/episode_test.go internal/repository/subscription.go internal/repository/subscription_delete_test.go internal/service/episode/service.go internal/service/episode/service_test.go
git commit -m "Add episode ledger state service"
```

## Task 3: 将定时 RSS 决策接入台账

**Files:**
- Modify: `internal/service/scheduler/scheduler.go`
- Modify: `internal/service/scheduler/language_filter.go`
- Modify: `internal/service/scheduler/smart_fetch.go`
- Modify: `internal/api/router/router.go`
- Test: `internal/service/scheduler/scheduler_test.go`
- Test: `internal/service/scheduler/small_torrent_guard_test.go`
- Test: `internal/service/scheduler/smart_fetch_completed_test.go`

- [ ] **Step 1: Write failing scheduler regression tests**

新增集成风格测试：

```go
func TestRSSCheckDoesNotReplaceDownloadedEpisodeWithDifferentHash(t *testing.T) {
    fx := newSchedulerEpisodeFixture(t)
    sub, oldDownload := fx.seedDownloadedEpisode(1, "old-hash", "https://x/old")
    fx.parser.items = []rss.RSSItem{{
        Title: "Anime - 01 v2", Episode: 1, TorrentHash: "new-hash",
        TorrentURL: "https://x/new", PubTime: time.Now(),
    }}

    fx.scheduler.checkRSSFeeds()

    assert.Equal(t, 0, fx.qb.addCalls)
    assert.Equal(t, 0, fx.qb.deletePayloadCalls)
    require.NoError(t, fx.db.First(&model.Download{}, oldDownload.ID).Error)
    ledger, err := fx.episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 1)
    require.NoError(t, err)
    candidates, err := fx.episodeRepo.ListCandidates(ledger.ID)
    require.NoError(t, err)
    require.Len(t, candidates, 1)
    assert.Equal(t, model.CandidateStatusPending, candidates[0].Status)
}

func TestRSSCheckDownloadsNewMissingEpisodeOnce(t *testing.T) {
    fx := newSchedulerEpisodeFixture(t)
    fx.seedSubscription()
    now := time.Now()
    fx.parser.items = []rss.RSSItem{
        {Title: "Anime - 02", Episode: 2, TorrentHash: "hash-a", TorrentURL: "https://x/a", PubTime: now},
        {Title: "Anime - 02 v2", Episode: 2, TorrentHash: "hash-b", TorrentURL: "https://x/b", PubTime: now.Add(time.Second)},
    }

    fx.scheduler.checkRSSFeeds()

    assert.Equal(t, 1, fx.qb.addCalls)
    var downloads int64
    require.NoError(t, fx.db.Model(&model.Download{}).Count(&downloads).Error)
    assert.EqualValues(t, 1, downloads)
    var candidates int64
    require.NoError(t, fx.db.Model(&model.EpisodeResourceCandidate{}).Count(&candidates).Error)
    assert.EqualValues(t, 1, candidates)
}
```

在同一测试文件实现 `newSchedulerEpisodeFixture`，使用内存 SQLite，迁移 `Subscription`、`Download`、`SubscriptionEpisode`、`EpisodeResourceCandidate` 和 `Config`；fake parser 返回可配置 items；fake qB 分别记录 add、remove task 和 delete payload 次数。

- [ ] **Step 2: Run focused scheduler tests and verify RED**

Run: `go test ./internal/service/scheduler -run 'TestRSSCheckDoesNotReplace|TestRSSCheckDownloadsNewMissing' -count=1`

Expected: 第一个测试 FAIL，因为现有代码会进入语言版本替换或删除旧任务。

- [ ] **Step 3: Inject episode service into scheduler**

修改构造函数签名：

```go
func NewScheduler(
    db *gorm.DB,
    subscriptionRepo repository.SubscriptionRepository,
    downloadRepo repository.DownloadRepository,
    configRepo repository.ConfigRepository,
    rssCheckInterval string,
    rssParser rss.Parser,
    qbClient downloader.QBittorrentClient,
    episodeService *episode.Service,
) Scheduler
```

在 router 中初始化 `episodeRepo := repository.NewEpisodeRepository(db)` 和 `episodeService := episode.NewService(db, episodeRepo, subscriptionRepo)`，再传给 scheduler。

`NewSmartFetchFilter` 同步改成：

```go
func NewSmartFetchFilter(downloadRepo repository.DownloadRepository, episodeRepo repository.EpisodeRepository) *SmartFetchFilter
```

- [ ] **Step 4: Replace automatic replacement branch**

在 RSS 循环中，完成发布时间、创建时间、偏移和总集数检查后先调用 `ObserveRSSItem`，保证已知最新集数进入台账；随后执行关键词和语言过滤。只有过滤通过的资源才调用：

```go
decision, err := s.episodeService.EvaluateRSSItem(ctx, &sub, episode.RSSResource{
    OriginalEpisode: item.Episode,
    Resource: model.EpisodeResource{Hash: item.TorrentHash, URL: item.TorrentURL, Title: item.Title},
    Fansub: item.Fansub,
    Language: string(item.Language),
    PubTime: item.PubTime,
    SourceRSSURL: sub.RssURL,
}, false)
```

只有 `DecisionDownload` 调用 `processDownloadItem`。删除以下行为：

- `LanguageFilter.CheckLanguageAllow` 驱动的自动 replacement ID。
- RSS 调度器中的 `DeleteTorrentWithPayload` 替换分支。
- `processDownloadItem` 的 `replaceDownloadID` 参数和事务内旧记录删除。

保留 `LanguageFilter` 只做语言允许/拒绝，不再返回替换目标；若语言偏好不再适用于“单资源”模式，则在本任务中把它简化成下载前过滤，不查询同集历史资源。

- [ ] **Step 5: Attach created download to claimed episode**

让 `EvaluateRSSItem` 返回 `EpisodeID`。`processDownloadItem` 创建下载记录后在同一数据库事务调用 `AttachDownload`；qB 添加失败时调用 `MarkMissingIfActiveDownload(download.ID)`。

- [ ] **Step 6: Move smart-fetch completeness to ledger**

修改 `SmartFetchFilter` 注入 `EpisodeRepository`，`getMissingEpisodes` 查询台账。`downloaded`、`marked_downloaded`、`ignored` 不缺失；`downloading` 不列入“需要再次创建任务”，但可在状态说明中标为处理中。

- [ ] **Step 7: Run scheduler tests and full package tests**

Run: `go test ./internal/service/scheduler -count=1`

Expected: PASS，且现有小种子保护测试不再传入 replacement ID。

- [ ] **Step 8: Commit scheduler integration**

```bash
git add internal/service/scheduler internal/api/router/router.go
git commit -m "Use episode ledger for RSS scheduling"
```

## Task 4: 增加 RSS 换源基线对账

**Files:**
- Modify: `internal/api/handler/subscription.go`
- Modify: `internal/service/scheduler/scheduler.go`
- Modify: `internal/api/router/router.go`
- Test: `internal/api/handler/subscription_test.go`
- Test: `internal/service/scheduler/scheduler_test.go`

- [ ] **Step 1: Write failing update-handler test**

```go
func TestUpdateMarksRSSBaselinePendingOnlyWhenNormalizedURLChanges(t *testing.T) {
    existing := &model.Subscription{ID: 1, RssURL: "https://old.test/feed"}
    handler, repo := newSubscriptionUpdateFixture(existing)

    changed := performSubscriptionUpdate(t, handler, 1, `{"rss_url":"https://new.test/feed"}`)
    assert.Equal(t, http.StatusOK, changed.Code)
    assert.True(t, repo.saved.RSSBaselinePending)

    existing.RssURL = "https://same.test/feed"
    existing.RSSBaselinePending = false
    unchanged := performSubscriptionUpdate(t, handler, 1, `{"rss_url":"  https://same.test/feed  ","name":"Renamed"}`)
    assert.Equal(t, http.StatusOK, unchanged.Code)
    assert.False(t, repo.saved.RSSBaselinePending)
}

func TestCreateAndUpdateEnsureKnownEpisodeRange(t *testing.T) {
    handler, fx := newSubscriptionEpisodeHandlerFixture(t)
    created := performSubscriptionCreate(t, handler, `{"name":"Range Anime","rss_url":"https://range.test/rss","total_episodes":3}`)
    require.Equal(t, http.StatusOK, created.Code)
    assert.Equal(t, []int{1, 2, 3}, fx.listLedgerNumbers())
    assert.True(t, fx.subscription.RSSBaselinePending)

    updated := performSubscriptionUpdate(t, handler, fx.subscription.ID, `{"total_episodes":5}`)
    require.Equal(t, http.StatusOK, updated.Code)
    assert.Equal(t, []int{1, 2, 3, 4, 5}, fx.listLedgerNumbers())

    reduced := performSubscriptionUpdate(t, handler, fx.subscription.ID, `{"total_episodes":2}`)
    require.Equal(t, http.StatusOK, reduced.Code)
    assert.Equal(t, []int{1, 2, 3, 4, 5}, fx.listLedgerNumbers())
}
```

- [ ] **Step 2: Write failing baseline scheduler test**

```go
func TestRSSBaselineCreatesMissingRowsAndCandidatesWithoutDownloadingHistory(t *testing.T) {
    fx := newSchedulerEpisodeFixture(t)
    sub, _ := fx.seedDownloadedEpisode(1, "old-hash", "https://old.test/e01")
    require.NoError(t, fx.db.Model(&model.Subscription{}).Where("id = ?", sub.ID).Updates(map[string]any{
        "rss_url": "https://new.test/feed", "rss_baseline_pending": true,
    }).Error)
    base := time.Now().Add(-time.Hour)
    fx.parser.items = []rss.RSSItem{
        {Title: "E01 new", Episode: 1, TorrentHash: "new-hash", TorrentURL: "https://new.test/e01", PubTime: base},
        {Title: "E02", Episode: 2, TorrentHash: "hash-2", TorrentURL: "https://new.test/e02", PubTime: base.Add(time.Minute)},
        {Title: "E03", Episode: 3, TorrentHash: "hash-3", TorrentURL: "https://new.test/e03", PubTime: base.Add(2 * time.Minute)},
    }

    fx.scheduler.checkRSSFeeds()

    assert.Equal(t, 0, fx.qb.addCalls)
    for _, episodeNumber := range []int{2, 3} {
        ledger, err := fx.episodeRepo.GetBySubscriptionAndEpisode(sub.ID, episodeNumber)
        require.NoError(t, err)
        assert.Equal(t, model.EpisodeStatusMissing, ledger.Status)
    }
    ledger, err := fx.episodeRepo.GetBySubscriptionAndEpisode(sub.ID, 1)
    require.NoError(t, err)
    candidates, err := fx.episodeRepo.ListCandidates(ledger.ID)
    require.NoError(t, err)
    require.Len(t, candidates, 1)
    require.NoError(t, fx.db.First(&sub, sub.ID).Error)
    assert.False(t, sub.RSSBaselinePending)
    assert.Equal(t, base.Add(2*time.Minute), *sub.LastRSSPubTime)
}

func TestSmartFetchAlwaysRunsPendingRSSBaseline(t *testing.T) {
    filter := NewSmartFetchFilter(nil, nil)
    filter.strategy = &SmartFetchStrategy{Enabled: true, CompletedStopDays: 1}
    completedAt := time.Now().Add(-30 * 24 * time.Hour)
    sub := model.Subscription{
        ID: 1, Name: "Completed", TotalEpisodes: 12, CurrentEpisode: 12,
        CompletedAt: &completedAt, RSSBaselinePending: true,
    }
    status, _ := filter.EvaluateSubscription(&sub)
    assert.True(t, status.ShouldFetch)
    assert.Equal(t, "rss_baseline_pending", status.FetchReason)
}
```

- [ ] **Step 3: Run tests and verify RED**

Run: `go test ./internal/api/handler ./internal/service/scheduler -run 'RSSBaseline|BaselinePending' -count=1`

Expected: FAIL，因为更新接口不标记基线且 scheduler 会按普通增量处理。

- [ ] **Step 4: Inject episode service and implement normalized URL change detection**

保持大量现有测试可用，给构造器增加最后一个可选 episode service 参数：

```go
func NewSubscriptionHandler(
    repo repository.SubscriptionRepository,
    downloadRepo repository.DownloadRepository,
    configRepo repository.ConfigRepository,
    qbClient downloader.QBittorrentClient,
    downloadPath string,
    episodeServices ...*episode.Service,
) *SubscriptionHandler
```

router 传入真实服务；旧测试未传入时只允许不触发剧集功能的路径，新增测试使用真实 SQLite service。

在 handler 中保存 `originalRSSURL`，统一 `strings.TrimSpace` 后比较：

```go
if rssURL, ok := updates["rss_url"].(string); ok {
    normalized := strings.TrimSpace(rssURL)
    if normalized != strings.TrimSpace(existing.RssURL) {
        existing.RSSBaselinePending = normalized != ""
    }
    existing.RssURL = normalized
}
```

不清空 `LastRSSPubTime`，由成功的基线同步统一替换水位线。

新建带 RSS URL 的订阅默认写 `RSSBaselinePending=true`。订阅创建成功后调用 `EnsureRange(subscription.ID, subscription.TotalEpisodes)`；更新 `total_episodes` 后同样调用。范围初始化失败时创建/更新请求返回 500，并记录错误，不能静默留下部分语义。

- [ ] **Step 5: Implement baseline pass**

`SmartFetchFilter.EvaluateSubscription` 在 calendar-only 判断之后、完结和时间窗口判断之前检查 `RSSBaselinePending`，强制 `ShouldFetch=true` 和 `FetchReason=rss_baseline_pending`。

当 `sub.RSSBaselinePending` 为 true 时，对本次所有 RSS item 调用 `EvaluateRSSItem(ctx, &sub, resource, true)`；不调用下载创建。仅在完整遍历且候选/台账写入均成功后，事务更新 `LastRSSPubTime`、`LastCheckTime` 和 `RSSBaselinePending=false`。抓取或数据库失败时保持 pending。

- [ ] **Step 6: Run baseline tests**

Run: `go test ./internal/api/handler ./internal/service/scheduler -run 'RSSBaseline|BaselinePending' -count=1`

Expected: PASS。

- [ ] **Step 7: Commit baseline behavior**

```bash
git add internal/api/handler/subscription.go internal/api/handler/subscription_test.go internal/service/scheduler/scheduler.go internal/service/scheduler/scheduler_test.go internal/api/router/router.go
git commit -m "Reconcile episode ledger after RSS source changes"
```

## Task 5: 让预览和手动采集保持非破坏性

**Files:**
- Modify: `internal/api/handler/subscription.go`
- Test: `internal/api/handler/subscription_test.go`

- [ ] **Step 1: Write failing preview and collection tests**

```go
func TestPreviewMarksDifferentResourceAsManualCandidate(t *testing.T) {
    handler, fx := newSubscriptionEpisodeHandlerFixture(t)
    fx.seedDownloadedEpisode(1, "old-hash", "https://x/old")
    fx.parser.items = []rss.RSSItem{{Title: "E01 v2", Episode: 1, TorrentHash: "new-hash", TorrentURL: "https://x/new"}}

    recorder := performPreview(t, handler, SubscriptionPreviewRequest{ID: 1, Name: "Anime", RssURL: "https://x/rss", Season: 1})
    require.Equal(t, http.StatusOK, recorder.Code)
    item := decodeFirstPreviewItem(t, recorder.Body.Bytes())
    assert.Equal(t, "manual_review", item.Action)
    assert.Equal(t, "episode_already_owned_different_resource", item.Reason)
    assert.Zero(t, item.CandidateID)
}

func TestCollectEpisodesNeverDeletesExistingSameEpisode(t *testing.T) {
    handler, fx := newSubscriptionEpisodeHandlerFixture(t)
    _, oldDownload := fx.seedDownloadedEpisode(1, "old-hash", "https://x/old")
    fx.parser.items = []rss.RSSItem{{Title: "E01 v2", Episode: 1, TorrentHash: "new-hash", TorrentURL: "https://x/new"}}

    result := runCollectEpisodesSynchronously(t, handler, 1)
    assert.Equal(t, 0, result.Collected)
    assert.Equal(t, 1, result.Candidates)
    assert.Equal(t, 0, fx.qb.deletePayloadCalls)
    require.NoError(t, fx.db.First(&model.Download{}, oldDownload.ID).Error)
}
```

测试 fixture 使用内存 SQLite 和真实 episode service；通过 handler 已有的 parser 注入点设置 fake parser；`runCollectEpisodesSynchronously` 直接调用 `doCollectEpisodes` 并读取 task result，避免依赖异步轮询。

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./internal/api/handler -run 'PreviewMarksDifferent|CollectEpisodesNeverDeletes' -count=1`

Expected: FAIL，现有手动采集会删除旧记录。

- [ ] **Step 3: Use the injected episode service in preview and collection**

复用 Task 4 已注入的 episode service。preview 调用纯判断方法；实际采集调用持久化决策方法。若 handler 没有 episode service 且请求进入剧集路径，返回明确 500，不能回退到旧的自动替换逻辑。

- [ ] **Step 4: Replace episodeMap deletion logic**

`doCollectEpisodes` 不再构建用于删除旧任务的 `episodeMap`。每个条目调用台账决策：

- `download`：创建任务并 attach。
- `candidate`：计入 `manual_review`，不删除任何记录或 qB 数据。
- `skip/ignored`：跳过。

删除 `deletedCount` 的替换含义；为了 API 兼容保留 `deleted: 0`，新增 `candidates` 计数。

- [ ] **Step 5: Update preview response semantics**

将同集不同资源的 `Action` 改为 `manual_review`，`Reason` 为 `episode_already_owned_different_resource`。预览必须调用不写数据库的纯判断方法 `PreviewRSSItem`，因此 `CandidateID` 保持 0；只有实际定时同步或手动采集才持久化候选。不要再返回或使用 `ExistingDownloadID` 作为自动替换指令。

- [ ] **Step 6: Run handler tests**

Run: `go test ./internal/api/handler -count=1`

Expected: PASS。

- [ ] **Step 7: Commit non-destructive manual paths**

```bash
git add internal/api/handler/subscription.go internal/api/handler/subscription_test.go
git commit -m "Stop destructive same-episode collection"
```

## Task 6: 用下载完成、失败和删除驱动台账状态

**Files:**
- Modify: `internal/repository/download.go`
- Test: `internal/repository/download_status_test.go`
- Modify: `internal/service/downloader/monitor.go`
- Modify: `internal/service/downloader/completion_handler.go`
- Modify: `internal/service/downloader/status_sync.go`
- Modify: `internal/service/organizer/organizer.go`
- Modify: `internal/api/handler/download.go`
- Test: `internal/service/downloader/completion_handler_test.go`
- Test: `internal/service/downloader/status_sync_test.go`
- Test: `internal/service/organizer/organizer_test.go`
- Test: `internal/api/handler/download_test.go`
- Test: `internal/service/disk/monitor_test.go`

- [ ] **Step 1: Write failing completion tests**

覆盖：

```go
func TestHandleCompleteMarksEpisodeDownloadedOnlyAfterRenameSucceeds(t *testing.T) {
    fx := newCompletionEpisodeFixture(t, true)
    fx.qb.files = []TorrentFile{{Name: "episode.mkv", Size: 1024}}
    fx.qb.renameResult = nil

    require.NoError(t, fx.handler.HandleComplete(&fx.download, &TorrentInfo{Hash: fx.download.TorrentHash, SavePath: fx.tempDir}, &fx.subscription))

    ledger := fx.loadLedger()
    assert.Equal(t, model.EpisodeStatusDownloaded, ledger.Status)
    assert.Equal(t, fx.download.ID, *ledger.ActiveDownloadID)
    assert.NotNil(t, ledger.DownloadedAt)
}

func TestHandleCompleteLeavesEpisodeDownloadingWhenRenameFails(t *testing.T) {
    fx := newCompletionEpisodeFixture(t, true)
    fx.qb.files = []TorrentFile{{Name: "episode.mkv", Size: 1024}}
    fx.qb.renameResult = errors.New("rename failed")

    err := fx.handler.HandleComplete(&fx.download, &TorrentInfo{Hash: fx.download.TorrentHash, SavePath: fx.tempDir}, &fx.subscription)

    require.ErrorContains(t, err, "rename failed")
    ledger := fx.loadLedger()
    assert.Equal(t, model.EpisodeStatusDownloading, ledger.Status)
    var saved model.Download
    require.NoError(t, fx.db.First(&saved, fx.download.ID).Error)
    assert.NotEqual(t, model.DownloadStatusCompleted, saved.Status)
}

func TestHandleCompleteWithoutRenameMarksEpisodeDownloaded(t *testing.T) {
    fx := newCompletionEpisodeFixture(t, false)
    require.NoError(t, fx.handler.HandleComplete(&fx.download, &TorrentInfo{Hash: fx.download.TorrentHash, SavePath: fx.tempDir}, &fx.subscription))
    assert.Equal(t, model.EpisodeStatusDownloaded, fx.loadLedger().Status)
}
```

`newCompletionEpisodeFixture` 创建真实 SQLite download 和 downloading 台账，使用可配置 `renameResult` 的 fake qB client，并构造带真实 episode completion service 的 handler。

第一个测试断言下载记录和台账在同一成功路径中持久化；第二个断言现有“重命名失败仍 completed”的行为被修正。

- [ ] **Step 2: Write failing delete test**

```go
func TestDeleteDownloadDetachesEpisodeWithoutChangingDownloadedStatus(t *testing.T) {
    handler, fx := newDownloadHandlerEpisodeFixture(t)
    ledger, download := fx.seedDownloadedEpisode()

    recorder := performDeleteDownload(t, handler, download.ID)

    assert.Equal(t, http.StatusOK, recorder.Code)
    got, err := fx.episodeRepo.GetBySubscriptionAndEpisode(ledger.SubscriptionID, ledger.Episode)
    require.NoError(t, err)
    assert.Equal(t, model.EpisodeStatusDownloaded, got.Status)
    assert.Nil(t, got.ActiveDownloadID)
    assert.Equal(t, ledger.ActiveTorrentHash, got.ActiveTorrentHash)
}

func TestDeleteDownloadingTaskRestoresEpisodeMissing(t *testing.T) {
    repo, fx := newDownloadRepositoryEpisodeFixture(t)
    download := fx.seedDownloadingEpisode()
    require.NoError(t, repo.Delete(download.ID))
    ledger := fx.loadLedger()
    assert.Equal(t, model.EpisodeStatusMissing, ledger.Status)
    assert.Nil(t, ledger.ActiveDownloadID)
}
```

- [ ] **Step 3: Run tests and verify RED**

Run: `go test ./internal/service/downloader ./internal/service/organizer ./internal/api/handler -run 'MarksEpisode|LeavesEpisode|DetachesEpisode' -count=1`

Expected: FAIL，因为完成和删除流程尚未调用 episode service。

- [ ] **Step 4: Inject episode service into monitor and completion handler**

给 `NewDownloadMonitor`、`NewCompletionHandler` 增加 episode service 依赖，并在 router 中传入。测试 mock 可以实现窄接口：

```go
type EpisodeCompletionService interface {
    MarkDownloadCompleted(download *model.Download, subscription *model.Subscription, completedAt time.Time) error
    MarkDownloadCompletedInTx(tx *gorm.DB, download *model.Download, subscription *model.Subscription, completedAt time.Time) error
    MarkDownloadFailed(downloadID uint) error
    DetachDownload(downloadID uint) error
}
```

该接口定义在 `downloader` 包，不能让 downloader import `service/episode`。构造器使用接口：

```go
func NewDownloadMonitor(
    db *gorm.DB,
    qbClient QBittorrentClient,
    downloadRepo repository.DownloadRepository,
    subscriptionRepo repository.SubscriptionRepository,
    configRepo repository.ConfigRepository,
    episodeService EpisodeCompletionService,
    renameTemplate string,
    mediaLibrarySvc ...MediaLibraryRefresher,
) *DownloadMonitor

func NewCompletionHandler(
    subscriptionRepo repository.SubscriptionRepository,
    downloadRepo repository.DownloadRepository,
    notificationSvc NotificationService,
    renamerSvc *RenameService,
    qbClient QBittorrentClient,
    db *gorm.DB,
    episodeService EpisodeCompletionService,
    mediaLibrary ...MediaLibraryRefresher,
) CompletionHandler
```

- [ ] **Step 5: Make completion atomic at database layer**

重构 `HandleComplete`：

- `RenameEnabled && Episode > 0` 时，rename error 必须返回错误，下载保持非 completed，台账保持 downloading。
- rename 成功或 rename disabled 后，在 `db.Transaction` 内更新 download 为 completed，并调用 `episodeService.MarkDownloadCompletedInTx(tx, download, subscription, now)`。
- 事务成功后再发通知和刷新媒体库，避免外部副作用先于状态提交。

合集 `Episode == 0` 不建单集台账，保持现有合集处理。

- [ ] **Step 6: Wire failure and organizer success**

下载明确失败、取消或重试耗尽时调用 `MarkDownloadFailed`。organizer 定义相同形状的窄接口，在单个 `db.Transaction` 中更新 download 并调用 `MarkDownloadCompletedInTx`；移动失败保持 missing/downloading 的规则由 active download 关联决定。

- [ ] **Step 7: Make every repository deletion update the ledger**

重构 `DownloadRepository.Delete`、`BatchDelete`、`DeleteByStatus` 和 `DeleteAll`，在同一事务中先更新所有匹配的台账：

```sql
UPDATE subscription_episodes
SET active_download_id = NULL,
    status = CASE WHEN status = 'downloading' THEN 'missing' ELSE status END,
    updated_at = CURRENT_TIMESTAMP
WHERE active_download_id IN (...);
```

随后删除 download。这样 `downloaded`、`marked_downloaded` 和 `ignored` 保持不变，而被取消的 `downloading` 恢复缺失。handler、磁盘清理和 download monitor 继续调用 repository，不得直接 `tx.Delete(&model.Download{})` 绕过该规则。qB payload 删除行为保持用户当前删除动作的语义，但不影响最终拥有状态。

删除 helper 在更新台账前使用 `tx.Migrator().HasTable(&model.SubscriptionEpisode{})`；这保证迁移过程和仍只创建旧表的窄单元测试不会因表不存在失败。新建的集成测试必须迁移台账表并验证真实更新。

- [ ] **Step 8: Add disk cleanup regression coverage**

在 `monitor_test.go` 建立 completed download 和 downloaded ledger，执行现有 cleanup item 流程；断言 download 被删、台账仍 downloaded、active ID 为空。若磁盘清理当前直接持有 mock repository，则补充真实 repository 的 SQLite 集成测试。

- [ ] **Step 9: Run affected tests**

Run: `go test ./internal/service/downloader ./internal/service/organizer ./internal/api/handler -count=1`

Expected: PASS。

- [ ] **Step 10: Commit lifecycle integration**

```bash
git add internal/repository/download.go internal/repository/download_status_test.go internal/service/downloader internal/service/organizer internal/service/disk/monitor_test.go internal/api/handler/download.go internal/api/handler/download_test.go internal/api/router/router.go
git commit -m "Persist episode ownership through download lifecycle"
```

## Task 7: 增加剧集管理 API

**Files:**
- Create: `internal/api/handler/episode.go`
- Create: `internal/api/handler/episode_test.go`
- Modify: `internal/api/router/router.go`
- Test: `internal/api/router/router_test.go`

- [ ] **Step 1: Write failing API tests**

测试路由和 handler：

```go
func TestEpisodeHandlerListReturnsLedgerAndCandidateCounts(t *testing.T) {
    handler, fx := newEpisodeHandlerFixture(t)
    ledger := fx.seedLedger(1, model.EpisodeStatusDownloaded)
    fx.seedCandidate(ledger.ID, model.CandidateStatusPending)

    recorder := performEpisodeRequest(t, handler.List, http.MethodGet, "/subscriptions/1/episodes", nil, gin.Params{{Key: "id", Value: "1"}})
    assert.Equal(t, http.StatusOK, recorder.Code)
    item := decodeFirstEpisodeItem(t, recorder.Body.Bytes())
    assert.EqualValues(t, 1, item.ActionRequiredCandidateCount)
}

func TestEpisodeHandlerBatchStatusMarksEpisodesWithoutFilesystemAccess(t *testing.T) {
    handler, fx := newEpisodeHandlerFixture(t)
    fx.seedLedger(1, model.EpisodeStatusMissing)

    recorder := performEpisodeRequest(t, handler.BatchUpdateStatus, http.MethodPut, "/subscriptions/1/episodes/status",
        []byte(`{"episodes":[1],"status":"marked_downloaded"}`), gin.Params{{Key: "id", Value: "1"}})

    assert.Equal(t, http.StatusOK, recorder.Code)
    ledger, err := fx.repo.GetBySubscriptionAndEpisode(1, 1)
    require.NoError(t, err)
    assert.Equal(t, model.EpisodeStatusMarkedDownloaded, ledger.Status)
    assert.Equal(t, 0, fx.filesystemCalls)
}

func TestEpisodeHandlerRejectsMissingWhileActiveDownloadExists(t *testing.T) {
    handler, fx := newEpisodeHandlerFixture(t)
    fx.seedDownloadingLedger(1)
    recorder := performEpisodeRequest(t, handler.BatchUpdateStatus, http.MethodPut, "/subscriptions/1/episodes/status",
        []byte(`{"episodes":[1],"status":"missing"}`), gin.Params{{Key: "id", Value: "1"}})
    assert.Equal(t, http.StatusConflict, recorder.Code)
    assert.Contains(t, recorder.Body.String(), "active_download_must_be_resolved")
}

func TestEpisodeHandlerKeepCandidateIsIdempotent(t *testing.T) {
    handler, fx := newEpisodeHandlerFixture(t)
    ledger := fx.seedLedger(1, model.EpisodeStatusDownloaded)
    candidate := fx.seedCandidate(ledger.ID, model.CandidateStatusPending)
    params := gin.Params{{Key: "id", Value: "1"}, {Key: "episode", Value: "1"}, {Key: "candidate_id", Value: strconv.Itoa(int(candidate.ID))}}

    first := performEpisodeRequest(t, handler.KeepExisting, http.MethodPost, "/keep", nil, params)
    second := performEpisodeRequest(t, handler.KeepExisting, http.MethodPost, "/keep", nil, params)
    assert.Equal(t, http.StatusOK, first.Code)
    assert.Equal(t, http.StatusOK, second.Code)
}

func TestEpisodeHandlerRejectsCandidateFromAnotherEpisode(t *testing.T) {
    handler, fx := newEpisodeHandlerFixture(t)
    otherLedger := fx.seedLedgerForSubscription(2, 7, model.EpisodeStatusDownloaded)
    candidate := fx.seedCandidate(otherLedger.ID, model.CandidateStatusPending)
    params := gin.Params{{Key: "id", Value: "1"}, {Key: "episode", Value: "1"}, {Key: "candidate_id", Value: strconv.Itoa(int(candidate.ID))}}
    recorder := performEpisodeRequest(t, handler.KeepExisting, http.MethodPost, "/keep", nil, params)
    assert.Equal(t, http.StatusNotFound, recorder.Code)
}
```

fixture 必须只使用数据库和 repository；`filesystemCalls` 保持零用于证明 list/status handler 没有注入或调用文件检查器。

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./internal/api/handler ./internal/api/router -run Episode -count=1`

Expected: FAIL，路由和 handler 不存在。

- [ ] **Step 3: Implement handler request and response types**

在 `episode.go` 实现：

```go
type BatchEpisodeStatusRequest struct {
    Episodes []int  `json:"episodes" binding:"required,min=1"`
    Status   string `json:"status" binding:"required,oneof=missing marked_downloaded ignored"`
}

type EpisodeListItem struct {
    model.SubscriptionEpisode
    ActionRequiredCandidateCount int64 `json:"action_required_candidate_count"`
}
```

List handler 直接使用 repository 的 `ListWithCandidateCounts` 结果映射响应，不进行 N+1 候选查询。

`SetStatus` 对不存在的正整数集数先创建台账，因此总集数未知时也可手动标记。`missing` 更新前若存在关联的 pending/downloading/stalled/organizing 下载，返回 HTTP 409 和 `active_download_must_be_resolved`。状态修改成功后调用 `RefreshSubscriptionProgress`。集数小于等于 0 返回 400。

所有候选操作先通过 `(subscription_id, episode, candidate_id)` 联表查询验证归属；不匹配统一返回 404，不能只按 candidate ID 更新。

- [ ] **Step 4: Register routes**

```go
subscriptions.GET("/:id/episodes", episodeHandler.List)
subscriptions.PUT("/:id/episodes/status", episodeHandler.BatchUpdateStatus)
subscriptions.GET("/:id/episodes/:episode/candidates", episodeHandler.ListCandidates)
subscriptions.POST("/:id/episodes/:episode/candidates/:candidate_id/keep", episodeHandler.KeepExisting)
```

注意静态路由和 `/:id` 的顺序，避免与 groups/statistics 冲突。

- [ ] **Step 5: Run handler and router tests**

Run: `go test ./internal/api/handler ./internal/api/router -count=1`

Expected: PASS。

- [ ] **Step 6: Commit episode APIs**

```bash
git add internal/api/handler/episode.go internal/api/handler/episode_test.go internal/api/router/router.go internal/api/router/router_test.go
git commit -m "Add episode ledger management API"
```

## Task 8: 实现可恢复的人工替换服务

**Files:**
- Create: `internal/service/episode/replacement.go`
- Create: `internal/service/episode/replacement_downloader.go`
- Create: `internal/service/episode/replacement_test.go`
- Modify: `internal/model/subscription_episode.go`
- Modify: `internal/repository/episode.go`
- Modify: `internal/service/downloader/monitor.go`

- [ ] **Step 1: Write failing happy-path replacement test**

使用临时目录和 fake qB client：

```go
func TestReplacementPromotesNewFileBeforeRemovingOldTask(t *testing.T) {
    fx := newReplacementFixture(t)
    oldPath := fx.writeOldFile("old-content")
    candidate := fx.seedPendingCandidate(oldPath)
    fx.downloader.stagedContent = []byte("new-content")

    require.NoError(t, fx.service.Replace(context.Background(), candidate.ID))

    assert.Equal(t, "new-content", string(requireReadFile(t, oldPath)))
    assert.Equal(t, []string{"detach_new_torrent", "pause_old", "backup_old", "promote", "switch", "remove_old_torrent", "remove_rollback"}, fx.events)
    ledger := fx.loadLedger()
    assert.NotEqual(t, fx.oldDownload.ID, *ledger.ActiveDownloadID)
    got := fx.loadCandidate(candidate.ID)
    assert.Equal(t, model.CandidateStatusAccepted, got.Status)
    assert.NoFileExists(t, got.RollbackPath)
}
```

fake client 记录调用顺序，明确断言旧 hash 从未传给 `DeleteTorrentWithPayload`。

- [ ] **Step 2: Write failing rollback and recovery tests**

```go
func TestReplacementRenameFailureRestoresOldFileAndLedger(t *testing.T) {
    fx := newReplacementFixture(t)
    oldPath := fx.writeOldFile("old-content")
    candidate := fx.seedPendingCandidate(oldPath)
    fx.filePromoter.failPromote = errors.New("promote failed")

    err := fx.service.Replace(context.Background(), candidate.ID)

    require.ErrorContains(t, err, "promote failed")
    assert.Equal(t, "old-content", string(requireReadFile(t, oldPath)))
    assert.Equal(t, fx.oldDownload.ID, *fx.loadLedger().ActiveDownloadID)
    assert.Equal(t, model.CandidateStatusFailed, fx.loadCandidate(candidate.ID).Status)
    assert.Contains(t, fx.events, "resume_old")
    assert.NotContains(t, fx.deletePayloadHashes, fx.oldDownload.TorrentHash)
}

func TestReplacementCleanupFailureEndsAcceptedCleanupFailed(t *testing.T) {
    fx := newReplacementFixture(t)
    candidate := fx.seedPendingCandidate(fx.writeOldFile("old-content"))
    fx.torrentRemover.err = errors.New("remove failed")

    err := fx.service.Replace(context.Background(), candidate.ID)

    require.ErrorContains(t, err, "remove failed")
    assert.Equal(t, model.CandidateStatusAcceptedCleanupFailed, fx.loadCandidate(candidate.ID).Status)
    assert.NotEqual(t, fx.oldDownload.ID, *fx.loadLedger().ActiveDownloadID)
}

func TestReplacementRecoveryResumesPersistedStage(t *testing.T) {
    fx := newReplacementFixture(t)
    candidate := fx.seedCandidateAtStage(episode.ReplacementStagePromoted)

    require.NoError(t, fx.service.RecoverIncomplete(context.Background()))

    assert.Equal(t, model.CandidateStatusAccepted, fx.loadCandidate(candidate.ID).Status)
    assert.Equal(t, episode.ReplacementStageDone, fx.loadCandidate(candidate.ID).ReplacementStage)
}

func TestReplacementRejectsConcurrentReplacingCandidate(t *testing.T) {
    fx := newReplacementFixture(t)
    first := fx.seedReplacingCandidate()
    second := fx.seedPendingCandidateForSameEpisode()

    err := fx.service.Replace(context.Background(), second.ID)

    require.ErrorIs(t, err, episode.ErrReplacementInProgress)
    assert.Equal(t, model.CandidateStatusReplacing, fx.loadCandidate(first.ID).Status)
    assert.Equal(t, model.CandidateStatusPending, fx.loadCandidate(second.ID).Status)
}

func TestReplacementMarkedDownloadedWithoutTrackedOldResourceSwitchesLedger(t *testing.T) {
    fx := newReplacementFixture(t)
    candidate := fx.seedCandidateForUntrackedMarkedDownloadedEpisode()

    require.NoError(t, fx.service.Replace(context.Background(), candidate.ID))

    ledger := fx.loadLedger()
    assert.Equal(t, model.EpisodeStatusDownloaded, ledger.Status)
    assert.NotNil(t, ledger.ActiveDownloadID)
    assert.Equal(t, "new-hash", ledger.ActiveTorrentHash)
    assert.Contains(t, fx.events, "detach_new_torrent")
    assert.NotContains(t, fx.events, "pause_old")
    assert.NotContains(t, fx.events, "remove_old_torrent")
}

func TestReplacementDownloadFailureDeletesOnlyNewPayload(t *testing.T) {
    fx := newReplacementFixture(t)
    candidate := fx.seedPendingCandidate(fx.writeOldFile("old-content"))
    fx.downloader.downloadErr = errors.New("download failed")

    err := fx.service.Replace(context.Background(), candidate.ID)

    require.ErrorContains(t, err, "download failed")
    assert.NotContains(t, fx.deletePayloadHashes, fx.oldDownload.TorrentHash)
    assert.Contains(t, fx.deletePayloadHashes, fx.newDownload.TorrentHash)
}
```

fixture 使用真实临时文件和 SQLite；fake downloader、torrent remover、file promoter 都记录事件。`seedCandidateAtStage` 必须创建阶段所需的 staged/final/download 记录，使恢复测试从持久化状态出发而非内存状态。

- [ ] **Step 3: Run replacement tests and verify RED**

Run: `go test ./internal/service/episode -run Replacement -count=1`

Expected: FAIL，replacement service 不存在。

- [ ] **Step 4: Define narrow external interfaces**

```go
type ReplacementDownloader interface {
    DownloadToStage(ctx context.Context, candidate model.EpisodeResourceCandidate, stagedDir string) (*model.Download, string, error)
    CleanupFailedDownload(download *model.Download) error
}

type TorrentTaskController interface {
    PauseTorrent(hash string) error
    ResumeTorrent(hash string) error
    RemoveTorrentTask(hash string) error
}

type FilePromoter interface {
    Move(source, destination string) error
    Remove(path string) error
    Exists(path string) bool
}
```

生产构造保持依赖显式：

```go
func NewQBReplacementDownloader(
    db *gorm.DB,
    downloadRepo repository.DownloadRepository,
    configRepo repository.ConfigRepository,
    qbClient downloader.QBittorrentClient,
    renameTemplate string,
    downloadRoot string,
) ReplacementDownloader

func NewReplacementService(
    db *gorm.DB,
    episodeRepo repository.EpisodeRepository,
    downloadRepo repository.DownloadRepository,
    subscriptionRepo repository.SubscriptionRepository,
    downloader ReplacementDownloader,
    torrentController TorrentTaskController,
    filePromoter FilePromoter,
) *ReplacementService

func NewOSFilePromoter() FilePromoter
```

`NewOSFilePromoter` 只使用 `os.Rename` 和 `os.Remove`，不做跨文件系统 copy fallback；由于暂存和回滚目录已设计在 final path 同级，`os.Rename` 失败必须停止并恢复，不能退化成非原子复制。router 使用 `renameTemplate` 和 `cfg.DownloadPath` 构造 downloader，再构造 replacement service；测试直接注入 fake interfaces。

`replacement_downloader.go` 的生产实现：在目标剧集正式目录旁创建 `.auto-rss-replacements/<candidate-id>/` 暂存目录和 `.auto-rss-rollback/` 回滚目录，保证与 final path 位于同一文件系统；创建 `Purpose=replacement`、`ReplacementCandidateID=candidate.ID` 的下载记录，把 qB save path 指向候选专属暂存目录，每 5 秒调用 `GetTorrentInfo`，直到完成、context 取消或 qB 报错；完成后使用现有重命名规则生成暂存文件名并验证文件存在，然后调用 `RemoveTorrentTask` 仅移除新 qB 任务、保留暂存数据，再返回 staged path。这样后续 `os.Rename` 不会破坏 qB 的文件跟踪。普通 `DownloadMonitor` 遇到 `Purpose=replacement` 时只同步基础下载进度，不调用常规 `CompletionHandler`，避免台账提前切换。下载尚未完成时失败或 context 取消，`CleanupFailedDownload` 可以对“新 replacement download 的 hash”调用 `DeleteTorrentWithPayload` 清理临时任务和数据，但旧台账资源 hash 绝不能传入该方法。

- [ ] **Step 5: Implement persistent stage machine**

阶段常量至少包括：

```go
const (
    ReplacementStageQueued       = "queued"
    ReplacementStageDownloading  = "downloading"
    ReplacementStageStaged       = "staged"
    ReplacementStageOldBackedUp  = "old_backed_up"
    ReplacementStagePromoted     = "promoted"
    ReplacementStageSwitched     = "switched"
    ReplacementStageCleaning     = "cleaning"
    ReplacementStageDone         = "done"
)
```

每个外部副作用前后持久化阶段和路径。完整流程：

1. 原子把候选从 `pending/failed` 改为 `replacing + queued`，并拒绝同集第二个 replacing 候选。
2. `DownloadToStage` 完成并仅移除新 qB 任务后，保存 replacement download ID 和 staged path。
3. 从旧 active download 选择 `RenamedPath`，为空时回退 `FilePath`；若旧资源仍有 qB hash，调用 `PauseTorrent`，暂停失败立即停止，不移动文件。
4. 把旧文件移动到 rollback path，再把 staged file 移动到 final path。
5. 在数据库事务中切换 ledger 当前资源、候选状态和 replacement download；事务失败时把新文件移回 staged、旧文件移回 final，并 `ResumeTorrent`。
6. 事务成功后调用 `RemoveTorrentTask`，删除 rollback 文件和旧下载记录。
7. 第 6 步任一失败改为 `accepted_cleanup_failed`，不得回滚已切换的新资源。

所有失败路径禁止对旧资源调用 `DeleteTorrentWithPayload`。只有尚未采用的新 replacement download 可以用该方法清理临时数据。

- [ ] **Step 6: Implement recovery scan**

`RecoverIncomplete(ctx)` 查询 `replacing` 和 `accepted_cleanup_failed` 候选，根据 `replacement_stage`：

- `queued/downloading`：读取 replacement download 和 qB 状态；仍在下载则保持，已完成则生成或确认 staged path，任务丢失则标记 failed 且不碰旧资源。
- `staged`：可重新开始备份旧文件。
- `old_backed_up`：若 final 不存在则恢复旧文件；若 staged 存在且可继续则提升。
- `promoted`：完成数据库切换。
- `switched/cleaning`：只重试非媒体残留清理。

无法安全判断时保持状态并写入明确 `failure_reason`，不猜测删除文件。

- [ ] **Step 7: Run replacement tests**

Run: `go test ./internal/service/episode -run Replacement -count=1`

Expected: PASS。

- [ ] **Step 8: Commit replacement service**

```bash
git add internal/model/subscription_episode.go internal/repository/episode.go internal/service/episode/replacement.go internal/service/episode/replacement_downloader.go internal/service/episode/replacement_test.go internal/service/downloader/monitor.go
git commit -m "Add recoverable manual episode replacement"
```

## Task 9: 暴露替换操作并接入启动恢复

**Files:**
- Modify: `internal/api/handler/episode.go`
- Modify: `internal/api/handler/episode_test.go`
- Modify: `internal/api/router/router.go`
- Test: `internal/api/router/router_test.go`

- [ ] **Step 1: Write failing replacement API tests**

```go
func TestEpisodeHandlerAcceptCandidateStartsReplacement(t *testing.T) {
    handler, fx := newEpisodeHandlerFixture(t)
    candidate := fx.seedReplaceableCandidate()
    recorder := performCandidateAction(t, handler.Replace, 1, 1, candidate.ID)
    assert.Equal(t, http.StatusAccepted, recorder.Code)
    assert.Contains(t, recorder.Body.String(), "task_id")
    assert.Equal(t, candidate.ID, fx.replacement.startedCandidateID)
}

func TestEpisodeHandlerAcceptCandidateReturnsConflictWhenAlreadyReplacing(t *testing.T) {
    handler, fx := newEpisodeHandlerFixture(t)
    candidate := fx.seedReplacingCandidate()
    fx.replacement.startErr = episode.ErrReplacementInProgress
    recorder := performCandidateAction(t, handler.Replace, 1, 1, candidate.ID)
    assert.Equal(t, http.StatusConflict, recorder.Code)
}

func TestEpisodeHandlerRetryCleanupOnlyRetriesAcceptedCleanupFailed(t *testing.T) {
    handler, fx := newEpisodeHandlerFixture(t)
    pending := fx.seedCandidateWithStatus(model.CandidateStatusPending)
    rejected := performCandidateAction(t, handler.RetryCleanup, 1, 1, pending.ID)
    assert.Equal(t, http.StatusConflict, rejected.Code)

    cleanupFailed := fx.seedCandidateWithStatus(model.CandidateStatusAcceptedCleanupFailed)
    accepted := performCandidateAction(t, handler.RetryCleanup, 1, 1, cleanupFailed.ID)
    assert.Equal(t, http.StatusAccepted, accepted.Code)
    assert.Equal(t, cleanupFailed.ID, fx.replacement.cleanupCandidateID)
}
```

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./internal/api/handler ./internal/api/router -run 'Candidate|Replacement' -count=1`

Expected: FAIL，replace 和 retry-cleanup 路由不存在。

- [ ] **Step 3: Add asynchronous task endpoints**

注册：

```go
subscriptions.POST("/:id/episodes/:episode/candidates/:candidate_id/replace", episodeHandler.Replace)
subscriptions.POST("/:id/episodes/:episode/candidates/:candidate_id/retry-cleanup", episodeHandler.RetryCleanup)
```

复用 `task.Manager` 返回 task ID。响应必须区分：候选不存在 404、状态不允许 409、任务创建失败 500、成功 202。

handler 内定义窄接口并通过构造器注入：

```go
type ReplacementActions interface {
    Replace(ctx context.Context, candidateID uint) error
    RetryCleanup(ctx context.Context, candidateID uint) error
}

func NewEpisodeHandler(service *episode.Service, replacement ReplacementActions) *EpisodeHandler
```

router 注入 Task 8 的真实 replacement service；handler 测试注入 fake `ReplacementActions`。

- [ ] **Step 4: Run recovery during server startup**

在 router/service 初始化完成后调用一次 `replacementService.RecoverIncomplete`。恢复操作异步运行，但启动日志必须输出扫描数量；不要阻止 HTTP router 返回。为测试暴露可替换的 recovery function，避免真实文件操作。

- [ ] **Step 5: Run API and router tests**

Run: `go test ./internal/api/handler ./internal/api/router -count=1`

Expected: PASS。

- [ ] **Step 6: Commit replacement API**

```bash
git add internal/api/handler/episode.go internal/api/handler/episode_test.go internal/api/router/router.go internal/api/router/router_test.go
git commit -m "Expose manual episode replacement actions"
```

## Task 10: 将剧集拥有状态纳入备份恢复

**Files:**
- Modify: `internal/service/backup/backup.go`
- Modify: `internal/service/backup/backup_test.go`

- [ ] **Step 1: Write failing backup round-trip test**

```go
func TestBackupRoundTripPreservesEpisodeLedgerWithoutRuntimeLinks(t *testing.T) {
    source := newBackupTestDB(t)
    sub := seedBackupSubscription(t, source, "https://backup.test/rss", 1)
    downloadID := uint(99)
    require.NoError(t, source.Create(&model.SubscriptionEpisode{
        SubscriptionID: sub.ID, Episode: 3, Status: model.EpisodeStatusMarkedDownloaded,
        ActiveDownloadID: &downloadID, ActiveTorrentHash: "hash-3",
        ActiveTorrentURL: "https://backup.test/e03", ActiveTitle: "E03", StatusSource: "user",
    }).Error)

    pkg, err := NewService(source).Export(false)
    require.NoError(t, err)
    require.Len(t, pkg.Episodes, 1)

    target := newBackupTestDB(t)
    data, err := json.Marshal(pkg)
    require.NoError(t, err)
    _, err = NewService(target).Import(data, SourceAutoRSS, StrategyOverwrite)
    require.NoError(t, err)

    var restored model.SubscriptionEpisode
    require.NoError(t, target.First(&restored).Error)
    assert.Equal(t, 3, restored.Episode)
    assert.Equal(t, model.EpisodeStatusMarkedDownloaded, restored.Status)
    assert.Nil(t, restored.ActiveDownloadID)
    assert.Equal(t, "hash-3", restored.ActiveTorrentHash)
}
```

- [ ] **Step 2: Write failing candidate runtime-state normalization test**

```go
func TestBackupImportNormalizesInterruptedReplacementState(t *testing.T) {
    pkg := backupPackageWithCandidate(model.CandidateStatusReplacing, episode.ReplacementStagePromoted, "/old", "/staged")
    db := newBackupTestDB(t)
    data, err := json.Marshal(pkg)
    require.NoError(t, err)

    _, err = NewService(db).Import(data, SourceAutoRSS, StrategyOverwrite)
    require.NoError(t, err)

    var candidate model.EpisodeResourceCandidate
    require.NoError(t, db.First(&candidate).Error)
    assert.Equal(t, model.CandidateStatusFailed, candidate.Status)
    assert.Empty(t, candidate.ReplacementStage)
    assert.Empty(t, candidate.StagedPath)
    assert.Empty(t, candidate.RollbackPath)
    assert.Contains(t, candidate.FailureReason, "restored_without_runtime_task")
}
```

- [ ] **Step 3: Run tests and verify RED**

Run: `go test ./internal/service/backup -run 'EpisodeLedger|InterruptedReplacement' -count=1`

Expected: FAIL，备份包尚无 episode/candidate 字段。

- [ ] **Step 4: Add stable backup records**

将 schema version 升到 `1.1`，新增：

```go
type EpisodeRecord struct {
    SubscriptionKey    string     `json:"subscription_key"`
    Episode            int        `json:"episode"`
    Status             string     `json:"status"`
    ActiveTorrentHash  string     `json:"active_torrent_hash,omitempty"`
    ActiveTorrentURL   string     `json:"active_torrent_url,omitempty"`
    ActiveTitle        string     `json:"active_title,omitempty"`
    StatusSource       string     `json:"status_source"`
    DownloadedAt       *time.Time `json:"downloaded_at,omitempty"`
}

type CandidateRecord struct {
    SubscriptionKey string     `json:"subscription_key"`
    Episode         int        `json:"episode"`
    TorrentHash     string     `json:"torrent_hash,omitempty"`
    TorrentURL      string     `json:"torrent_url"`
    Title           string     `json:"title"`
    Fansub          string     `json:"fansub,omitempty"`
    Language        string     `json:"language,omitempty"`
    PubTime         *time.Time `json:"pub_time,omitempty"`
    SourceRSSURL    string     `json:"source_rss_url,omitempty"`
    Status          string     `json:"status"`
    FailureReason   string     `json:"failure_reason,omitempty"`
}
```

`Package` 和 `PackageSummary` 增加 `Episodes`、`EpisodeCandidates`。不导出数据库 ID、active download ID、replacement download ID 或任何 staged/rollback/final path。

同时让 `newBackupTestDB` AutoMigrate `SubscriptionEpisode` 和 `EpisodeResourceCandidate`。`backupPackageWithCandidate` 创建包含 subscription、episode record 和 candidate record 的完整 `Package`，使测试不依赖数据库 ID。

`ParsePackage` 继续接受 `1.0` 包，缺少 episodes/candidates 时按空数组处理；`1.1` 导出不破坏旧备份导入。

- [ ] **Step 5: Restore after subscriptions**

`applyPackage` 完成订阅 upsert 后，使用 `subscriptionKeyFromModel` 解析目标 subscription ID，再按 `(subscription_id, episode)` upsert 台账。策略规则：

- `skip`：已有台账不改变。
- `merge`：只创建目标不存在的台账和候选。
- `overwrite`：覆盖状态和资源身份，但 `ActiveDownloadID=nil`。

候选的 `replacing`、`accepted_cleanup_failed` 或任何带运行阶段的输入统一恢复为 `failed`，清空运行字段，并追加 `restored_without_runtime_task`。候选使用正常 resource key 幂等写入。

- [ ] **Step 6: Run backup tests**

Run: `go test ./internal/service/backup -count=1`

Expected: PASS。

- [ ] **Step 7: Commit backup support**

```bash
git add internal/service/backup/backup.go internal/service/backup/backup_test.go
git commit -m "Back up episode ownership state"
```

## Task 11: 增加前端剧集类型和纯 UI 逻辑

**Files:**
- Create: `web/src/api/episode.ts`
- Create: `web/src/utils/episode-ledger.ts`
- Create: `web/tests/episode-ledger.test.ts`
- Modify: `web/src/api/index.ts`
- Modify: `web/package.json`

- [ ] **Step 1: Write failing Node tests**

```ts
test('ignored and owned statuses count as not missing', () => {
  assert.equal(isEpisodeOwned('downloaded'), true)
  assert.equal(isEpisodeOwned('marked_downloaded'), true)
  assert.equal(isEpisodeOwned('ignored'), true)
  assert.equal(isEpisodeOwned('downloading'), false)
})

test('candidate resource comparison never recommends automatic replacement', () => {
  const result = describeCandidateDifference(oldResource, newResource)
  assert.equal(result.action, 'manual_review')
})
```

- [ ] **Step 2: Run test and verify RED**

Run: `cd web && node --experimental-strip-types --test tests/episode-ledger.test.ts`

Expected: FAIL，模块不存在。

- [ ] **Step 3: Implement API types and calls**

`web/src/api/episode.ts` 导出：

```ts
export type EpisodeStatus = 'missing' | 'downloading' | 'downloaded' | 'marked_downloaded' | 'ignored'
export type CandidateStatus = 'pending' | 'kept_existing' | 'replacing' | 'accepted' | 'accepted_cleanup_failed' | 'failed'

export interface SubscriptionEpisode {
  id: number
  subscription_id: number
  episode: number
  status: EpisodeStatus
  active_download_id?: number | null
  active_torrent_hash?: string
  active_torrent_url?: string
  active_title?: string
  downloaded_at?: string | null
  action_required_candidate_count: number
}

export interface EpisodeResourceCandidate {
  id: number
  subscription_episode_id: number
  torrent_hash?: string
  torrent_url: string
  title: string
  fansub?: string
  language?: string
  pub_time?: string | null
  source_rss_url?: string
  status: CandidateStatus
  failure_reason?: string
  replacement_stage?: string
}

export const episodeApi = {
  list: (subscriptionId: number) => api.get(`/subscriptions/${subscriptionId}/episodes`),
  updateStatus: (subscriptionId: number, episodes: number[], status: EpisodeStatus) =>
    api.put(`/subscriptions/${subscriptionId}/episodes/status`, { episodes, status }),
  listCandidates: (subscriptionId: number, episode: number) =>
    api.get(`/subscriptions/${subscriptionId}/episodes/${episode}/candidates`),
  keepExisting: (subscriptionId: number, episode: number, candidateId: number) =>
    api.post(`/subscriptions/${subscriptionId}/episodes/${episode}/candidates/${candidateId}/keep`),
  replace: (subscriptionId: number, episode: number, candidateId: number) =>
    api.post(`/subscriptions/${subscriptionId}/episodes/${episode}/candidates/${candidateId}/replace`),
  retryCleanup: (subscriptionId: number, episode: number, candidateId: number) =>
    api.post(`/subscriptions/${subscriptionId}/episodes/${episode}/candidates/${candidateId}/retry-cleanup`)
}
```

在 `web/src/api/index.ts` 末尾增加 `export * from './episode'`，保持页面从 `@/api` 导入的现有约定。

- [ ] **Step 4: Implement pure status helpers**

提供 `episodeStatusLabel`、`episodeStatusType`、`isEpisodeOwned`、`canRestoreMissing`、`describeCandidateDifference`。候选差异只返回信息字段和 `manual_review`，不得出现自动选择逻辑。

- [ ] **Step 5: Update test script and run tests**

将 `test:episodes` 改为同时运行原有和新测试：

```json
"test:episodes": "node --experimental-strip-types --test tests/episodes.test.ts tests/episode-ledger.test.ts"
```

Run: `cd web && npm run test:episodes`

Expected: PASS。

- [ ] **Step 6: Commit frontend data layer**

```bash
git add web/src/api/episode.ts web/src/api/index.ts web/src/utils/episode-ledger.ts web/tests/episode-ledger.test.ts web/package.json
git commit -m "Add episode ledger frontend data model"
```

## Task 12: 构建剧集管理抽屉

**Files:**
- Create: `web/src/components/EpisodeManagerDrawer.vue`
- Modify: `web/src/views/Subscriptions.vue`
- Modify: `web/src/style.css`

- [ ] **Step 1: Add the feature entry with a stable icon button**

在订阅卡片和列表操作中使用现有图标库的剧集/列表图标，增加 tooltip“剧集管理”。点击设置当前订阅并打开抽屉。不要新增顶级路由或营销式页面。

- [ ] **Step 2: Implement the drawer states**

`EpisodeManagerDrawer.vue` 使用固定宽度响应式抽屉：

- 顶部显示订阅名、连续进度和待处理候选数量。
- 主体使用紧凑集数网格，每个格子尺寸固定，颜色不依赖单一色相。
- 支持状态筛选、批量选择、标记已下载、恢复缺失、忽略。
- 提供数字输入/stepper 添加并选择某一集，使总集数未知且 RSS 尚未观察到该集时也能直接手动标记；提交仍复用批量状态 API，由后端 upsert 台账。
- `downloading` 且存在活动任务时禁用恢复缺失，并用 tooltip 说明先处理下载任务。
- loading、empty、error、partial update 均有明确状态。

不要在卡片内嵌套卡片；候选详情使用 modal。

- [ ] **Step 3: Implement candidate comparison modal**

展示当前资源和候选资源的标题、字幕组、语言、发布时间、hash 摘要和 URL。操作：

- “保留现有资源”：调用 keep endpoint。
- “使用新资源”：二次确认文案明确“新资源下载和整理成功后，系统才会移除旧任务与旧文件”。
- 当前台账没有可跟踪的旧下载或旧路径时，确认框改为说明“系统会采用新资源，但无法保证清理未受跟踪的旧文件”。
- `failed` 提供重新替换；`accepted_cleanup_failed` 仅提供重试清理。

替换进行中时轮询现有 task API 或 websocket task 更新，不通过读取文件系统判断进度。

- [ ] **Step 4: Keep layout stable and accessible**

为集数格子设置固定 `aspect-ratio` 和最小宽度；长状态文字换行，不遮挡按钮。所有图标按钮有 tooltip 和 `aria-label`；批量操作使用 checkbox；状态筛选使用 tabs 或 segmented control。

- [ ] **Step 5: Run frontend tests and build**

Run: `cd web && npm run test:episodes`

Expected: PASS。

Run: `cd web && npm run build`

Expected: `vue-tsc` 和 Vite build 成功，无 TypeScript 错误。

- [ ] **Step 6: Start dev server and verify desktop/mobile**

Run: `cd web && npm run dev -- --host 127.0.0.1`

Expected: Vite 输出本地 URL。使用 in-app browser 检查至少 `1440x900` 和 `390x844`：抽屉不溢出、集数文字不重叠、modal 操作可见、订阅卡片尺寸不因新按钮跳动。

- [ ] **Step 7: Commit frontend UI**

```bash
git add web/src/components/EpisodeManagerDrawer.vue web/src/views/Subscriptions.vue web/src/style.css
git commit -m "Add episode management interface"
```

## Task 13: 更新文档并执行完整验证

**Files:**
- Modify: `README.md`
- Modify: `docs/API.md`
- Modify: `docs/LANGUAGE_FEATURE.md`

- [ ] **Step 1: Update behavior documentation**

在 README 将“同集替换”改成“同集资源候选与人工替换”。在 `docs/LANGUAGE_FEATURE.md` 删除高版本自动替换描述，明确语言和版本只用于人工判断。`docs/API.md` 增加剧集列表、批量状态、候选保留、替换和清理重试接口，记录 409 错误语义，并补充备份 schema `1.1` 中的 episodes/candidates 与运行时路径剥离规则。

- [ ] **Step 2: Run formatting**

Run: `gofmt -w internal/model internal/repository internal/service/episode internal/service/scheduler internal/service/downloader internal/service/organizer internal/api/handler internal/api/router cmd/server`

Expected: 命令成功，只产生格式化差异。

- [ ] **Step 3: Run focused backend tests**

Run: `go test ./internal/pkg/database ./internal/repository ./internal/service/episode ./internal/service/scheduler ./internal/service/downloader ./internal/service/organizer ./internal/service/disk ./internal/service/backup ./internal/api/handler ./internal/api/router -count=1`

Expected: PASS。

- [ ] **Step 4: Run full backend suite**

Run: `go test ./... -count=1`

Expected: PASS，0 failures。

- [ ] **Step 5: Run frontend tests and production build**

Run: `cd web && npm run test:episodes && npm run build`

Expected: PASS，Vite build 成功。

- [ ] **Step 6: Re-run migration idempotency and uniqueness verification**

Run: `go test ./internal/pkg/database -run 'TestRunMigrations(BackfillsEpisodeLedger|MergesSameEpisodeResources|CreatesMissingRows|EpisodeLedgerConstraints|MarksExistingRSS)' -count=1`

Expected: PASS；迁移重复运行不重复建账，唯一约束拒绝同订阅同相对集数的第二条记录，且 `current_episode` 不会被推断为已下载范围。

- [ ] **Step 7: Verify destructive behavior is absent**

Run:

```bash
rg -n "replaceDownloadID|newer_version_replace_existing|Found newer version.*replacing" internal
```

Expected: 不存在自动 RSS/手动采集的同集替换路径。人工替换对旧资源只能调用 `PauseTorrent`、`ResumeTorrent` 和 `RemoveTorrentTask`；`DeleteTorrentWithPayload` 只用于清理未采用的新暂存下载，并有测试断言其 hash 不等于旧资源 hash。

- [ ] **Step 8: Review diff and commit docs/verification fixes**

Run: `git diff --check && git status --short`

Expected: 无 whitespace error；只包含本计划范围内文件。

```bash
git add README.md docs/API.md docs/LANGUAGE_FEATURE.md
git commit -m "Document episode ledger behavior"
```

## 规格覆盖检查清单

- [ ] `(subscription_id, relative_episode)` 是唯一剧集身份。
- [ ] 语言、字幕组、分辨率、版本和 hash 不产生第二份自动收藏。
- [ ] 下载并完成重命名后才自动标记已下载；关闭重命名时下载完成即可。
- [ ] 用户可以单集和批量标记已下载、缺失或忽略。
- [ ] 所有自动和手动采集路径都不再替换同集旧资源。
- [ ] 同集不同资源持久化为人工候选，且候选幂等。
- [ ] RSS 换源执行一次不回灌历史的基线对账。
- [ ] 下载历史、qB 任务和文件清理不会隐式恢复为缺失。
- [ ] 订阅进度、缺集和完结以台账为事实来源，并兼容原始集号 API。
- [ ] 人工替换先暂存新资源，失败时保留或恢复旧资源。
- [ ] 替换阶段持久化，服务重启后可恢复。
- [ ] 普通剧集管理和进度计算不访问媒体磁盘。
