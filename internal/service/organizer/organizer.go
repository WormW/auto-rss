package organizer

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/pkg/utils"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/bangumi"
	"github.com/WormW/auto-rss/internal/service/downloader"
	"github.com/fsnotify/fsnotify"
	"gorm.io/gorm"
)

// FileOrganizer 文件整理服务
type FileOrganizer struct {
	watchDir         string                              // 监控目录
	destDir          string                              // 目标目录
	parser           *FileNameParser                     // 文件名解析器
	renameService    *downloader.RenameService           // 重命名服务
	subscriptionRepo repository.SubscriptionRepository   // 订阅仓库
	downloadRepo     repository.DownloadRepository       // 下载仓库
	db               *gorm.DB                            // 数据库连接
	bangumiService   *bangumi.BangumiService             // Bangumi API 服务
	watcher          *fsnotify.Watcher                   // 文件监控器
	stopChan         chan struct{}                       // 停止信号
	minMatchScore    float64                             // 最小匹配分数
	stabilizeTime    time.Duration                       // 文件稳定时间（避免处理未完成的文件）
	processing       map[string]bool                     // 正在处理的文件
	procMux          sync.RWMutex                        // 保护 processing map 的互斥锁
}

// NewFileOrganizer 创建文件整理服务
func NewFileOrganizer(
	watchDir string,
	destDir string,
	subscriptionRepo repository.SubscriptionRepository,
	downloadRepo repository.DownloadRepository,
	db *gorm.DB,
	bangumiService *bangumi.BangumiService,
	renameTemplate string,
) (*FileOrganizer, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	return &FileOrganizer{
		watchDir:         watchDir,
		destDir:          destDir,
		parser:           NewFileNameParser(),
		renameService:    downloader.NewRenameService(renameTemplate),
		subscriptionRepo: subscriptionRepo,
		downloadRepo:     downloadRepo,
		db:               db,
		bangumiService:   bangumiService,
		watcher:          watcher,
		stopChan:         make(chan struct{}),
		minMatchScore:    0.7,             // 70% 相似度
		stabilizeTime:    5 * time.Second, // 等待5秒确保文件写入完成
		processing:       make(map[string]bool),
	}, nil
}

// Start 启动文件监控服务
func (f *FileOrganizer) Start() error {
	// 添加监控目录（递归）
	if err := f.addWatchRecursively(f.watchDir); err != nil {
		return fmt.Errorf("failed to add watch directory: %w", err)
	}

	logger.Info("File organizer started",
		"watch_dir", f.watchDir,
		"dest_dir", f.destDir,
		"stabilize_time", f.stabilizeTime)

	go f.watchLoop()

	// 启动时扫描一次现有文件
	go func() {
		time.Sleep(2 * time.Second)
		f.scanExistingFiles()
	}()

	return nil
}

// addWatchRecursively 递归添加监控目录
func (f *FileOrganizer) addWatchRecursively(dir string) error {
	// 添加当前目录
	if err := f.watcher.Add(dir); err != nil {
		return err
	}

	// 遍历子目录
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			subDir := filepath.Join(dir, entry.Name())
			// 递归添加子目录
			if err := f.addWatchRecursively(subDir); err != nil {
				logger.Warn("Failed to add watch for subdirectory",
					"dir", subDir,
					"error", err)
			}
		}
	}

	return nil
}

// Stop 停止文件监控服务
func (f *FileOrganizer) Stop() {
	close(f.stopChan)
	if f.watcher != nil {
		f.watcher.Close()
	}
	logger.Info("File organizer stopped")
}

// TriggerScan 手动触发文件扫描
func (f *FileOrganizer) TriggerScan() {
	logger.Info("Manual file scan triggered")
	go f.scanExistingFiles()
}

// watchLoop 监控循环
func (f *FileOrganizer) watchLoop() {
	for {
		select {
		case event, ok := <-f.watcher.Events:
			if !ok {
				return
			}

			// 处理新创建的目录（如番剧下载目录）
			if event.Op&fsnotify.Create == fsnotify.Create {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					// 添加新目录到监控列表
					if err := f.watcher.Add(event.Name); err != nil {
						logger.Warn("Failed to add new directory to watch",
							"dir", event.Name,
							"error", err)
					} else {
						logger.Debug("Added new directory to watch", "dir", event.Name)
					}
				}
			}

			// 只处理创建和写入事件（文件）
			if event.Op&fsnotify.Create == fsnotify.Create ||
				event.Op&fsnotify.Write == fsnotify.Write {
				f.handleNewFile(event.Name)
			}

		case err, ok := <-f.watcher.Errors:
			if !ok {
				return
			}
			logger.Error("File watcher error", "error", err)

		case <-f.stopChan:
			return
		}
	}
}

// scanExistingFiles 扫描现有文件
func (f *FileOrganizer) scanExistingFiles() {
	logger.Info("Scanning existing files in watch directory")

	err := filepath.Walk(f.watchDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 跳过目录
		if info.IsDir() {
			return nil
		}

		// 处理视频文件
		if f.isVideoFile(path) {
			f.handleNewFile(path)
		}

		return nil
	})

	if err != nil {
		logger.Error("Failed to scan existing files", "error", err)
	} else {
		logger.Info("Existing files scan completed")
	}
}

// handleNewFile 处理新文件
func (f *FileOrganizer) handleNewFile(filePath string) {
	// 检查是否是视频文件
	if !f.isVideoFile(filePath) {
		return
	}

	// 防止重复处理 - 使用读锁检查
	f.procMux.RLock()
	if f.processing[filePath] {
		f.procMux.RUnlock()
		return
	}
	f.procMux.RUnlock()

	logger.Debug("New file detected", "path", filePath)

	// 标记为正在处理 - 使用写锁
	f.procMux.Lock()
	f.processing[filePath] = true
	f.procMux.Unlock()

	// 异步处理文件（等待文件稳定后再处理）
	go func() {
		// 等待文件写入完成
		time.Sleep(f.stabilizeTime)

		// 验证文件是否仍然存在且可访问
		if !f.isFileReady(filePath) {
			logger.Debug("File not ready or no longer exists", "path", filePath)
			f.procMux.Lock()
			delete(f.processing, filePath)
			f.procMux.Unlock()
			return
		}

		// 处理文件
		if err := f.organizeFile(filePath); err != nil {
			logger.Error("Failed to organize file", "path", filePath, "error", err)
		}

		// 移除处理标记
		f.procMux.Lock()
		delete(f.processing, filePath)
		f.procMux.Unlock()
	}()
}

// organizeFile 整理文件
func (f *FileOrganizer) organizeFile(filePath string) error {
	logger.Info("Organizing file", "path", filePath)

	// 验证源文件路径在监控目录内（防止路径遍历）
	if _, err := utils.ValidatePath(filePath, f.watchDir); err != nil {
		return fmt.Errorf("source file path escapes watch directory: %w", err)
	}

	// 1. 解析文件名
	filename := filepath.Base(filePath)
	info := f.parser.Parse(filename)

	logger.Debug("Parsed file info",
		"title", info.Title,
		"episode", info.Episode,
		"fansub", info.Fansub,
		"resolution", info.Resolution)

	// 2. 匹配订阅
	subscription, matchScore := f.findMatchingSubscription(info)
	if subscription == nil {
		logger.Warn("No matching subscription found",
			"file", filename,
			"parsed_title", info.Title)
		return fmt.Errorf("no matching subscription found")
	}

	logger.Info("Matched subscription",
		"subscription", subscription.Name,
		"match_score", matchScore,
		"file", filename)

	// 3. 查找下载记录
	var download *model.Download
	if subscription != nil && f.downloadRepo != nil {
		downloads, err := f.downloadRepo.GetBySubscriptionAndEpisodeWithLang(subscription.ID, info.Episode)
		if err == nil && len(downloads) > 0 {
			download = &downloads[0] // Use first match
			logger.Debug("Found existing download record",
				"download_id", download.ID,
				"current_status", download.Status)
		}
	}

	// 4. 状态机：更新数据库为 organizing 状态（先数据库，后文件）
	if download != nil && f.downloadRepo != nil {
		download.Status = model.DownloadStatusOrganizing
		if err := f.downloadRepo.Update(download); err != nil {
			logger.Error("Failed to set organizing status",
				"download_id", download.ID,
				"error", err)
			// Continue anyway - we can still organize the file
		} else {
			logger.Debug("Set download status to organizing", "download_id", download.ID)
		}
	}

	// 4. 生成新文件名和路径
	newPath := f.generateNewPath(subscription, info)
	if newPath == "" {
		return fmt.Errorf("failed to generate new file path")
	}

	// 验证目标路径在目的目录内（防止路径遍历）
	if _, err := utils.ValidatePath(newPath, f.destDir); err != nil {
		return fmt.Errorf("generated path escapes destination directory: %w", err)
	}

	// 3.5. 检查是否需要移动（避免重复处理已整理的文件）
	if filePath == newPath {
		logger.Debug("File already in correct location, skipping", "path", filePath)
		return nil
	}

	// 检查文件名是否已经符合规范格式
	if f.isAlreadyOrganized(filePath, subscription) {
		logger.Debug("File already organized, skipping", "path", filePath)
		return nil
	}

	// 4. 创建目标目录
	targetDir := filepath.Dir(newPath)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	// 5. 移动文件
	logger.Info("Moving file",
		"from", filePath,
		"to", newPath)

	// 先将目标路径标记为正在处理，防止移动后触发新事件导致死循环
	f.procMux.Lock()
	f.processing[newPath] = true
	f.procMux.Unlock()

	if err := f.moveFile(filePath, newPath); err != nil {
		f.procMux.Lock()
		delete(f.processing, newPath) // 移动失败，清除标记
		f.procMux.Unlock()

		// 状态机：更新下载状态为 failed
		if download != nil && f.downloadRepo != nil {
			download.Status = model.DownloadStatusFailed
			download.LastError = err.Error()
			if updateErr := f.downloadRepo.Update(download); updateErr != nil {
				logger.Error("Failed to update download status to failed",
					"download_id", download.ID,
					"error", updateErr)
			}
		}

		return fmt.Errorf("failed to move file: %w", err)
	}

	// 状态机：移动成功，更新下载状态为 completed
	if download != nil && f.downloadRepo != nil {
		download.Status = model.DownloadStatusCompleted
		download.FilePath = newPath
		if err := f.downloadRepo.Update(download); err != nil {
			logger.Error("File moved but failed to update database",
				"download_id", download.ID,
				"new_path", newPath,
				"error", err)
			// File moved but DB update failed — log for manual recovery
		} else {
			logger.Debug("Updated download status to completed",
				"download_id", download.ID,
				"file_path", newPath)
		}
	}

	// 移动成功后，设置一个延迟清除标记的定时器
	go func() {
		time.Sleep(10 * time.Second) // 10秒后清除标记
		f.procMux.Lock()
		delete(f.processing, newPath)
		f.procMux.Unlock()
		logger.Debug("Cleared processing flag for moved file", "path", newPath)
	}()

	logger.Info("File organized successfully",
		"original", filename,
		"new_path", newPath)

	return nil
}

// findMatchingSubscription 查找匹配的订阅
func (f *FileOrganizer) findMatchingSubscription(info *FileNameInfo) (*model.Subscription, float64) {
	// 获取所有订阅
	subscriptions, _, err := f.subscriptionRepo.List(0, 10000)
	if err != nil {
		logger.Error("Failed to list subscriptions", "error", err)
		return nil, 0
	}

	var bestMatch *model.Subscription
	var bestScore float64

	// 首先尝试本地匹配
	for _, sub := range subscriptions {
		// 计算标题相似度
		score := f.parser.MatchTitle(info.Title, sub.Name)

		// 如果有字幕组信息，且订阅也指定了字幕组，则额外加分
		if info.Fansub != "" && sub.Fansub != "" {
			if strings.EqualFold(info.Fansub, sub.Fansub) {
				score += 0.1 // 字幕组匹配加10分
			}
		}

		// 更新最佳匹配
		if score > bestScore {
			bestScore = score
			bestMatch = &sub
		}
	}

	// 如果本地匹配成功，直接返回
	if bestScore >= f.minMatchScore {
		logger.Debug("Local match successful",
			"file_title", info.Title,
			"subscription_name", bestMatch.Name,
			"score", bestScore)
		return bestMatch, bestScore
	}

	// 本地匹配失败，尝试通过 Bangumi API 查询
	logger.Info("Local match failed, trying Bangumi API",
		"file_title", info.Title,
		"best_score", bestScore)

	if f.bangumiService != nil {
		subject, err := f.bangumiService.SearchByName(info.Title)
		if err != nil {
			logger.Warn("Failed to search Bangumi",
				"title", info.Title,
				"error", err)
		} else if subject != nil {
			logger.Info("Found Bangumi match",
				"file_title", info.Title,
				"bangumi_id", subject.ID,
				"bangumi_name", subject.Name,
				"bangumi_name_cn", subject.NameCN)

			// 使用 Bangumi ID 或中文名再次匹配订阅
			for _, sub := range subscriptions {
				// 优先通过 Bangumi ID 匹配
				if sub.BangumiID == subject.ID {
					logger.Info("Matched by Bangumi ID",
						"subscription_name", sub.Name,
						"bangumi_id", subject.ID)
					return &sub, 1.0
				}

				// 如果 ID 不匹配，尝试匹配中文名或日文名
				cnScore := f.parser.MatchTitle(subject.NameCN, sub.Name)
				jpScore := f.parser.MatchTitle(subject.Name, sub.Name)
				maxScore := cnScore
				if jpScore > maxScore {
					maxScore = jpScore
				}

				if maxScore > bestScore {
					bestScore = maxScore
					bestMatch = &sub
				}
			}

			if bestScore >= f.minMatchScore {
				logger.Info("Matched by Bangumi name",
					"subscription_name", bestMatch.Name,
					"score", bestScore)
				return bestMatch, bestScore
			}
		}
	}

	// 仍然未匹配成功
	logger.Warn("No matching subscription found",
		"file_title", info.Title,
		"best_score", bestScore)
	return nil, bestScore
}

// generateNewPath 生成新文件路径
func (f *FileOrganizer) generateNewPath(subscription *model.Subscription, info *FileNameInfo) string {
	// 使用重命名服务生成文件名
	ctx := &downloader.RenameContext{
		Subscription: subscription,
		Download: &model.Download{
			Episode: info.Episode,
		},
		OriginalName: info.OriginalName,
		Extension:    info.Extension,
		Resolution:   info.Resolution,
	}

	newFileName := f.renameService.GenerateFileName(ctx)

	// 检查是否需要移除重复的番剧名目录
	// 如果 destDir 已经以番剧名结尾，而 newFileName 也以番剧名开头
	// 则移除 newFileName 中的第一层目录，避免重复
	destDirBase := filepath.Base(f.destDir)
	sanitizedTitle := sanitizeDirectoryName(subscription.Name)

	// 检查 destDir 最后一层是否与番剧名相似
	if isSimilarDirectoryName(destDirBase, sanitizedTitle) {
		// destDir 已经包含番剧名，移除 newFileName 的第一层目录
		parts := strings.Split(filepath.Clean(newFileName), string(filepath.Separator))
		if len(parts) > 1 && isSimilarDirectoryName(parts[0], sanitizedTitle) {
			// 移除第一层目录
			newFileName = filepath.Join(parts[1:]...)
			logger.Debug("Removed duplicate title directory from path",
				"destDir", f.destDir,
				"original_path", ctx.OriginalName,
				"adjusted_path", newFileName)
		}
	}

	// 拼接完整路径
	fullPath := filepath.Join(f.destDir, newFileName)

	return fullPath
}

// sanitizeDirectoryName 清理目录名（移除非法字符）
func sanitizeDirectoryName(name string) string {
	// 移除或替换文件系统不允许的字符
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, ":", "_")
	name = strings.ReplaceAll(name, "*", "_")
	name = strings.ReplaceAll(name, "?", "_")
	name = strings.ReplaceAll(name, "\"", "_")
	name = strings.ReplaceAll(name, "<", "_")
	name = strings.ReplaceAll(name, ">", "_")
	name = strings.ReplaceAll(name, "|", "_")
	return strings.TrimSpace(name)
}

// isSimilarDirectoryName 检查两个目录名是否相似（用于避免重复）
func isSimilarDirectoryName(name1, name2 string) bool {
	// 标准化：转小写，移除特殊字符
	normalize := func(s string) string {
		s = strings.ToLower(s)
		var result strings.Builder
		for _, r := range s {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				result.WriteRune(r)
			}
		}
		return result.String()
	}

	normalized1 := normalize(name1)
	normalized2 := normalize(name2)

	// 完全相同
	if normalized1 == normalized2 {
		return true
	}

	// 如果长度差异太大（>30%），认为不相似
	maxLen := len(normalized1)
	if len(normalized2) > maxLen {
		maxLen = len(normalized2)
	}
	if maxLen == 0 {
		return false
	}

	lenDiff := float64(abs(len(normalized1)-len(normalized2))) / float64(maxLen)
	if lenDiff > 0.3 {
		return false
	}

	// 检查包含关系
	return strings.Contains(normalized1, normalized2) || strings.Contains(normalized2, normalized1)
}

// abs 返回整数的绝对值
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// moveFile 移动文件
func (f *FileOrganizer) moveFile(src, dest string) error {
	// 如果目标文件已存在，添加后缀
	if _, err := os.Stat(dest); err == nil {
		// 文件已存在，添加时间戳后缀
		ext := filepath.Ext(dest)
		base := strings.TrimSuffix(dest, ext)
		timestamp := time.Now().Format("20060102_150405")
		dest = fmt.Sprintf("%s_%s%s", base, timestamp, ext)

		logger.Warn("Target file already exists, using new name", "new_path", dest)
	}

	// 尝试重命名（如果在同一文件系统上）
	err := os.Rename(src, dest)
	if err == nil {
		return nil
	}

	// 如果重命名失败（跨文件系统），则复制后删除
	logger.Debug("Rename failed, trying copy + delete", "error", err)

	if err := f.copyFile(src, dest); err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}

	if err := os.Remove(src); err != nil {
		logger.Warn("Failed to remove source file after copy", "path", src, "error", err)
		// 不返回错误，因为文件已成功复制
	}

	return nil
}

// copyFile 复制文件
func (f *FileOrganizer) copyFile(src, dest string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer destFile.Close()

	if _, err := destFile.ReadFrom(sourceFile); err != nil {
		return err
	}

	// 复制文件权限
	sourceInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	return os.Chmod(dest, sourceInfo.Mode())
}

// isVideoFile 检查是否是视频文件
func (f *FileOrganizer) isVideoFile(filePath string) bool {
	videoExts := []string{".mkv", ".mp4", ".avi", ".flv", ".ts", ".m2ts", ".mov", ".wmv"}
	ext := strings.ToLower(filepath.Ext(filePath))

	for _, videoExt := range videoExts {
		if ext == videoExt {
			return true
		}
	}

	return false
}

// isFileReady 检查文件是否准备好（完整写入且可访问）
func (f *FileOrganizer) isFileReady(filePath string) bool {
	// 检查文件是否存在
	info, err := os.Stat(filePath)
	if err != nil {
		return false
	}

	// 检查文件大小是否稳定（两次检查间隔1秒，大小不变）
	size1 := info.Size()
	time.Sleep(1 * time.Second)

	info, err = os.Stat(filePath)
	if err != nil {
		return false
	}

	size2 := info.Size()

	// 如果大小发生变化，说明文件仍在写入
	if size1 != size2 {
		return false
	}

	// 尝试打开文件（检查是否被锁定）
	file, err := os.Open(filePath)
	if err != nil {
		return false
	}
	file.Close()

	return true
}

// isAlreadyOrganized 检查文件是否已经被整理过（匹配规范格式）
func (f *FileOrganizer) isAlreadyOrganized(filePath string, subscription *model.Subscription) bool {
	filename := filepath.Base(filePath)

	// 检查文件名是否匹配规范格式模式
	// 规范格式示例: "番剧名 S01E01.mkv"
	// 使用正则表达式检查

	// 清理番剧名用于匹配
	sanitizedTitle := sanitizeDirectoryName(subscription.Name)

	// 检查文件名是否以番剧名开头，并包含 SxxExx 格式
	if !strings.Contains(strings.ToLower(filename), strings.ToLower(sanitizedTitle)) {
		return false
	}

	// 检查是否包含季集格式 (S01E01, S1E1等)
	seasonEpisodePattern := `[Ss]\d{1,2}[Ee]\d{1,2}`
	matched, err := regexp.MatchString(seasonEpisodePattern, filename)
	if err != nil {
		logger.Error("Failed to match season/episode pattern", "error", err)
		return false
	}

	if !matched {
		return false
	}

	// 检查文件是否在 Season 目录下
	parentDir := filepath.Base(filepath.Dir(filePath))
	if strings.HasPrefix(strings.ToLower(parentDir), "season") {
		logger.Debug("File appears to be already organized",
			"path", filePath,
			"parent_dir", parentDir)
		return true
	}

	return false
}
