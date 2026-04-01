package downloader

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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
	StateCompleted   = "uploading" // qBittorrent完成后会变成uploading状态
	StatePaused      = "pausedDL"
	StateError       = "error"

	// ReconcileGracePeriod 避免把刚创建/刚入队的任务误判为丢失
	ReconcileGracePeriod = 10 * time.Minute
)

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
	return &DownloadMonitor{
		db:               db,
		qbClient:         qbClient,
		downloadRepo:     downloadRepo,
		subscriptionRepo: subscriptionRepo,
		configRepo:       configRepo,
		renameService:    NewRenameService(renameTemplate),
		retryService:     NewRetryService(downloadRepo),
		stopChan:         make(chan struct{}),
	}
}

// NotificationService 通知服务接口（避免循环导入）
type NotificationService interface {
	Send(payload model.NotificationPayload)
}

// SetNotificationService 设置通知服务
func (m *DownloadMonitor) SetNotificationService(svc NotificationService) {
	m.notificationSvc = svc
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

		// 确定保存路径（使用系统配置的下载路径）
		basePath := constants.DefaultDownloadPath // 默认路径
		if m.configRepo != nil {
			if downloadPathConfig, err := m.configRepo.Get("download_path"); err == nil && downloadPathConfig != nil && downloadPathConfig.Value != "" {
				basePath = downloadPathConfig.Value
			}
		}
		savePath := utils.GenerateDownloadPath(basePath, subscription.Name)

		// 添加到qBittorrent：对于 .torrent URL 优先走“先下载文件再上传”路径，兼容需要代理/重定向的站点。
		var torrentHash string
		if strings.HasSuffix(strings.ToLower(download.TorrentURL), ".torrent") || strings.Contains(download.TorrentURL, "/Download/") {
			if m.configRepo != nil {
				if proxyConfig, proxyErr := m.configRepo.Get("system_proxy"); proxyErr == nil && proxyConfig != nil && proxyConfig.Value != "" {
					_ = m.qbClient.SetProxy(proxyConfig.Value)
				}
			}

			if qbDownloader, ok := m.qbClient.(interface {
				DownloadTorrentFile(url string) ([]byte, error)
			}); ok {
				fileContent, downloadErr := qbDownloader.DownloadTorrentFile(download.TorrentURL)
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
			// 使用重试服务标记失败
			if markErr := m.retryService.MarkFailed(&download, err, "qbittorrent_add_failed"); markErr != nil {
				logger.Error("Failed to mark download as failed",
					"download_id", download.ID,
					"error", markErr.Error())
			}
			// 发送下载失败通知（只在最终失败或重试间隔较长时发送）
			if m.notificationSvc != nil && download.RetryCount >= download.MaxRetries {
				m.sendFailedNotification(&download, download.ErrorMessage)
			}
			continue
		}

		// 如果没有获取到hash，记录警告并跳过更新（等待下次重试）
		if torrentHash == "" {
			logger.Warn("Torrent added but hash not retrieved, will retry later",
				"download_id", download.ID,
				"title", download.Title)
			continue
		}

		// 返回 hash 若与当前记录不同，先做去重检查，避免产生重复 hash 记录。
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

	// 首先处理需要重试的失败任务
	m.processFailedRetries()

	// 处理等待中的下载任务
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

	// 对账：数据库里仍是 downloading/stalled，但 qB 中已不存在的任务应尽快回收。
	m.reconcileMissingDownloadingTasks(torrents)
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
	if newStatus == "downloading" && isTorrentComplete(torrent) {
		newStatus = "completed"
	}

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
		"episode", download.Episode,
		"subscription_id", download.SubscriptionID)

	// 获取订阅信息
	subscription, err := m.subscriptionRepo.GetByID(download.SubscriptionID)
	if err != nil {
		logger.Error("Failed to get subscription",
			"subscription_id", download.SubscriptionID,
			"error", err.Error())
		return
	}

	// 发送下载完成通知
	if m.notificationSvc != nil {
		var episodeInfo string
		if download.Episode > 0 {
			episodeInfo = fmt.Sprintf("第 %d 集", download.Episode)
		} else {
			episodeInfo = "合集"
		}
		m.notificationSvc.Send(model.NotificationPayload{
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

	// 记录下载完成时间
	now := time.Now()
	download.DownloadedAt = &now
	download.FilePath = torrent.SavePath

	// 如果启用了重命名，执行文件重命名
	// 合集种子（Episode=0）需要批量重命名所有视频文件
	if subscription.RenameEnabled {
		if download.Episode > 0 {
			// 单集重命名
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
		} else {
			// 合集种子批量重命名
			renamedCount, err := m.renameCollectionFiles(download, subscription, torrent)
			if err != nil {
				logger.Error("Failed to rename collection files",
					"download_id", download.ID,
					"error", err.Error())
			} else {
				logger.Info("Collection files renamed successfully",
					"download_id", download.ID,
					"renamed_count", renamedCount)
			}
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

	// 生成新文件路径 (包含目录结构，如: 剑来/Season 2/剑来 S02E01.mp4)
	newRelativePath := m.renameService.GenerateFileName(ctx)

	// 分离目录和文件名
	newDir := filepath.Dir(newRelativePath)
	newFileName := filepath.Base(newRelativePath)

	// Step 1: 移动种子到新位置（如果需要）
	// 目标位置 = 当前保存路径的基础路径 + 新目录结构
	targetLocation := filepath.Join(torrent.SavePath, newDir)
	if torrent.SavePath != targetLocation {
		logger.Info("Moving torrent to new location",
			"download_id", download.ID,
			"from", torrent.SavePath,
			"to", targetLocation)

		if err := m.qbClient.SetLocation(torrent.Hash, targetLocation); err != nil {
			logger.Warn("Failed to move torrent location",
				"download_id", download.ID,
				"error", err.Error())
			// 移动失败不阻止重命名
		}
	}

	// Step 2: 重命名文件
	// 注意：需要将文件从原位置（可能在子目录中）重命名到目标位置（不带子目录）
	oldFileName := fileInfo.Name
	oldFileBaseName := filepath.Base(oldFileName)

	if oldFileName != newFileName {
		logger.Info("Renaming file via qBittorrent API",
			"download_id", download.ID,
			"from", oldFileName,
			"to", newFileName)

		if err := m.qbClient.RenameTorrentFile(torrent.Hash, oldFileName, newFileName); err != nil {
			// 如果是409错误(文件已存在)，不视为失败
			if strings.Contains(err.Error(), "409") {
				logger.Info("File already renamed, skipping",
					"download_id", download.ID,
					"path", newFileName)
			} else {
				// 如果重命名失败，尝试只改文件名不改路径
				if oldFileBaseName != newFileName {
					// 构建保持原目录结构的新路径
					newFilePathWithDir := newFileName
					if strings.Contains(oldFileName, string(filepath.Separator)) || strings.Contains(oldFileName, "/") {
						oldDir := filepath.Dir(oldFileName)
						newFilePathWithDir = filepath.Join(oldDir, newFileName)
					}
					if err2 := m.qbClient.RenameTorrentFile(torrent.Hash, oldFileName, newFilePathWithDir); err2 != nil {
						if !strings.Contains(err2.Error(), "409") {
							return "", fmt.Errorf("failed to rename in qBittorrent: %w", err)
						}
					}
				} else {
					return "", fmt.Errorf("failed to rename in qBittorrent: %w", err)
				}
			}
		}
	}

	// 返回完整路径
	fullPath := filepath.Join(targetLocation, newFileName)
	return fullPath, nil
}

// renameCollectionFiles 重命名合集种子中的所有视频文件
func (m *DownloadMonitor) renameCollectionFiles(download *model.Download, subscription *model.Subscription, torrent *TorrentInfo) (int, error) {
	// 获取种子文件列表
	files, err := m.qbClient.GetTorrentFiles(torrent.Hash)
	if err != nil {
		return 0, fmt.Errorf("failed to get torrent files: %w", err)
	}

	if len(files) == 0 {
		return 0, fmt.Errorf("no files found in torrent")
	}

	// 视频文件扩展名
	videoExts := map[string]bool{
		".mkv": true, ".mp4": true, ".avi": true, ".flv": true,
		".ts": true, ".m2ts": true, ".wmv": true, ".mov": true,
	}

	// 收集所有视频文件并解析集数
	type videoFileInfo struct {
		file    TorrentFile
		episode int
	}
	var videoFiles []videoFileInfo

	for _, file := range files {
		ext := strings.ToLower(filepath.Ext(file.Name))
		if !videoExts[ext] {
			continue
		}

		// 从文件名解析集数
		episode := extractEpisodeFromFilename(filepath.Base(file.Name))
		if episode > 0 {
			videoFiles = append(videoFiles, videoFileInfo{file: file, episode: episode})
		} else {
			logger.Warn("Could not extract episode from filename, skipping",
				"filename", file.Name)
		}
	}

	if len(videoFiles) == 0 {
		return 0, fmt.Errorf("no video files with episode numbers found")
	}

	// 按集数排序
	sort.Slice(videoFiles, func(i, j int) bool {
		return videoFiles[i].episode < videoFiles[j].episode
	})

	logger.Info("Found video files in collection",
		"download_id", download.ID,
		"video_count", len(videoFiles),
		"first_episode", videoFiles[0].episode,
		"last_episode", videoFiles[len(videoFiles)-1].episode)

	// 先移动种子到目标目录（只需要执行一次）
	// 使用第一个文件生成目标目录路径
	firstCtx := &RenameContext{
		Subscription: subscription,
		Download: &model.Download{
			Episode: videoFiles[0].episode,
		},
		OriginalName: videoFiles[0].file.Name,
		Extension:    filepath.Ext(videoFiles[0].file.Name),
		Resolution:   extractResolution(videoFiles[0].file.Name),
	}
	firstRelativePath := m.renameService.GenerateFileName(firstCtx)
	newDir := filepath.Dir(firstRelativePath)
	targetLocation := filepath.Join(torrent.SavePath, newDir)

	if torrent.SavePath != targetLocation {
		logger.Info("Moving collection torrent to new location",
			"download_id", download.ID,
			"from", torrent.SavePath,
			"to", targetLocation)

		if err := m.qbClient.SetLocation(torrent.Hash, targetLocation); err != nil {
			logger.Warn("Failed to move collection torrent location",
				"download_id", download.ID,
				"error", err.Error())
			// 移动失败不阻止重命名
		}
	}

	renamedCount := 0

	for _, vf := range videoFiles {
		ext := filepath.Ext(vf.file.Name)

		// 构建重命名上下文
		ctx := &RenameContext{
			Subscription: subscription,
			Download: &model.Download{
				Episode: vf.episode,
			},
			OriginalName: vf.file.Name,
			Extension:    ext,
			Resolution:   extractResolution(vf.file.Name),
		}

		// 生成新文件路径（完整路径，如: 剑来/Season 2/剑来 S02E01.mp4）
		newRelativePath := m.renameService.GenerateFileName(ctx)
		newFileName := filepath.Base(newRelativePath)

		oldFileName := vf.file.Name
		oldFileBaseName := filepath.Base(oldFileName)

		if oldFileName == newFileName {
			// 文件名相同，跳过
			renamedCount++
			continue
		}

		// 尝试将文件重命名到根目录（去掉原有子目录）
		if err := m.qbClient.RenameTorrentFile(torrent.Hash, oldFileName, newFileName); err != nil {
			// 如果是409错误(文件已存在)，不视为失败
			if strings.Contains(err.Error(), "409") {
				logger.Debug("File already renamed, skipping",
					"episode", vf.episode,
					"path", newFileName)
				renamedCount++
				continue
			}

			// 如果重命名到根目录失败，尝试保持原目录结构只改文件名
			if oldFileBaseName != newFileName {
				newFilePathWithDir := newFileName
				if strings.Contains(oldFileName, string(filepath.Separator)) || strings.Contains(oldFileName, "/") {
					oldDir := filepath.Dir(oldFileName)
					newFilePathWithDir = filepath.Join(oldDir, newFileName)
				}
				if err2 := m.qbClient.RenameTorrentFile(torrent.Hash, oldFileName, newFilePathWithDir); err2 != nil {
					if !strings.Contains(err2.Error(), "409") {
						logger.Warn("Failed to rename file in collection",
							"episode", vf.episode,
							"original", oldFileName,
							"target", newFilePathWithDir,
							"error", err2.Error())
						continue
					}
				}
				renamedCount++
				logger.Debug("Renamed collection file (kept directory structure)",
					"episode", vf.episode,
					"original", oldFileBaseName,
					"new", newFileName)
				continue
			}

			logger.Warn("Failed to rename file in collection",
				"episode", vf.episode,
				"original", oldFileName,
				"target", newFileName,
				"error", err.Error())
			continue
		}

		renamedCount++
		logger.Debug("Renamed collection file",
			"episode", vf.episode,
			"original", oldFileBaseName,
			"new", newFileName)
	}

	return renamedCount, nil
}

// extractEpisodeFromFilename 从文件名中提取集数
func extractEpisodeFromFilename(filename string) int {
	// 常见集数格式:
	// - 第12集, 12话, 12話
	// - E12, EP12, Ep.12
	// - S01E12, S1E12
	// - - 01, - 12 (常见于番剧标题后)
	// - [01], [12] (方括号内的数字)
	// - _01_, _12_ (下划线包围)
	patterns := []string{
		`第?\s*(\d+)\s*[集话話]`,        // 第12集, 12话
		`[Ee][Pp]?\.?\s*(\d+)`,      // E12, EP12, Ep.12
		`Episode\s*(\d+)`,           // Episode 12
		`[Ss]\d{1,2}[Ee](\d+)`,      // S01E12, S1E12
		`[\[\(](\d{2,3})[\]\)]`,     // [01], (12)
		`[\s\-_](\d{2})[\s\-_\[\.]`, // -01-, _12_, 空格01空格
		`[\s\-](\d{2})$`,            // 以-01或空格01结尾
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(filename)
		if len(matches) > 1 {
			episode, err := strconv.Atoi(matches[1])
			if err == nil && episode > 0 && episode < 1000 {
				return episode
			}
		}
	}

	return 0
}

// updateSubscriptionStats 更新订阅统计信息（使用事务）
func (m *DownloadMonitor) updateSubscriptionStats(subscription *model.Subscription, download *model.Download) {
	// 更新当前集数
	if download.Episode > subscription.CurrentEpisode {
		subscription.CurrentEpisode = download.Episode
	}

	// 更新最后下载时间
	now := time.Now()
	subscription.LastDownloadAt = &now

	// 使用事务保存到数据库
	err := m.db.Transaction(func(tx *gorm.DB) error {
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
	case strings.Contains(qbState, "error") || qbState == "missingFiles":
		return "failed"
	case qbState == "uploading" || strings.HasSuffix(qbState, "UP"):
		return "completed"
	case qbState == "downloading" || qbState == "forcedDL":
		// 仅把真实下载态归为 downloading。
		return "downloading"
	default:
		// 其他 DL 态（stalled/paused/queued/checking/meta 等）统一归为 stalled，
		// 避免前端把非活跃下载显示成 downloading。
		return "stalled"
	}
}

func isTorrentComplete(torrent *TorrentInfo) bool {
	if torrent == nil {
		return false
	}
	if torrent.Size > 0 && torrent.Downloaded >= torrent.Size {
		return true
	}
	return torrent.Progress >= 0.9999
}

func shouldSkipReconcileByGracePeriod(download *model.Download, now time.Time) bool {
	if download == nil {
		return true
	}
	return now.Sub(download.UpdatedAt) < ReconcileGracePeriod
}

// reconcileMissingDownloadingTasks 将已不在 qB 中的 downloading/stalled 任务回收为 failed，避免状态长期漂移。
func (m *DownloadMonitor) reconcileMissingDownloadingTasks(torrents []*TorrentInfo) {
	torrentHashSet := make(map[string]struct{}, len(torrents))
	for _, torrent := range torrents {
		hash := strings.ToLower(strings.TrimSpace(torrent.Hash))
		if hash != "" {
			torrentHashSet[hash] = struct{}{}
		}
	}

	downloadingTasks, _, err := m.downloadRepo.List(0, 10000, "downloading")
	if err != nil {
		logger.Error("Failed to list downloading tasks for reconciliation", "error", err.Error())
		return
	}
	stalledTasks, _, err := m.downloadRepo.List(0, 10000, "stalled")
	if err != nil {
		logger.Error("Failed to list stalled tasks for reconciliation", "error", err.Error())
		return
	}
	tasks := append(downloadingTasks, stalledTasks...)

	now := time.Now()
	reconciled := 0
	skippedGrace := 0

	for i := range tasks {
		download := &tasks[i]
		hash := strings.ToLower(strings.TrimSpace(download.TorrentHash))
		if hash == "" {
			continue
		}
		if _, exists := torrentHashSet[hash]; exists {
			continue
		}

		if shouldSkipReconcileByGracePeriod(download, now) {
			skippedGrace++
			continue
		}

		download.Status = "failed"
		download.ErrorMessage = "Torrent missing in qBittorrent during monitor reconciliation"
		if err := m.downloadRepo.Update(download); err != nil {
			logger.Error("Failed to reconcile missing downloading task",
				"download_id", download.ID,
				"hash", hash,
				"error", err.Error())
			continue
		}
		reconciled++
		// 发送下载失败通知
		if m.notificationSvc != nil {
			m.sendFailedNotification(download, download.ErrorMessage)
		}
	}

	if reconciled > 0 || skippedGrace > 0 {
		logger.Info("Download task reconciliation completed",
			"reconciled_to_failed", reconciled,
			"skipped_in_grace_period", skippedGrace,
			"grace_period", ReconcileGracePeriod.String(),
			"scanned_statuses", "downloading,stalled")
	}
}

// processFailedRetries 处理失败任务的重试
func (m *DownloadMonitor) processFailedRetries() {
	// 获取准备好重试的失败任务
	retryTasks, err := m.downloadRepo.GetFailedDownloadsReadyForRetry(10)
	if err != nil {
		logger.Error("Failed to get failed downloads for retry", "error", err.Error())
		return
	}

	if len(retryTasks) == 0 {
		return
	}

	logger.Info("Processing failed downloads for retry", "count", len(retryTasks))

	for i := range retryTasks {
		download := &retryTasks[i]

		// 再次检查是否应该重试
		shouldRetry, reason := m.retryService.ShouldRetry(download)
		if !shouldRetry {
			logger.Debug("Skipping retry for download",
				"download_id", download.ID,
				"reason", reason,
				"retry_count", download.RetryCount)
			continue
		}

		// 准备重试
		if err := m.retryService.PrepareRetry(download, "auto_retry"); err != nil {
			logger.Error("Failed to prepare download for retry",
				"download_id", download.ID,
				"error", err.Error())
			continue
		}

		logger.Info("Download queued for retry",
			"download_id", download.ID,
			"title", download.Title,
			"retry_count", download.RetryCount,
			"next_retry_at", download.NextRetryAt.Format("2006-01-02 15:04:05"))
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
