package scheduler

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	// Start 启动调度器
	Start() error
	// Stop 停止调度器
	Stop()
	// AddJob 添加定时任务
	AddJob(spec string, cmd func()) (cron.EntryID, error)
	// RunRSSCheckNow 手动触发一次 RSS 检查（异步）
	RunRSSCheckNow() error
}

type scheduler struct {
	db               *gorm.DB
	cron             *cron.Cron
	subscriptionRepo repository.SubscriptionRepository
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

	// 获取所有激活的订阅
	subscriptions, err := s.subscriptionRepo.GetActiveSubscriptions()
	if err != nil {
		logger.Error("Failed to get active subscriptions", "error", err)
		return
	}

	// 使用智能过滤器评估每个订阅
	s.smartFetchFilter.LoadConfigFromDB(s.configRepo)
	fetchStatuses, needsUpdateIndexes := s.smartFetchFilter.FilterSubscriptions(subscriptions)

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

	// 过滤出需要拉取的订阅
	var subscriptionsToFetch []model.Subscription
	for _, status := range fetchStatuses {
		if status.ShouldFetch {
			subscriptionsToFetch = append(subscriptionsToFetch, *status.Subscription)
		} else {
			logger.Debug("Skipping subscription based on smart fetch strategy",
				"subscription", status.Subscription.Name,
				"reason", status.FetchReason,
				"next_fetch_in", status.NextFetchInterval)
		}
	}

	logger.Info("Checking RSS feeds", "total", len(subscriptions), "will_fetch", len(subscriptionsToFetch))

	// 构建订阅ID到拉取状态的映射，用于日志
	fetchStatusMap := make(map[uint]*SubscriptionFetchStatus)
	for i := range fetchStatuses {
		if fetchStatuses[i].ShouldFetch {
			fetchStatusMap[fetchStatuses[i].Subscription.ID] = &fetchStatuses[i]
		}
	}

	for _, sub := range subscriptionsToFetch {
		// 解析 RSS Feed
		items, err := s.rssParser.FetchAndParse(sub.RssURL)
		if err != nil {
			logger.Error("Failed to parse RSS feed",
				"subscription", sub.Name,
				"url", sub.RssURL,
				"error", err)
			continue
		}

		// 获取该订阅的拉取原因
		fetchReason := ""
		if status, ok := fetchStatusMap[sub.ID]; ok {
			fetchReason = status.FetchReason
		}

		logger.Info("Parsed RSS feed",
			"subscription", sub.Name,
			"items", len(items),
			"fetch_reason", fetchReason)

		if sub.RSSBaselinePending {
			if err := s.reconcileRSSBaseline(&sub, items); err != nil {
				logger.Error("Failed to reconcile RSS source baseline",
					"subscription", sub.Name,
					"subscription_id", sub.ID,
					"error", err)
			}
			continue
		}

		// 首次启用时间水位线时，优先按“已存在下载记录”回推到对应的最新 pubDate，避免误跳过后续新集。
		if sub.LastRSSPubTime == nil {
			bootstrapFromExisting := false
			existingDownloads, err := s.downloadRepo.ListBySubscriptionID(sub.ID)
			if err == nil && len(existingDownloads) > 0 {
				existingHashes := make(map[string]struct{}, len(existingDownloads))
				for _, d := range existingDownloads {
					if d.TorrentHash != "" {
						existingHashes[d.TorrentHash] = struct{}{}
					}
				}

				for _, item := range items {
					if item.PubTime.IsZero() {
						continue
					}
					if _, ok := existingHashes[item.TorrentHash]; !ok {
						continue
					}
					if sub.LastRSSPubTime == nil || item.PubTime.After(*sub.LastRSSPubTime) {
						pubCopy := item.PubTime
						sub.LastRSSPubTime = &pubCopy
						bootstrapFromExisting = true
					}
				}

				if bootstrapFromExisting {
					now := time.Now()
					sub.LastCheckTime = &now
					s.subscriptionRepo.Update(&sub)
					logger.Info("Bootstrapped last_rss_pub_time from existing downloads",
						"subscription", sub.Name,
						"last_rss_pub_time", sub.LastRSSPubTime,
						"existing_downloads", len(existingDownloads))
				}
			}

			// 没有历史下载时，按当前 RSS 最新发布时间初始化，避免历史回灌。
			if sub.LastRSSPubTime == nil {
				for _, item := range items {
					if !item.PubTime.IsZero() {
						pubCopy := item.PubTime
						sub.LastRSSPubTime = &pubCopy
						now := time.Now()
						sub.LastCheckTime = &now
						s.subscriptionRepo.Update(&sub)
						logger.Info("Initialized last_rss_pub_time for subscription",
							"subscription", sub.Name,
							"last_rss_pub_time", sub.LastRSSPubTime)
						break
					}
				}
				if sub.LastRSSPubTime != nil {
					continue
				}
			}
		}

		maxPubTime := sub.LastRSSPubTime
		retryableProcessingFailure := false
		for _, item := range items {
			if !item.PubTime.IsZero() {
				if maxPubTime == nil || item.PubTime.After(*maxPubTime) {
					pubCopy := item.PubTime
					maxPubTime = &pubCopy
				}
				if sub.LastRSSPubTime != nil && !item.PubTime.After(*sub.LastRSSPubTime) {
					continue
				}
			}

			// 只处理订阅创建时间之后发布的条目（定时检查不收集历史种子）
			if !item.PubTime.IsZero() && item.PubTime.Before(sub.CreatedAt) {
				logger.Debug("Skipping item published before subscription creation",
					"subscription", sub.Name,
					"item_title", item.Title,
					"pub_time", item.PubTime,
					"created_at", sub.CreatedAt)
				continue
			}

			// 计算相对集数（考虑偏移）
			relativeEpisode := sub.RelativeEpisode(item.Episode)
			if relativeEpisode <= 0 {
				logger.Debug("Skipping episode before offset",
					"subscription", sub.Name,
					"episode", item.Episode,
					"offset", sub.EpisodeOffset,
					"relative_episode", relativeEpisode)
				continue
			}

			// 如果设置了总集数，只收集在范围内的
			if sub.TotalEpisodes > 0 && relativeEpisode > sub.TotalEpisodes {
				logger.Debug("Skipping episode beyond total",
					"subscription", sub.Name,
					"episode", item.Episode,
					"relative_episode", relativeEpisode,
					"total_episodes", sub.TotalEpisodes)
				continue
			}

			if s.episodeService == nil {
				logger.Error("Skipping RSS item because episode service is unavailable",
					"subscription_id", sub.ID,
					"episode", item.Episode)
				continue
			}

			if _, err := s.episodeService.ObserveRSSItem(&sub, relativeEpisode); err != nil {
				retryableProcessingFailure = true
				logger.Error("Failed to observe RSS episode",
					"subscription_id", sub.ID,
					"episode", item.Episode,
					"error", err)
				continue
			}

			// 被资源过滤器拒绝的条目也已经贡献 latest known episode。
			if !s.matchesFilter(&sub, item.Title) {
				continue
			}

			langFilter := NewLanguageFilter(s.downloadRepo)
			allowed, skipReason := langFilter.CheckLanguageAllow(&sub, item.Language)
			if !allowed {
				logger.Debug("Skipping download based on language policy",
					"subscription", sub.Name,
					"episode", item.Episode,
					"title", item.Title,
					"language", item.Language,
					"reason", skipReason)
				continue
			}

			preflight := s.downloadPreflight(&sub, &item)
			if preflight.skip {
				if preflight.retryable {
					retryableProcessingFailure = true
				}
				continue
			}

			resource := rssItemResource(&item)
			decision, err := s.episodeService.EvaluateRSSItem(context.Background(), &sub, episode.RSSResource{
				OriginalEpisode: item.Episode,
				RelativeEpisode: relativeEpisode,
				Resource:        resource,
				Fansub:          item.Fansub,
				Language:        string(item.Language),
				PubTime:         item.PubTime,
				SourceRSSURL:    sub.RssURL,
			}, false)
			if err != nil {
				retryableProcessingFailure = true
				logger.Error("Failed to evaluate RSS item against episode ledger",
					"subscription_id", sub.ID,
					"episode", item.Episode,
					"error", err)
				continue
			}
			if decision.Action != episode.DecisionDownload {
				continue
			}

			_, err = s.processDownloadItem(&sub, &item, decision.EpisodeID)
			if err != nil {
				retryableProcessingFailure = true
				// 错误已在 processDownloadItem 中记录
				continue
			}
		}

		// 使用事务更新订阅检查时间
		watermark := maxPubTime
		if retryableProcessingFailure {
			watermark = sub.LastRSSPubTime
		}
		if err := s.updateSubscriptionCheckTime(&sub, watermark); err != nil {
			logger.Error("Failed to update subscription check time",
				"subscription", sub.Name,
				"subscription_id", sub.ID,
				"error", err)
		}
	}

	logger.Info("RSS feed check completed")
}

func (s *scheduler) reconcileRSSBaseline(sub *model.Subscription, items []rss.RSSItem) error {
	if s.episodeService == nil {
		return errors.New("episode service is required")
	}

	return s.db.Transaction(func(tx *gorm.DB) error {
		var current model.Subscription
		err := tx.Where(
			"id = ? AND rss_url = ? AND rss_baseline_pending = ? AND updated_at = ?",
			sub.ID,
			sub.RssURL,
			true,
			sub.UpdatedAt,
		).First(&current).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("rss source changed while baseline was running")
		}
		if err != nil {
			return err
		}

		txEpisodeService := episode.NewService(repository.NewEpisodeRepository(tx))
		var maxPubTime *time.Time
		for _, item := range items {
			relativeEpisode := sub.RelativeEpisode(item.Episode)
			if relativeEpisode <= 0 {
				continue
			}
			if sub.TotalEpisodes > 0 && relativeEpisode > sub.TotalEpisodes {
				continue
			}
			if !item.PubTime.IsZero() && (maxPubTime == nil || item.PubTime.After(*maxPubTime)) {
				pubCopy := item.PubTime
				maxPubTime = &pubCopy
			}
			_, err := txEpisodeService.EvaluateRSSItem(context.Background(), sub, episode.RSSResource{
				OriginalEpisode: item.Episode,
				RelativeEpisode: relativeEpisode,
				Resource:        rssItemResource(&item),
				Fansub:          item.Fansub,
				Language:        string(item.Language),
				PubTime:         item.PubTime,
				SourceRSSURL:    sub.RssURL,
			}, true)
			if err != nil {
				return err
			}
		}

		now := time.Now()
		updates := map[string]any{
			"last_rss_pub_time":    maxPubTime,
			"last_check_time":      now,
			"rss_baseline_pending": false,
		}
		result := tx.Model(&model.Subscription{}).
			Where(
				"id = ? AND rss_url = ? AND rss_baseline_pending = ? AND updated_at = ?",
				sub.ID,
				sub.RssURL,
				true,
				sub.UpdatedAt,
			).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("rss source changed while baseline was running")
		}
		return nil
	})
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
func (s *scheduler) processDownloadItem(sub *model.Subscription, item *rss.RSSItem, episodeID uint) (bool, error) {
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
		SubscriptionID: sub.ID,
		Title:          item.Title,
		Episode:        item.Episode,
		Fansub:         item.Fansub,
		Language:       string(item.Language),
		TorrentURL:     item.TorrentURL,
		TorrentHash:    item.TorrentHash,
		Status:         model.DownloadStatusPending,
		Purpose:        model.DownloadPurposeNormal,
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

// updateSubscriptionCheckTime 更新订阅的检查时间（带事务）
func (s *scheduler) updateSubscriptionCheckTime(sub *model.Subscription, maxPubTime *time.Time) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		sub.LastCheckTime = &now
		if maxPubTime != nil {
			pubCopy := *maxPubTime
			sub.LastRSSPubTime = &pubCopy
		}
		return tx.Save(sub).Error
	})
}

// Stop 停止调度器
func (s *scheduler) Stop() {
	s.cron.Stop()
}

// AddJob 添加定时任务
func (s *scheduler) AddJob(spec string, cmd func()) (cron.EntryID, error) {
	return s.cron.AddFunc(spec, cmd)
}
