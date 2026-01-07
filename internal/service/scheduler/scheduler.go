package scheduler

import (
	"fmt"
	"strings"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/pkg/utils"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/downloader"
	"github.com/WormW/auto-rss/internal/service/rss"
	"github.com/robfig/cron/v3"
)

// Scheduler 调度器接口
type Scheduler interface {
	// Start 启动调度器
	Start() error
	// Stop 停止调度器
	Stop()
	// AddJob 添加定时任务
	AddJob(spec string, cmd func()) (cron.EntryID, error)
}

type scheduler struct {
	cron               *cron.Cron
	subscriptionRepo   repository.SubscriptionRepository
	downloadRepo       repository.DownloadRepository
	configRepo         repository.ConfigRepository
	rssCheckInterval   string
	rssParser          rss.Parser
	qbClient           downloader.QBittorrentClient
}

// NewScheduler 创建调度器实例
func NewScheduler(
	subscriptionRepo repository.SubscriptionRepository,
	downloadRepo repository.DownloadRepository,
	configRepo repository.ConfigRepository,
	rssCheckInterval string,
	rssParser rss.Parser,
	qbClient downloader.QBittorrentClient,
) Scheduler {
	return &scheduler{
		cron:             cron.New(),
		subscriptionRepo: subscriptionRepo,
		downloadRepo:     downloadRepo,
		configRepo:       configRepo,
		rssCheckInterval: rssCheckInterval,
		rssParser:        rssParser,
		qbClient:         qbClient,
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

	// 添加下载状态检查任务（每 5 分钟）
	_, err = s.AddJob("@every 5m", s.checkDownloadStatus)
	if err != nil {
		return fmt.Errorf("failed to add download status check job: %w", err)
	}
	logger.Info("Download status check job added", "interval", "@every 5m")

	// 启动调度器
	s.cron.Start()
	logger.Info("Scheduler started")

	return nil
}

// checkRSSFeeds RSS 检查任务
func (s *scheduler) checkRSSFeeds() {
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

	logger.Info("Checking RSS feeds", "count", len(subscriptions))

	for _, sub := range subscriptions {
		// 解析 RSS Feed
		items, err := s.rssParser.FetchAndParse(sub.RssURL)
		if err != nil {
			logger.Error("Failed to parse RSS feed",
				"subscription", sub.Name,
				"url", sub.RssURL,
				"error", err)
			continue
		}

		logger.Info("Parsed RSS feed",
			"subscription", sub.Name,
			"items", len(items))

		// 处理每个 RSS 条目
		for _, item := range items {
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

			// 检查同一订阅的同一集数是否已存在
			if item.Episode > 0 {
				existingEpisode, _ := s.downloadRepo.GetBySubscriptionAndEpisode(sub.ID, item.Episode)
				if existingEpisode != nil {
					// 删除旧的下载任务（通常是非V2版本）
					logger.Info("Found duplicate episode, removing old version",
						"subscription", sub.Name,
						"episode", item.Episode,
						"old_title", existingEpisode.Title,
						"new_title", item.Title)

					// 如果旧任务有 qBittorrent hash，尝试删除种子
					if existingEpisode.TorrentHash != "" {
						if err := s.qbClient.DeleteTorrent(existingEpisode.TorrentHash, true); err != nil {
							logger.Error("Failed to delete old torrent from qBittorrent",
								"hash", existingEpisode.TorrentHash,
								"error", err)
						}
					}

					// 删除数据库记录
					if err := s.downloadRepo.Delete(existingEpisode.ID); err != nil {
						logger.Error("Failed to delete old download record",
							"id", existingEpisode.ID,
							"error", err)
					}
				}
			}

			// 创建下载任务
			download := &model.Download{
				SubscriptionID: sub.ID,
				Title:          item.Title,
				Episode:        item.Episode,
				Fansub:         item.Fansub,
				TorrentURL:     item.TorrentURL,
				TorrentHash:    item.TorrentHash,
				Status:         "pending",
			}

			if err := s.downloadRepo.Create(download); err != nil {
				logger.Error("Failed to create download",
					"title", item.Title,
					"error", err)
				continue
			}

			logger.Info("Download task created",
				"subscription", sub.Name,
				"title", item.Title,
				"episode", item.Episode,
				"fansub", item.Fansub)

			// 生成带番剧名的下载路径
			// 使用系统配置的下载路径，而不是订阅级别的路径
			basePath := "/downloads" // 默认值
			if s.configRepo != nil {
				if downloadPathConfig, err := s.configRepo.Get("download_path"); err == nil && downloadPathConfig != nil && downloadPathConfig.Value != "" {
					basePath = downloadPathConfig.Value
				}
			}
			downloadPath := utils.GenerateDownloadPath(basePath, sub.Name)

			// 添加到 qBittorrent
			_, err = s.qbClient.AddTorrent(item.TorrentURL, downloadPath, "")
			if err != nil {
				logger.Error("Failed to add torrent to qBittorrent",
					"title", item.Title,
					"download_path", downloadPath,
					"error", err)
				download.Status = "failed"
				download.ErrorMessage = err.Error()
				s.downloadRepo.Update(download)
				continue
			}

			logger.Debug("Torrent added with path",
				"subscription", sub.Name,
				"episode", item.Episode,
				"download_path", downloadPath)

			download.Status = "downloading"
			s.downloadRepo.Update(download)
		}

		// 更新最后检查时间
		now := time.Now()
		sub.LastCheckTime = &now
		s.subscriptionRepo.Update(&sub)
	}

	logger.Info("RSS feed check completed")
}

// checkDownloadStatus 下载状态检查任务
func (s *scheduler) checkDownloadStatus() {
	logger.Info("Checking download status")

	// 获取正在下载的任务
	downloads, _, err := s.downloadRepo.List(0, 1000, "downloading")
	if err != nil {
		logger.Error("Failed to get downloading tasks", "error", err)
		return
	}

	for _, download := range downloads {
		// 查询 qBittorrent 状态
		info, err := s.qbClient.GetTorrentInfo(download.TorrentHash)
		if err != nil {
			logger.Error("Failed to get torrent info",
				"hash", download.TorrentHash,
				"error", err)
			continue
		}

		// 更新下载进度
		if info.State == "completed" || info.Progress >= 1.0 {
			download.Status = "completed"
			now := time.Now()
			download.DownloadedAt = &now
			download.FilePath = info.SavePath

			if err := s.downloadRepo.Update(&download); err != nil {
				logger.Error("Failed to update download status",
					"id", download.ID,
					"error", err)
			}

			logger.Info("Download completed",
				"title", download.Title,
				"path", info.SavePath)
		}
	}

	logger.Info("Download status check completed")
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

// Stop 停止调度器
func (s *scheduler) Stop() {
	s.cron.Stop()
}

// AddJob 添加定时任务
func (s *scheduler) AddJob(spec string, cmd func()) (cron.EntryID, error) {
	return s.cron.AddFunc(spec, cmd)
}
