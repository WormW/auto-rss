package organizer

import (
	"context"
	"errors"
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
	ctx              context.Context
	cancel           context.CancelFunc
	stopOnce         sync.Once
	wg               sync.WaitGroup
	lifecycleMux     sync.Mutex
	stopped          bool
	stabilizeTime    time.Duration
	processing       map[string]bool
	procMux          sync.RWMutex
	scanMux          sync.Mutex
	scanRunning      bool
	scanOnStart      bool
	recoveryMux      sync.Mutex
	recoveryInterval time.Duration
	// New service interfaces
	parser         *FileNameParser
	matcher        SubscriptionMatcher
	mover          FileMover
	renameService  *downloader.RenameService
	mediaLibrary   *medialibrary.Service
	episodeService downloader.EpisodeCompletionService
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
	episodeService downloader.EpisodeCompletionService,
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

	ctx, cancel := context.WithCancel(context.Background())
	return &FileOrganizer{
		watchDir:         watchDir,
		destDir:          destDir,
		subscriptionRepo: subscriptionRepo,
		downloadRepo:     downloadRepo,
		db:               db,
		bangumiService:   bangumiService,
		watcher:          watcher,
		stopChan:         make(chan struct{}),
		ctx:              ctx,
		cancel:           cancel,
		stabilizeTime:    5 * time.Second,
		processing:       make(map[string]bool),
		scanOnStart:      false,
		recoveryInterval: time.Minute,
		parser:           parser,
		matcher:          matcher,
		mover:            mover,
		renameService:    downloader.NewRenameService(renameTemplate),
		mediaLibrary:     mediaSvc,
		episodeService:   episodeService,
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

	f.startTask(f.watchLoop)
	f.startTask(f.recoveryLoop)
	if f.scanOnStart {
		f.startTask(func() {
			select {
			case <-time.After(2 * time.Second):
				f.scanExistingFiles()
			case <-f.ctx.Done():
			}
		})
	}

	return nil
}

func (f *FileOrganizer) recoveryLoop() {
	if f.ctx.Err() != nil {
		return
	}
	f.recoverOrganizingDownloads()
	ticker := time.NewTicker(f.recoveryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			f.recoverOrganizingDownloads()
		case <-f.ctx.Done():
			return
		}
	}
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
	f.stopOnce.Do(func() {
		f.lifecycleMux.Lock()
		f.stopped = true
		f.cancel()
		if f.watcher != nil {
			_ = f.watcher.Close()
		}
		f.lifecycleMux.Unlock()
		f.wg.Wait()
	})
	logger.Info("File organizer stopped")
}

func (f *FileOrganizer) startTask(fn func()) bool {
	f.lifecycleMux.Lock()
	defer f.lifecycleMux.Unlock()
	if f.stopped || f.ctx.Err() != nil {
		return false
	}
	f.wg.Add(1)
	go func() { defer f.wg.Done(); fn() }()
	return true
}

// TriggerScan 手动触发文件扫描
func (f *FileOrganizer) TriggerScan() {
	logger.Info("Manual file scan triggered")
	f.startTask(f.scanExistingFiles)
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

		case <-f.ctx.Done():
			return
		}
	}
}

// scanExistingFiles 扫描现有文件
func (f *FileOrganizer) scanExistingFiles() {
	if f.ctx.Err() != nil {
		return
	}
	if !f.beginScan() {
		logger.Warn("File scan already running, skipping duplicate trigger", "watch_dir", f.watchDir)
		return
	}
	defer f.endScan()

	logger.Info("Scanning existing files in watch directory")

	err := filepath.Walk(f.watchDir, func(path string, info os.FileInfo, err error) error {
		if f.ctx.Err() != nil {
			return f.ctx.Err()
		}
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
	if f.ctx.Err() != nil {
		return
	}
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

	f.startTask(func() {
		select {
		case <-time.After(f.stabilizeTime):
		case <-f.ctx.Done():
			f.procMux.Lock()
			delete(f.processing, filePath)
			f.procMux.Unlock()
			return
		}

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
	})
}

// organizeFile 整理文件
func (f *FileOrganizer) organizeFile(filePath string) error {
	if err := f.ctx.Err(); err != nil {
		return err
	}
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

	newPath := f.generateNewPath(subscription, info)
	if newPath == "" {
		return fmt.Errorf("failed to generate new file path")
	}

	if _, err := utils.ValidatePath(newPath, f.destDir); err != nil {
		return fmt.Errorf("generated path escapes destination directory: %w", err)
	}
	f.recoveryMux.Lock()
	defer f.recoveryMux.Unlock()

	alreadyAtTarget := filePath == newPath
	if alreadyAtTarget && download != nil && download.Status == model.DownloadStatusCompleted &&
		download.FilePath == newPath && download.RenamedPath == newPath {
		return nil
	}
	if !alreadyAtTarget && f.mover.IsAlreadyOrganized(filePath, subscription) {
		logger.Debug("File already organized, skipping", "path", filePath)
		return nil
	}

	var originalDownload *model.Download
	if download != nil && f.downloadRepo != nil {
		if err := f.ctx.Err(); err != nil {
			return err
		}
		original := *download
		originalDownload = &original
		download.Status = model.DownloadStatusOrganizing
		download.FilePath = filePath
		download.RenamedPath = newPath
		if err := f.downloadRepo.Update(download); err != nil {
			*download = original
			return fmt.Errorf("failed to persist organizing checkpoint: %w", err)
		}
	}

	moved := false
	if !alreadyAtTarget {
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

		actualPath, err := f.mover.Move(filePath, newPath)
		if err != nil {
			f.procMux.Lock()
			delete(f.processing, newPath)
			f.procMux.Unlock()

			return fmt.Errorf("failed to move file: %w", err)
		}
		newPath = actualPath
		moved = true
	} else {
		logger.Debug("File already in correct location; completing persistence", "path", filePath)
	}

	if download != nil && f.downloadRepo != nil {
		if err := f.ctx.Err(); err != nil {
			return err
		}
		updateErr := f.completeOrganizingDownload(download, subscription, newPath)
		if updateErr != nil {
			var compensationErr error
			if moved {
				_, compensationErr = f.mover.Move(newPath, filePath)
				f.procMux.Lock()
				delete(f.processing, newPath)
				f.procMux.Unlock()
			}
			logger.Error("File moved but failed to update database",
				"download_id", download.ID,
				"new_path", newPath,
				"error", updateErr)
			persistErr := fmt.Errorf("failed to persist organized download: %w", updateErr)
			if compensationErr != nil {
				return errors.Join(persistErr, fmt.Errorf("failed to compensate file move from %s to %s: %w", newPath, filePath, compensationErr))
			}
			if originalDownload != nil {
				if restoreErr := f.downloadRepo.Update(originalDownload); restoreErr != nil {
					return errors.Join(persistErr, fmt.Errorf("failed to restore download after compensation: %w", restoreErr))
				}
				*download = *originalDownload
			}
			return persistErr
		} else {
			logger.Debug("Updated download status to completed",
				"download_id", download.ID,
				"file_path", newPath)
		}

	}

	if moved {
		go func() {
			time.Sleep(10 * time.Second)
			f.procMux.Lock()
			delete(f.processing, newPath)
			f.procMux.Unlock()
			logger.Debug("Cleared processing flag for moved file", "path", newPath)
		}()
	}

	logger.Info("File organized successfully",
		"original", filename,
		"new_path", newPath)

	return nil
}

func (f *FileOrganizer) completeOrganizingDownload(download *model.Download, subscription *model.Subscription, targetPath string) error {
	checkpoint := *download
	completedAt := time.Now()
	download.Status = model.DownloadStatusCompleted
	download.FilePath = targetPath
	download.RenamedPath = targetPath
	download.DownloadedAt = &completedAt
	var err error
	if f.db == nil {
		err = f.downloadRepo.Update(download)
		if err == nil && download.Episode > 0 && f.episodeService != nil {
			err = f.episodeService.MarkDownloadCompleted(download, subscription, completedAt)
		}
	} else {
		err = f.db.Transaction(func(tx *gorm.DB) error {
			if updateErr := f.downloadRepo.UpdateInTx(tx, download); updateErr != nil {
				return updateErr
			}
			if download.Episode > 0 && f.episodeService != nil {
				return f.episodeService.MarkDownloadCompletedInTx(tx, download, subscription, completedAt)
			}
			return nil
		})
	}
	if err != nil {
		*download = checkpoint
		return err
	}
	if f.mediaLibrary != nil {
		result := f.mediaLibrary.RefreshDownloadAfterImport(download)
		logger.Info("Media library refresh after file organization",
			"download_id", download.ID,
			"status", result.Status,
			"path", result.Path,
			"message", result.Message)
	}
	return nil
}

func (f *FileOrganizer) recoverOrganizingDownloads() {
	if f.ctx.Err() != nil {
		return
	}
	f.recoveryMux.Lock()
	defer f.recoveryMux.Unlock()
	if f.downloadRepo == nil {
		return
	}
	lastID := uint(0)
	for f.ctx.Err() == nil {
		var downloads []model.Download
		if f.db == nil {
			return
		}
		err := f.db.Preload("Subscription").Where("status = ? AND id > ?", model.DownloadStatusOrganizing, lastID).
			Order("id ASC").Limit(500).Find(&downloads).Error
		if err != nil {
			logger.Error("Failed to list organizing checkpoints", "error", err)
			return
		}
		if len(downloads) == 0 {
			return
		}
		for i := range downloads {
			lastID = downloads[i].ID
			if err := f.recoverOrganizingDownload(&downloads[i]); err != nil {
				logger.Warn("Failed to recover organizing checkpoint",
					"download_id", downloads[i].ID, "source", downloads[i].FilePath,
					"target", downloads[i].RenamedPath, "error", err)
			}
		}
	}
}

func (f *FileOrganizer) recoverOrganizingDownload(download *model.Download) error {
	sourcePath := strings.TrimSpace(download.FilePath)
	targetPath := strings.TrimSpace(download.RenamedPath)
	if sourcePath == "" || targetPath == "" {
		return fmt.Errorf("organizing checkpoint is missing source or target path")
	}
	if _, err := utils.ValidatePath(sourcePath, f.watchDir); err != nil {
		return fmt.Errorf("checkpoint source escapes watch directory: %w", err)
	}
	if _, err := utils.ValidatePath(targetPath, f.destDir); err != nil {
		return fmt.Errorf("checkpoint target escapes destination directory: %w", err)
	}
	targetInfo, targetErr := os.Stat(targetPath)
	sourceInfo, sourceErr := os.Stat(sourcePath)
	targetExists := targetErr == nil && !targetInfo.IsDir()
	sourceExists := sourceErr == nil && !sourceInfo.IsDir()
	if targetExists && sourceExists && !os.SameFile(sourceInfo, targetInfo) {
		return fmt.Errorf("organizing checkpoint conflict: source and target both exist")
	}
	if !targetExists {
		if !sourceExists {
			return fmt.Errorf("neither checkpoint source nor target exists")
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return fmt.Errorf("failed to create checkpoint target directory: %w", err)
		}
		actualPath, err := f.mover.Move(sourcePath, targetPath)
		if err != nil {
			return fmt.Errorf("failed to resume checkpoint move: %w", err)
		}
		targetPath = actualPath
	}
	if err := f.ctx.Err(); err != nil {
		return err
	}
	subscription := &download.Subscription
	if subscription.ID == 0 {
		loaded, err := f.subscriptionRepo.GetByID(download.SubscriptionID)
		if err != nil {
			return fmt.Errorf("failed to load checkpoint subscription: %w", err)
		}
		subscription = loaded
	}
	if err := f.completeOrganizingDownload(download, subscription, targetPath); err != nil {
		return fmt.Errorf("failed to complete organizing checkpoint: %w", err)
	}
	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
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
