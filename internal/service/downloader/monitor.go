package downloader

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/repository"
)

const (
	// AutoRssCategory qBittorrent分类名称
	AutoRssCategory = "AutoRss"

	// 下载状态映射
	StateDownloading = "downloading"
	StateCompleted   = "uploading" // qBittorrent完成后会变成uploading状态
	StatePaused      = "pausedDL"
	StateError       = "error"
)

// DownloadMonitor 下载监控服务
type DownloadMonitor struct {
	qbClient         QBittorrentClient
	downloadRepo     repository.DownloadRepository
	subscriptionRepo repository.SubscriptionRepository
	renameService    *RenameService
	ticker           *time.Ticker
	stopChan         chan struct{}
}

// NewDownloadMonitor 创建下载监控服务
func NewDownloadMonitor(
	qbClient QBittorrentClient,
	downloadRepo repository.DownloadRepository,
	subscriptionRepo repository.SubscriptionRepository,
	renameTemplate string,
) *DownloadMonitor {
	return &DownloadMonitor{
		qbClient:         qbClient,
		downloadRepo:     downloadRepo,
		subscriptionRepo: subscriptionRepo,
		renameService:    NewRenameService(renameTemplate),
		stopChan:         make(chan struct{}),
	}
}

// Start 启动监控服务
func (m *DownloadMonitor) Start(interval time.Duration) {
	m.ticker = time.NewTicker(interval)

	logger.Info("Download monitor started",
		"interval", interval.String(),
		"category", AutoRssCategory)

	go func() {
		// 延迟2秒后执行第一次检查，确保 qBittorrent 客户端已完全初始化
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
	// 获取所有等待中的下载任务（限制每次处理10个，避免一次性处理过多）
	pendingDownloads, _, err := m.downloadRepo.List(0, 10, "pending")
	if err != nil {
		logger.Error("Failed to get pending downloads", "error", err.Error())
		return
	}

	if len(pendingDownloads) == 0 {
		return
	}

	logger.Info("Processing pending downloads", "count", len(pendingDownloads))

	// 获取qBittorrent中现有的所有种子hash，用于检查是否已存在
	existingTorrents, err := m.qbClient.GetTorrentsByCategory(AutoRssCategory)
	if err != nil {
		logger.Error("Failed to get existing torrents from qBittorrent",
			"error", err.Error())
		// 如果获取失败，不继续处理，避免重复添加
		return
	}

	existingHashes := make(map[string]bool)
	for _, torrent := range existingTorrents {
		existingHashes[torrent.Hash] = true
	}

	for _, download := range pendingDownloads {
		// 如果数据库中有hash，检查qBittorrent中是否真的存在
		if download.TorrentHash != "" {
			if existingHashes[download.TorrentHash] {
				// 种子已存在于qBittorrent中，更新状态为downloading
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
			// hash存在但qBittorrent中没有，可能被删除了，重新添加
			logger.Info("Torrent hash exists in DB but not in qBittorrent, re-adding",
				"download_id", download.ID,
				"hash", download.TorrentHash)
		}

		// 获取订阅信息以获取下载路径
		subscription, err := m.subscriptionRepo.GetByID(download.SubscriptionID)
		if err != nil {
			logger.Error("Failed to get subscription for pending download",
				"download_id", download.ID,
				"subscription_id", download.SubscriptionID,
				"error", err.Error())
			continue
		}

		// 确定保存路径
		savePath := "/downloads" // 默认路径
		if subscription.DownloadPath != "" {
			savePath = subscription.DownloadPath
		}

		// 添加到qBittorrent
		torrentHash, err := m.qbClient.AddTorrent(
			download.TorrentURL,
			savePath,
			AutoRssCategory,
		)

		if err != nil {
			logger.Error("Failed to add pending download to qBittorrent",
				"download_id", download.ID,
				"title", download.Title,
				"error", err.Error())
			// 更新状态为失败
			download.Status = "failed"
			download.ErrorMessage = fmt.Sprintf("Failed to add to qBittorrent: %v", err)
			m.downloadRepo.Update(&download)
			continue
		}

		// 如果没有获取到hash，记录警告并跳过更新（等待下次重试）
		if torrentHash == "" {
			logger.Warn("Torrent added but hash not retrieved, will retry later",
				"download_id", download.ID,
				"title", download.Title)
			continue
		}

		// 更新下载记录
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

// checkDownloads 检查下载状态
func (m *DownloadMonitor) checkDownloads() {
	logger.Debug("Checking downloads...")

	// 首先处理等待中的下载任务
	m.processPendingDownloads()

	// 获取AutoRss分类的所有torrents
	torrents, err := m.qbClient.GetTorrentsByCategory(AutoRssCategory)
	if err != nil {
		logger.Error("Failed to get torrents from qBittorrent",
			"error", err.Error())
		return
	}

	logger.Debug("Found torrents in qBittorrent",
		"count", len(torrents),
		"category", AutoRssCategory)

	// 更新数据库中的下载状态
	for _, torrent := range torrents {
		m.updateDownloadStatus(torrent)
	}
}

// updateDownloadStatus 更新下载状态
func (m *DownloadMonitor) updateDownloadStatus(torrent *TorrentInfo) {
	// 通过torrent hash查找下载任务
	download, err := m.downloadRepo.GetByHash(torrent.Hash)
	if err != nil {
		logger.Debug("Download not found in database",
			"hash", torrent.Hash,
			"name", torrent.Name)
		return
	}

	// 检查状态是否发生变化
	oldStatus := download.Status
	newStatus := mapQBStateToStatus(torrent.State)

	if oldStatus == newStatus {
		return
	}

	logger.Info("Download status changed",
		"id", download.ID,
		"title", download.Title,
		"old_status", oldStatus,
		"new_status", newStatus,
		"progress", fmt.Sprintf("%.2f%%", torrent.Progress*100))

	// 更新状态
	download.Status = newStatus

	// 如果下载完成，执行后续操作
	if newStatus == "completed" && oldStatus != "completed" {
		m.handleDownloadComplete(download, torrent)
	}

	// 如果下载失败，记录错误信息
	if newStatus == "failed" {
		download.ErrorMessage = "Download failed in qBittorrent"
	}

	// 保存到数据库
	if err := m.downloadRepo.Update(download); err != nil {
		logger.Error("Failed to update download status",
			"id", download.ID,
			"error", err.Error())
	}
}

// handleDownloadComplete 处理下载完成的任务
func (m *DownloadMonitor) handleDownloadComplete(download *model.Download, torrent *TorrentInfo) {
	logger.Info("Download completed",
		"id", download.ID,
		"title", download.Title,
		"subscription_id", download.SubscriptionID)

	// 获取订阅信息
	subscription, err := m.subscriptionRepo.GetByID(download.SubscriptionID)
	if err != nil {
		logger.Error("Failed to get subscription",
			"subscription_id", download.SubscriptionID,
			"error", err.Error())
		return
	}

	// 记录下载完成时间
	now := time.Now()
	download.DownloadedAt = &now
	download.FilePath = torrent.SavePath

	// 如果启用了重命名，执行文件重命名
	if subscription.RenameEnabled {
		newPath, err := m.renameFile(download, subscription, torrent)
		if err != nil {
			logger.Error("Failed to rename file",
				"download_id", download.ID,
				"error", err.Error())
		} else {
			download.RenamedPath = newPath
			logger.Info("File renamed successfully",
				"download_id", download.ID,
				"old_path", torrent.SavePath,
				"new_path", newPath)
		}
	}

	// 更新订阅统计信息
	m.updateSubscriptionStats(subscription, download)
}

// renameFile 重命名下载的文件
func (m *DownloadMonitor) renameFile(download *model.Download, subscription *model.Subscription, torrent *TorrentInfo) (string, error) {
	// 获取种子文件列表
	files, err := m.qbClient.GetTorrentFiles(torrent.Hash)
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

	// 生成新文件路径 (包含目录结构)
	newRelativePath := m.renameService.GenerateFileName(ctx)

	// 调用qBittorrent API重命名文件
	if err := m.qbClient.RenameTorrentFile(torrent.Hash, fileInfo.Name, newRelativePath); err != nil {
		// 如果是409错误(文件已存在)，不视为失败
		if strings.Contains(err.Error(), "409") {
			logger.Info("File already renamed, skipping",
				"download_id", download.ID,
				"path", newRelativePath)
			return newRelativePath, nil
		}
		return "", fmt.Errorf("failed to rename in qBittorrent: %w", err)
	}

	// 返回完整路径
	fullPath := filepath.Join(torrent.SavePath, newRelativePath)
	return fullPath, nil
}

// updateSubscriptionStats 更新订阅统计信息
func (m *DownloadMonitor) updateSubscriptionStats(subscription *model.Subscription, download *model.Download) {
	// 更新当前集数
	if download.Episode > subscription.CurrentEpisode {
		subscription.CurrentEpisode = download.Episode
	}

	// 更新最后下载时间
	now := time.Now()
	subscription.LastDownloadAt = &now

	// 保存到数据库
	if err := m.subscriptionRepo.Update(subscription); err != nil {
		logger.Error("Failed to update subscription stats",
			"subscription_id", subscription.ID,
			"error", err.Error())
		return
	}

	logger.Info("Subscription stats updated",
		"subscription_id", subscription.ID,
		"current_episode", subscription.CurrentEpisode,
		"last_download_at", subscription.LastDownloadAt)
}

// mapQBStateToStatus 将qBittorrent状态映射到数据库状态
func mapQBStateToStatus(qbState string) string {
	// qBittorrent状态说明:
	// downloading: 正在下载
	// uploading: 下载完成，正在做种
	// pausedDL: 暂停下载
	// pausedUP: 暂停做种
	// queuedDL: 排队等待下载
	// queuedUP: 排队等待做种
	// checkingDL: 检查下载
	// checkingUP: 检查上传
	// stalledDL: 下载停滞
	// stalledUP: 做种停滞
	// metaDL: 下载元数据
	// error: 错误

	switch {
	case strings.Contains(qbState, "error"):
		return "failed"
	case qbState == "uploading" || qbState == "pausedUP" || qbState == "queuedUP" || qbState == "stalledUP":
		return "completed"
	default:
		// 所有其他状态（downloading, pausedDL, queuedDL, stalledDL, metaDL, checkingDL等）
		// 都认为是下载中，只要种子已添加到qBittorrent就保持downloading状态
		return "downloading"
	}
}
