package downloader

import (
	"fmt"
	"strings"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/constants"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/pkg/utils"
	"github.com/WormW/auto-rss/internal/repository"
	"gorm.io/gorm"
)

const (
	// AutoRssCategory qBittorrent分类名称
	AutoRssCategory = "AutoRss"

	// 下载状态映射
	StateDownloading = "downloading"
	StateCompleted   = "uploading"
	StatePaused      = "pausedDL"
	StateError       = "error"

	// ReconcileGracePeriod 避免把刚创建/刚入队的任务误判为丢失
	ReconcileGracePeriod = 10 * time.Minute
)

// NotificationService 通知服务接口（避免循环导入）
type NotificationService interface {
	Send(payload model.NotificationPayload)
}

// DownloadMonitor 下载监控服务
type DownloadMonitor struct {
	db               *gorm.DB
	qbClient         QBittorrentClient
	downloadRepo     repository.DownloadRepository
	subscriptionRepo repository.SubscriptionRepository
	configRepo       repository.ConfigRepository
	renameService    *RenameService
	retryService     *RetryService
	notificationSvc  NotificationService
	ticker           *time.Ticker
	stopChan         chan struct{}
	// New service interfaces
	statusSync        StatusSync
	completionHandler CompletionHandler
}

// NewDownloadMonitor 创建下载监控服务
func NewDownloadMonitor(
	db *gorm.DB,
	qbClient QBittorrentClient,
	downloadRepo repository.DownloadRepository,
	subscriptionRepo repository.SubscriptionRepository,
	configRepo repository.ConfigRepository,
	renameTemplate string,
) *DownloadMonitor {
	retrySvc := NewRetryService(downloadRepo)
	renameSvc := NewRenameService(renameTemplate)

	return &DownloadMonitor{
		db:               db,
		qbClient:         qbClient,
		downloadRepo:     downloadRepo,
		subscriptionRepo: subscriptionRepo,
		configRepo:       configRepo,
		retryService:     retrySvc,
		renameService:    renameSvc,
		stopChan:         make(chan struct{}),
	}
}

// SetNotificationService 设置通知服务
func (m *DownloadMonitor) SetNotificationService(svc NotificationService) {
	m.notificationSvc = svc
	// Initialize services that need notification service
	m.statusSync = NewStatusSync(m.downloadRepo, svc)
	m.completionHandler = NewCompletionHandler(m.subscriptionRepo, m.downloadRepo, svc, m.renameService, m.qbClient, m.db)
}

// Start 启动监控服务
func (m *DownloadMonitor) Start(interval time.Duration) {
	m.ticker = time.NewTicker(interval)

	logger.Info("Download monitor started",
		"interval", interval.String(),
		"category", AutoRssCategory)

	go func() {
		time.Sleep(2 * time.Second)
		m.checkDownloads()

		for {
			select {
			case <-m.ticker.C:
				m.checkDownloads()
			case <-m.stopChan:
				logger.Info("Download monitor stopped")
				return
			}
		}
	}()
}

// Stop 停止监控服务
func (m *DownloadMonitor) Stop() {
	if m.ticker != nil {
		m.ticker.Stop()
	}
	close(m.stopChan)
}

// processPendingDownloads 处理等待中的下载任务
func (m *DownloadMonitor) processPendingDownloads() {
	pendingDownloads, _, err := m.downloadRepo.List(0, 10, "pending")
	if err != nil {
		logger.Error("Failed to get pending downloads", "error", err.Error())
		return
	}

	if len(pendingDownloads) == 0 {
		return
	}

	logger.Info("Processing pending downloads", "count", len(pendingDownloads))

	existingTorrents, err := m.qbClient.GetTorrentsByCategory(AutoRssCategory)
	if err != nil {
		logger.Error("Failed to get existing torrents from qBittorrent",
			"error", err.Error())
		return
	}

	existingHashes := make(map[string]bool)
	for _, torrent := range existingTorrents {
		existingHashes[torrent.Hash] = true
	}

	for _, download := range pendingDownloads {
		if download.TorrentHash != "" {
			if existingHashes[download.TorrentHash] {
				if download.Status == "pending" {
					download.Status = "downloading"
					if err := m.downloadRepo.Update(&download); err != nil {
						logger.Error("Failed to update existing torrent status",
							"download_id", download.ID,
							"error", err.Error())
					} else {
						logger.Info("Updated existing torrent status to downloading",
							"download_id", download.ID,
							"hash", download.TorrentHash)
					}
				}
				continue
			}
			logger.Info("Torrent hash exists in DB but not in qBittorrent, re-adding",
				"download_id", download.ID,
				"hash", download.TorrentHash)
		}

		subscription, err := m.subscriptionRepo.GetByID(download.SubscriptionID)
		if err != nil {
			logger.Error("Failed to get subscription for pending download",
				"download_id", download.ID,
				"subscription_id", download.SubscriptionID,
				"error", err.Error())
			continue
		}

		basePath := constants.DefaultDownloadPath
		if m.configRepo != nil {
			if downloadPathConfig, err := m.configRepo.Get("download_path"); err == nil && downloadPathConfig != nil && downloadPathConfig.Value != "" {
				basePath = downloadPathConfig.Value
			}
		}
		savePath := utils.GenerateDownloadPath(basePath, subscription.Name)

		var torrentHash string
		if isTorrentFileURL(download.TorrentURL) {
			if m.configRepo != nil {
				if proxyConfig, proxyErr := m.configRepo.Get("system_proxy"); proxyErr == nil && proxyConfig != nil && proxyConfig.Value != "" {
					_ = m.qbClient.SetProxy(proxyConfig.Value)
				}
			}

			fileContent, downloadErr := m.qbClient.DownloadTorrentFile(download.TorrentURL)
			if downloadErr != nil {
				err = fmt.Errorf("download torrent file failed: %w", downloadErr)
			} else {
				torrentHash, err = m.qbClient.AddTorrentFile(
					"torrent.torrent",
					fileContent,
					savePath,
					AutoRssCategory,
				)
			}
		} else {
			torrentHash, err = m.qbClient.AddTorrent(
				download.TorrentURL,
				savePath,
				AutoRssCategory,
			)
		}

		if err != nil {
			logger.Error("Failed to add pending download to qBittorrent",
				"download_id", download.ID,
				"title", download.Title,
				"error", err.Error())
			if markErr := m.retryService.MarkFailed(&download, err, "qbittorrent_add_failed"); markErr != nil {
				logger.Error("Failed to mark download as failed",
					"download_id", download.ID,
					"error", markErr.Error())
			}
			if m.notificationSvc != nil && download.RetryCount >= download.MaxRetries {
				m.sendFailedNotification(&download, download.ErrorMessage)
			}
			continue
		}

		if torrentHash == "" {
			logger.Warn("Torrent added but hash not retrieved, will retry later",
				"download_id", download.ID,
				"title", download.Title)
			continue
		}

		if torrentHash != download.TorrentHash {
			existing, _ := m.downloadRepo.GetByHash(torrentHash)
			if existing != nil && existing.ID != download.ID {
				logger.Warn("Torrent hash already belongs to another download, removing duplicate pending task",
					"download_id", download.ID,
					"existing_download_id", existing.ID,
					"hash", torrentHash)
				_ = m.downloadRepo.Delete(download.ID)
				continue
			}
		}

		download.TorrentHash = torrentHash
		download.Status = "downloading"
		if err := m.downloadRepo.Update(&download); err != nil {
			logger.Error("Failed to update pending download status",
				"download_id", download.ID,
				"error", err.Error())
		} else {
			logger.Info("Pending download added to qBittorrent successfully",
				"download_id", download.ID,
				"title", download.Title,
				"episode", download.Episode,
				"hash", torrentHash)
		}
	}
}

// isTorrentFileURL 检查是否是.torrent文件URL
func isTorrentFileURL(url string) bool {
	return len(url) > 8 && (url[len(url)-8:] == ".torrent" || strings.Contains(url, "/Download/"))
}

// checkDownloads 检查下载状态
func (m *DownloadMonitor) checkDownloads() {
	logger.Debug("Checking downloads...")

	// Process retries using retry service
	m.retryService.ProcessRetries(10)

	// Process pending downloads
	m.processPendingDownloads()

	// Get torrents from qBittorrent
	torrents, err := m.qbClient.GetTorrentsByCategory(AutoRssCategory)
	if err != nil {
		logger.Error("Failed to get torrents from qBittorrent",
			"error", err.Error())
		return
	}

	logger.Debug("Found torrents in qBittorrent",
		"count", len(torrents),
		"category", AutoRssCategory)

	// Update status for all torrents using status sync service
	for _, torrent := range torrents {
		download, err := m.downloadRepo.GetByHash(torrent.Hash)
		if err != nil {
			logger.Debug("Download not found in database",
				"hash", torrent.Hash,
				"name", torrent.Name)
			continue
		}

		if m.statusSync != nil {
			changed, _ := m.statusSync.UpdateStatus(download, torrent)
			if changed && download.Status == "completed" && m.completionHandler != nil {
				subscription, _ := m.subscriptionRepo.GetByID(download.SubscriptionID)
				if subscription != nil {
					m.completionHandler.HandleComplete(download, torrent, subscription)
				}
			}
		}
	}

	// Reconcile missing tasks using status sync service
	if m.statusSync != nil {
		downloading, _, _ := m.downloadRepo.List(0, 10000, "downloading")
		stalled, _, _ := m.downloadRepo.List(0, 10000, "stalled")
		m.statusSync.Reconcile(torrents, downloading, stalled)
	}
}

// sendFailedNotification 发送下载失败通知
func (m *DownloadMonitor) sendFailedNotification(download *model.Download, errorMsg string) {
	subscription, err := m.subscriptionRepo.GetByID(download.SubscriptionID)
	if err != nil {
		logger.Error("Failed to get subscription for failed notification",
			"subscription_id", download.SubscriptionID,
			"error", err.Error())
		subscription = &model.Subscription{Name: "Unknown"}
	}
	var episodeInfo string
	if download.Episode > 0 {
		episodeInfo = fmt.Sprintf("第 %d 集", download.Episode)
	} else {
		episodeInfo = "合集"
	}
	if len(errorMsg) > 200 {
		errorMsg = errorMsg[:200] + "..."
	}
	m.notificationSvc.Send(model.NotificationPayload{
		Event:   model.EventDownloadFailed,
		Title:   fmt.Sprintf("❌ 下载失败: %s", subscription.Name),
		Message: fmt.Sprintf("%s %s\n文件名: %s\n错误: %s", subscription.Name, episodeInfo, download.Title, errorMsg),
		Data: map[string]any{
			"download_id":     download.ID,
			"subscription_id": download.SubscriptionID,
			"subscription":    subscription.Name,
			"episode":         download.Episode,
			"title":           download.Title,
			"error":           errorMsg,
		},
		Timestamp: time.Now(),
	})
}
