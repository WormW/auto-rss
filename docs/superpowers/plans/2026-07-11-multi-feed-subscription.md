# 多 RSS Feed 订阅 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让一个番剧订阅安全关联多个字幕组 RSS feed，并按 feed 独立偏移和水位线执行先到先得下载、同集候选记录及健康诊断。

**Architecture:** 在订阅下增加 `subscription_feeds` 子资源，feed 持有 URL、字幕组、偏移、基线和检查状态；订阅继续持有共享过滤、季度和重命名规则。调度器按启用 feed 独立抓取，将原始集数转换成相对集数后交给剧集台账服务原子占用；任何后到的不同资源只进入候选，不创建第二个下载任务。

**Tech Stack:** Go 1.22、Gin、GORM、gormigrate、SQLite、Vue 3、TypeScript、Naive UI、Node test runner。

---

## 前置条件

先完整执行 `docs/superpowers/plans/2026-07-11-episode-ledger-manual-resource-replacement.md`，并确认其中的 `subscription_episodes`、`episode_resource_candidates`、`repository.EpisodeRepository` 和 `episode.Service` 已落地且测试通过。

本计划接管 RSS 偏移、水位线和基线状态的事实来源：

- `subscriptions.episode_offset`、`subscriptions.last_rss_pub_time` 和 `subscriptions.rss_baseline_pending` 只保留兼容投影，不再由调度器读取。
- `episode.Service.EvaluateRSSItem` 必须接收调用方已经计算好的相对集数，不能再调用 `Subscription.RelativeEpisode`。
- 旧 API 的单 RSS 字段通过唯一默认 feed 代理；多 feed 订阅禁止用旧字段进行有歧义的更新。
- `subscriptions.episode_offset` 作为旧进度字段的投影偏移：迁移订阅保留原值，新订阅初始化为第一条 feed 的偏移；增加第二条 feed 后不因 feed 排序改变。新 UI 和台账始终展示相对集数。

## 文件映射

新增文件：

- `internal/model/subscription_feed.go`：feed 模型、输入快照和兼容投影方法。
- `internal/repository/subscription_feed.go`：feed 查询、事务写入和检查状态更新。
- `internal/repository/subscription_feed_test.go`：唯一约束、生命周期和批量查询测试。
- `internal/service/subscriptionfeed/service.go`：URL 规范化、预览、服务端复验和生命周期规则。
- `internal/service/subscriptionfeed/service_test.go`：映射、空 feed、URL/偏移变化和删除语义测试。
- `internal/api/handler/subscription_feed.go`：feed CRUD 与预览 API。
- `internal/api/handler/subscription_feed_test.go`：HTTP 状态和响应结构测试。
- `web/src/api/subscription-feed.ts`：feed 类型和 API 客户端。
- `web/src/components/SubscriptionFeedsEditor.vue`：订阅创建/编辑中的 feed 列表、表单和预览。
- `web/src/utils/subscription-feeds.ts`：草稿判重、变更检测和保存计划纯函数。
- `web/tests/subscription-feeds.test.ts`：前端纯函数测试。

修改文件：

- `internal/model/subscription.go`、`internal/model/download.go`、`internal/model/subscription_episode.go`：兼容关联和来源快照。
- `internal/pkg/database/migration.go`、`internal/pkg/database/database.go`、`internal/pkg/database/migration_test.go`：建表、回填默认 feed 和幂等迁移。
- `internal/service/episode/service.go`、`internal/service/episode/service_test.go`：改为接收相对集数和 feed 来源。
- `internal/service/scheduler/scheduler.go`、`internal/service/scheduler/scheduler_test.go`、`internal/service/scheduler/smart_fetch.go`：遍历 feed、独立基线和并发占用。
- `internal/api/handler/subscription.go`、`internal/api/handler/subscription_test.go`：创建初始 feeds 和旧字段代理。
- `internal/api/router/router.go`、`internal/api/router/router_test.go`：依赖注入和路由。
- `internal/service/rss/health.go`、`internal/service/rss/health_test.go`：feed 级健康检查和订阅汇总。
- `internal/api/handler/rss_health.go`、`internal/api/handler/subscription_diagnostics.go` 及测试：返回 feed 明细。
- `internal/service/backup/backup.go`、`internal/service/backup/backup_test.go`：备份恢复 feeds。
- `web/src/api/index.ts`、`web/src/views/Subscriptions.vue`、`web/package.json`：接入 feed 编辑器和测试命令。
- `README.md`、`docs/API.md`、`docs/FANSUB_DESIGN.md`：更新使用方式和兼容语义。

## Task 1: 定义 feed 模型和迁移

**Files:**
- Create: `internal/model/subscription_feed.go`
- Create: `internal/pkg/utils/feed_url.go`
- Create: `internal/pkg/utils/feed_url_test.go`
- Modify: `internal/model/subscription.go`
- Modify: `internal/model/download.go`
- Modify: `internal/model/subscription_episode.go`
- Modify: `internal/pkg/database/migration.go`
- Modify: `internal/pkg/database/database.go`
- Test: `internal/pkg/database/migration_test.go`

- [ ] **Step 1: Write failing migration tests**

在 `internal/pkg/database/migration_test.go` 增加前三个迁移测试；在 `internal/pkg/utils/feed_url_test.go` 增加最后一个 URL 规范化测试：

```go
func TestRunMigrationsBackfillsDefaultSubscriptionFeed(t *testing.T) {
    db := openMigrationTestDB(t)
    require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}))
    waterline := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
    sub := model.Subscription{
        Name: "Offset Anime", RssURL: " https://EXAMPLE.test:443/feed?b=2&a=1 ",
        Fansub: "Group A", EpisodeOffset: 100, LastRSSPubTime: &waterline,
        Status: "active", Enabled: true,
    }
    require.NoError(t, db.Create(&sub).Error)

    require.NoError(t, RunMigrations(db))

    var feeds []model.SubscriptionFeed
    require.NoError(t, db.Find(&feeds).Error)
    require.Len(t, feeds, 1)
    assert.Equal(t, sub.ID, feeds[0].SubscriptionID)
    assert.Equal(t, "Group A", feeds[0].Name)
    assert.Equal(t, "Group A", feeds[0].Fansub)
    assert.Equal(t, 100, feeds[0].EpisodeOffset)
    assert.Equal(t, waterline, *feeds[0].LastRSSPubTime)
    assert.False(t, feeds[0].BaselinePending)
    assert.Equal(t, "https://example.test/feed?a=1&b=2", feeds[0].RSSURLNormalized)
}

func TestRunMigrationsSubscriptionFeedBackfillIsIdempotent(t *testing.T) {
    db := openMigrationTestDB(t)
    require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}))
    sub := model.Subscription{Name: "Anime", RssURL: "https://example.test/rss", Enabled: true}
    require.NoError(t, db.Create(&sub).Error)

    require.NoError(t, RunMigrations(db))
    require.NoError(t, RunMigrations(db))

    var count int64
    require.NoError(t, db.Model(&model.SubscriptionFeed{}).Where("subscription_id = ?", sub.ID).Count(&count).Error)
    assert.EqualValues(t, 1, count)
}

func TestRunMigrationsAddsNullableFeedReferences(t *testing.T) {
    db := openMigrationTestDB(t)
    require.NoError(t, RunMigrations(db))
    assert.True(t, db.Migrator().HasColumn(&model.Download{}, "subscription_feed_id"))
    assert.True(t, db.Migrator().HasColumn(&model.EpisodeResourceCandidate{}, "subscription_feed_id"))
    assert.True(t, db.Migrator().HasColumn(&model.EpisodeResourceCandidate{}, "source_feed_name"))
    assert.True(t, db.Migrator().HasTable(&model.SubscriptionFeedSeenItem{}))
}

func TestNormalizeFeedURLPreservesQueryMeaningAndSortsKeys(t *testing.T) {
    got := utils.NormalizeFeedURL(" HTTPS://Example.COM:443/rss?b=2&a=1&a=3 ")
    assert.Equal(t, "https://example.com/rss?a=1&a=3&b=2", got)
}
```

- [ ] **Step 2: Run migration tests and verify RED**

Run: `go test ./internal/pkg/database ./internal/pkg/utils -run 'TestRunMigrations(BackfillsDefaultSubscriptionFeed|SubscriptionFeedBackfillIsIdempotent|AddsNullableFeedReferences)|TestNormalizeFeedURL' -count=1`

Expected: FAIL，提示 `model.SubscriptionFeed` 或 feed 来源字段不存在。

- [ ] **Step 3: Add model types**

创建 `internal/model/subscription_feed.go`：

```go
package model

import "time"

type SubscriptionFeed struct {
    ID               uint       `json:"id" gorm:"primaryKey"`
    SubscriptionID   uint       `json:"subscription_id" gorm:"uniqueIndex:idx_subscription_feed_url,priority:1;index;not null"`
    Name             string     `json:"name" gorm:"size:100;not null"`
    Fansub           string     `json:"fansub" gorm:"size:100"`
    RSSURL           string     `json:"rss_url" gorm:"type:text;not null"`
    RSSURLNormalized string     `json:"-" gorm:"column:rss_url_normalized;size:2048;uniqueIndex:idx_subscription_feed_url,priority:2;not null"`
    EpisodeOffset    int        `json:"episode_offset" gorm:"not null;default:0"`
    Enabled          bool       `json:"enabled" gorm:"not null;default:true;index"`
    LastRSSPubTime   *time.Time `json:"last_rss_pub_time"`
    BaselinePending  bool       `json:"baseline_pending" gorm:"not null;default:true;index"`
    LastCheckTime    *time.Time `json:"last_check_time"`
    LastSuccessAt    *time.Time `json:"last_success_at"`
    LastError        string     `json:"last_error" gorm:"type:text"`
    CreatedAt        time.Time  `json:"created_at"`
    UpdatedAt        time.Time  `json:"updated_at"`
}

func (SubscriptionFeed) TableName() string { return "subscription_feeds" }

func (f SubscriptionFeed) RelativeEpisode(original int) int {
    if original <= f.EpisodeOffset {
        return 0
    }
    return original - f.EpisodeOffset
}

type SubscriptionFeedSeenItem struct {
    ID                 uint      `json:"id" gorm:"primaryKey"`
    SubscriptionFeedID uint      `json:"subscription_feed_id" gorm:"uniqueIndex:idx_feed_seen_resource,priority:1;index;not null"`
    ResourceKey        string    `json:"resource_key" gorm:"uniqueIndex:idx_feed_seen_resource,priority:2;size:512;not null"`
    OriginalEpisode    int       `json:"original_episode"`
    FirstSeenAt        time.Time `json:"first_seen_at" gorm:"not null"`
}

func (SubscriptionFeedSeenItem) TableName() string { return "subscription_feed_seen_items" }
```

给 `Download` 增加：

```go
SubscriptionFeedID *uint `json:"subscription_feed_id" gorm:"index;constraint:OnDelete:SET NULL"`
```

给 `EpisodeResourceCandidate` 增加：

```go
SubscriptionFeedID *uint  `json:"subscription_feed_id" gorm:"index;constraint:OnDelete:SET NULL"`
SourceFeedName      string `json:"source_feed_name" gorm:"size:100"`
SourceFansub        string `json:"source_fansub" gorm:"size:100"`
SourceEpisodeOffset int    `json:"source_episode_offset"`
```

给 `Subscription` 增加只用于预加载的关系：

```go
Feeds []SubscriptionFeed `json:"feeds,omitempty" gorm:"foreignKey:SubscriptionID"`
```

在 `internal/pkg/utils/feed_url.go` 实现共享规范化函数：

```go
func NormalizeFeedURL(raw string) string {
    parsed, err := url.Parse(strings.TrimSpace(raw))
    if err != nil || parsed.Scheme == "" || parsed.Host == "" {
        return ""
    }
    parsed.Scheme = strings.ToLower(parsed.Scheme)
    host := strings.ToLower(parsed.Hostname())
    port := parsed.Port()
    if (parsed.Scheme == "https" && port == "443") || (parsed.Scheme == "http" && port == "80") {
        port = ""
    }
    if port != "" {
        parsed.Host = net.JoinHostPort(host, port)
    } else {
        parsed.Host = host
    }
    parsed.Fragment = ""
    parsed.RawQuery = parsed.Query().Encode()
    return parsed.String()
}
```

不要删除、重命名或过滤查询参数；Mikan 的 `bangumiId` 和 `subgroupid` 属于资源身份。

- [ ] **Step 4: Add idempotent migration and backfill**

在剧集台账迁移之后增加迁移 ID `202607110002`：

```go
{
    ID: "202607110002",
    Migrate: func(tx *gorm.DB) error {
        if err := tx.AutoMigrate(
            &model.SubscriptionFeed{},
            &model.SubscriptionFeedSeenItem{},
            &model.Download{},
            &model.EpisodeResourceCandidate{},
        ); err != nil {
            return err
        }
        return backfillSubscriptionFeeds(tx)
    },
}
```

`backfillSubscriptionFeeds` 查询 `rss_url != ''` 的订阅，并调用 `utils.NormalizeFeedURL`。对每条订阅使用：

```go
feed := model.SubscriptionFeed{
    SubscriptionID: sub.ID,
    Name: fallbackFeedName(sub.Fansub),
    Fansub: sub.Fansub,
    RSSURL: strings.TrimSpace(sub.RssURL),
    RSSURLNormalized: utils.NormalizeFeedURL(sub.RssURL),
    EpisodeOffset: max(sub.EpisodeOffset, 0),
    Enabled: sub.Enabled && sub.Status == "active",
    LastRSSPubTime: sub.LastRSSPubTime,
    BaselinePending: false,
}
err := tx.Where("subscription_id = ? AND rss_url_normalized = ?", feed.SubscriptionID, feed.RSSURLNormalized).
    FirstOrCreate(&feed).Error
```

空 RSS、calendar-only 和合集-only 订阅不创建 feed。同步更新全新数据库和测试数据库的 `AutoMigrate` 模型列表。

- [ ] **Step 5: Run migration tests and verify GREEN**

Run: `go test ./internal/pkg/database -count=1`

Expected: PASS。

- [ ] **Step 6: Commit schema and migration**

```bash
git add internal/model/subscription_feed.go internal/model/subscription.go internal/model/download.go internal/model/subscription_episode.go internal/pkg/utils/feed_url.go internal/pkg/utils/feed_url_test.go internal/pkg/database/migration.go internal/pkg/database/database.go internal/pkg/database/migration_test.go
git commit -m "Add subscription feed schema"
```

## Task 2: 实现 feed 仓储和预览服务

**Files:**
- Create: `internal/repository/subscription_feed.go`
- Create: `internal/repository/subscription_feed_test.go`
- Create: `internal/service/subscriptionfeed/service.go`
- Create: `internal/service/subscriptionfeed/service_test.go`

- [ ] **Step 1: Write failing repository and service tests**

```go
func TestPreviewMapsOriginalEpisodesWithFeedOffset(t *testing.T) {
    parser := &fakeParser{items: []rss.RSSItem{
        {Title: "Anime 101", Episode: 101},
        {Title: "Anime 100", Episode: 100},
    }}
    svc := newFeedServiceFixture(t, parser)
    preview, err := svc.Preview(context.Background(), subscriptionfeed.Input{
        RSSURL: "https://example.test/rss", EpisodeOffset: 100,
    })
    require.NoError(t, err)
    require.Len(t, preview.Items, 2)
    assert.Equal(t, 1, preview.Items[0].RelativeEpisode)
    assert.True(t, preview.Items[0].Valid)
    assert.False(t, preview.Items[1].Valid)
    assert.Equal(t, "relative_episode_not_positive", preview.Items[1].InvalidReason)
}

func TestCreateRejectsFeedWhoseNonEmptyItemsHaveNoValidMapping(t *testing.T) {
    parser := &fakeParser{items: []rss.RSSItem{{Title: "Anime 100", Episode: 100}}}
    svc := newFeedServiceFixture(t, parser)
    _, err := svc.Create(context.Background(), 1, subscriptionfeed.Input{
        Name: "B", RSSURL: "https://example.test/rss", EpisodeOffset: 100,
    })
    assert.ErrorIs(t, err, subscriptionfeed.ErrNoMappableEpisodes)
}

func TestUpdateURLOrOffsetResetsBaselineButRenameDoesNot(t *testing.T) {
    svc, repo, db := newPersistedFeedServiceFixture(t)
    feed := seedFeed(t, repo, model.SubscriptionFeed{
        SubscriptionID: 1, Name: "A", RSSURL: "https://a.test/rss",
        RSSURLNormalized: "https://a.test/rss", EpisodeOffset: 0,
        BaselinePending: false,
    })
    require.NoError(t, db.Create(&model.SubscriptionFeedSeenItem{
        SubscriptionFeedID: feed.ID, ResourceKey: "hash:old", OriginalEpisode: 1, FirstSeenAt: time.Now(),
    }).Error)

    renamed, err := svc.Update(context.Background(), feed.ID, subscriptionfeed.Input{
        Name: "A renamed", RSSURL: feed.RSSURL, EpisodeOffset: 0, Enabled: true,
    })
    require.NoError(t, err)
    assert.False(t, renamed.BaselinePending)
    var seenCount int64
    require.NoError(t, db.Model(&model.SubscriptionFeedSeenItem{}).Where("subscription_feed_id = ?", feed.ID).Count(&seenCount).Error)
    assert.EqualValues(t, 1, seenCount)

    changed, err := svc.Update(context.Background(), feed.ID, subscriptionfeed.Input{
        Name: "A renamed", RSSURL: feed.RSSURL, EpisodeOffset: 100, Enabled: true,
    })
    require.NoError(t, err)
    assert.True(t, changed.BaselinePending)
    assert.Nil(t, changed.LastRSSPubTime)
    require.NoError(t, db.Model(&model.SubscriptionFeedSeenItem{}).Where("subscription_feed_id = ?", feed.ID).Count(&seenCount).Error)
    assert.Zero(t, seenCount)
}

func TestDeleteFeedDetachesRuntimeReferencesAndKeepsCandidateSnapshot(t *testing.T) {
    repo, db := setupSubscriptionFeedRepository(t)
    feed := seedFeed(t, repo, model.SubscriptionFeed{
        SubscriptionID: 1, Name: "B", RSSURL: "https://b.test/rss",
        RSSURLNormalized: "https://b.test/rss", Enabled: true,
    })
    download := model.Download{SubscriptionID: 1, SubscriptionFeedID: &feed.ID, Title: "B 101", TorrentURL: "https://b.test/101", TorrentHash: "b101"}
    require.NoError(t, db.Create(&download).Error)
    ledger := model.SubscriptionEpisode{SubscriptionID: 1, Episode: 1, Status: model.EpisodeStatusDownloaded, StatusSource: model.EpisodeStatusSourceAutomatic}
    require.NoError(t, db.Create(&ledger).Error)
    candidate := model.EpisodeResourceCandidate{SubscriptionEpisodeID: ledger.ID, SubscriptionFeedID: &feed.ID, SourceFeedName: "B", ResourceKey: "hash:b101", Status: model.CandidateStatusPending}
    require.NoError(t, db.Create(&candidate).Error)
    require.NoError(t, db.Create(&model.SubscriptionFeedSeenItem{SubscriptionFeedID: feed.ID, ResourceKey: "hash:b101", OriginalEpisode: 101, FirstSeenAt: time.Now()}).Error)

    require.NoError(t, repo.Delete(feed.ID))

    require.NoError(t, db.First(&download, download.ID).Error)
    require.NoError(t, db.First(&candidate, candidate.ID).Error)
    assert.Nil(t, download.SubscriptionFeedID)
    assert.Nil(t, candidate.SubscriptionFeedID)
    assert.Equal(t, "B", candidate.SourceFeedName)
    var seenCount int64
    require.NoError(t, db.Model(&model.SubscriptionFeedSeenItem{}).Where("subscription_feed_id = ?", feed.ID).Count(&seenCount).Error)
    assert.Zero(t, seenCount)
}
```

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./internal/repository ./internal/service/subscriptionfeed -run 'Feed|PreviewMaps|CreateRejects|UpdateURLOrOffset' -count=1`

Expected: FAIL，提示仓储和 service 不存在。

- [ ] **Step 3: Implement repository interface**

在 `internal/repository/subscription_feed.go` 定义：

```go
type SubscriptionFeedRepository interface {
    ListBySubscription(subscriptionID uint) ([]model.SubscriptionFeed, error)
    ListEnabledBySubscriptionIDs(subscriptionIDs []uint) ([]model.SubscriptionFeed, error)
    GetByID(id uint) (*model.SubscriptionFeed, error)
    Create(feed *model.SubscriptionFeed) error
    CreateInTx(tx *gorm.DB, feed *model.SubscriptionFeed) error
    Update(feed *model.SubscriptionFeed) error
    UpdateInTx(tx *gorm.DB, feed *model.SubscriptionFeed) error
    Delete(id uint) error
    DeleteInTx(tx *gorm.DB, id uint) error
    CountBySubscription(subscriptionID uint) (int64, error)
    HasSeenItem(feedID uint, resourceKey string) (bool, error)
    MarkSeenItem(feedID uint, resourceKey string, originalEpisode int, firstSeenAt time.Time) error
    UpdateCheckSuccess(id uint, checkedAt time.Time, maxPubTime *time.Time, baselineComplete bool) error
    UpdateCheckFailure(id uint, checkedAt time.Time, message string) error
}
```

`ListEnabledBySubscriptionIDs` 使用单次 `WHERE enabled = true AND subscription_id IN ?` 查询并按 `subscription_id, id` 排序。`MarkSeenItem` 使用 `(subscription_feed_id, resource_key)` 的 `FirstOrCreate` 保持幂等。`Delete` 在事务中先删除 `subscription_feed_seen_items`，再把 `downloads.subscription_feed_id` 和 `episode_resource_candidates.subscription_feed_id` 置空，最后删除 feed；来源快照字段保持不变。

- [ ] **Step 4: Implement preview and lifecycle service**

在 `internal/service/subscriptionfeed/service.go` 定义：

```go
var (
    ErrInvalidURL = errors.New("invalid feed URL")
    ErrNegativeOffset = errors.New("episode offset must be non-negative")
    ErrNoMappableEpisodes = errors.New("feed contains items but none map to a positive relative episode")
)

type FetchError struct{ Err error }

func (e *FetchError) Error() string { return "fetch feed: " + e.Err.Error() }
func (e *FetchError) Unwrap() error { return e.Err }

type Input struct {
    Name          string `json:"name"`
    Fansub        string `json:"fansub"`
    RSSURL        string `json:"rss_url"`
    EpisodeOffset int    `json:"episode_offset"`
    Enabled       bool   `json:"enabled"`
}

type PreviewItem struct {
    Title           string `json:"title"`
    OriginalEpisode int    `json:"original_episode"`
    EpisodeOffset   int    `json:"episode_offset"`
    RelativeEpisode int    `json:"relative_episode"`
    Valid           bool   `json:"valid"`
    InvalidReason   string `json:"invalid_reason"`
}

type Preview struct {
    Items       []PreviewItem `json:"items"`
    ParsedItems int           `json:"parsed_items"`
    ValidItems  int           `json:"valid_items"`
    Warning     string        `json:"warning,omitempty"`
}

type Prepared struct {
    Feed    model.SubscriptionFeed
    Preview Preview
}
```

service 暴露以下方法，使订阅创建能够在同一事务内写 subscription 和初始 feeds：

```go
func (s *Service) Preview(ctx context.Context, input Input) (Preview, error)
func (s *Service) Prepare(ctx context.Context, input Input) (Prepared, error)
func (s *Service) Create(ctx context.Context, subscriptionID uint, input Input) (*model.SubscriptionFeed, error)
func (s *Service) CreatePreparedInTx(tx *gorm.DB, subscriptionID uint, prepared Prepared) (*model.SubscriptionFeed, error)
func (s *Service) Update(ctx context.Context, feedID uint, input Input) (*model.SubscriptionFeed, error)
func (s *Service) Delete(feedID uint) error
```

`Preview` 拉取并解析 RSS，解析器错误包装成 `*FetchError`。它校验全部条目，但响应 `Items` 最多返回 50 条，并通过 `ParsedItems/ValidItems` 保留完整计数。空 RSS 返回成功并设置 `Warning: "empty_feed"`；非空 RSS 没有任何正相对集数时返回 `ErrNoMappableEpisodes`。`Prepare` 执行同一预览并构造规范化后的 feed；`Create` 调用 `Prepare` 后持久化，`CreatePreparedInTx` 不再次发起网络请求。URL/偏移变化的 `Update` 必须再次调用 `Prepare`，成功后才持久化；同一事务清空 `LastRSSPubTime` 和该 feed 的 `subscription_feed_seen_items`，并设置 `BaselinePending=true`。只改名称、字幕组或启用状态保持原水位线、已见快照和基线状态。

`Create`、`Update` 和 `Delete` 使用 service 持有的 `*gorm.DB` 开事务，并在同一事务调用私有 `refreshLegacyProjectionInTx(tx, subscriptionID)`：按 feed `created_at, id` 选择第一条作为旧 `rss_url/fansub/episode_offset` 投影，没有 feed 时清空这些字段。调度器永远不读取该投影。事务提交后 handler 调用 `episodeService.RefreshSubscriptionProgress`，使旧进度字段按新的投影偏移重算。

- [ ] **Step 5: Run package tests and verify GREEN**

Run: `go test ./internal/pkg/utils ./internal/repository ./internal/service/subscriptionfeed -count=1`

Expected: PASS。

- [ ] **Step 6: Commit feed domain service**

```bash
git add internal/repository/subscription_feed.go internal/repository/subscription_feed_test.go internal/service/subscriptionfeed/service.go internal/service/subscriptionfeed/service_test.go
git commit -m "Add subscription feed service"
```

## Task 3: 增加 feed API 和旧单 RSS 兼容代理

**Files:**
- Create: `internal/api/handler/subscription_feed.go`
- Create: `internal/api/handler/subscription_feed_test.go`
- Create: `internal/service/subscription/creator.go`
- Create: `internal/service/subscription/creator_test.go`
- Modify: `internal/service/subscription/batch.go`
- Modify: `internal/service/subscription/batch_test.go`
- Modify: `internal/api/handler/subscription.go`
- Modify: `internal/api/handler/subscription_test.go`
- Modify: `internal/repository/subscription.go`
- Modify: `internal/repository/subscription_stats_test.go`
- Modify: `internal/api/router/router.go`
- Modify: `internal/api/router/router_test.go`

- [ ] **Step 1: Write failing handler and compatibility tests**

```go
func TestCreateSubscriptionWithFeedsPersistsSubscriptionAndFeeds(t *testing.T) {
    fx := newSubscriptionFeedHandlerFixture(t)
    recorder := fx.postJSON("/subscriptions", `{
      "name":"Anime","season":1,
      "feeds":[
        {"name":"A","rss_url":"https://a.test/rss","episode_offset":0,"enabled":true},
        {"name":"B","rss_url":"https://b.test/rss","episode_offset":100,"enabled":true}
      ]
    }`)
    require.Equal(t, http.StatusOK, recorder.Code)
    var feeds []model.SubscriptionFeed
    require.NoError(t, fx.db.Order("id").Find(&feeds).Error)
    require.Len(t, feeds, 2)
    assert.Equal(t, []int{0, 100}, []int{feeds[0].EpisodeOffset, feeds[1].EpisodeOffset})
}

func TestLegacyRSSUpdateRejectsAmbiguousMultiFeedSubscription(t *testing.T) {
    fx := newSubscriptionFeedHandlerFixture(t)
    sub := fx.seedSubscriptionWithFeeds(2)
    recorder := fx.putJSON(fmt.Sprintf("/subscriptions/%d", sub.ID), `{"rss_url":"https://new.test/rss"}`)
    assert.Equal(t, http.StatusConflict, recorder.Code)
    assert.Contains(t, recorder.Body.String(), "feed API")
}

func TestFeedPreviewAndCRUDRoutes(t *testing.T) {
    fx := newSubscriptionFeedHandlerFixture(t)
    sub := fx.seedSubscriptionWithFeeds(0)
    preview := fx.postJSON(fmt.Sprintf("/subscriptions/%d/feeds/preview", sub.ID), `{"rss_url":"https://a.test/rss","episode_offset":100}`)
    assert.Equal(t, http.StatusOK, preview.Code)
    created := fx.postJSON(fmt.Sprintf("/subscriptions/%d/feeds", sub.ID), `{"name":"A","rss_url":"https://a.test/rss","episode_offset":100,"enabled":true}`)
    assert.Equal(t, http.StatusOK, created.Code)
}

func TestCreateAllowsSameFeedURLInDifferentSubscriptions(t *testing.T) {
    fx := newSubscriptionFeedHandlerFixture(t)
    first := fx.postJSON("/subscriptions", `{"name":"Anime A","season":1,"feeds":[{"name":"A","rss_url":"https://shared.test/rss","episode_offset":0,"enabled":true}]}`)
    second := fx.postJSON("/subscriptions", `{"name":"Anime B","season":1,"feeds":[{"name":"A","rss_url":"https://shared.test/rss","episode_offset":0,"enabled":true}]}`)
    assert.Equal(t, http.StatusOK, first.Code)
    assert.Equal(t, http.StatusOK, second.Code)
}

func TestSubscriptionStatsReturnsFeedCountWithoutMultiplyingDownloads(t *testing.T) {
    repo, db := setupSubscriptionStatsRepository(t)
    sub := model.Subscription{Name: "Anime"}
    require.NoError(t, db.Create(&sub).Error)
    require.NoError(t, db.Create(&[]model.SubscriptionFeed{
        {SubscriptionID: sub.ID, Name: "A", RSSURL: "https://a.test/rss", RSSURLNormalized: "https://a.test/rss"},
        {SubscriptionID: sub.ID, Name: "B", RSSURL: "https://b.test/rss", RSSURLNormalized: "https://b.test/rss"},
    }).Error)
    require.NoError(t, db.Create(&[]model.Download{
        {SubscriptionID: sub.ID, Title: "E01", TorrentURL: "https://x/1", TorrentHash: "h1", Status: model.DownloadStatusDownloading},
        {SubscriptionID: sub.ID, Title: "E02", TorrentURL: "https://x/2", TorrentHash: "h2", Status: model.DownloadStatusDownloading},
    }).Error)

    rows, err := repo.GetSubscriptionsWithDownloadCount()
    require.NoError(t, err)
    require.Len(t, rows, 1)
    assert.EqualValues(t, 2, rows[0].FeedCount)
    assert.EqualValues(t, 2, rows[0].DownloadingCount)
}

func TestCreatorRejectsDuplicateInitialFeedsWithoutPartialWrite(t *testing.T) {
    creator, db := newSubscriptionCreatorFixture(t)
    sub := &model.Subscription{Name: "Anime", Season: 1, Enabled: true}
    err := creator.Create(context.Background(), sub, []subscriptionfeed.Input{
        {Name: "A", RSSURL: "https://same.test/rss", EpisodeOffset: 0, Enabled: true},
        {Name: "Duplicate", RSSURL: "https://same.test/rss", EpisodeOffset: 100, Enabled: true},
    })
    require.Error(t, err)
    var subscriptions, feeds int64
    require.NoError(t, db.Model(&model.Subscription{}).Count(&subscriptions).Error)
    require.NoError(t, db.Model(&model.SubscriptionFeed{}).Count(&feeds).Error)
    assert.Zero(t, subscriptions)
    assert.Zero(t, feeds)
}
```

在同一测试文件实现 `newSubscriptionFeedHandlerFixture`：使用内存 SQLite 迁移 subscription、feed、episode 和 download 模型；fake parser 按 URL 返回至少一条可映射条目；`postJSON`/`putJSON` 统一设置 `Content-Type: application/json`。

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./internal/api/handler ./internal/api/router ./internal/repository ./internal/service/subscription -run 'SubscriptionWithFeeds|LegacyRSSUpdate|FeedPreviewAndCRUD|SameFeedURL|FeedCountWithoutMultiplying|CreatorRejectsDuplicate' -count=1`

Expected: FAIL，提示 `feeds` 未保存或 feed 路由不存在。

- [ ] **Step 3: Implement feed handler**

`SubscriptionFeedHandler` 注入 `subscriptionfeed.Service` 和 `SubscriptionFeedRepository`，注册：

```go
subscriptions.GET("/:id/feeds", feedHandler.List)
subscriptions.POST("/:id/feeds", feedHandler.Create)
subscriptions.PUT("/:id/feeds/:feedId", feedHandler.Update)
subscriptions.DELETE("/:id/feeds/:feedId", feedHandler.Delete)
subscriptions.POST("/:id/feeds/preview", feedHandler.PreviewNew)
subscriptions.POST("/:id/feeds/:feedId/preview", feedHandler.PreviewExisting)
```

handler 必须验证 `feed.SubscriptionID == :id`；跨订阅访问返回 404。错误映射固定为：非法 URL/负偏移/无有效映射返回 422，重复 URL 返回 409，不存在返回 404，拉取失败返回 502。

- [ ] **Step 4: Add create payload and transactional initial feeds**

在 `subscription.go` 增加仅用于请求的 DTO：

```go
type subscriptionWriteRequest struct {
    model.Subscription
    Feeds []subscriptionfeed.Input `json:"feeds"`
}
```

创建 `internal/service/subscription/creator.go`：

```go
type Creator interface {
    Create(ctx context.Context, sub *model.Subscription, feeds []subscriptionfeed.Input) error
}

type creator struct {
    db          *gorm.DB
    feedService *subscriptionfeed.Service
}
```

`Create` 先对每条输入调用 `feedService.Prepare`，并在内存中按 `RSSURLNormalized` 检查同一请求重复；再在一个 GORM 事务中创建 subscription，并逐条调用 `CreatePreparedInTx`。至少一条 feed 时 `SourceType` 不能被 `normalizeSubscriptionSource` 改成 calendar；第一条 prepared feed 投影到旧字段：

```go
subscription.RssURL = preparedFeeds[0].Feed.RSSURL
subscription.Fansub = preparedFeeds[0].Feed.Fansub
subscription.EpisodeOffset = preparedFeeds[0].Feed.EpisodeOffset
subscription.LastRSSPubTime = nil
subscription.RSSBaselinePending = false
```

新 UI 创建的订阅仍保留旧投影，以兼容列表、备份键和旧客户端；调度器不读取这些字段。移除现有基于 `GetByRSSURLAndSeason` 的全局 URL 冲突判断，重复约束只作用于同一订阅内的 `(subscription_id, rss_url_normalized)`。

`SubscriptionHandler.Create`、`batchImporter.Import`、JSON import 和 OPML import 都调用同一个 `Creator`。`batchImporter` 保留导入列表内部去重，但移除数据库层面的全局 `GetByRSSURLAndSeason` 冲突判断；它把选中的字幕组转换成一条 feed 输入。

给 `SubscriptionWithStats` 增加：

```go
FeedCount int64 `json:"feed_count" gorm:"column:feed_count"`
```

统计查询用相关子查询计算 feed 数量：

```sql
(SELECT COUNT(*) FROM subscription_feeds sf WHERE sf.subscription_id = subscriptions.id) AS feed_count
```

不要把 `subscription_feeds` 直接 JOIN 到现有 downloads 聚合，否则 feed 数会把 downloading count 成倍放大。

- [ ] **Step 5: Implement legacy single-feed proxy rules**

旧 `POST /subscriptions` 只传 `rss_url` 时，把旧字段转换成一条默认 feed 输入并走同一验证和事务流程。旧 `PUT /subscriptions/:id` 修改 `rss_url`、`fansub` 或 `episode_offset` 时：

```go
feeds, err := feedRepo.ListBySubscription(id)
if err != nil { return internalError }
if len(feeds) != 1 {
    return http.StatusConflict, "subscription has multiple feeds; use feed API"
}
```

唯一 feed 更新通过 feed service 的事务同步旧投影；多 feed 订阅的旧字段更新直接返回 409。feed API 的 create/update/delete 成功后，handler 调用 `episodeService.RefreshSubscriptionProgress(id)` 重新计算旧 `current_episode/latest_episode` 投影。删除最后一条 feed 时 service 清空旧 RSS 字段，但不把订阅自动改成 calendar。

- [ ] **Step 6: Run handler and router tests**

Run: `go test ./internal/api/handler ./internal/api/router -count=1`

Expected: PASS。

- [ ] **Step 7: Commit API and compatibility layer**

```bash
git add internal/api/handler/subscription_feed.go internal/api/handler/subscription_feed_test.go internal/service/subscription/creator.go internal/service/subscription/creator_test.go internal/service/subscription/batch.go internal/service/subscription/batch_test.go internal/api/handler/subscription.go internal/api/handler/subscription_test.go internal/repository/subscription.go internal/repository/subscription_stats_test.go internal/api/router/router.go internal/api/router/router_test.go
git commit -m "Expose subscription feed APIs"
```

## Task 4: 让剧集台账接收 feed 相对集数和来源快照

**Files:**
- Modify: `internal/service/episode/service.go`
- Modify: `internal/service/episode/service_test.go`
- Modify: `internal/repository/episode.go`
- Modify: `internal/repository/episode_test.go`

- [ ] **Step 1: Write failing feed mapping and candidate snapshot tests**

```go
func TestEvaluateRSSItemUsesProvidedRelativeEpisode(t *testing.T) {
    svc, repo := setupEpisodeService(t)
    sub := &model.Subscription{ID: 1, EpisodeOffset: 0}
    decision, err := svc.EvaluateRSSItem(context.Background(), sub, episode.RSSResource{
        OriginalEpisode: 101,
        RelativeEpisode: 1,
        SubscriptionFeedID: 9,
        Resource: model.EpisodeResource{Hash: "hash-101", URL: "https://b.test/101"},
    }, false)
    require.NoError(t, err)
    assert.Equal(t, episode.DecisionDownload, decision.Action)
    ledger, err := repo.GetBySubscriptionAndEpisode(sub.ID, 1)
    require.NoError(t, err)
    assert.Equal(t, 1, ledger.Episode)
}

func TestCandidatePersistsFeedSnapshot(t *testing.T) {
    svc, repo := setupEpisodeService(t)
    seedDownloadedEpisode(t, svc, 1, 1, model.EpisodeResource{Hash: "old"})
    decision, err := svc.EvaluateRSSItem(context.Background(), &model.Subscription{ID: 1}, episode.RSSResource{
        OriginalEpisode: 101, RelativeEpisode: 1, SubscriptionFeedID: 9,
        SourceFeedName: "B", Fansub: "Group B", SourceEpisodeOffset: 100,
        SourceRSSURL: "https://b.test/rss",
        Resource: model.EpisodeResource{Hash: "new", URL: "https://b.test/101"},
    }, false)
    require.NoError(t, err)
    candidates, err := repo.ListCandidates(decision.EpisodeID)
    require.NoError(t, err)
    require.Len(t, candidates, 1)
    assert.Equal(t, "B", candidates[0].SourceFeedName)
    assert.Equal(t, 100, candidates[0].SourceEpisodeOffset)
}
```

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./internal/service/episode ./internal/repository -run 'ProvidedRelativeEpisode|CandidatePersistsFeedSnapshot' -count=1`

Expected: FAIL，因为 `RSSResource` 没有相对集数和 feed 来源。

- [ ] **Step 3: Extend RSSResource and remove subscription-offset mapping**

修改 `RSSResource`：

```go
type RSSResource struct {
    OriginalEpisode    int
    RelativeEpisode    int
    SubscriptionFeedID uint
    SourceFeedName     string
    SourceEpisodeOffset int
    Resource           model.EpisodeResource
    Fansub             string
    Language           string
    PubTime            time.Time
    SourceRSSURL       string
}
```

`ObserveRSSItem` 改成接收 `relativeEpisode int`；`EvaluateRSSItem` 和 `PreviewRSSItem` 只使用 `item.RelativeEpisode`。相对集数小于等于 0 返回 `skip`，不得回退到 `sub.RelativeEpisode(item.OriginalEpisode)`。

创建候选时写入 `SubscriptionFeedID` 和全部来源快照。`ClaimForDownload` 的资源身份仍只比较 hash/URL，不把 feed ID 作为第二份收藏依据。

- [ ] **Step 4: Run episode tests and verify GREEN**

Run: `go test ./internal/service/episode ./internal/repository -count=1`

Expected: PASS。

- [ ] **Step 5: Commit ledger feed mapping**

```bash
git add internal/service/episode/service.go internal/service/episode/service_test.go internal/repository/episode.go internal/repository/episode_test.go
git commit -m "Map feed episodes into the ledger"
```

## Task 5: 把调度器切换为 feed 级抓取、基线和水位线

**Files:**
- Modify: `internal/service/scheduler/scheduler.go`
- Modify: `internal/service/scheduler/scheduler_test.go`
- Modify: `internal/service/scheduler/smart_fetch.go`
- Modify: `internal/service/scheduler/smart_fetch_completed_test.go`
- Modify: `internal/api/handler/subscription.go`
- Modify: `internal/api/handler/subscription_test.go`
- Modify: `internal/api/router/router.go`
- Modify: `internal/api/router/router_test.go`

- [ ] **Step 1: Write failing multi-feed scheduler tests**

```go
func TestRSSCheckMapsDifferentFeedOffsetsToOneDownload(t *testing.T) {
    fx := newMultiFeedSchedulerFixture(t)
    sub := fx.seedSubscription()
    a := fx.seedFeed(sub.ID, "A", "https://a.test/rss", 0, false)
    b := fx.seedFeed(sub.ID, "B", "https://b.test/rss", 100, false)
    now := time.Now()
    fx.parser.set(a.RSSURL, []rss.RSSItem{{Title: "A 01", Episode: 1, TorrentHash: "a", TorrentURL: "https://a.test/1", PubTime: now}})
    fx.parser.set(b.RSSURL, []rss.RSSItem{{Title: "B 101", Episode: 101, TorrentHash: "b", TorrentURL: "https://b.test/101", PubTime: now}})

    fx.scheduler.checkRSSFeeds()

    assert.Equal(t, 1, fx.qb.addCalls.Load())
    var downloads []model.Download
    require.NoError(t, fx.db.Find(&downloads).Error)
    require.Len(t, downloads, 1)
    assert.NotNil(t, downloads[0].SubscriptionFeedID)
    var candidates int64
    require.NoError(t, fx.db.Model(&model.EpisodeResourceCandidate{}).Count(&candidates).Error)
    assert.EqualValues(t, 1, candidates)
}

func TestNewFeedBaselineDoesNotDownloadHistoricalMissingEpisodes(t *testing.T) {
    fx := newMultiFeedSchedulerFixture(t)
    sub := fx.seedSubscription()
    feed := fx.seedFeed(sub.ID, "B", "https://b.test/rss", 100, true)
    base := time.Now().Add(-time.Hour)
    fx.parser.set(feed.RSSURL, []rss.RSSItem{
        {Title: "B 101", Episode: 101, TorrentHash: "b1", TorrentURL: "https://b.test/101", PubTime: base},
        {Title: "B 102", Episode: 102, TorrentHash: "b2", TorrentURL: "https://b.test/102", PubTime: base.Add(time.Minute)},
    })

    fx.scheduler.checkRSSFeeds()

    assert.Zero(t, fx.qb.addCalls.Load())
    for _, episodeNumber := range []int{1, 2} {
        ledger, err := fx.episodeRepo.GetBySubscriptionAndEpisode(sub.ID, episodeNumber)
        require.NoError(t, err)
        assert.Equal(t, model.EpisodeStatusMissing, ledger.Status)
    }
    stored, err := fx.feedRepo.GetByID(feed.ID)
    require.NoError(t, err)
    assert.False(t, stored.BaselinePending)
    assert.Equal(t, base.Add(time.Minute), *stored.LastRSSPubTime)
}

func TestOneFeedFailureDoesNotBlockAnotherFeed(t *testing.T) {
    fx := newMultiFeedSchedulerFixture(t)
    sub := fx.seedSubscription()
    failed := fx.seedFeed(sub.ID, "A", "https://a.test/rss", 0, false)
    healthy := fx.seedFeed(sub.ID, "B", "https://b.test/rss", 100, false)
    fx.parser.fail(failed.RSSURL, errors.New("timeout"))
    fx.parser.set(healthy.RSSURL, []rss.RSSItem{{Title: "B 101", Episode: 101, TorrentHash: "b", TorrentURL: "https://b.test/101", PubTime: time.Now()}})

    fx.scheduler.checkRSSFeeds()

    assert.Equal(t, 1, fx.qb.addCalls.Load())
    failedStored, _ := fx.feedRepo.GetByID(failed.ID)
    healthyStored, _ := fx.feedRepo.GetByID(healthy.ID)
    assert.Contains(t, failedStored.LastError, "timeout")
    assert.Empty(t, healthyStored.LastError)
    assert.NotNil(t, healthyStored.LastSuccessAt)
}

func TestFeedWithoutPublicationTimesStillFindsNewEpisodesIdempotently(t *testing.T) {
    fx := newMultiFeedSchedulerFixture(t)
    sub := fx.seedSubscription()
    feed := fx.seedFeed(sub.ID, "A", "https://a.test/rss", 0, false)
    fx.parser.set(feed.RSSURL, []rss.RSSItem{{Title: "A 01", Episode: 1, TorrentHash: "a1", TorrentURL: "https://a.test/1"}})

    fx.scheduler.checkRSSFeeds()
    fx.scheduler.checkRSSFeeds()

    assert.Equal(t, 1, fx.qb.addCalls.Load())
    var downloads int64
    require.NoError(t, fx.db.Model(&model.Download{}).Count(&downloads).Error)
    assert.EqualValues(t, 1, downloads)
}

func TestBaselineWithoutPublicationTimesDoesNotBackfillOnSecondCheck(t *testing.T) {
    fx := newMultiFeedSchedulerFixture(t)
    sub := fx.seedSubscription()
    feed := fx.seedFeed(sub.ID, "B", "https://b.test/rss", 100, true)
    fx.parser.set(feed.RSSURL, []rss.RSSItem{{Title: "B 101", Episode: 101, TorrentHash: "b1", TorrentURL: "https://b.test/101"}})

    fx.scheduler.checkRSSFeeds()
    fx.scheduler.checkRSSFeeds()
    assert.Zero(t, fx.qb.addCalls.Load())

    fx.parser.set(feed.RSSURL, []rss.RSSItem{
        {Title: "B 101", Episode: 101, TorrentHash: "b1", TorrentURL: "https://b.test/101"},
        {Title: "B 102", Episode: 102, TorrentHash: "b2", TorrentURL: "https://b.test/102"},
    })
    fx.scheduler.checkRSSFeeds()
    assert.Equal(t, 1, fx.qb.addCalls.Load())
}

func TestCollectEpisodesDelegatesToMultiFeedCollector(t *testing.T) {
    collector := &fakeSubscriptionCollector{summary: scheduler.CollectSummary{FeedsChecked: 2, DownloadsCreated: 1, CandidatesCreated: 1}}
    handler := newSubscriptionHandlerWithCollector(collector)
    task := &task.Task{}
    err := handler.doCollectEpisodes(context.Background(), task, &model.Subscription{ID: 7, Name: "Anime"})
    require.NoError(t, err)
    assert.Equal(t, uint(7), collector.subscriptionID)
    assert.Equal(t, 1, collector.calls)
}
```

在同一测试文件实现 `newMultiFeedSchedulerFixture`：fake parser 以 URL 为 key 保存 items/error；fake qB 使用 `atomic.Int64` 记录创建次数；SQLite 至少迁移 subscription、feed、download、episode、candidate 和 config。fixture 的 `seedFeed` 显式设置 `LastRSSPubTime=nil`，避免测试依赖本地时间。

- [ ] **Step 2: Run scheduler tests and verify RED**

Run: `go test ./internal/service/scheduler ./internal/api/handler -run 'DifferentFeedOffsets|NewFeedBaseline|OneFeedFailure|WithoutPublicationTimes|BaselineWithoutPublication|DelegatesToMultiFeedCollector' -count=1`

Expected: FAIL，因为调度器仍读取订阅级 RSS 和偏移。

- [ ] **Step 3: Inject feed repository and remove subscription RSS reads**

修改构造器：

```go
func NewScheduler(
    db *gorm.DB,
    subscriptionRepo repository.SubscriptionRepository,
    feedRepo repository.SubscriptionFeedRepository,
    downloadRepo repository.DownloadRepository,
    configRepo repository.ConfigRepository,
    rssCheckInterval string,
    rssParser rss.Parser,
    qbClient downloader.QBittorrentClient,
    episodeService *episode.Service,
) Scheduler
```

扩展 scheduler 对外接口：

```go
type CollectSummary struct {
    FeedsChecked      int `json:"feeds_checked"`
    ItemsScanned      int `json:"items_scanned"`
    DownloadsCreated  int `json:"downloads_created"`
    CandidatesCreated int `json:"candidates_created"`
    FeedErrors        int `json:"feed_errors"`
}

type SubscriptionCollector interface {
    CollectSubscription(ctx context.Context, subscriptionID uint) (CollectSummary, error)
}
```

`Scheduler` 组合 `SubscriptionCollector`。`CollectSubscription` 绕过智能拉取的日期/完结窗口，但继续遵守 feed 基线、水位线、订阅过滤、最小种子大小和剧集台账决策。

`checkRSSFeeds` 获取活跃订阅后，一次批量查询其启用 feeds。没有 feed 的 calendar/合集订阅跳过 RSS；有 feed 的订阅不读取 `sub.RssURL`、`sub.EpisodeOffset`、`sub.LastRSSPubTime` 或 `sub.RSSBaselinePending`。

- [ ] **Step 4: Fetch feeds concurrently and serialize ledger writes**

增加：

```go
const maxConcurrentFeedChecks = 4

type feedFetchResult struct {
    Feed      model.SubscriptionFeed
    Items     []rss.RSSItem
    CheckedAt time.Time
    Err       error
}
```

使用容量为 4 的 semaphore 和 `sync.WaitGroup` 并发执行 `FetchAndParse`，完成后把 `feedFetchResult` 发送到 channel。协调 goroutine 按 channel 接收顺序串行调用 `processFetchedFeedItems` 和 feed 状态更新：抓取先完成的 feed 先尝试占用剧集，不使用 feed slice 顺序表达优先级，也避免多个 goroutine 同时争用 SQLite 写锁。台账原子约束仍负责手动采集、API 或其他调度入口产生的真实并发。

- [ ] **Step 5: Implement feed-level baseline and incremental decisions**

实现以下函数；返回值是本次成功扫描到的最大非零发布时间，没有有效发布时间时返回 nil：

```go
func (s *scheduler) processFetchedFeedItems(
    ctx context.Context,
    sub *model.Subscription,
    feed *model.SubscriptionFeed,
    items []rss.RSSItem,
) (*time.Time, error)
```

每条 item 先计算：

```go
relativeEpisode := feed.RelativeEpisode(item.Episode)
resource := episode.RSSResource{
    OriginalEpisode: item.Episode,
    RelativeEpisode: relativeEpisode,
    SubscriptionFeedID: feed.ID,
    SourceFeedName: feed.Name,
    SourceEpisodeOffset: feed.EpisodeOffset,
    Resource: model.EpisodeResource{Hash: item.TorrentHash, URL: item.TorrentURL, Title: item.Title},
    Fansub: preferredFansub(item.Fansub, feed.Fansub),
    Language: string(item.Language),
    PubTime: item.PubTime,
    SourceRSSURL: feed.RSSURL,
}
```

同文件增加：

```go
func preferredFansub(itemFansub, configuredFansub string) string {
    if strings.TrimSpace(itemFansub) != "" {
        return strings.TrimSpace(itemFansub)
    }
    return strings.TrimSpace(configuredFansub)
}
```

先用 `episode.ResourceKey(resource.Resource)` 计算稳定资源 key。没有 hash 和 URL 的条目记录 `resource_identity_missing` 并跳过，不能进入无幂等保证的下载或候选流程。

`BaselinePending=true` 时遍历全部条目并调用 `EvaluateRSSItem(..., true)`，不创建下载；每条成功对账后调用 `MarkSeenItem`。全部数据库写入成功后才调用 `UpdateCheckSuccess(..., maxPubTime, true)`。

普通增量规则为：

```go
if item.PubTime.IsZero() {
    seen, err := s.feedRepo.HasSeenItem(feed.ID, resourceKey)
    if err != nil { return nil, err }
    if seen { continue }
} else if feed.LastRSSPubTime != nil && !item.PubTime.After(*feed.LastRSSPubTime) {
    continue
}
```

条目完成相同资源跳过、候选创建或下载创建后调用 `MarkSeenItem`；决策返回错误时不标记，留待下次重试。创建 download 时写入 `SubscriptionFeedID=&feed.ID`。单条过滤也应标记已见并允许推进该 feed 水位线，避免无发布时间的不合规资源每轮重复告警；整体抓取/解析失败不推进。

同一订阅的全部 feed 结果处理完后，把 `subscriptions.last_check_time` 更新为本轮最大 `CheckedAt`，仅作为旧列表 API 的汇总投影；不得写订阅级 `last_rss_pub_time` 或 `rss_baseline_pending`。

`SubscriptionHandler` 接收可选 `scheduler.SubscriptionCollector` 依赖；router 传入真实 scheduler。`doCollectEpisodes` 删除现有单 RSS 抓取、旧记录替换和订阅级水位线代码，只调用 `CollectSubscription`，并把 `CollectSummary` 写入 task result。collector 未注入时返回明确错误，仅供尚未更新的单元测试发现遗漏。

- [ ] **Step 6: Make smart fetch aware of pending feeds**

`SmartFetchFilter.EvaluateSubscription` 增加 `hasPendingFeed bool` 参数。在 calendar-only 判断之后先处理：

```go
if hasPendingFeed {
    return SmartFetchStatus{ShouldFetch: true, FetchReason: "feed_baseline_pending"}, nil
}
```

调用方从该订阅的启用 feeds 计算 `hasPendingFeed`。订阅级 `RSSBaselinePending` 不再影响调度。

- [ ] **Step 7: Run scheduler and router tests**

Run: `go test ./internal/service/scheduler ./internal/api/router -count=1`

Expected: PASS，且 `-race` 下同集只产生一个 download。

Run: `go test -race ./internal/service/scheduler -run 'DifferentFeedOffsets' -count=1`

Expected: PASS，无 data race。

- [ ] **Step 8: Commit scheduler cutover**

```bash
git add internal/service/scheduler/scheduler.go internal/service/scheduler/scheduler_test.go internal/service/scheduler/smart_fetch.go internal/service/scheduler/smart_fetch_completed_test.go internal/api/handler/subscription.go internal/api/handler/subscription_test.go internal/api/router/router.go internal/api/router/router_test.go
git commit -m "Schedule subscription feeds independently"
```

## Task 6: 将健康检查和订阅诊断下沉到 feed

**Files:**
- Modify: `internal/service/rss/health.go`
- Create: `internal/service/rss/health_test.go`
- Modify: `internal/api/handler/rss_health.go`
- Modify: `internal/api/handler/rss_health_test.go`
- Modify: `internal/api/handler/subscription_diagnostics.go`
- Modify: `internal/api/handler/subscription_diagnostics_test.go`

- [ ] **Step 1: Write failing health aggregation tests**

```go
func TestCheckSubscriptionFeedsReportsHealthyWhenOneFeedWorks(t *testing.T) {
    checker, server := newFeedHealthFixture(t)
    defer server.Close()
    sub := model.Subscription{ID: 1, Name: "Anime"}
    feeds := []model.SubscriptionFeed{
        {ID: 10, SubscriptionID: 1, Name: "A", RSSURL: server.URL + "/dead", Enabled: true},
        {ID: 11, SubscriptionID: 1, Name: "B", RSSURL: server.URL + "/healthy", Enabled: true},
    }
    result := checker.CheckSubscriptionFeeds(context.Background(), &sub, feeds)
    assert.Equal(t, rss.HealthStatusHealthy, result.Status)
    require.Len(t, result.Feeds, 2)
    assert.Equal(t, rss.HealthStatusDead, result.Feeds[0].Status)
    assert.Equal(t, rss.HealthStatusHealthy, result.Feeds[1].Status)
}

func TestCheckSubscriptionFeedsIsDeadOnlyWhenAllEnabledFeedsAreDead(t *testing.T) {
    checker, server := newFeedHealthFixture(t)
    defer server.Close()
    result := checker.CheckSubscriptionFeeds(context.Background(), &model.Subscription{ID: 1}, []model.SubscriptionFeed{
        {ID: 1, RSSURL: server.URL + "/dead", Enabled: true},
        {ID: 2, RSSURL: server.URL + "/dead", Enabled: true},
    })
    assert.Equal(t, rss.HealthStatusDead, result.Status)
}
```

`newFeedHealthFixture` 使用 `httptest.Server` 提供 `/healthy` 的有效 RSS 和 `/dead` 的 503 响应，并把 checker 的 client 指向标准 HTTP client；测试不访问外网。

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./internal/service/rss ./internal/api/handler -run 'SubscriptionFeeds|FeedHealth' -count=1`

Expected: FAIL，因为健康检查只接受 `Subscription.RssURL`。

- [ ] **Step 3: Add feed health result and aggregate rules**

定义：

```go
type FeedHealthCheckResult struct {
    SubscriptionFeedID uint         `json:"subscription_feed_id"`
    Name               string       `json:"name"`
    Fansub             string       `json:"fansub"`
    RSSURL             string       `json:"rss_url"`
    Status             HealthStatus `json:"status"`
    ResponseTime       int64        `json:"response_time_ms"`
    ErrorMessage       string       `json:"error_message,omitempty"`
    LastPostDate       *time.Time   `json:"last_post_date,omitempty"`
}

type HealthCheckResult struct {
    SubscriptionID uint                    `json:"subscription_id"`
    Name           string                  `json:"name"`
    Status         HealthStatus            `json:"status"`
    LastCheckTime  time.Time               `json:"last_check_time"`
    Feeds          []FeedHealthCheckResult `json:"feeds"`
}
```

汇总规则：任一启用 feed healthy 则订阅 healthy；否则任一 unhealthy 则 unhealthy；全部 dead 才 dead；无启用 feed 为 unknown。`CheckAllSubscriptions` 批量读取 feeds，禁止每个订阅单独查询。

构造器改为：

```go
func NewHealthChecker(
    subRepo repository.SubscriptionRepository,
    feedRepo repository.SubscriptionFeedRepository,
) *RSSHealthChecker
```

router、`RSSHealthHandler` 和 `SubscriptionDiagnosticsHandler` 统一注入同一个 feed repository。

- [ ] **Step 4: Update diagnostics response**

在 `SubscriptionDiagnosticsResponse` 增加：

```go
Feeds []rss.FeedHealthCheckResult `json:"feeds"`
```

`buildRSSReachabilityCheck` 使用 feed 汇总结果；`buildRSSFreshnessCheck` 使用启用 feed 的最大 `LastSuccessAt` 和最大 `LastRSSPubTime`。单个 feed 失败形成 warning；全部启用 feed 失败形成 error。诊断详情列出 feed 名称和错误，不读取旧订阅 RSS 字段。

- [ ] **Step 5: Run health and handler tests**

Run: `go test ./internal/service/rss ./internal/api/handler -count=1`

Expected: PASS。

- [ ] **Step 6: Commit feed health diagnostics**

```bash
git add internal/service/rss/health.go internal/service/rss/health_test.go internal/api/handler/rss_health.go internal/api/handler/rss_health_test.go internal/api/handler/subscription_diagnostics.go internal/api/handler/subscription_diagnostics_test.go
git commit -m "Report health per subscription feed"
```

## Task 7: 让备份和订阅导入导出保留多 feed 配置

**Files:**
- Modify: `internal/service/backup/backup.go`
- Modify: `internal/service/backup/backup_test.go`
- Modify: `internal/api/handler/subscription.go`
- Modify: `internal/api/handler/subscription_test.go`

- [ ] **Step 1: Write failing backup round-trip test**

```go
func TestBackupRoundTripPreservesSubscriptionFeedsWithoutRuntimeState(t *testing.T) {
    source := newBackupTestDB(t)
    sub := model.Subscription{Name: "Anime", Season: 1, RssURL: "https://a.test/rss"}
    require.NoError(t, source.Create(&sub).Error)
    require.NoError(t, source.Create(&[]model.SubscriptionFeed{
        {SubscriptionID: sub.ID, Name: "A", RSSURL: "https://a.test/rss", RSSURLNormalized: "https://a.test/rss", EpisodeOffset: 0, Enabled: true, BaselinePending: false},
        {SubscriptionID: sub.ID, Name: "B", RSSURL: "https://b.test/rss", RSSURLNormalized: "https://b.test/rss", EpisodeOffset: 100, Enabled: true, BaselinePending: false, LastError: "old timeout"},
    }).Error)

    pkg, err := backup.NewService(source).Export(false)
    require.NoError(t, err)
    require.Len(t, pkg.Subscriptions[0].Feeds, 2)
    assert.Empty(t, pkg.Subscriptions[0].Feeds[1].LastError)
    assert.Nil(t, pkg.Subscriptions[0].Feeds[1].LastRSSPubTime)

    target := newBackupTestDB(t)
    encoded, err := json.Marshal(pkg)
    require.NoError(t, err)
    _, err = backup.NewService(target).Import(encoded, backup.SourceAutoRSS, backup.StrategyOverwrite)
    require.NoError(t, err)
    var restored []model.SubscriptionFeed
    require.NoError(t, target.Order("episode_offset").Find(&restored).Error)
    require.Len(t, restored, 2)
    assert.Equal(t, []int{0, 100}, []int{restored[0].EpisodeOffset, restored[1].EpisodeOffset})
    assert.True(t, restored[0].BaselinePending)
    assert.True(t, restored[1].BaselinePending)
}

func TestSubscriptionOPMLRoundTripGroupsFeedsIntoOneSubscription(t *testing.T) {
    fx := newSubscriptionFeedHandlerFixture(t)
    sub := fx.seedSubscriptionWithFeeds(2)
    exported := fx.get(fmt.Sprintf("/subscriptions/export?format=opml&id=%d", sub.ID))
    require.Equal(t, http.StatusOK, exported.Code)
    assert.Contains(t, exported.Body.String(), `autoRssOffset="100"`)

    target := newSubscriptionFeedHandlerFixture(t)
    imported := target.postJSON("/subscriptions/import", marshalImportRequest("opml", exported.Body.String()))
    require.Equal(t, http.StatusOK, imported.Code)
    var subscriptions, feeds int64
    require.NoError(t, target.db.Model(&model.Subscription{}).Count(&subscriptions).Error)
    require.NoError(t, target.db.Model(&model.SubscriptionFeed{}).Count(&feeds).Error)
    assert.EqualValues(t, 1, subscriptions)
    assert.EqualValues(t, 2, feeds)
}

func marshalImportRequest(format, data string) string {
    encoded, err := json.Marshal(map[string]string{"format": format, "data": data})
    if err != nil {
        panic(err)
    }
    return string(encoded)
}
```

`newBackupTestDB` 在现有 backup 测试 helper 的 `AutoMigrate` 列表中加入 `SubscriptionFeed`；不要另建第二套数据库初始化函数。

- [ ] **Step 2: Run backup test and verify RED**

Run: `go test ./internal/service/backup ./internal/api/handler -run 'PreservesSubscriptionFeeds|OPMLRoundTripGroupsFeeds' -count=1`

Expected: FAIL，因为 `SubscriptionRecord` 不包含 feeds。

- [ ] **Step 3: Export and restore feed records**

给 `SubscriptionRecord` 增加：

```go
Feeds []model.SubscriptionFeed `json:"feeds,omitempty"`
```

把 `SchemaVersion` 从 `1.0` 提升到 `1.1`。解析器继续接受 `1.0`：没有 `feeds` 时按旧 `rss_url/fansub/episode_offset` 生成默认 feed；导出统一写 `1.1`。

导出时按 `subscription_id, id` 查询 feeds 并挂到对应 record；清空 `ID`、`SubscriptionID`、水位线、检查时间、成功时间和错误。`subscription_feed_seen_items` 属于运行时快照，既不导出也不恢复。导入时先解析 subscription，再为每条 feed 重建 `SubscriptionID`、`RSSURLNormalized`，并强制：

```go
feed.ID = 0
feed.SubscriptionID = restoredSubscription.ID
feed.LastRSSPubTime = nil
feed.LastCheckTime = nil
feed.LastSuccessAt = nil
feed.LastError = ""
feed.BaselinePending = true
```

旧备份没有 `feeds` 但有 `rss_url` 时，生成一条默认 feed。overwrite 删除并重建目标订阅 feeds；merge 按规范化 URL 增补，不覆盖目标 feed 的运行时水位线。

- [ ] **Step 4: Update JSON and OPML subscription export/import**

JSON export 版本改为 `2.0`，每个 subscription record 包含 `feeds`；JSON import 接受 `1.0` 单 RSS 和 `2.0` feeds，并统一调用 Task 3 的 `subscription.Creator`。

OPML 每条 feed 输出一个 outline，并增加扩展属性：

```xml
<outline type="rss" text="Anime - B" title="Anime" xmlUrl="https://b.test/rss" autoRssSubscription="Anime" autoRssSeason="1" autoRssFeed="B" autoRssFansub="Group B" autoRssOffset="100" />
```

导入时按 `autoRssSubscription + autoRssSeason` 分组为一个 subscription 和多条 feeds。没有扩展属性的传统 OPML 继续按“一条 outline = 一个订阅的一条默认 feed”处理。属性值统一使用 XML escape/unescape，offset 非法时返回该条导入失败，不默认为 0。

- [ ] **Step 5: Run backup and handler tests**

Run: `go test ./internal/service/backup ./internal/api/handler -count=1`

Expected: PASS。

- [ ] **Step 6: Commit backup and import/export support**

```bash
git add internal/service/backup/backup.go internal/service/backup/backup_test.go internal/api/handler/subscription.go internal/api/handler/subscription_test.go
git commit -m "Preserve feeds in subscription exports"
```

## Task 8: 增加前端 feed 类型、API 和纯保存逻辑

**Files:**
- Create: `web/src/api/subscription-feed.ts`
- Create: `web/src/utils/subscription-feeds.ts`
- Create: `web/tests/subscription-feeds.test.ts`
- Modify: `web/package.json`

- [ ] **Step 1: Write failing frontend utility tests**

```ts
import assert from 'node:assert/strict'
import test from 'node:test'
import { buildFeedSavePlan, normalizeFeedURLForComparison } from '../src/utils/subscription-feeds.ts'

test('不同偏移的两个 feed 保持为两个独立保存项', () => {
  const plan = buildFeedSavePlan(
    [{ id: 1, name: 'A', rss_url: 'https://a.test/rss', episode_offset: 0, enabled: true }],
    [
      { id: 1, name: 'A', rss_url: 'https://a.test/rss', episode_offset: 0, enabled: true },
      { name: 'B', rss_url: 'https://b.test/rss', episode_offset: 100, enabled: true }
    ]
  )
  assert.deepEqual(plan.create.map(item => item.episode_offset), [100])
  assert.equal(plan.update.length, 0)
  assert.equal(plan.remove.length, 0)
})

test('URL 或偏移变化要求重新预览', () => {
  const plan = buildFeedSavePlan(
    [{ id: 1, name: 'A', rss_url: 'https://a.test/rss', episode_offset: 0, enabled: true }],
    [{ id: 1, name: 'A', rss_url: 'https://a.test/rss', episode_offset: 100, enabled: true }]
  )
  assert.equal(plan.update[0].requiresPreview, true)
})

test('比较 URL 时忽略 host 大小写和默认端口', () => {
  assert.equal(
    normalizeFeedURLForComparison('HTTPS://Example.COM:443/rss?b=2&a=1'),
    'https://example.com/rss?a=1&b=2'
  )
})
```

- [ ] **Step 2: Run frontend test and verify RED**

Run: `cd web && node --experimental-strip-types --test tests/subscription-feeds.test.ts`

Expected: FAIL，提示模块不存在。

- [ ] **Step 3: Add API types and client**

在 `subscription-feed.ts` 导出：

```ts
import { api } from './index'

export interface SubscriptionFeed {
  id: number
  subscription_id: number
  name: string
  fansub: string
  rss_url: string
  episode_offset: number
  enabled: boolean
  baseline_pending: boolean
  last_rss_pub_time?: string
  last_check_time?: string
  last_success_at?: string
  last_error: string
}

export interface SubscriptionFeedInput {
  id?: number
  name: string
  fansub?: string
  rss_url: string
  episode_offset: number
  enabled: boolean
}

export const subscriptionFeedApi = {
  list: (subscriptionId: number) => api.get(`/subscriptions/${subscriptionId}/feeds`),
  preview: (subscriptionId: number, input: SubscriptionFeedInput, feedId?: number) =>
    api.post(feedId ? `/subscriptions/${subscriptionId}/feeds/${feedId}/preview` : `/subscriptions/${subscriptionId}/feeds/preview`, input),
  create: (subscriptionId: number, input: SubscriptionFeedInput) => api.post(`/subscriptions/${subscriptionId}/feeds`, input),
  update: (subscriptionId: number, feedId: number, input: SubscriptionFeedInput) => api.put(`/subscriptions/${subscriptionId}/feeds/${feedId}`, input),
  remove: (subscriptionId: number, feedId: number) => api.delete(`/subscriptions/${subscriptionId}/feeds/${feedId}`)
}
```

- [ ] **Step 4: Implement deterministic save plan**

在 `web/src/utils/subscription-feeds.ts` 实现：

```ts
import type { SubscriptionFeedInput } from '../api/subscription-feed.ts'

export type PlannedFeed = SubscriptionFeedInput & { requiresPreview: boolean }

export interface FeedSavePlan {
  create: PlannedFeed[]
  update: PlannedFeed[]
  remove: number[]
}

export function normalizeFeedURLForComparison(raw: string): string {
  const parsed = new URL(raw.trim())
  parsed.hash = ''
  parsed.searchParams.sort()
  return parsed.toString()
}

function changed(before: SubscriptionFeedInput, after: SubscriptionFeedInput): boolean {
  return before.name !== after.name ||
    (before.fansub || '') !== (after.fansub || '') ||
    normalizeFeedURLForComparison(before.rss_url) !== normalizeFeedURLForComparison(after.rss_url) ||
    before.episode_offset !== after.episode_offset ||
    before.enabled !== after.enabled
}

export function buildFeedSavePlan(
  original: SubscriptionFeedInput[],
  current: SubscriptionFeedInput[]
): FeedSavePlan {
  const originalById = new Map(original.filter(item => item.id).map(item => [item.id!, item]))
  const currentIds = new Set(current.filter(item => item.id).map(item => item.id!))
  const create = current.filter(item => !item.id).map(item => ({ ...item, requiresPreview: true }))
  const update = current.flatMap(item => {
    if (!item.id) return []
    const before = originalById.get(item.id)
    if (!before || !changed(before, item)) return []
    const requiresPreview = normalizeFeedURLForComparison(before.rss_url) !== normalizeFeedURLForComparison(item.rss_url) ||
      before.episode_offset !== item.episode_offset
    return [{ ...item, requiresPreview }]
  })
  const remove = original.filter(item => item.id && !currentIds.has(item.id)).map(item => item.id!)
  return { create, update, remove }
}
```

前端规范化只用于即时重复提示，后端仍是最终判定。调用前先验证 URL 可由 `new URL` 解析，并把解析错误显示在对应行。

- [ ] **Step 5: Add test script and verify GREEN**

在 `web/package.json` 增加：

```json
"test:subscription-feeds": "node --experimental-strip-types --test tests/subscription-feeds.test.ts"
```

Run: `cd web && npm run test:subscription-feeds`

Expected: PASS，3 tests passed。

- [ ] **Step 6: Commit frontend feed domain**

```bash
git add web/src/api/subscription-feed.ts web/src/utils/subscription-feeds.ts web/tests/subscription-feeds.test.ts web/package.json
git commit -m "Add subscription feed frontend model"
```

## Task 9: 构建 feed 编辑器并接入订阅创建和编辑

**Files:**
- Create: `web/src/components/SubscriptionFeedsEditor.vue`
- Modify: `web/src/views/Subscriptions.vue`
- Modify: `web/src/api/index.ts`

- [ ] **Step 1: Build the feed editor component**

`SubscriptionFeedsEditor.vue` 接收：

```ts
const props = defineProps<{
  subscriptionId?: number
  modelValue: SubscriptionFeedInput[]
  readonly?: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: SubscriptionFeedInput[]]
  'validation-change': [valid: boolean]
}>()
```

组件使用紧凑 `n-data-table` 展示启用、名称/字幕组、URL、偏移、基线状态、最近成功和错误；行尾使用编辑、删除和预览图标按钮及 tooltip。新增/编辑使用单层 modal，不嵌套 card。URL 或偏移变化后将该草稿标为 `previewValid=false`；成功预览且响应至少为空 feed warning或一个有效条目后设为 true。

- [ ] **Step 2: Render mapping preview**

预览区固定列：标题、原始集数、偏移、相对集数、状态。无效行使用 warning 标签并显示 `invalid_reason`；空 RSS 显示“RSS 当前没有条目，保存后将等待首次发布”，不阻止保存。

- [ ] **Step 3: Replace single RSS fields in subscription modal**

`Subscriptions.vue` 删除面向用户的单一 `rss_url`、`fansub` 和 `episode_offset` 表单项及快捷偏移编辑入口，加入：

```vue
<SubscriptionFeedsEditor
  v-if="!isCalendarOnlyForm"
  v-model="feedDrafts"
  :subscription-id="editingId"
  @validation-change="feedsValid = $event"
/>
```

从 Mikan 搜索订阅时创建一条 feed 草稿：名称使用字幕组，URL 使用搜索结果，偏移默认为 0。编辑订阅时并行加载订阅和 feeds。calendar-only 清空草稿；合集-only 允许 feeds 为空。

- [ ] **Step 4: Save create and edit flows**

创建请求包含 `feeds: feedDrafts`。编辑时先更新订阅共享字段，再按 `buildFeedSavePlan` 顺序执行 create、update、remove；任一步失败时保留 modal 和草稿，显示具体 feed 名称及错误，不清空已成功保存项，随后重新加载服务端 feeds 供用户继续处理。

提交按钮禁用条件固定为：共享表单无效，或存在 URL/偏移变化但未完成有效预览。不要通过 feed 行顺序表达优先级；列表标题旁显示“所有 feed 平等，先到先得”。

- [ ] **Step 5: Update subscription and diagnostics display**

订阅卡片用“RSS × N”标签替代单字幕组标签；展开或编辑时展示 feed 明细。诊断 modal 增加 feed 列表，逐条显示 healthy/unhealthy/dead、最近成功和错误。候选资源区域显示 `source_feed_name`、`source_fansub` 和原始/相对集数。

- [ ] **Step 6: Run frontend tests and production build**

Run: `cd web && npm run test:episodes && npm run test:episode-ledger && npm run test:subscription-feeds`

Expected: PASS；所有 Node tests 通过。

Run: `cd web && npm run build`

Expected: PASS；`vue-tsc` 和 Vite build 退出码为 0。

- [ ] **Step 7: Commit frontend integration**

```bash
git add web/src/components/SubscriptionFeedsEditor.vue web/src/views/Subscriptions.vue web/src/api/index.ts
git commit -m "Manage multiple feeds per subscription"
```

## Task 10: 更新文档并执行完整验证

**Files:**
- Modify: `README.md`
- Modify: `docs/API.md`
- Modify: `docs/FANSUB_DESIGN.md`

- [ ] **Step 1: Update user and API documentation**

README 增加以下行为说明：一个订阅可添加多个平等 feed；每个 feed 手动配置偏移；第一个成功受理的同集资源下载，后到资源进入候选；新 feed 首次同步不自动下载历史缺集。

`docs/API.md` 写明 feed CRUD/preview 请求与响应、422/409/502 错误、旧单 RSS 更新在多 feed 订阅上的 409 行为，以及 JSON 2.0/OPML 扩展属性。`docs/FANSUB_DESIGN.md` 把单一 `subgroup_id` 定位更新为 feed 级字幕组入口，并链接正式多 feed 设计。

- [ ] **Step 2: Format changed Go files**

Run: `gofmt -w internal/model internal/pkg/utils internal/repository internal/service/subscription internal/service/subscriptionfeed internal/service/episode internal/service/scheduler internal/service/rss internal/service/backup internal/api/handler internal/api/router`

Expected: 完成且无错误。

- [ ] **Step 3: Run focused backend verification**

Run: `go test ./internal/pkg/database ./internal/pkg/utils ./internal/repository ./internal/service/subscription ./internal/service/subscriptionfeed ./internal/service/episode ./internal/service/scheduler ./internal/service/rss ./internal/service/backup ./internal/api/handler ./internal/api/router -count=1`

Expected: PASS；所有包 0 failures。

- [ ] **Step 4: Run full backend verification**

Run: `go test ./... -count=1`

Expected: PASS；所有包 0 failures。

- [ ] **Step 5: Run race-sensitive scheduler verification**

Run: `go test -race ./internal/service/scheduler ./internal/service/episode -count=1`

Expected: PASS；无 data race，同一相对集数只创建一个下载任务。

- [ ] **Step 6: Run frontend verification**

Run: `cd web && npm run test:episodes && npm run test:episode-ledger && npm run test:subscription-feeds && npm run build`

Expected: PASS；Node tests、`vue-tsc` 和 Vite build 均成功。

- [ ] **Step 7: Verify migration and compatibility scenarios manually**

使用临时 SQLite 数据库完成以下检查并记录结果：

```text
1. 旧单 RSS 订阅升级后只有一条 baseline_pending=false 的 feed。
2. A(offset=0) 第1集和 B(offset=100) 第101集只产生一个下载与一个后到候选。
3. 中途新增 B 时首次同步只建立 missing/候选，不下载历史内容。
4. A 拉取失败时 B 仍能下载新集，A/B 水位线互不覆盖。
5. 删除 B 后已有下载和候选仍显示来源快照。
6. 多 feed 订阅通过旧 rss_url 更新接口返回 409。
7. 备份恢复后两条 feed 都存在并重新进入 baseline_pending。
8. 无发布时间的 feed 完成基线后，第二轮不回灌历史；新增资源 key 能正常下载。
```

- [ ] **Step 8: Commit documentation**

```bash
git add README.md docs/API.md docs/FANSUB_DESIGN.md
git commit -m "Document multi-feed subscriptions"
```
