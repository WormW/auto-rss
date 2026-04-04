package scheduler

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/constants"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/pkg/utils"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/disk"
	"github.com/WormW/auto-rss/internal/service/downloader"
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
	qbClient         downloader.QBittorrentClient
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
	qbClient downloader.QBittorrentClient,
) Scheduler {
	return &scheduler{
		db:               db,
		cron:             cron.New(),
		subscriptionRepo: subscriptionRepo,
		downloadRepo:     downloadRepo,
		configRepo:       configRepo,
		rssCheckInterval: rssCheckInterval,
		rssParser:        rssParser,
		qbClient:         qbClient,
		smartFetchFilter: NewSmartFetchFilter(downloadRepo),
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
				"completed_at", subscriptions[idx].CompletedAt.Format("2006-01-02 15:04:05"))
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

			// 检查是否已存在相同 hash
			existing, _ := s.downloadRepo.GetByHash(item.TorrentHash)
			if existing != nil {
				continue
			}

			// 应用关键词过滤
			if !s.matchesFilter(&sub, item.Title) {
				continue
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
			offset := sub.EpisodeOffset
			relativeEpisode := item.Episode
			if offset > 0 {
				relativeEpisode = item.Episode - offset
				// 如果相对集数 <= 0，说明这集在偏移之前，跳过
				if relativeEpisode <= 0 {
					logger.Debug("Skipping episode before offset",
						"subscription", sub.Name,
						"episode", item.Episode,
						"offset", offset,
						"relative_episode", relativeEpisode)
					continue
				}
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

			// 检查同一订阅的同一集数是否已存在（考虑语言）
			shouldDownload := true
			skipReason := ""
			var replaceDownloadID uint = 0

			if item.Episode > 0 {
				// 初始化语言过滤器
				langFilter := NewLanguageFilter(s.downloadRepo)

				// 检查语言策略
				allowed, reason, replaceID := langFilter.CheckLanguageAllow(&sub, item.Episode, item.Language, item.Title)
				shouldDownload = allowed
				skipReason = reason
				replaceDownloadID = replaceID

				// 如果需要替换现有下载（更高版本），先在 qBittorrent 中删除旧种子
				// 数据库记录将在 processDownloadItem 的事务中删除
				if replaceDownloadID > 0 {
					existingEpisode, _ := s.downloadRepo.GetByID(replaceDownloadID)
					if existingEpisode != nil {
						logger.Info("Found newer version, replacing old download",
							"task_action", "replace_old_task",
							"subscription", sub.Name,
							"subscription_id", sub.ID,
							"download_id", existingEpisode.ID,
							"episode", item.Episode,
							"language", item.Language,
							"old_title", existingEpisode.Title,
							"new_title", item.Title,
							"trigger_context", "scheduler_rss_check")

						// 如果旧任务有 qBittorrent hash，尝试删除种子（数据库记录在 processDownloadItem 事务中处理）
						if existingEpisode.TorrentHash != "" {
							if err := s.qbClient.DeleteTorrent(existingEpisode.TorrentHash, true); err != nil {
								logger.Error("Failed to delete old torrent from qBittorrent",
									"task_action", "replace_old_task_delete_torrent",
									"subscription_id", sub.ID,
									"download_id", existingEpisode.ID,
									"episode", item.Episode,
									"torrent_hash_prefix", utils.HashPrefix(existingEpisode.TorrentHash),
									"trigger_context", "scheduler_rss_check",
									"error", err)
							}
						}
					}
				}
			}

			// 根据语言策略决定是否跳过
			if !shouldDownload {
				logger.Debug("Skipping download based on language policy",
					"subscription", sub.Name,
					"episode", item.Episode,
					"title", item.Title,
					"language", item.Language,
					"reason", skipReason)
				continue
			}

			// 使用事务方法处理下载创建
			_, err := s.processDownloadItem(&sub, &item, replaceDownloadID)
			if err != nil {
				// 错误已在 processDownloadItem 中记录
				continue
			}
		}

		// 使用事务更新订阅检查时间
		if err := s.updateSubscriptionCheckTime(&sub, maxPubTime); err != nil {
			logger.Error("Failed to update subscription check time",
				"subscription", sub.Name,
				"subscription_id", sub.ID,
				"error", err)
		}
	}

	logger.Info("RSS feed check completed")
}

// matchesFilter 检查标题是否匹配过滤条件
func (s *scheduler) matchesFilter(sub *model.Subscription, title string) bool {
	// 检查包含关键词
	if sub.FilterKeywords != "" {
		keywords := strings.Split(sub.FilterKeywords, ",")
		matched := false
		for _, keyword := range keywords {
			if strings.Contains(strings.ToLower(title), strings.ToLower(strings.TrimSpace(keyword))) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// 检查排除关键词
	if sub.ExcludeKeywords != "" {
		keywords := strings.Split(sub.ExcludeKeywords, ",")
		for _, keyword := range keywords {
			if strings.Contains(strings.ToLower(title), strings.ToLower(strings.TrimSpace(keyword))) {
				return false
			}
		}
	}

	return true
}

// processDownloadItem 处理单个下载条目（在事务中）
// 返回是否成功创建下载任务
func (s *scheduler) processDownloadItem(sub *model.Subscription, item *rss.RSSItem, replaceDownloadID uint) (bool, error) {
	// 检查是否因磁盘空间危险而暂停下载
	if disk.IsDownloadsPaused() {
		logger.Info("Skipping download creation because downloads are paused",
			"subscription", sub.Name,
			"title", item.Title)
		return false, nil
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
		Status:         "pending",
	}

	// 使用事务包装数据库操作
	err := s.db.Transaction(func(tx *gorm.DB) error {
		// 如果需要替换旧下载，先删除旧记录
		if replaceDownloadID > 0 {
			if err := tx.Delete(&model.Download{}, replaceDownloadID).Error; err != nil {
				return fmt.Errorf("failed to delete old download: %w", err)
			}
		}

		// 创建新下载记录
		if err := tx.Create(download).Error; err != nil {
			return fmt.Errorf("failed to create download: %w", err)
		}

		return nil
	})

	if err != nil {
		return false, err
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

	// 生成带番剧名的下载路径
	basePath := constants.DefaultDownloadPath
	if s.configRepo != nil {
		if downloadPathConfig, err := s.configRepo.Get("download_path"); err == nil && downloadPathConfig != nil && downloadPathConfig.Value != "" {
			basePath = downloadPathConfig.Value
		}
	}
	downloadPath := utils.GenerateDownloadPath(basePath, sub.Name)

	// 添加到 qBittorrent（在事务外，因为可能涉及网络操作）
	_, err = s.qbClient.AddTorrent(item.TorrentURL, downloadPath, "")
	if err != nil {
		logger.Error("Failed to add torrent to qBittorrent",
			"task_action", "add_torrent",
			"subscription_id", sub.ID,
			"download_id", download.ID,
			"episode", item.Episode,
			"title", item.Title,
			"download_path", downloadPath,
			"trigger_context", "scheduler_rss_check",
			"error", err)

		// 更新下载状态为失败
		download.Status = "failed"
		download.ErrorMessage = err.Error()
		s.downloadRepo.Update(download)
		return false, err
	}

	logger.Debug("Torrent added with path",
		"task_action", "add_torrent",
		"subscription", sub.Name,
		"subscription_id", sub.ID,
		"download_id", download.ID,
		"episode", item.Episode,
		"download_path", downloadPath,
		"trigger_context", "scheduler_rss_check")

	// 更新状态为 downloading
	download.Status = "downloading"
	if err := s.downloadRepo.Update(download); err != nil {
		logger.Error("Failed to update download status",
			"download_id", download.ID,
			"error", err)
	}

	return true, nil
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


