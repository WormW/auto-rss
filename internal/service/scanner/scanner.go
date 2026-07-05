package scanner

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/constants"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/pkg/utils"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/downloader"
	"github.com/WormW/auto-rss/internal/service/organizer"
	"gorm.io/gorm"
)

// videoExts 支持的视频扩展名
var videoExts = map[string]bool{
	".mp4": true, ".mkv": true, ".avi": true, ".wmv": true,
	".mov": true, ".flv": true, ".webm": true, ".m4v": true,
	".ts": true, ".m2ts": true,
}

// Scanner 文件夹扫描服务
type Scanner struct {
	db               *gorm.DB
	subscriptionRepo repository.SubscriptionRepository
	downloadRepo     repository.DownloadRepository
	configRepo       repository.ConfigRepository
	parser           *organizer.FileNameParser
	defaultRoot      string
}

// NewScanner 创建扫描服务
func NewScanner(
	db *gorm.DB,
	subscriptionRepo repository.SubscriptionRepository,
	downloadRepo repository.DownloadRepository,
	configRepo repository.ConfigRepository,
	defaultRoot ...string,
) *Scanner {
	root := constants.DefaultDownloadPath
	if len(defaultRoot) > 0 && strings.TrimSpace(defaultRoot[0]) != "" {
		root = defaultRoot[0]
	}
	return &Scanner{
		db:               db,
		subscriptionRepo: subscriptionRepo,
		downloadRepo:     downloadRepo,
		configRepo:       configRepo,
		parser:           organizer.NewFileNameParser(),
		defaultRoot:      root,
	}
}

// Request 扫描请求
type Request struct {
	FolderPath  string `json:"folder_path"`
	DryRun      bool   `json:"dry_run"`
	RenameFiles bool   `json:"rename_files"`
}

// FileEntry 单个扫描到的文件
type FileEntry struct {
	Path        string  `json:"path"`
	Episode     int     `json:"episode"`
	Season      int     `json:"season"`
	Resolution  string  `json:"resolution"`
	VideoCodec  string  `json:"video_codec"`
	Fansub      string  `json:"fansub"`
	Language    string  `json:"language"`
	SizeMB      float64 `json:"size_mb"`
	RenameTo    string  `json:"rename_to,omitempty"`
	WillRename  bool    `json:"will_rename"`
	Renamed     bool    `json:"renamed,omitempty"`
	RenameError string  `json:"rename_error,omitempty"`
}

// Stats 扫描统计
type Stats struct {
	TotalSizeGB         float64        `json:"total_size_gb"`
	ResolutionBreakdown map[string]int `json:"resolution_breakdown"`
	CodecBreakdown      map[string]int `json:"codec_breakdown"`
}

// Result 扫描结果
type Result struct {
	SubscriptionID   uint        `json:"subscription_id"`
	SubscriptionName string      `json:"subscription_name"`
	Season           int         `json:"season"`
	Folder           string      `json:"folder"`
	Scanned          int         `json:"scanned"`
	Matched          int         `json:"matched"`
	Orphan           int         `json:"orphan"`
	EpisodesOnDisk   []int       `json:"episodes_on_disk"`
	MissingEpisodes  []int       `json:"missing_episodes"`
	Files            []FileEntry `json:"files"`
	RenameCount      int         `json:"rename_count"`
	RenamedCount     int         `json:"renamed_count,omitempty"`
	Stats            Stats       `json:"stats"`
	DryRun           bool        `json:"dry_run"`
}

// Scan 扫描指定文件夹
func (s *Scanner) Scan(sub *model.Subscription, req *Request) (*Result, error) {
	// 验证路径存在
	if err := s.validateScanFolder(req.FolderPath); err != nil {
		return nil, err
	}

	info, err := os.Stat(req.FolderPath)
	if err != nil {
		return nil, fmt.Errorf("文件夹不存在: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("路径不是文件夹: %s", req.FolderPath)
	}

	result := &Result{
		SubscriptionID:   sub.ID,
		SubscriptionName: sub.Name,
		Season:           sub.Season,
		Folder:           req.FolderPath,
		Files:            make([]FileEntry, 0),
		DryRun:           req.DryRun,
	}
	if result.Season == 0 {
		result.Season = 1
	}

	// 1. 扫描文件夹
	entries, err := s.scanFolder(req.FolderPath)
	if err != nil {
		return nil, err
	}
	result.Scanned = len(entries)

	// 2. 解析每个文件
	episodeSet := make(map[int]bool)
	stats := Stats{
		ResolutionBreakdown: make(map[string]int),
		CodecBreakdown:      make(map[string]int),
	}
	var totalSize int64

	for _, entry := range entries {
		parsed := s.parser.Parse(filepath.Base(entry.path))
		if parsed.Episode <= 0 || parsed.Episode >= 1000 {
			result.Orphan++
			result.Files = append(result.Files, FileEntry{
				Path:       entry.path,
				Resolution: parsed.Resolution,
				VideoCodec: parsed.VideoCodec,
				Fansub:     parsed.Fansub,
				Language:   parsed.Language,
				SizeMB:     entry.sizeMB,
			})
			continue
		}

		result.Matched++
		episodeSet[parsed.Episode] = true
		totalSize += entry.sizeBytes

		file := FileEntry{
			Path:       entry.path,
			Episode:    parsed.Episode,
			Season:     result.Season,
			Resolution: parsed.Resolution,
			VideoCodec: parsed.VideoCodec,
			Fansub:     parsed.Fansub,
			Language:   parsed.Language,
			SizeMB:     entry.sizeMB,
		}

		// 生成重命名目标路径
		if req.RenameFiles || req.DryRun {
			file.RenameTo = s.generateRenamePath(sub, parsed, req.FolderPath)
			if file.RenameTo != "" && file.RenameTo != entry.path {
				file.WillRename = true
				result.RenameCount++
			}
		}

		result.Files = append(result.Files, file)
	}

	// 3. 聚合统计
	for _, f := range result.Files {
		if f.Resolution != "" {
			stats.ResolutionBreakdown[f.Resolution]++
		}
		if f.VideoCodec != "" {
			stats.CodecBreakdown[f.VideoCodec]++
		}
	}
	stats.TotalSizeGB = float64(totalSize) / (1024 * 1024 * 1024)
	result.Stats = stats

	// 4. 集数统计
	for ep := range episodeSet {
		result.EpisodesOnDisk = append(result.EpisodesOnDisk, ep)
	}
	sort.Ints(result.EpisodesOnDisk)

	// 5. 缺失集数（相对于 latest_episode）
	latestEp := sub.LatestEpisode
	maxOnDisk := 0
	if len(result.EpisodesOnDisk) > 0 {
		maxOnDisk = result.EpisodesOnDisk[len(result.EpisodesOnDisk)-1]
	}
	reference := latestEp
	if maxOnDisk > reference {
		reference = maxOnDisk
	}
	if reference > 0 {
		for ep := 1; ep <= reference; ep++ {
			if !episodeSet[ep] {
				result.MissingEpisodes = append(result.MissingEpisodes, ep)
			}
		}
	}

	// 6. dry_run 模式到此为止
	if req.DryRun {
		return result, nil
	}

	// 7. 执行重命名
	if req.RenameFiles {
		applicableFiles := s.filterRenameableFiles(result.Files)
		// 记录原始路径→新路径的映射，用于后续同步 result.Files
		type renameRecord struct {
			entry   *FileEntry
			oldPath string
		}
		var renamed []renameRecord

		for i := range applicableFiles {
			f := &applicableFiles[i]
			oldPath := f.Path
			newPath := f.RenameTo

			// 校验目标路径不穿越目标根目录
			targetRoot := filepath.Dir(req.FolderPath)
			if _, err := utils.ValidatePath(newPath, targetRoot); err != nil {
				f.RenameError = fmt.Sprintf("路径校验失败: %v", err)
				logger.Error("Rename target path validation failed",
					"from", oldPath, "to", newPath, "error", err)
				continue
			}

			targetDir := filepath.Dir(newPath)
			if err := os.MkdirAll(targetDir, 0755); err != nil {
				f.RenameError = fmt.Sprintf("创建目录失败: %v", err)
				logger.Error("Failed to create target directory for rename",
					"from", oldPath, "to", newPath, "error", err)
				continue
			}
			if err := os.Rename(oldPath, newPath); err != nil {
				f.RenameError = fmt.Sprintf("重命名失败: %v", err)
				logger.Error("Failed to rename file",
					"from", oldPath, "to", newPath, "error", err)
				continue
			}
			f.Renamed = true
			f.Path = newPath
			result.RenamedCount++
			renamed = append(renamed, renameRecord{entry: f, oldPath: oldPath})
			logger.Info("File renamed",
				"from", oldPath, "to", newPath)
		}

		// 同步重命名结果到 result.Files（按原始路径匹配）
		for _, rec := range renamed {
			for i := range result.Files {
				if result.Files[i].Path == rec.oldPath {
					result.Files[i] = *rec.entry
					break
				}
			}
		}
	}

	// 8. 写入 DB
	if err := s.applyToDB(sub, result); err != nil {
		return result, fmt.Errorf("写入数据库失败: %w", err)
	}

	return result, nil
}

func (s *Scanner) validateScanFolder(folderPath string) error {
	folderPath = strings.TrimSpace(folderPath)
	if folderPath == "" {
		return fmt.Errorf("folder_path is required")
	}

	scanAbs, err := filepath.Abs(folderPath)
	if err != nil {
		return fmt.Errorf("invalid folder_path: %w", err)
	}
	scanAbs = filepath.Clean(scanAbs)
	if scanAbs == filepath.Clean(filepath.VolumeName(scanAbs)+string(filepath.Separator)) {
		return fmt.Errorf("refusing to scan filesystem root; choose a subscription folder")
	}

	downloadRoot := s.getDownloadPath()
	rootAbs, err := filepath.Abs(downloadRoot)
	if err != nil {
		return fmt.Errorf("invalid download root: %w", err)
	}
	rootAbs = filepath.Clean(rootAbs)

	rel, err := filepath.Rel(rootAbs, scanAbs)
	if err != nil {
		return fmt.Errorf("invalid scan folder relative to download root: %w", err)
	}
	if rel == "." {
		return fmt.Errorf("refusing to scan download root; choose a subscription folder")
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("folder_path must be inside download_path")
	}

	return nil
}

func (s *Scanner) getDownloadPath() string {
	if s.configRepo != nil {
		if cfg, err := s.configRepo.Get("download_path"); err == nil && cfg != nil && strings.TrimSpace(cfg.Value) != "" {
			return cfg.Value
		}
	}
	return s.defaultRoot
}

type fileEntry struct {
	path      string
	sizeBytes int64
	sizeMB    float64
}

// scanFolder 扫描文件夹中的视频文件
func (s *Scanner) scanFolder(folderPath string) ([]fileEntry, error) {
	var entries []fileEntry

	err := filepath.WalkDir(folderPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// 跳过隐藏目录
			if strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if !videoExts[ext] {
			return nil
		}

		info, err := d.Info()
		if err != nil || info.Size() == 0 {
			return nil
		}

		entries = append(entries, fileEntry{
			path:      path,
			sizeBytes: info.Size(),
			sizeMB:    float64(info.Size()) / (1024 * 1024),
		})
		return nil
	})

	return entries, err
}

// generateRenamePath 生成重命名目标路径
func (s *Scanner) generateRenamePath(sub *model.Subscription, parsed *organizer.FileNameInfo, baseFolder string) string {
	// 读取重命名模板
	template := s.getRenameTemplate()

	renameCtx := &downloader.RenameContext{
		Subscription: sub,
		Download: &model.Download{
			Episode: parsed.Episode,
		},
		OriginalName: parsed.OriginalName,
		Extension:    parsed.Extension,
		Resolution:   parsed.Resolution,
	}

	// 用临时 service 生成路径（确保使用当前模板）
	tempService := downloader.NewRenameService(template)
	newRelativePath := tempService.GenerateFileName(renameCtx)

	// 目标路径基于 baseFolder 的父级（即目标库根目录）
	// 约定：baseFolder 是用户选的源文件夹，rename 目标放在 baseFolder 的父级
	// 如果 baseFolder 已经看起来像番剧目录，则视为目标根目录本身
	targetRoot := filepath.Dir(baseFolder)
	destDirBase := filepath.Base(baseFolder)
	sanitizedTitle := sanitizeFileName(sub.Name)
	if isSimilarDirectoryName(destDirBase, sanitizedTitle) {
		targetRoot = baseFolder
	}

	// 防止重复前缀（与 organizer 相同的去重逻辑）
	parts := strings.Split(filepath.Clean(newRelativePath), string(filepath.Separator))
	if len(parts) > 1 && isSimilarDirectoryName(parts[0], sanitizedTitle) {
		destDirBase2 := filepath.Base(targetRoot)
		if isSimilarDirectoryName(destDirBase2, sanitizedTitle) {
			newRelativePath = filepath.Join(parts[1:]...)
		}
	}

	return filepath.Join(targetRoot, newRelativePath)
}

// getRenameTemplate 从配置中读取重命名模板
func (s *Scanner) getRenameTemplate() string {
	defaultTemplate := "${title}/Season ${season}/${title} S${seasonFormat}E${episodeFormat}"
	if s.configRepo == nil {
		return defaultTemplate
	}
	cfg, err := s.configRepo.Get("rename_template")
	if err != nil || cfg == nil || cfg.Value == "" {
		return defaultTemplate
	}
	return cfg.Value
}

// filterRenameableFiles 筛选需要重命名的文件（排除无集数、无目标、已在目标位置的文件）
func (s *Scanner) filterRenameableFiles(files []FileEntry) []FileEntry {
	var result []FileEntry
	for _, f := range files {
		if f.Episode <= 0 || f.RenameTo == "" || f.RenameTo == f.Path {
			continue
		}
		result = append(result, f)
	}
	return result
}

// applyToDB 将扫描结果写入数据库
func (s *Scanner) applyToDB(sub *model.Subscription, result *Result) error {
	// 收集磁盘上的最大集数
	maxEpisodeOnDisk := 0
	for _, ep := range result.EpisodesOnDisk {
		if ep > maxEpisodeOnDisk {
			maxEpisodeOnDisk = ep
		}
	}

	return s.db.Transaction(func(tx *gorm.DB) error {
		now := time.Now()

		for _, file := range result.Files {
			if file.Episode <= 0 {
				continue
			}

			// 重命名后的实际路径
			filePath := file.Path
			if file.Renamed && file.RenameTo != "" {
				filePath = file.RenameTo
			}

			// 查找已有记录
			existingDownloads, err := s.downloadRepo.GetBySubscriptionAndEpisodeWithLang(sub.ID, file.Episode)
			if err != nil || len(existingDownloads) == 0 {
				// 创建 synthetic 记录
				syntheticHash := fmt.Sprintf("__scan__%d_%d_%d", sub.ID, file.Episode, now.UnixNano())
				download := &model.Download{
					SubscriptionID: sub.ID,
					Title:          filepath.Base(filePath),
					Episode:        file.Episode,
					Fansub:         file.Fansub,
					Language:       file.Language,
					TorrentURL:     "",
					TorrentHash:    syntheticHash,
					FilePath:       filePath,
					RenamedPath:    filePath,
					Status:         model.DownloadStatusCompleted,
					DownloadedAt:   &now,
				}
				if err := s.downloadRepo.CreateInTx(tx, download); err != nil {
					logger.Error("Failed to create synthetic download",
						"subscription_id", sub.ID,
						"episode", file.Episode,
						"error", err)
					return err
				}
				logger.Info("Created synthetic download",
					"subscription_id", sub.ID,
					"episode", file.Episode,
					"path", filePath)
			} else {
				// 更新已有记录
				for i := range existingDownloads {
					d := &existingDownloads[i]
					if d.Status != model.DownloadStatusCompleted || d.RenamedPath != filePath {
						d.Status = model.DownloadStatusCompleted
						d.RenamedPath = filePath
						if d.FilePath == "" {
							d.FilePath = filePath
						}
						if err := s.downloadRepo.UpdateInTx(tx, d); err != nil {
							logger.Error("Failed to update download",
								"download_id", d.ID,
								"error", err)
							return err
						}
						logger.Info("Updated download status via scan",
							"download_id", d.ID,
							"episode", file.Episode,
							"path", filePath)
					}
				}
			}
		}

		// 更新订阅的 current_episode 和 latest_episode
		needsUpdate := false
		if maxEpisodeOnDisk > sub.CurrentEpisode {
			sub.CurrentEpisode = maxEpisodeOnDisk
			needsUpdate = true
		}
		if maxEpisodeOnDisk > sub.LatestEpisode {
			sub.LatestEpisode = maxEpisodeOnDisk
			needsUpdate = true
		}
		if needsUpdate {
			if err := s.subscriptionRepo.UpdateInTx(tx, sub); err != nil {
				logger.Error("Failed to update subscription stats",
					"subscription_id", sub.ID,
					"error", err)
				return err
			}
			logger.Info("Updated subscription stats",
				"subscription_id", sub.ID,
				"current_episode", sub.CurrentEpisode,
				"latest_episode", sub.LatestEpisode)
		}

		return nil
	})
}

func sanitizeFileName(name string) string {
	illegalChars := []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|"}
	result := name
	for _, char := range illegalChars {
		result = strings.ReplaceAll(result, char, "_")
	}
	result = strings.TrimSpace(result)
	return result
}

func isSimilarDirectoryName(dirName, subName string) bool {
	normalized1 := normalizeDirName(dirName)
	normalized2 := normalizeDirName(subName)
	if normalized1 == normalized2 {
		return true
	}
	if strings.Contains(normalized1, normalized2) || strings.Contains(normalized2, normalized1) {
		return true
	}
	return false
}

func normalizeDirName(name string) string {
	var result strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			result.WriteRune(r)
		}
	}
	return result.String()
}
