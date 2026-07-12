package downloader

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/constants"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/pkg/utils"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/disk"
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

const pendingDownloadCategoryPrefix = AutoRssCategory + ":pending:"
const retryTorrentPlaceholderPrefix = AutoRssCategory + ":retry:"
const retryCleanupLeaseTimeout = 5 * time.Minute

func pendingDownloadCategory(downloadID uint) string {
	return pendingDownloadCategoryPrefix + strconv.FormatUint(uint64(downloadID), 10)
}

func retryTorrentPlaceholder(downloadID uint) string {
	return retryTorrentPlaceholderPrefix + strconv.FormatUint(uint64(downloadID), 10)
}

func parsePendingDownloadCategory(category string) (uint, bool) {
	if !strings.HasPrefix(category, pendingDownloadCategoryPrefix) {
		return 0, false
	}
	rawID := strings.TrimPrefix(category, pendingDownloadCategoryPrefix)
	parsed, err := strconv.ParseUint(rawID, 10, 64)
	if err != nil || parsed == 0 || uint64(uint(parsed)) != parsed {
		return 0, false
	}
	id := uint(parsed)
	if category != pendingDownloadCategory(id) {
		return 0, false
	}
	return id, true
}

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
	mediaLibrarySvc  MediaLibraryRefresher
	episodeService   EpisodeCompletionService
	ticker           *time.Ticker
	stopChan         chan struct{}
	downloadsPaused  func() bool
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
	episodeService EpisodeCompletionService,
	mediaLibrarySvc ...MediaLibraryRefresher,
) *DownloadMonitor {
	retrySvc := NewRetryService(downloadRepo)
	renameSvc := NewRenameService(renameTemplate)
	var mediaSvc MediaLibraryRefresher
	if len(mediaLibrarySvc) > 0 {
		mediaSvc = mediaLibrarySvc[0]
	}

	return &DownloadMonitor{
		db:               db,
		qbClient:         qbClient,
		downloadRepo:     downloadRepo,
		subscriptionRepo: subscriptionRepo,
		configRepo:       configRepo,
		retryService:     retrySvc,
		renameService:    renameSvc,
		mediaLibrarySvc:  mediaSvc,
		episodeService:   episodeService,
		stopChan:         make(chan struct{}),
		downloadsPaused:  disk.IsDownloadsPaused,
	}
}

// SetNotificationService 设置通知服务
func (m *DownloadMonitor) SetNotificationService(svc NotificationService) {
	m.notificationSvc = svc
	// Initialize services that need notification service
	m.statusSync = NewStatusSync(m.downloadRepo, svc, m.episodeService)
	m.completionHandler = NewCompletionHandler(m.subscriptionRepo, m.downloadRepo, svc, m.renameService, m.qbClient, m.db, m.episodeService, m.mediaLibrarySvc)
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
	m.processRetryCleanupDownloads()

	if m.areDownloadsPaused() {
		logger.Info("Skipping pending downloads because downloads are paused")
		return
	}

	pendingDownloads, _, err := m.downloadRepo.List(0, 10, "pending")
	if err != nil {
		logger.Error("Failed to get pending downloads", "error", err.Error())
		return
	}

	if len(pendingDownloads) == 0 {
		return
	}

	logger.Info("Processing pending downloads", "count", len(pendingDownloads))

	existingTorrents, err := m.qbClient.GetTorrentsByCategory("")
	if err != nil {
		logger.Error("Failed to get existing torrents from qBittorrent",
			"error", err.Error())
		return
	}

	existingHashes := make(map[string]bool)
	pendingTorrents := make(map[uint]*TorrentInfo)
	for _, torrent := range existingTorrents {
		if torrent == nil {
			continue
		}
		hash := strings.ToLower(strings.TrimSpace(torrent.Hash))
		switch {
		case torrent.Category == AutoRssCategory && hash != "":
			existingHashes[hash] = true
		case torrent.Category != AutoRssCategory:
			if downloadID, ok := parsePendingDownloadCategory(torrent.Category); ok {
				pendingTorrents[downloadID] = torrent
			}
		}
	}

	for _, download := range pendingDownloads {
		if pendingTorrent := pendingTorrents[download.ID]; pendingTorrent != nil {
			m.checkpointPendingDownload(&download, pendingTorrent.Hash)
			continue
		}

		if download.TorrentHash != "" {
			hash := strings.ToLower(strings.TrimSpace(download.TorrentHash))
			if existingHashes[hash] {
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
		pendingCategory := pendingDownloadCategory(download.ID)
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
					pendingCategory,
				)
			}
		} else {
			torrentHash, err = m.qbClient.AddTorrent(
				download.TorrentURL,
				savePath,
				pendingCategory,
			)
		}

		if err != nil {
			logger.Error("Failed to add pending download to qBittorrent",
				"download_id", download.ID,
				"title", download.Title,
				"error", err.Error())
			original := download
			m.retryService.PrepareFailure(&download, err, "qbittorrent_add_failed")
			releaseEpisode := shouldReleaseEpisodeAfterFailure(&download)
			failurePersisted := false
			if markErr := persistDownloadFailure(m.downloadRepo, m.episodeService, &download, releaseEpisode); markErr != nil {
				download = original
				logger.Error("Failed to mark download as failed",
					"download_id", download.ID,
					"error", markErr.Error())
			} else {
				failurePersisted = true
				m.retryService.logFailure(&download, err, "qbittorrent_add_failed")
			}
			if m.notificationSvc != nil && failurePersisted && releaseEpisode {
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

		m.checkpointPendingDownload(&download, torrentHash)
	}
}

func (m *DownloadMonitor) processRetryCleanupDownloads() {
	if m.db == nil || m.qbClient == nil {
		return
	}
	now := time.Now()
	if err := m.db.Model(&model.Download{}).
		Where("status = ? AND updated_at < ?", model.DownloadStatusRetryCleanupProcessing, now.Add(-retryCleanupLeaseTimeout)).
		Updates(map[string]any{"status": model.DownloadStatusRetryCleanup, "updated_at": now}).Error; err != nil {
		logger.Error("Failed to recover stale retry cleanup claims", "error", err.Error())
		return
	}
	cleanupDownloads, _, err := m.downloadRepo.List(0, 10, model.DownloadStatusRetryCleanup)
	if err != nil {
		logger.Error("Failed to get retry cleanup downloads", "error", err.Error())
		return
	}
	for i := range cleanupDownloads {
		download := &cleanupDownloads[i]
		oldHash := strings.TrimSpace(download.TorrentHash)
		claim := m.db.Model(&model.Download{}).
			Where("id = ? AND status = ? AND torrent_hash = ?", download.ID, model.DownloadStatusRetryCleanup, download.TorrentHash).
			Updates(map[string]any{"status": model.DownloadStatusRetryCleanupProcessing, "updated_at": time.Now()})
		if claim.Error != nil {
			logger.Error("Failed to claim retry cleanup", "download_id", download.ID, "error", claim.Error.Error())
			continue
		}
		if claim.RowsAffected != 1 {
			continue
		}
		if oldHash != "" {
			if err := m.qbClient.DeleteTorrentWithPayload(oldHash); err != nil {
				logger.Warn("Failed to delete retry cleanup torrent; checkpoint retained",
					"download_id", download.ID, "hash", oldHash, "error", err.Error())
				m.releaseRetryCleanupClaim(download)
				continue
			}
		}
		result := m.db.Model(&model.Download{}).
			Where("id = ? AND status = ? AND torrent_hash = ?", download.ID, model.DownloadStatusRetryCleanupProcessing, download.TorrentHash).
			Updates(map[string]any{
				"status":       model.DownloadStatusPending,
				"torrent_hash": retryTorrentPlaceholder(download.ID),
			})
		if result.Error != nil {
			logger.Error("Failed to finalize retry cleanup; checkpoint retained",
				"download_id", download.ID, "hash", oldHash, "error", result.Error.Error())
			m.releaseRetryCleanupClaim(download)
			continue
		}
		if result.RowsAffected != 1 {
			logger.Debug("Retry cleanup checkpoint changed before finalize", "download_id", download.ID)
		}
	}
}

func (m *DownloadMonitor) releaseRetryCleanupClaim(download *model.Download) {
	if download == nil || m.db == nil {
		return
	}
	if err := m.db.Model(&model.Download{}).
		Where("id = ? AND status = ? AND torrent_hash = ?", download.ID, model.DownloadStatusRetryCleanupProcessing, download.TorrentHash).
		Update("status", model.DownloadStatusRetryCleanup).Error; err != nil {
		logger.Error("Failed to release retry cleanup claim", "download_id", download.ID, "error", err.Error())
	}
}

func (m *DownloadMonitor) checkpointPendingDownload(download *model.Download, actualHash string) {
	actualHash = strings.TrimSpace(actualHash)
	if download == nil || actualHash == "" {
		return
	}

	if !strings.EqualFold(strings.TrimSpace(download.TorrentHash), actualHash) {
		existing, _ := m.downloadRepo.GetByHash(actualHash)
		if existing != nil && existing.ID != download.ID {
			logger.Warn("Torrent hash already belongs to another download; pending checkpoint retained",
				"download_id", download.ID,
				"existing_download_id", existing.ID,
				"hash", actualHash)
			return
		}
		download.TorrentHash = actualHash
		download.Status = model.DownloadStatusPending
		if err := m.downloadRepo.Update(download); err != nil {
			logger.Error("Failed to checkpoint pending torrent hash",
				"download_id", download.ID,
				"hash", actualHash,
				"error", err.Error())
			return
		}
	}

	if err := m.qbClient.SetCategory(actualHash, AutoRssCategory); err != nil {
		logger.Error("Failed to promote pending torrent category",
			"download_id", download.ID,
			"hash", actualHash,
			"error", err.Error())
		return
	}

	download.Status = model.DownloadStatusDownloading
	if err := m.downloadRepo.Update(download); err != nil {
		logger.Error("Failed to update pending download status",
			"download_id", download.ID,
			"error", err.Error())
		return
	}
	logger.Info("Pending download added to qBittorrent successfully",
		"download_id", download.ID,
		"title", download.Title,
		"episode", download.Episode,
		"hash", actualHash)
}

func (m *DownloadMonitor) areDownloadsPaused() bool {
	if m.downloadsPaused == nil {
		return disk.IsDownloadsPaused()
	}
	return m.downloadsPaused()
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
			if changed && downloadStatusForTorrent(torrent) == model.DownloadStatusCompleted && m.completionHandler != nil && shouldRunCompletionHandler(download) {
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

func shouldRunCompletionHandler(download *model.Download) bool {
	return download != nil && download.Purpose != model.DownloadPurposeReplacement
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
