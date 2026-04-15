package recovery

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/pkg/utils"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/bangumi"
	"github.com/WormW/auto-rss/internal/service/organizer"
	"gorm.io/gorm"
)

// videoExts 支持的视频扩展名
var videoExts = map[string]bool{
	".mp4": true, ".mkv": true, ".avi": true, ".wmv": true,
	".mov": true, ".flv": true, ".webm": true, ".m4v": true,
	".ts": true, ".m2ts": true,
}

// seasonEpisodePattern 已整理文件的集数格式：S01E01
var seasonEpisodePattern = regexp.MustCompile(`[Ss](\d{1,2})[Ee](\d{1,3})`)

// bracketEpisodePattern 兜底模式：[01]、[12]
var bracketEpisodePattern = regexp.MustCompile(`\[(\d{1,3})\]`)

// Scanner 磁盘扫描恢复服务
type Scanner struct {
	db               *gorm.DB
	subscriptionRepo repository.SubscriptionRepository
	downloadRepo     repository.DownloadRepository
	configRepo       repository.ConfigRepository
	parser           *organizer.FileNameParser
	matcher          organizer.SubscriptionMatcher
}

// NewScanner 创建扫描恢复服务
func NewScanner(
	db *gorm.DB,
	subscriptionRepo repository.SubscriptionRepository,
	downloadRepo repository.DownloadRepository,
	configRepo repository.ConfigRepository,
	bangumiService *bangumi.BangumiService,
) *Scanner {
	parser := organizer.NewFileNameParser()
	matcher := organizer.NewSubscriptionMatcher(parser, subscriptionRepo, bangumiService)
	return &Scanner{
		db:               db,
		subscriptionRepo: subscriptionRepo,
		downloadRepo:     downloadRepo,
		configRepo:       configRepo,
		parser:           parser,
		matcher:          matcher,
	}
}

// ScanRequest 扫描请求参数
type ScanRequest struct {
	DryRun         bool   `json:"dry_run"`
	SubscriptionID *uint  `json:"subscription_id,omitempty"`
}

// EpisodeFile 磁盘上匹配到的单集文件信息
type EpisodeFile struct {
	Path     string `json:"path"`
	Episode  int    `json:"episode"`
	Season   int    `json:"season"`
}

// SubscriptionScanResult 单个订阅的扫描结果
type SubscriptionScanResult struct {
	SubscriptionID        uint          `json:"subscription_id"`
	Name                  string        `json:"name"`
	CurrentEpisodeOld     int           `json:"current_episode_old"`
	CurrentEpisodeNew     int           `json:"current_episode_new"`
	LatestEpisodeOld      int           `json:"latest_episode_old"`
	LatestEpisodeNew      int           `json:"latest_episode_new"`
	EpisodesOnDisk        []int         `json:"episodes_on_disk"`
	DownloadsToUpdate     []uint        `json:"downloads_to_update"`
	DownloadsToCreate     []int         `json:"downloads_to_create"`
	DownloadsMissing      []uint        `json:"downloads_missing"`
}

// ScanResult 整体扫描结果
type ScanResult struct {
	ScannedFiles     int                      `json:"scanned_files"`
	MatchedFiles     int                      `json:"matched_files"`
	OrphanFiles      []string                 `json:"orphan_files"`
	Subscriptions    []SubscriptionScanResult `json:"subscriptions"`
	BackupPath       string                   `json:"backup_path,omitempty"`
	Applied          bool                     `json:"applied"`
}

// Scan 执行扫描与恢复
func (s *Scanner) Scan(req *ScanRequest) (*ScanResult, error) {
	result := &ScanResult{
		OrphanFiles:   make([]string, 0),
		Subscriptions: make([]SubscriptionScanResult, 0),
	}

	// 1. 获取下载根目录
	rootPath := s.getDownloadPath()

	// 2. 加载所有订阅
	subscriptions, _, err := s.subscriptionRepo.List(0, 10000)
	if err != nil {
		return nil, fmt.Errorf("failed to list subscriptions: %w", err)
	}

	// 如果限定单个订阅
	if req.SubscriptionID != nil {
		var filtered []model.Subscription
		for _, sub := range subscriptions {
			if sub.ID == *req.SubscriptionID {
				filtered = append(filtered, sub)
				break
			}
		}
		subscriptions = filtered
	}

	// 3. 扫描磁盘文件并匹配
	diskEpisodes := make(map[uint]map[int][]EpisodeFile) // subscription_id -> episode -> files

	err = filepath.WalkDir(rootPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		// 忽略隐藏文件
		if strings.HasPrefix(filepath.Base(path), ".") {
			return nil
		}
		// 只处理视频文件
		ext := strings.ToLower(filepath.Ext(path))
		if !videoExts[ext] {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() == 0 {
			return nil
		}

		result.ScannedFiles++

		sub, episode, season := s.matchFile(path, subscriptions, rootPath)
		if sub == nil {
			result.OrphanFiles = append(result.OrphanFiles, path)
			return nil
		}

		result.MatchedFiles++
		if diskEpisodes[sub.ID] == nil {
			diskEpisodes[sub.ID] = make(map[int][]EpisodeFile)
		}
		diskEpisodes[sub.ID][episode] = append(diskEpisodes[sub.ID][episode], EpisodeFile{
			Path:    path,
			Episode: episode,
			Season:  season,
		})
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk directory: %w", err)
	}

	// 4. 对每个订阅进行对账
	for _, sub := range subscriptions {
		epMap, hasFiles := diskEpisodes[sub.ID]
		if !hasFiles || len(epMap) == 0 {
			continue
		}

		// 收集磁盘上的所有集数
		episodesOnDisk := make([]int, 0, len(epMap))
		maxEp := 0
		for ep := range epMap {
			episodesOnDisk = append(episodesOnDisk, ep)
			if ep > maxEp {
				maxEp = ep
			}
		}
		sort.Ints(episodesOnDisk)

		// 查询该订阅的下载记录
		downloads, err := s.downloadRepo.ListBySubscriptionID(sub.ID)
		if err != nil {
			logger.Warn("Failed to list downloads for subscription", "subscription_id", sub.ID, "error", err)
			downloads = []model.Download{}
		}

		downloadsByEpisode := make(map[int][]model.Download)
		for _, d := range downloads {
			downloadsByEpisode[d.Episode] = append(downloadsByEpisode[d.Episode], d)
		}

		subResult := SubscriptionScanResult{
			SubscriptionID:    sub.ID,
			Name:              sub.Name,
			CurrentEpisodeOld: sub.CurrentEpisode,
			LatestEpisodeOld:  sub.LatestEpisode,
			CurrentEpisodeNew: sub.CurrentEpisode,
			LatestEpisodeNew:  sub.LatestEpisode,
			EpisodesOnDisk:    episodesOnDisk,
			DownloadsToUpdate: make([]uint, 0),
			DownloadsToCreate: make([]int, 0),
			DownloadsMissing:  make([]uint, 0),
		}

		// 修正 current_episode 和 latest_episode
		if maxEp != sub.CurrentEpisode {
			subResult.CurrentEpisodeNew = maxEp
		}
		if maxEp > sub.LatestEpisode {
			subResult.LatestEpisodeNew = maxEp
		}

		// 对每一集在磁盘上的文件进行 downloads 对账
		for _, ep := range episodesOnDisk {
			existing := downloadsByEpisode[ep]
			firstFile := epMap[ep][0]

			if len(existing) == 0 {
				// 需要创建 synthetic 记录
				subResult.DownloadsToCreate = append(subResult.DownloadsToCreate, ep)
			} else {
				// 检查是否需要更新状态为 completed
				needsUpdate := false
				for _, d := range existing {
					if d.Status != model.DownloadStatusCompleted {
						subResult.DownloadsToUpdate = append(subResult.DownloadsToUpdate, d.ID)
						needsUpdate = true
					} else if d.RenamedPath != firstFile.Path {
						// 路径不一致也需要更新
						subResult.DownloadsToUpdate = append(subResult.DownloadsToUpdate, d.ID)
						needsUpdate = true
					}
				}
				if !needsUpdate {
					// 至少有一个 completed 且路径正确即可视为无需改动
					_ = firstFile
				}
			}
		}

		// 检查 DB 中 completed 但磁盘上已丢失的记录
		for _, d := range downloads {
			if d.Status != model.DownloadStatusCompleted {
				continue
			}
			pathToCheck := d.RenamedPath
			if pathToCheck == "" {
				pathToCheck = d.FilePath
			}
			if pathToCheck == "" {
				continue
			}
			if _, ok := diskEpisodes[sub.ID][d.Episode]; !ok {
				if _, err := os.Stat(pathToCheck); err != nil {
					subResult.DownloadsMissing = append(subResult.DownloadsMissing, d.ID)
				}
			}
		}

		// 如果没有任何变动，仍然加入结果（方便用户看到扫描到了）
		result.Subscriptions = append(result.Subscriptions, subResult)
	}

	// 5. 若需要应用，执行写入
	if !req.DryRun {
		backupPath, err := s.backupDB()
		if err != nil {
			return nil, fmt.Errorf("failed to backup database: %w", err)
		}
		result.BackupPath = backupPath

		if err := s.apply(result, diskEpisodes, subscriptions); err != nil {
			return nil, fmt.Errorf("failed to apply changes: %w", err)
		}
		result.Applied = true
	}

	return result, nil
}

// matchFile 将单个文件匹配到订阅
func (s *Scanner) matchFile(filePath string, subscriptions []model.Subscription, rootPath string) (*model.Subscription, int, int) {
	filename := filepath.Base(filePath)

	// 先尝试目录名匹配
	relPath, _ := filepath.Rel(rootPath, filePath)
	parts := strings.Split(relPath, string(filepath.Separator))
	if len(parts) > 0 {
		dirName := parts[0]
		for i := range subscriptions {
			sub := &subscriptions[i]
			sanitized := utils.SanitizeDirectoryName(sub.Name)
			if isSimilarDirectoryName(dirName, sanitized) {
				ep, season := s.extractEpisode(filename, sub)
				return sub, ep, season
			}
		}
	}

	// 回退：用文件名解析器提取标题再匹配
	if s.matcher != nil {
		info := s.parser.Parse(filename)
		sub, _ := s.matcher.Match(info)
		if sub != nil {
			ep, season := s.extractEpisode(filename, sub)
			return sub, ep, season
		}
	}

	return nil, 0, 0
}

// extractEpisode 提取集数和季度
func (s *Scanner) extractEpisode(filename string, sub *model.Subscription) (int, int) {
	season := sub.Season
	if season == 0 {
		season = 1
	}

	// 路径 A：已整理文件 SxxExx
	matches := seasonEpisodePattern.FindStringSubmatch(filename)
	if len(matches) == 3 {
		s, _ := strconv.Atoi(matches[1])
		ep, _ := strconv.Atoi(matches[2])
		if s > 0 {
			season = s
		}
		if ep > 0 && ep < 1000 {
			return ep, season
		}
	}

	// 路径 B：复用 parser 的 Parse 结果（内部已调用 extractEpisode）
	info := s.parser.Parse(filename)
	if info.Episode > 0 && info.Episode < 1000 {
		return info.Episode, season
	}

	// 兜底：方括号纯数字 [01]
	matches = bracketEpisodePattern.FindStringSubmatch(filename)
	if len(matches) == 2 {
		ep, _ := strconv.Atoi(matches[1])
		if ep > 0 && ep < 1000 {
			return ep, season
		}
	}

	return 0, season
}

// apply 将扫描结果写入数据库
func (s *Scanner) apply(result *ScanResult, diskEpisodes map[uint]map[int][]EpisodeFile, subscriptions []model.Subscription) error {
	// 构建订阅 id -> 对象映射
	subMap := make(map[uint]*model.Subscription)
	for i := range subscriptions {
		subMap[subscriptions[i].ID] = &subscriptions[i]
	}

	return s.db.Transaction(func(tx *gorm.DB) error {
		for _, sr := range result.Subscriptions {
			sub := subMap[sr.SubscriptionID]
			if sub == nil {
				continue
			}

			// 更新订阅字段
			if sr.CurrentEpisodeNew != sr.CurrentEpisodeOld || sr.LatestEpisodeNew != sr.LatestEpisodeOld {
				sub.CurrentEpisode = sr.CurrentEpisodeNew
				sub.LatestEpisode = sr.LatestEpisodeNew
				if err := s.subscriptionRepo.UpdateInTx(tx, sub); err != nil {
					logger.Error("Failed to update subscription in recovery", "subscription_id", sub.ID, "error", err)
					return err
				}
				logger.Info("Updated subscription stats via recovery",
					"subscription_id", sub.ID,
					"name", sub.Name,
					"current_episode", sr.CurrentEpisodeOld, "->", sr.CurrentEpisodeNew,
					"latest_episode", sr.LatestEpisodeOld, "->", sr.LatestEpisodeNew)
			}

			epMap := diskEpisodes[sub.ID]

			// 更新已有下载记录
			for _, id := range sr.DownloadsToUpdate {
				download, err := s.downloadRepo.GetByID(id)
				if err != nil {
					logger.Warn("Failed to get download for update", "download_id", id, "error", err)
					continue
				}
				oldStatus := download.Status
				download.Status = model.DownloadStatusCompleted
				file := epMap[download.Episode][0]
				download.RenamedPath = file.Path
				if download.FilePath == "" {
					download.FilePath = file.Path
				}
				now := time.Now()
				if download.DownloadedAt == nil {
					download.DownloadedAt = &now
				}
				if err := s.downloadRepo.UpdateInTx(tx, download); err != nil {
					logger.Error("Failed to update download in recovery", "download_id", id, "error", err)
					return err
				}
				logger.Info("Updated download status via recovery",
					"download_id", id,
					"old_status", oldStatus,
					"new_status", model.DownloadStatusCompleted,
					"path", file.Path)
			}

			// 创建 synthetic 下载记录
			for _, ep := range sr.DownloadsToCreate {
				file := epMap[ep][0]
				parsed := s.parser.Parse(filepath.Base(file.Path))
				now := time.Now()
				download := &model.Download{
					SubscriptionID: sub.ID,
					Title:          filepath.Base(file.Path),
					Episode:        ep,
					Fansub:         parsed.Fansub,
					Language:       parsed.Language,
					TorrentURL:     "",
					TorrentHash:    fmt.Sprintf("__recovery__%d_%d_%d", sub.ID, ep, now.UnixNano()),
					FilePath:       file.Path,
					RenamedPath:    file.Path,
					Status:         model.DownloadStatusCompleted,
					DownloadedAt:   &now,
				}
				if err := s.downloadRepo.CreateInTx(tx, download); err != nil {
					logger.Error("Failed to create synthetic download in recovery", "subscription_id", sub.ID, "episode", ep, "error", err)
					return err
				}
				logger.Info("Created synthetic download via recovery",
					"subscription_id", sub.ID,
					"episode", ep,
					"path", file.Path)
			}
		}
		return nil
	})
}

// backupDB 备份数据库文件
func (s *Scanner) backupDB() (string, error) {
	dbPath := "./data/auto-rss.db"
	// 尝试从 db 获取底层路径（不一定总是成功，失败则用默认值）
	if s.db != nil {
		if sqlDB, err := s.db.DB(); err == nil {
			_ = sqlDB
		}
	}

	timestamp := time.Now().Format("20060102_150405")
	backupPath := fmt.Sprintf("./data/auto-rss.db.backup.%s", timestamp)

	src, err := os.Open(dbPath)
	if err != nil {
		return "", fmt.Errorf("failed to open db for backup: %w", err)
	}
	defer src.Close()

	dst, err := os.Create(backupPath)
	if err != nil {
		return "", fmt.Errorf("failed to create backup file: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return "", fmt.Errorf("failed to copy db to backup: %w", err)
	}

	info, err := os.Stat(backupPath)
	if err != nil || info.Size() == 0 {
		return "", fmt.Errorf("backup file is empty or inaccessible")
	}

	logger.Info("Database backed up before recovery", "backup_path", backupPath, "size", info.Size())
	return backupPath, nil
}

// getDownloadPath 获取下载路径
func (s *Scanner) getDownloadPath() string {
	if s.configRepo != nil {
		if cfg, err := s.configRepo.Get("download_path"); err == nil && cfg != nil {
			if cfg.Value != "" {
				return cfg.Value
			}
		}
	}
	return "/downloads"
}

// isSimilarDirectoryName 复用 organizer_helper 的逻辑
func isSimilarDirectoryName(name1, name2 string) bool {
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

	n1 := normalize(name1)
	n2 := normalize(name2)

	if n1 == n2 {
		return true
	}

	maxLen := len(n1)
	if len(n2) > maxLen {
		maxLen = len(n2)
	}
	if maxLen == 0 {
		return false
	}

	diff := float64(abs(len(n1)-len(n2))) / float64(maxLen)
	if diff > 0.3 {
		return false
	}

	return strings.Contains(n1, n2) || strings.Contains(n2, n1)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
