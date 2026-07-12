package scheduler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/disk"
	"github.com/WormW/auto-rss/internal/service/downloader"
	"github.com/WormW/auto-rss/internal/service/episode"
	"github.com/WormW/auto-rss/internal/service/rss"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

// Scheduler 调度器接口
type Scheduler interface {
	SubscriptionCollector
	// Start 启动调度器
	Start() error
	// Stop 停止调度器
	Stop()
	// AddJob 添加定时任务
	AddJob(spec string, cmd func()) (cron.EntryID, error)
	// RunRSSCheckNow 手动触发一次 RSS 检查（异步）
	RunRSSCheckNow() error
}

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

const maxConcurrentFeedChecks = 4

type feedFetchResult struct {
	Feed      model.SubscriptionFeed
	Items     []rss.RSSItem
	CheckedAt time.Time
	Err       error
}

type scheduler struct {
	db               *gorm.DB
	cron             *cron.Cron
	subscriptionRepo repository.SubscriptionRepository
	feedRepo         repository.SubscriptionFeedRepository
	downloadRepo     repository.DownloadRepository
	configRepo       repository.ConfigRepository
	rssCheckInterval string
	rssParser        rss.Parser
	episodeService   *episode.Service
	downloadsPaused  func() bool
	rssCheckRunning  atomic.Bool
	smartFetchFilter *SmartFetchFilter // 智能拉取过滤器
}

// NewScheduler 创建调度器实例
func NewScheduler(
	db *gorm.DB,
	subscriptionRepo repository.SubscriptionRepository,
	feedRepo repository.SubscriptionFeedRepository,
	downloadRepo repository.DownloadRepository,
	configRepo repository.ConfigRepository,
	rssCheckInterval string,
	rssParser rss.Parser,
	_ downloader.QBittorrentClient, // 保留构造参数兼容性；qB 首次 Add 由 DownloadMonitor 独占。
	episodeService *episode.Service,
) Scheduler {
	return &scheduler{
		db:               db,
		cron:             cron.New(),
		subscriptionRepo: subscriptionRepo,
		feedRepo:         feedRepo,
		downloadRepo:     downloadRepo,
		configRepo:       configRepo,
		rssCheckInterval: rssCheckInterval,
		rssParser:        rssParser,
		episodeService:   episodeService,
		downloadsPaused:  disk.IsDownloadsPaused,
		smartFetchFilter: NewSmartFetchFilter(downloadRepo, repository.NewEpisodeRepository(db)),
	}
}

// Start 启动调度器
func (s *scheduler) Start() error {
	// 添加 RSS 检查定时任务
	// 解析间隔字符串（例如 "30m"）
	duration, err := time.ParseDuration(s.rssCheckInterval)
	if err != nil {
		logger.Error("Invalid RSS check interval", "interval", s.rssCheckInterval, "error", err)
		duration = 30 * time.Minute // 默认 30 分钟
	}

	// 转换为 cron 表达式（每 N 分钟）
	minutes := int(duration.Minutes())
	if minutes < 1 {
		minutes = 30
	}
	cronSpec := fmt.Sprintf("@every %dm", minutes)

	// 添加 RSS 检查任务
	_, err = s.AddJob(cronSpec, s.checkRSSFeeds)
	if err != nil {
		return fmt.Errorf("failed to add RSS check job: %w", err)
	}
	logger.Info("RSS check job added", "interval", cronSpec)

	// 启动调度器
	s.cron.Start()
	logger.Info("Scheduler started")

	return nil
}

// RunRSSCheckNow 手动触发一次 RSS 检查（异步）
func (s *scheduler) RunRSSCheckNow() error {
	if s.rssCheckRunning.Load() {
		return fmt.Errorf("rss check is already running")
	}
	go s.checkRSSFeeds()
	return nil
}

// checkRSSFeeds RSS 检查任务
func (s *scheduler) checkRSSFeeds() {
	if !s.rssCheckRunning.CompareAndSwap(false, true) {
		logger.Warn("RSS feed check skipped because another run is still active")
		return
	}
	defer s.rssCheckRunning.Store(false)

	logger.Info("Starting RSS feed check")

	// 设置代理
	if s.configRepo != nil {
		proxyConfig, err := s.configRepo.Get("system_proxy")
		if err == nil && proxyConfig != nil && proxyConfig.Value != "" {
			if err := s.rssParser.SetProxy(proxyConfig.Value); err != nil {
				logger.Warn("Failed to set proxy for RSS parser", "error", err)
			} else {
				logger.Debug("Proxy set for RSS parser", "proxy", proxyConfig.Value)
			}
		}
	}

	// 获取所有激活的订阅及其启用 feed。
	subscriptions, err := s.subscriptionRepo.GetActiveSubscriptions()
	if err != nil {
		logger.Error("Failed to get active subscriptions", "error", err)
		return
	}

	subscriptionIDs := make([]uint, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		subscriptionIDs = append(subscriptionIDs, subscription.ID)
	}
	feeds, err := s.feedRepo.ListEnabledBySubscriptionIDs(subscriptionIDs)
	if err != nil {
		logger.Error("Failed to list enabled subscription feeds", "error", err)
		return
	}
	feedsBySubscription := make(map[uint][]model.SubscriptionFeed)
	pendingFeeds := make(map[uint]bool)
	for _, feed := range feeds {
		feedsBySubscription[feed.SubscriptionID] = append(feedsBySubscription[feed.SubscriptionID], feed)
		if feed.BaselinePending {
			pendingFeeds[feed.SubscriptionID] = true
		}
	}

	// 使用智能过滤器评估每个订阅。
	s.smartFetchFilter.LoadConfigFromDB(s.configRepo)
	fetchStatuses, needsUpdateIndexes := s.smartFetchFilter.FilterSubscriptions(subscriptions, pendingFeeds)

	// 保存需要更新的订阅（如刚完结的订阅需要更新 CompletedAt）
	for _, idx := range needsUpdateIndexes {
		if err := s.subscriptionRepo.Update(&subscriptions[idx]); err != nil {
			logger.Error("Failed to update subscription completion status",
				"subscription", subscriptions[idx].Name,
				"error", err)
		} else {
			logger.Info("Subscription completion status saved to database",
				"subscription", subscriptions[idx].Name,
				"completed_at", completedAtLogValue(subscriptions[idx].CompletedAt))
		}
	}

	summary := s.smartFetchFilter.GetFetchSummary(fetchStatuses)
	logger.Info("Smart fetch evaluation completed",
		"total", summary["total"],
		"should_fetch", summary["should_fetch"],
		"should_skip", summary["should_skip"],
		"in_window", summary["in_window"],
		"completed", summary["completed"])

	if len(summary["with_missing"].([]string)) > 0 {
		logger.Info("Subscriptions with missing episodes",
			"list", summary["with_missing"].([]string))
	}

	for _, status := range fetchStatuses {
		if !status.ShouldFetch {
			logger.Debug("Skipping subscription based on smart fetch strategy",
				"subscription", status.Subscription.Name,
				"reason", status.FetchReason,
				"next_fetch_in", status.NextFetchInterval)
			continue
		}
		subscriptionFeeds := feedsBySubscription[status.Subscription.ID]
		if len(subscriptionFeeds) == 0 {
			logger.Debug("Skipping subscription without enabled feeds",
				"subscription_id", status.Subscription.ID,
				"subscription", status.Subscription.Name)
			continue
		}
		if _, err := s.collectFeeds(context.Background(), status.Subscription, subscriptionFeeds); err != nil {
			logger.Error("Failed to collect subscription feeds",
				"subscription_id", status.Subscription.ID,
				"subscription", status.Subscription.Name,
				"error", err)
		}
	}

	logger.Info("RSS feed check completed")
}

func (s *scheduler) CollectSubscription(ctx context.Context, subscriptionID uint) (CollectSummary, error) {
	subscription, err := s.subscriptionRepo.GetByID(subscriptionID)
	if err != nil {
		return CollectSummary{}, err
	}
	feeds, err := s.feedRepo.ListBySubscription(subscriptionID)
	if err != nil {
		return CollectSummary{}, err
	}
	enabled := feeds[:0]
	for _, feed := range feeds {
		if feed.Enabled {
			enabled = append(enabled, feed)
		}
	}
	return s.collectFeeds(ctx, subscription, enabled)
}

func (s *scheduler) collectFeeds(
	ctx context.Context,
	subscription *model.Subscription,
	feeds []model.SubscriptionFeed,
) (CollectSummary, error) {
	var summary CollectSummary
	if len(feeds) == 0 {
		return summary, nil
	}

	results := make(chan feedFetchResult, len(feeds))
	semaphore := make(chan struct{}, maxConcurrentFeedChecks)
	var waitGroup sync.WaitGroup
	for _, feed := range feeds {
		feed := feed
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			items, err := s.rssParser.FetchAndParse(feed.RSSURL)
			results <- feedFetchResult{Feed: feed, Items: items, CheckedAt: time.Now(), Err: err}
		}()
	}
	go func() {
		waitGroup.Wait()
		close(results)
	}()

	var latestCheck time.Time
	for result := range results {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		summary.FeedsChecked++
		if result.CheckedAt.After(latestCheck) {
			latestCheck = result.CheckedAt
		}
		if result.Err != nil {
			summary.FeedErrors++
			_ = s.feedRepo.UpdateCheckFailure(result.Feed.ID, result.CheckedAt, result.Err.Error())
			continue
		}
		summary.ItemsScanned += len(result.Items)
		maxPubTime, itemSummary, err := s.processFetchedFeedItemsWithSummary(ctx, subscription, &result.Feed, result.Items)
		if err != nil {
			summary.FeedErrors++
			_ = s.feedRepo.UpdateCheckFailure(result.Feed.ID, result.CheckedAt, err.Error())
			continue
		}
		summary.DownloadsCreated += itemSummary.DownloadsCreated
		summary.CandidatesCreated += itemSummary.CandidatesCreated
		if err := s.feedRepo.UpdateCheckSuccess(
			result.Feed.ID,
			result.CheckedAt,
			maxPubTime,
			result.Feed.BaselinePending,
		); err != nil {
			summary.FeedErrors++
			continue
		}
	}
	if !latestCheck.IsZero() {
		if err := s.episodeService.RefreshSubscriptionProgress(subscription.ID); err != nil {
			return summary, err
		}
		if err := s.db.Model(&model.Subscription{}).Where("id = ?", subscription.ID).
			Update("last_check_time", latestCheck).Error; err != nil {
			return summary, err
		}
	}
	return summary, nil
}

func (s *scheduler) processFetchedFeedItems(
	ctx context.Context,
	subscription *model.Subscription,
	feed *model.SubscriptionFeed,
	items []rss.RSSItem,
) (*time.Time, error) {
	maxPubTime, _, err := s.processFetchedFeedItemsWithSummary(ctx, subscription, feed, items)
	return maxPubTime, err
}

func (s *scheduler) processFetchedFeedItemsWithSummary(
	ctx context.Context,
	subscription *model.Subscription,
	feed *model.SubscriptionFeed,
	items []rss.RSSItem,
) (*time.Time, CollectSummary, error) {
	var maxPubTime *time.Time
	var summary CollectSummary
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return nil, summary, err
		}
		if !item.PubTime.IsZero() && (maxPubTime == nil || item.PubTime.After(*maxPubTime)) {
			pubTime := item.PubTime
			maxPubTime = &pubTime
		}

		relativeEpisode := feed.RelativeEpisode(item.Episode)
		resource := episode.RSSResource{
			OriginalEpisode:     item.Episode,
			RelativeEpisode:     relativeEpisode,
			SubscriptionFeedID:  feed.ID,
			SourceFeedName:      feed.Name,
			SourceEpisodeOffset: feed.EpisodeOffset,
			Resource:            rssItemResource(&item),
			Fansub:              preferredFansub(item.Fansub, feed.Fansub),
			Language:            string(item.Language),
			PubTime:             item.PubTime,
			SourceRSSURL:        feed.RSSURL,
		}
		resourceKey := episode.ResourceKey(resource.Resource)
		if resourceKey == "" {
			logger.Warn("Skipping feed item without stable resource identity",
				"subscription_id", subscription.ID,
				"subscription_feed_id", feed.ID,
				"title", item.Title)
			continue
		}
		if item.PubTime.IsZero() {
			seen, err := s.feedRepo.HasSeenItem(feed.ID, resourceKey)
			if err != nil {
				return nil, summary, err
			}
			if seen {
				continue
			}
		} else if !feed.BaselinePending && feed.LastRSSPubTime != nil && !item.PubTime.After(*feed.LastRSSPubTime) {
			continue
		}

		if relativeEpisode <= 0 || (subscription.TotalEpisodes > 0 && relativeEpisode > subscription.TotalEpisodes) {
			if err := s.feedRepo.MarkSeenItem(feed.ID, resourceKey, item.Episode, time.Now()); err != nil {
				return nil, summary, err
			}
			continue
		}
		if feed.BaselinePending {
			decision, err := s.episodeService.EvaluateRSSItem(ctx, subscription, resource, true)
			if err != nil {
				return nil, summary, err
			}
			if decision.Action == episode.DecisionCandidate {
				summary.CandidatesCreated++
			}
			if err := s.feedRepo.MarkSeenItem(feed.ID, resourceKey, item.Episode, time.Now()); err != nil {
				return nil, summary, err
			}
			continue
		}

		if _, err := s.episodeService.ObserveRSSItem(subscription, relativeEpisode); err != nil {
			return nil, summary, err
		}
		if !s.matchesFilter(subscription, item.Title) {
			if err := s.feedRepo.MarkSeenItem(feed.ID, resourceKey, item.Episode, time.Now()); err != nil {
				return nil, summary, err
			}
			continue
		}
		allowed, _ := NewLanguageFilter(s.downloadRepo).CheckLanguageAllow(subscription, item.Language)
		if !allowed {
			if err := s.feedRepo.MarkSeenItem(feed.ID, resourceKey, item.Episode, time.Now()); err != nil {
				return nil, summary, err
			}
			continue
		}
		preflight := s.downloadPreflight(subscription, &item)
		if preflight.skip {
			if preflight.retryable {
				return nil, summary, errors.New(preflight.reason)
			}
			if err := s.feedRepo.MarkSeenItem(feed.ID, resourceKey, item.Episode, time.Now()); err != nil {
				return nil, summary, err
			}
			continue
		}
		if strings.TrimSpace(item.TorrentURL) == "" && strings.TrimSpace(item.TorrentHash) != "" {
			if err := s.feedRepo.MarkSeenItem(feed.ID, resourceKey, item.Episode, time.Now()); err != nil {
				return nil, summary, err
			}
			continue
		}

		decision, err := s.episodeService.EvaluateRSSItem(ctx, subscription, resource, false)
		if err != nil {
			return nil, summary, err
		}
		switch decision.Action {
		case episode.DecisionDownload:
			feedID := feed.ID
			downloadItem := item
			downloadItem.Fansub = preferredFansub(item.Fansub, feed.Fansub)
			created, err := s.processDownloadItem(subscription, &downloadItem, decision.EpisodeID, &feedID)
			if err != nil {
				return nil, summary, err
			}
			if created {
				summary.DownloadsCreated++
			}
		case episode.DecisionCandidate:
			summary.CandidatesCreated++
		}
		if err := s.feedRepo.MarkSeenItem(feed.ID, resourceKey, item.Episode, time.Now()); err != nil {
			return nil, summary, err
		}
	}
	return maxPubTime, summary, nil
}

func preferredFansub(itemFansub, configuredFansub string) string {
	if fansub := strings.TrimSpace(itemFansub); fansub != "" {
		return fansub
	}
	return strings.TrimSpace(configuredFansub)
}

func completedAtLogValue(completedAt *time.Time) any {
	if completedAt == nil {
		return nil
	}
	return completedAt.Format("2006-01-02 15:04:05")
}

// matchesFilter 检查标题是否匹配过滤条件
func (s *scheduler) matchesFilter(sub *model.Subscription, title string) bool {
	titleLower := strings.ToLower(title)
	var include []string
	var exclude []string

	// 检查包含关键词
	if sub.FilterKeywords != "" {
		for _, keyword := range splitSchedulerRuleTokens(sub.FilterKeywords) {
			if trimmed := strings.TrimSpace(keyword); trimmed != "" {
				include = append(include, trimmed)
			}
		}
	}

	// 检查排除关键词
	if sub.ExcludeKeywords != "" {
		for _, keyword := range splitSchedulerRuleTokens(sub.ExcludeKeywords) {
			if trimmed := strings.TrimSpace(keyword); trimmed != "" {
				exclude = append(exclude, trimmed)
			}
		}
	}

	for _, token := range splitSchedulerRuleTokens(sub.FilterRules) {
		keyword, excluded := parseSchedulerRuleToken(token)
		if keyword == "" {
			continue
		}
		if excluded {
			exclude = append(exclude, keyword)
		} else {
			include = append(include, keyword)
		}
	}

	for _, keyword := range exclude {
		if strings.Contains(titleLower, strings.ToLower(keyword)) {
			return false
		}
	}

	if len(include) == 0 {
		return true
	}

	for _, keyword := range include {
		if strings.Contains(titleLower, strings.ToLower(keyword)) {
			return true
		}
	}

	return false
}

func splitSchedulerRuleTokens(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == ';' || r == '，' || r == '；'
	})
}

func parseSchedulerRuleToken(token string) (string, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", false
	}
	lower := strings.ToLower(token)
	for _, prefix := range []string{"exclude:", "exclude=", "排除:", "排除=", "!", "-"} {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(token[len(prefix):]), true
		}
	}
	for _, prefix := range []string{"include:", "include=", "包含:", "包含=", "+"} {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(token[len(prefix):]), false
		}
	}
	return token, false
}

// processDownloadItem 处理单个下载条目（在事务中）
// 返回是否成功创建下载任务
func (s *scheduler) processDownloadItem(sub *model.Subscription, item *rss.RSSItem, episodeID uint, subscriptionFeedID *uint) (bool, error) {
	resource := rssItemResource(item)
	if s.episodeService == nil {
		return false, errors.New("episode service is required")
	}
	releaseClaim := func(cause error) error {
		if err := s.episodeService.ReleaseDownloadClaim(episodeID, resource); err != nil {
			return errors.Join(cause, fmt.Errorf("failed to release episode download claim: %w", err))
		}
		return cause
	}

	// 检查是否因磁盘空间危险而暂停下载
	if s.areDownloadsPaused() {
		logger.Info("Skipping download creation because downloads are paused",
			"subscription", sub.Name,
			"title", item.Title)
		return false, releaseClaim(errDownloadsPaused)
	}

	minSizeBytes := minTorrentSizeBytes(s.configRepo)
	if shouldSkipSmallTorrent(item, minSizeBytes) {
		s.logSmallTorrentSkip(sub, item, minSizeBytes)
		return false, releaseClaim(nil)
	}

	// 创建下载任务
	download := &model.Download{
		SubscriptionID:     sub.ID,
		SubscriptionFeedID: subscriptionFeedID,
		Title:              item.Title,
		Episode:            item.Episode,
		Fansub:             item.Fansub,
		Language:           string(item.Language),
		TorrentURL:         item.TorrentURL,
		TorrentHash:        item.TorrentHash,
		Status:             model.DownloadStatusPending,
		Purpose:            model.DownloadPurposeNormal,
	}

	// 使用事务包装数据库操作
	err := s.db.Transaction(func(tx *gorm.DB) error {
		// 创建新下载记录
		if err := tx.Create(download).Error; err != nil {
			return fmt.Errorf("failed to create download: %w", err)
		}
		if err := s.episodeService.AttachDownloadInTx(tx, episodeID, download.ID); err != nil {
			return fmt.Errorf("failed to attach download to episode: %w", err)
		}
		return nil
	})

	if err != nil {
		return false, releaseClaim(err)
	}

	logger.Info("Download task created",
		"task_action", "create_download_task",
		"subscription", sub.Name,
		"subscription_id", sub.ID,
		"download_id", download.ID,
		"title", item.Title,
		"episode", item.Episode,
		"fansub", item.Fansub,
		"language", item.Language,
		"trigger_context", "scheduler_rss_check")
	return true, nil
}

var errDownloadsPaused = errors.New("downloads are paused")

func (s *scheduler) areDownloadsPaused() bool {
	if s.downloadsPaused == nil {
		return disk.IsDownloadsPaused()
	}
	return s.downloadsPaused()
}

type downloadPreflightResult struct {
	skip      bool
	retryable bool
	reason    string
}

func (s *scheduler) downloadPreflight(sub *model.Subscription, item *rss.RSSItem) downloadPreflightResult {
	if s.areDownloadsPaused() {
		logger.Info("Skipping download creation because downloads are paused",
			"subscription", sub.Name,
			"title", item.Title)
		return downloadPreflightResult{skip: true, retryable: true, reason: "downloads_paused"}
	}
	minSizeBytes := minTorrentSizeBytes(s.configRepo)
	if shouldSkipSmallTorrent(item, minSizeBytes) {
		s.logSmallTorrentSkip(sub, item, minSizeBytes)
		return downloadPreflightResult{skip: true, reason: "torrent_below_minimum_size"}
	}
	return downloadPreflightResult{}
}

func rssItemResource(item *rss.RSSItem) model.EpisodeResource {
	return model.EpisodeResource{Hash: item.TorrentHash, URL: item.TorrentURL, Title: item.Title}
}

func (s *scheduler) logSmallTorrentSkip(sub *model.Subscription, item *rss.RSSItem, minSizeBytes int64) {
	logger.Info("Skipping torrent below minimum size threshold",
		"task_action", "skip_small_torrent",
		"subscription", sub.Name,
		"subscription_id", sub.ID,
		"title", item.Title,
		"episode", item.Episode,
		"torrent_url", item.TorrentURL,
		"reason", smallTorrentSkipMessage(item, minSizeBytes),
		"size_bytes", item.SizeBytes,
		"min_size_bytes", minSizeBytes,
		"trigger_context", "scheduler_rss_check")
}

// Stop 停止调度器
func (s *scheduler) Stop() {
	s.cron.Stop()
}

// AddJob 添加定时任务
func (s *scheduler) AddJob(spec string, cmd func()) (cron.EntryID, error) {
	return s.cron.AddFunc(spec, cmd)
}
