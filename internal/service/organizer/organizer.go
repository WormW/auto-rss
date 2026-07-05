package organizer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/pkg/utils"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/bangumi"
	"github.com/WormW/auto-rss/internal/service/downloader"
	"github.com/WormW/auto-rss/internal/service/medialibrary"
	"github.com/fsnotify/fsnotify"
	"gorm.io/gorm"
)

// FileOrganizer 文件整理服务
type FileOrganizer struct {
	watchDir         string
	destDir          string
	subscriptionRepo repository.SubscriptionRepository
	downloadRepo     repository.DownloadRepository
	db               *gorm.DB
	bangumiService   *bangumi.BangumiService
	watcher          *fsnotify.Watcher
	stopChan         chan struct{}
	stabilizeTime    time.Duration
	processing       map[string]bool
	procMux          sync.RWMutex
	scanMux          sync.Mutex
	scanRunning      bool
	scanOnStart      bool
	// New service interfaces
	parser        *FileNameParser
	matcher       SubscriptionMatcher
	mover         FileMover
	renameService *downloader.RenameService
	mediaLibrary  *medialibrary.Service
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
	mediaLibrarySvc ...*medialibrary.Service,
) (*FileOrganizer, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	parser := NewFileNameParser()
	matcher := NewSubscriptionMatcher(parser, subscriptionRepo, bangumiService)
	mover := NewFileMover()

	var mediaSvc *medialibrary.Service
	if len(mediaLibrarySvc) > 0 {
		mediaSvc = mediaLibrarySvc[0]
	}

	return &FileOrganizer{
		watchDir:         watchDir,
		destDir:          destDir,
		subscriptionRepo: subscriptionRepo,
		downloadRepo:     downloadRepo,
		db:               db,
		bangumiService:   bangumiService,
		watcher:          watcher,
		stopChan:         make(chan struct{}),
		stabilizeTime:    5 * time.Second,
		processing:       make(map[string]bool),
		scanOnStart:      false,
		parser:           parser,
		matcher:          matcher,
		mover:            mover,
		renameService:    downloader.NewRenameService(renameTemplate),
		mediaLibrary:     mediaSvc,
	}, nil
}

// Start 启动文件监控服务
func (f *FileOrganizer) Start() error {
	if err := f.addWatchRecursively(f.watchDir); err != nil {
		return fmt.Errorf("failed to add watch directory: %w", err)
	}

	logger.Info("File organizer started",
		"watch_dir", f.watchDir,
		"dest_dir", f.destDir,
		"stabilize_time", f.stabilizeTime,
		"scan_on_start", f.scanOnStart)

	go f.watchLoop()
	if f.scanOnStart {
		go func() {
			time.Sleep(2 * time.Second)
			f.scanExistingFiles()
		}()
	}

	return nil
}

func (f *FileOrganizer) SetScanOnStart(enabled bool) {
	f.scanOnStart = enabled
}

// addWatchRecursively 递归添加监控目录
func (f *FileOrganizer) addWatchRecursively(dir string) error {
	if err := f.watcher.Add(dir); err != nil {
		return err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			subDir := filepath.Join(dir, entry.Name())
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

			if event.Op&fsnotify.Create == fsnotify.Create {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					if err := f.watcher.Add(event.Name); err != nil {
						logger.Warn("Failed to add new directory to watch",
							"dir", event.Name,
							"error", err)
					} else {
						logger.Debug("Added new directory to watch", "dir", event.Name)
					}
				}
			}

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
	if !f.beginScan() {
		logger.Warn("File scan already running, skipping duplicate trigger", "watch_dir", f.watchDir)
		return
	}
	defer f.endScan()

	logger.Info("Scanning existing files in watch directory")

	err := filepath.Walk(f.watchDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if f.mover.IsVideoFile(path) {
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

func (f *FileOrganizer) beginScan() bool {
	f.scanMux.Lock()
	defer f.scanMux.Unlock()
	if f.scanRunning {
		return false
	}
	f.scanRunning = true
	return true
}

func (f *FileOrganizer) endScan() {
	f.scanMux.Lock()
	f.scanRunning = false
	f.scanMux.Unlock()
}

// handleNewFile 处理新文件
func (f *FileOrganizer) handleNewFile(filePath string) {
	if !f.mover.IsVideoFile(filePath) {
		return
	}

	f.procMux.RLock()
	if f.processing[filePath] {
		f.procMux.RUnlock()
		return
	}
	f.procMux.RUnlock()

	logger.Debug("New file detected", "path", filePath)

	f.procMux.Lock()
	f.processing[filePath] = true
	f.procMux.Unlock()

	go func() {
		time.Sleep(f.stabilizeTime)

		if !f.mover.IsFileReady(filePath) {
			logger.Debug("File not ready or no longer exists", "path", filePath)
			f.procMux.Lock()
			delete(f.processing, filePath)
			f.procMux.Unlock()
			return
		}

		if err := f.organizeFile(filePath); err != nil {
			logger.Error("Failed to organize file", "path", filePath, "error", err)
		}

		f.procMux.Lock()
		delete(f.processing, filePath)
		f.procMux.Unlock()
	}()
}

// organizeFile 整理文件
func (f *FileOrganizer) organizeFile(filePath string) error {
	logger.Info("Organizing file", "path", filePath)

	if _, err := utils.ValidatePath(filePath, f.watchDir); err != nil {
		return fmt.Errorf("source file path escapes watch directory: %w", err)
	}

	filename := filepath.Base(filePath)
	info := f.parser.Parse(filename)

	logger.Debug("Parsed file info",
		"title", info.Title,
		"episode", info.Episode,
		"fansub", info.Fansub,
		"resolution", info.Resolution)

	subscription, matchScore := f.matcher.Match(info)
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

	var download *model.Download
	if f.downloadRepo != nil {
		downloads, err := f.downloadRepo.GetBySubscriptionAndEpisodeWithLang(subscription.ID, info.Episode)
		if err == nil && len(downloads) > 0 {
			download = &downloads[0]
			logger.Debug("Found existing download record",
				"download_id", download.ID,
				"current_status", download.Status)
		}
	}

	if download != nil && f.downloadRepo != nil {
		download.Status = model.DownloadStatusOrganizing
		if err := f.downloadRepo.Update(download); err != nil {
			logger.Error("Failed to set organizing status",
				"download_id", download.ID,
				"error", err)
		} else {
			logger.Debug("Set download status to organizing", "download_id", download.ID)
		}
	}

	newPath := f.generateNewPath(subscription, info)
	if newPath == "" {
		return fmt.Errorf("failed to generate new file path")
	}

	if _, err := utils.ValidatePath(newPath, f.destDir); err != nil {
		return fmt.Errorf("generated path escapes destination directory: %w", err)
	}

	if filePath == newPath {
		logger.Debug("File already in correct location, skipping", "path", filePath)
		return nil
	}

	if f.mover.IsAlreadyOrganized(filePath, subscription) {
		logger.Debug("File already organized, skipping", "path", filePath)
		return nil
	}

	targetDir := filepath.Dir(newPath)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	logger.Info("Moving file",
		"from", filePath,
		"to", newPath)

	f.procMux.Lock()
	f.processing[newPath] = true
	f.procMux.Unlock()

	if err := f.mover.Move(filePath, newPath); err != nil {
		f.procMux.Lock()
		delete(f.processing, newPath)
		f.procMux.Unlock()

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

	if download != nil && f.downloadRepo != nil {
		download.Status = model.DownloadStatusCompleted
		download.FilePath = newPath
		download.RenamedPath = newPath
		if err := f.downloadRepo.Update(download); err != nil {
			logger.Error("File moved but failed to update database",
				"download_id", download.ID,
				"new_path", newPath,
				"error", err)
		} else {
			logger.Debug("Updated download status to completed",
				"download_id", download.ID,
				"file_path", newPath)
		}

		if f.mediaLibrary != nil {
			result := f.mediaLibrary.RefreshDownloadAfterImport(download)
			logger.Info("Media library refresh after file organization",
				"download_id", download.ID,
				"status", result.Status,
				"path", result.Path,
				"message", result.Message)
		}
	}

	go func() {
		time.Sleep(10 * time.Second)
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

// generateNewPath 生成新文件路径
func (f *FileOrganizer) generateNewPath(subscription *model.Subscription, info *FileNameInfo) string {
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

	destDirBase := filepath.Base(f.destDir)
	sanitizedTitle := sanitizeDirectoryName(subscription.Name)

	if isSimilarDirectoryName(destDirBase, sanitizedTitle) {
		parts := strings.Split(filepath.Clean(newFileName), string(filepath.Separator))
		if len(parts) > 1 && isSimilarDirectoryName(parts[0], sanitizedTitle) {
			newFileName = filepath.Join(parts[1:]...)
			logger.Debug("Removed duplicate title directory from path",
				"destDir", f.destDir,
				"original_path", ctx.OriginalName,
				"adjusted_path", newFileName)
		}
	}

	fullPath := filepath.Join(f.destDir, newFileName)
	return fullPath
}
