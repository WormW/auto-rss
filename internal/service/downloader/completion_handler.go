package downloader

import (
	"fmt"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/repository"
	"gorm.io/gorm"
)

// CompletionHandler 下载完成处理服务
type CompletionHandler interface {
	// HandleComplete 处理下载完成事件
	// 包括：发送通知、执行重命名、更新订阅统计
	HandleComplete(download *model.Download, torrent *TorrentInfo, subscription *model.Subscription) error
}

// completionHandler 完成处理服务实现
type completionHandler struct {
	subscriptionRepo repository.SubscriptionRepository
	downloadRepo     repository.DownloadRepository
	notificationSvc  NotificationService
	renamerSvc       *RenameService
	qbClient         QBittorrentClient
	db               *gorm.DB
}

// NewCompletionHandler 创建完成处理服务
func NewCompletionHandler(
	subscriptionRepo repository.SubscriptionRepository,
	downloadRepo repository.DownloadRepository,
	notificationSvc NotificationService,
	renamerSvc *RenameService,
	qbClient QBittorrentClient,
	db *gorm.DB,
) CompletionHandler {
	return &completionHandler{
		subscriptionRepo: subscriptionRepo,
		downloadRepo:     downloadRepo,
		notificationSvc:  notificationSvc,
		renamerSvc:       renamerSvc,
		qbClient:         qbClient,
		db:               db,
	}
}

// HandleComplete 处理下载完成事件
func (h *completionHandler) HandleComplete(download *model.Download, torrent *TorrentInfo, subscription *model.Subscription) error {
	logger.Info("Download completed",
		"id", download.ID,
		"title", download.Title,
		"episode", download.Episode,
		"subscription_id", download.SubscriptionID)

	// 发送下载完成通知
	if h.notificationSvc != nil {
		h.sendCompletionNotification(download, subscription)
	}

	// 记录下载完成时间
	now := time.Now()
	download.DownloadedAt = &now
	download.FilePath = torrent.SavePath

	// 如果启用了重命名，执行文件重命名
	// 合集种子（Episode=0）需要批量重命名所有视频文件
	if subscription.RenameEnabled {
		if download.Episode > 0 {
			// 单集重命名
			newPath, err := h.renameFile(download, subscription, torrent)
			if err != nil {
				logger.Error("Failed to rename file",
					"download_id", download.ID,
					"error", err.Error())
				// 重命名失败不阻止完成处理
			} else {
				download.RenamedPath = newPath
				logger.Info("File renamed successfully",
					"download_id", download.ID,
					"old_path", torrent.SavePath,
					"new_path", newPath)
			}
		} else {
			// 合集种子批量重命名
			renamedCount, err := h.renameCollectionFiles(download, subscription, torrent)
			if err != nil {
				logger.Error("Failed to rename collection files",
					"download_id", download.ID,
					"error", err.Error())
				// 重命名失败不阻止完成处理
			} else {
				logger.Info("Collection files renamed successfully",
					"download_id", download.ID,
					"renamed_count", renamedCount)
			}
		}
	}

	// 更新订阅统计信息
	if err := h.updateSubscriptionStats(subscription, download); err != nil {
		logger.Error("Failed to update subscription stats",
			"subscription_id", subscription.ID,
			"error", err.Error())
		// 继续处理，不返回错误
	}

	// 保存下载记录
	if err := h.downloadRepo.Update(download); err != nil {
		logger.Error("Failed to update download after completion",
			"download_id", download.ID,
			"error", err.Error())
		return err
	}

	return nil
}

// sendCompletionNotification 发送下载完成通知
func (h *completionHandler) sendCompletionNotification(download *model.Download, subscription *model.Subscription) {
	var episodeInfo string
	if download.Episode > 0 {
		episodeInfo = fmt.Sprintf("第 %d 集", download.Episode)
	} else {
		episodeInfo = "合集"
	}

	h.notificationSvc.Send(model.NotificationPayload{
		Event:   model.EventDownloadComplete,
		Title:   fmt.Sprintf("✅ 下载完成: %s", subscription.Name),
		Message: fmt.Sprintf("%s %s\n文件名: %s", subscription.Name, episodeInfo, download.Title),
		Data: map[string]any{
			"download_id":     download.ID,
			"subscription_id": download.SubscriptionID,
			"subscription":    subscription.Name,
			"episode":         download.Episode,
			"title":           download.Title,
		},
		Timestamp: time.Now(),
	})
}

// renameFile 重命名单个下载的文件
func (h *completionHandler) renameFile(download *model.Download, subscription *model.Subscription, torrent *TorrentInfo) (string, error) {
	// 获取种子文件列表
	files, err := h.qbClient.GetTorrentFiles(torrent.Hash)
	if err != nil {
		return "", fmt.Errorf("failed to get torrent files: %w", err)
	}

	if len(files) == 0 {
		return "", fmt.Errorf("no files found in torrent")
	}

	// 提取主视频文件信息
	fileInfo := ExtractFileInfo(files)
	if fileInfo == nil {
		return "", fmt.Errorf("no video file found in torrent")
	}

	// 构建重命名上下文
	ctx := &RenameContext{
		Subscription: subscription,
		Download:     download,
		OriginalName: fileInfo.Name,
		Extension:    fileInfo.Extension,
		Resolution:   fileInfo.Resolution,
	}

	// 生成新文件路径
	newRelativePath := h.renamerSvc.GenerateFileName(ctx)

	// 分离目录和文件名
	newDir := newRelativePath
	newFileName := fileInfo.Name

	// 如果路径包含目录分隔符，分离目录和文件名
	if idx := lastIndexOf(newRelativePath, '/'); idx >= 0 {
		newDir = newRelativePath[:idx]
		newFileName = newRelativePath[idx+1:]
	}

	// Step 1: 移动种子到新位置（如果需要）
	targetLocation := torrent.SavePath
	if newDir != "" && newDir != "." {
		targetLocation = torrent.SavePath + "/" + newDir
	}

	if torrent.SavePath != targetLocation {
		logger.Info("Moving torrent to new location",
			"download_id", download.ID,
			"from", torrent.SavePath,
			"to", targetLocation)

		if err := h.qbClient.SetLocation(torrent.Hash, targetLocation); err != nil {
			logger.Warn("Failed to move torrent location",
				"download_id", download.ID,
				"error", err.Error())
			// 移动失败不阻止重命名
		}
	}

	// Step 2: 重命名文件
	oldFileName := fileInfo.Name
	if oldFileName != newFileName {
		logger.Info("Renaming file via qBittorrent API",
			"download_id", download.ID,
			"from", oldFileName,
			"to", newFileName)

		if err := h.qbClient.RenameTorrentFile(torrent.Hash, oldFileName, newFileName); err != nil {
			// 如果是409错误(文件已存在)，不视为失败
			if isConflictError(err) {
				logger.Info("File already renamed, skipping",
					"download_id", download.ID,
					"path", newFileName)
			} else {
				return "", fmt.Errorf("failed to rename in qBittorrent: %w", err)
			}
		}
	}

	// 返回完整路径
	fullPath := targetLocation + "/" + newFileName
	return fullPath, nil
}

// renameCollectionFiles 重命名合集种子中的所有视频文件
func (h *completionHandler) renameCollectionFiles(download *model.Download, subscription *model.Subscription, torrent *TorrentInfo) (int, error) {
	// 获取种子文件列表
	files, err := h.qbClient.GetTorrentFiles(torrent.Hash)
	if err != nil {
		return 0, fmt.Errorf("failed to get torrent files: %w", err)
	}

	if len(files) == 0 {
		return 0, fmt.Errorf("no files found in torrent")
	}

	// 使用 RenameService 的 RenameCollection 方法
	renamedCount, err := h.renamerSvc.RenameCollection(h.qbClient, torrent.Hash, subscription)
	if err != nil {
		return 0, err
	}

	return renamedCount, nil
}

// updateSubscriptionStats 更新订阅统计信息（使用事务）
func (h *completionHandler) updateSubscriptionStats(subscription *model.Subscription, download *model.Download) error {
	// 更新当前集数
	if download.Episode > subscription.CurrentEpisode {
		subscription.CurrentEpisode = download.Episode
	}

	// 更新最后下载时间
	now := time.Now()
	subscription.LastDownloadAt = &now

	// 如果数据库连接为nil，仅更新内存中的对象（用于测试）
	if h.db == nil {
		logger.Debug("DB is nil, skipping database update for subscription stats",
			"subscription_id", subscription.ID)
		return nil
	}

	// 使用事务保存到数据库
	err := h.db.Transaction(func(tx *gorm.DB) error {
		// 保存订阅统计
		if err := tx.Save(subscription).Error; err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		logger.Error("Failed to update subscription stats",
			"subscription_id", subscription.ID,
			"error", err.Error())
		return err
	}

	logger.Info("Subscription stats updated",
		"subscription_id", subscription.ID,
		"current_episode", subscription.CurrentEpisode,
		"last_download_at", subscription.LastDownloadAt)

	return nil
}

// isConflictError 检查是否是冲突错误（如文件已存在）
func isConflictError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// qBittorrent 返回 409 表示冲突
	return containsSubstring(errStr, "409") || containsSubstring(errStr, "conflict")
}

// containsSubstring 检查字符串是否包含子串
func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// lastIndexOf 查找字符在字符串中最后出现的位置
func lastIndexOf(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}
