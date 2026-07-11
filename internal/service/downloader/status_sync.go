package downloader

import (
	"strings"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
)

// StatusSync 下载状态同步服务
type StatusSync interface {
	// Sync 同步 qBittorrent 状态到数据库
	// 处理所有活跃种子的状态更新
	Sync(torrents []*TorrentInfo) error

	// UpdateStatus 更新单个下载任务的状态
	// 返回状态是否发生变化
	UpdateStatus(download *model.Download, torrent *TorrentInfo) (bool, error)

	// Reconcile 对账：标记 qBittorrent 中已消失的任务为失败
	Reconcile(torrents []*TorrentInfo, downloadingTasks, stalledTasks []model.Download) (reconciled int, skipped int, err error)
}

// statusSync 状态同步服务实现
type statusSync struct {
	downloadRepo    statusSyncDownloadRepository
	notificationSvc NotificationService
	episodeService  EpisodeCompletionService
	gracePeriod     time.Duration
}

type statusSyncDownloadRepository interface {
	GetByHash(hash string) (*model.Download, error)
	Update(download *model.Download) error
}

// NewStatusSync 创建状态同步服务
func NewStatusSync(
	downloadRepo statusSyncDownloadRepository,
	notificationSvc NotificationService,
	episodeService EpisodeCompletionService,
) StatusSync {
	return &statusSync{
		downloadRepo:    downloadRepo,
		notificationSvc: notificationSvc,
		episodeService:  episodeService,
		gracePeriod:     ReconcileGracePeriod,
	}
}

// Sync 同步 qBittorrent 状态到数据库
func (s *statusSync) Sync(torrents []*TorrentInfo) error {
	for _, torrent := range torrents {
		// 通过 torrent hash 查找下载任务
		download, err := s.downloadRepo.GetByHash(torrent.Hash)
		if err != nil {
			logger.Error("Failed to get download by hash",
				"hash", torrent.Hash,
				"error", err.Error())
			return err
		}

		if download == nil {
			logger.Debug("Download not found in database",
				"hash", torrent.Hash,
				"name", torrent.Name)
			continue
		}

		// 更新状态
		changed, err := s.UpdateStatus(download, torrent)
		if err != nil {
			logger.Error("Failed to update download status",
				"download_id", download.ID,
				"error", err.Error())
			return err
		}

		if changed {
			logger.Debug("Download status synced",
				"download_id", download.ID,
				"title", download.Title,
				"status", download.Status)
		}
	}

	return nil
}

// UpdateStatus 更新单个下载任务的状态
// 返回状态是否发生变化
func (s *statusSync) UpdateStatus(download *model.Download, torrent *TorrentInfo) (bool, error) {
	// 获取旧状态
	oldStatus := download.Status

	// 映射 qB 状态到内部状态
	newStatus := downloadStatusForTorrent(torrent)

	// 状态未变化
	if oldStatus == newStatus {
		return false, nil
	}
	if newStatus == model.DownloadStatusCompleted {
		return true, nil
	}

	logger.Info("Download status changed",
		"id", download.ID,
		"title", download.Title,
		"old_status", oldStatus,
		"new_status", newStatus,
		"progress", torrent.Progress)

	// 更新状态
	download.Status = newStatus

	// 如果下载失败，记录错误信息
	if newStatus == "failed" {
		download.ErrorMessage = "Download failed in qBittorrent"
	}

	// 保存到数据库
	if err := s.downloadRepo.Update(download); err != nil {
		logger.Error("Failed to update download status",
			"id", download.ID,
			"error", err.Error())
		return false, err
	}
	if newStatus == model.DownloadStatusFailed && s.episodeService != nil && shouldReleaseEpisodeAfterFailure(download) {
		if err := s.episodeService.MarkDownloadFailed(download.ID); err != nil {
			return false, err
		}
	}

	return true, nil
}

// Reconcile 对账：标记 qBittorrent 中已消失的任务为失败
func (s *statusSync) Reconcile(torrents []*TorrentInfo, downloadingTasks, stalledTasks []model.Download) (reconciled int, skipped int, err error) {
	// 构建 hash 集合
	torrentHashSet := make(map[string]struct{}, len(torrents))
	for _, torrent := range torrents {
		hash := strings.ToLower(strings.TrimSpace(torrent.Hash))
		if hash != "" {
			torrentHashSet[hash] = struct{}{}
		}
	}

	// 合并 downloading 和 stalled 任务
	tasks := append(downloadingTasks, stalledTasks...)

	now := time.Now()
	reconciled = 0
	skipped = 0

	for i := range tasks {
		download := &tasks[i]
		hash := strings.ToLower(strings.TrimSpace(download.TorrentHash))

		// 跳过空 hash
		if hash == "" {
			continue
		}

		// 跳过仍存在于 qBittorrent 中的任务
		if _, exists := torrentHashSet[hash]; exists {
			continue
		}

		// 检查是否在宽限期内
		if shouldSkipReconcileByGracePeriod(download, now) {
			skipped++
			continue
		}

		// 标记为失败
		download.Status = "failed"
		download.ErrorMessage = "Torrent missing in qBittorrent during monitor reconciliation"

		if updateErr := s.downloadRepo.Update(download); updateErr != nil {
			logger.Error("Failed to reconcile missing downloading task",
				"download_id", download.ID,
				"hash", hash,
				"error", updateErr.Error())
			continue
		}
		if s.episodeService != nil && shouldReleaseEpisodeAfterFailure(download) {
			if markErr := s.episodeService.MarkDownloadFailed(download.ID); markErr != nil {
				logger.Error("Failed to release episode after download reconciliation",
					"download_id", download.ID,
					"error", markErr.Error())
				continue
			}
		}

		reconciled++

		// 发送下载失败通知
		if s.notificationSvc != nil {
			s.sendFailedNotification(download)
		}
	}

	if reconciled > 0 || skipped > 0 {
		logger.Info("Download task reconciliation completed",
			"reconciled_to_failed", reconciled,
			"skipped_in_grace_period", skipped,
			"grace_period", s.gracePeriod.String(),
			"scanned_statuses", "downloading,stalled")
	}

	return reconciled, skipped, nil
}

// sendFailedNotification 发送下载失败通知
func (s *statusSync) sendFailedNotification(download *model.Download) {
	var episodeInfo string
	if download.Episode > 0 {
		episodeInfo = "第 " + string(rune('0'+download.Episode)) + " 集"
	} else {
		episodeInfo = "合集"
	}

	errorMsg := download.ErrorMessage
	if len(errorMsg) > 200 {
		errorMsg = errorMsg[:200] + "..."
	}

	s.notificationSvc.Send(model.NotificationPayload{
		Event:   model.EventDownloadFailed,
		Title:   "❌ 下载失败: " + download.Title,
		Message: download.Title + " " + episodeInfo + "\n文件名: " + download.Title + "\n错误: " + errorMsg,
		Data: map[string]any{
			"download_id":     download.ID,
			"subscription_id": download.SubscriptionID,
			"episode":         download.Episode,
			"title":           download.Title,
			"error":           errorMsg,
		},
		Timestamp: time.Now(),
	})
}
