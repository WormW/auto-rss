package disk

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/repository"
	"gorm.io/gorm"
)

const (
	// StatusHealthy 磁盘状态：健康
	StatusHealthy = "healthy"
	// StatusWarning 磁盘状态：警告
	StatusWarning = "warning"
	// StatusCritical 磁盘状态：危险
	StatusCritical = "critical"

	// DefaultWarningThresholdGB 默认警告阈值（GB）
	DefaultWarningThresholdGB = 10
	// DefaultCriticalThresholdGB 默认危险阈值（GB）
	DefaultCriticalThresholdGB = 5

	// CheckInterval 磁盘检查间隔
	CheckInterval = 5 * time.Minute
)

// DiskInfo 磁盘信息
type DiskInfo struct {
	Path         string  `json:"path"`
	TotalGB      float64 `json:"total_gb"`
	UsedGB       float64 `json:"used_gb"`
	FreeGB       float64 `json:"free_gb"`
	TotalBytes   int64   `json:"total_bytes"`
	UsedBytes    int64   `json:"used_bytes"`
	FreeBytes    int64   `json:"free_bytes"`
	UsagePercent float64 `json:"usage_percent"`
	Status       string  `json:"status"`
}

const (
	CleanupTriggerManual = "manual"
	CleanupTriggerAuto   = "auto"

	MediaLibraryStatusUnconfigured = "unconfigured"
	MediaLibraryStatusConnected    = "connected"
	MediaLibraryStatusFailed       = "failed"
)

// CleanupOptions controls a cleanup run.
type CleanupOptions struct {
	Trigger         string
	Strategy        CleanupStrategy
	KeepDays        int
	KeepGB          int64
	DownloadPath    string
	ProtectWatching bool
}

// CleanupResult is shared by manual and automatic cleanup callers.
type CleanupResult struct {
	Cleaned            bool          `json:"cleaned"`
	DeletedCount       int           `json:"deleted_count"`
	SkippedCount       int           `json:"skipped_count"`
	FreedBytes         int64         `json:"freed_bytes"`
	BeforeFreeGB       float64       `json:"before_free_gb"`
	AfterFreeGB        float64       `json:"after_free_gb"`
	BeforeFreeBytes    int64         `json:"before_free_bytes"`
	AfterFreeBytes     int64         `json:"after_free_bytes"`
	MediaLibraryStatus string        `json:"media_library_status"`
	Items              []CleanupItem `json:"items"`
}

// CleanupItem records the outcome for a single candidate.
type CleanupItem struct {
	DownloadID uint   `json:"download_id"`
	Path       string `json:"path"`
	Action     string `json:"action"`
	Reason     string `json:"reason,omitempty"`
	FreedBytes int64  `json:"freed_bytes"`
}

type mediaLibraryState struct {
	Status              string
	Message             string
	ProtectedPaths      map[string]struct{}
	ConservativeSkipAll bool
}

func (s mediaLibraryState) IsProtected(download model.Download) bool {
	if len(s.ProtectedPaths) == 0 {
		return false
	}
	paths := []string{download.FilePath, download.RenamedPath}
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		if realPath, err := filepath.EvalSymlinks(abs); err == nil {
			abs = realPath
		}
		if _, ok := s.ProtectedPaths[abs]; ok {
			return true
		}
		for protected := range s.ProtectedPaths {
			if isSameOrChild(abs, protected) || isSameOrChild(protected, abs) {
				return true
			}
		}
	}
	return false
}

// CleanupStrategy 清理策略类型
type CleanupStrategy string

const (
	// CleanupByAge 按年龄清理（删除N天前的文件）
	CleanupByAge CleanupStrategy = "age"
	// CleanupBySpace 按空间清理（保留N GB空间）
	CleanupBySpace CleanupStrategy = "space"
	// CleanupHybrid 混合策略
	CleanupHybrid CleanupStrategy = "hybrid"
)

// Config 磁盘监控配置
type Config struct {
	Enabled                bool            `json:"enabled"`                  // 是否启用监控
	WarningThresholdGB     int64           `json:"warning_threshold_gb"`     // 警告阈值
	CriticalThresholdGB    int64           `json:"critical_threshold_gb"`    // 危险阈值
	AutoCleanupEnabled     bool            `json:"auto_cleanup_enabled"`     // 自动清理开关
	CleanupStrategy        CleanupStrategy `json:"cleanup_strategy"`         // 清理策略
	CleanupKeepDays        int             `json:"cleanup_keep_days"`        // 保留天数
	CleanupKeepGB          int64           `json:"cleanup_keep_gb"`          // 预留空间
	CleanupProtectWatching bool            `json:"cleanup_protect_watching"` // 保护正在观看的
	PauseOnCritical        bool            `json:"pause_on_critical"`        // 危险时暂停下载
}

// downloadPaused 全局原子标志，用于暂停新下载
var downloadPaused atomic.Bool

// DefaultConfig 默认配置
func DefaultConfig() *Config {
	return &Config{
		Enabled:                true,
		WarningThresholdGB:     DefaultWarningThresholdGB,
		CriticalThresholdGB:    DefaultCriticalThresholdGB,
		AutoCleanupEnabled:     false,
		CleanupStrategy:        CleanupByAge,
		CleanupKeepDays:        30,
		CleanupKeepGB:          50,
		CleanupProtectWatching: true,
		PauseOnCritical:        true,
	}
}

// Monitor 磁盘监控服务
type Monitor struct {
	db               *gorm.DB
	config           *Config
	downloadRepo     repository.DownloadRepository
	subscriptionRepo repository.SubscriptionRepository
	configRepo       repository.ConfigRepository
	notificationSvc  NotificationService
	ticker           *time.Ticker
	stopChan         chan struct{}
	lastStatus       string
}

// NotificationService 通知服务接口
type NotificationService interface {
	Send(payload model.NotificationPayload)
}

// NewMonitor 创建磁盘监控服务
func NewMonitor(
	db *gorm.DB,
	downloadRepo repository.DownloadRepository,
	subscriptionRepo repository.SubscriptionRepository,
	configRepo repository.ConfigRepository,
) *Monitor {
	return &Monitor{
		db:               db,
		config:           DefaultConfig(),
		downloadRepo:     downloadRepo,
		subscriptionRepo: subscriptionRepo,
		configRepo:       configRepo,
		stopChan:         make(chan struct{}),
		lastStatus:       StatusHealthy,
	}
}

// SetNotificationService 设置通知服务
func (m *Monitor) SetNotificationService(svc NotificationService) {
	m.notificationSvc = svc
}

// LoadConfig 从数据库加载配置
func (m *Monitor) LoadConfig() error {
	// 从数据库加载配置
	if m.configRepo == nil {
		return nil
	}

	// 加载警告阈值
	if cfg, err := m.configRepo.Get("disk.warning_threshold_gb"); err == nil && cfg != nil {
		if val, err := strconv.ParseInt(cfg.Value, 10, 64); err == nil {
			m.config.WarningThresholdGB = val
		}
	}

	// 加载危险阈值
	if cfg, err := m.configRepo.Get("disk.critical_threshold_gb"); err == nil && cfg != nil {
		if val, err := strconv.ParseInt(cfg.Value, 10, 64); err == nil {
			m.config.CriticalThresholdGB = val
		}
	}

	// 加载自动清理开关
	if cfg, err := m.configRepo.Get("disk.auto_cleanup_enabled"); err == nil && cfg != nil {
		if val, err := strconv.ParseBool(cfg.Value); err == nil {
			m.config.AutoCleanupEnabled = val
		}
	}

	// 加载清理策略
	if cfg, err := m.configRepo.Get("disk.cleanup_strategy"); err == nil && cfg != nil {
		m.config.CleanupStrategy = CleanupStrategy(cfg.Value)
	}

	// 加载保留天数
	if cfg, err := m.configRepo.Get("disk.cleanup_keep_days"); err == nil && cfg != nil {
		if val, err := strconv.Atoi(cfg.Value); err == nil {
			m.config.CleanupKeepDays = val
		}
	}

	if cfg, err := m.configRepo.Get("disk.cleanup_keep_gb"); err == nil && cfg != nil {
		if val, err := strconv.ParseInt(cfg.Value, 10, 64); err == nil {
			m.config.CleanupKeepGB = val
		}
	}

	if cfg, err := m.configRepo.Get("disk.cleanup_protect_watching"); err == nil && cfg != nil {
		if val, err := strconv.ParseBool(cfg.Value); err == nil {
			m.config.CleanupProtectWatching = val
		}
	}

	if cfg, err := m.configRepo.Get("disk.pause_on_critical"); err == nil && cfg != nil {
		if val, err := strconv.ParseBool(cfg.Value); err == nil {
			m.config.PauseOnCritical = val
		}
	}

	return nil
}

// Start 启动监控服务
func (m *Monitor) Start() {
	if !m.config.Enabled {
		logger.Info("Disk monitor is disabled")
		return
	}

	m.ticker = time.NewTicker(CheckInterval)

	logger.Info("Disk monitor started",
		"check_interval", CheckInterval.String(),
		"warning_threshold_gb", m.config.WarningThresholdGB,
		"critical_threshold_gb", m.config.CriticalThresholdGB)

	// 立即执行一次检查
	m.checkDisk()

	go func() {
		for {
			select {
			case <-m.ticker.C:
				m.checkDisk()
			case <-m.stopChan:
				logger.Info("Disk monitor stopped")
				return
			}
		}
	}()
}

// Stop 停止监控服务
func (m *Monitor) Stop() {
	if m.ticker != nil {
		m.ticker.Stop()
	}
	close(m.stopChan)
}

// checkDisk 检查磁盘空间
func (m *Monitor) checkDisk() {
	// 获取下载路径
	downloadPath := m.getDownloadPath()

	// 获取磁盘信息
	diskInfo, err := m.GetDiskInfo(downloadPath)
	if err != nil {
		logger.Error("Failed to get disk info", "error", err.Error())
		return
	}

	logger.Debug("Disk check completed",
		"path", diskInfo.Path,
		"free_gb", fmt.Sprintf("%.2f", diskInfo.FreeGB),
		"usage_percent", fmt.Sprintf("%.1f%%", diskInfo.UsagePercent),
		"status", diskInfo.Status)

	if err := m.RecordDiskSample(downloadPath, diskInfo); err != nil {
		logger.Warn("Failed to persist disk sample", "error", err.Error())
	}

	// 检查状态变化
	if diskInfo.Status != m.lastStatus {
		m.handleStatusChange(diskInfo)
		m.lastStatus = diskInfo.Status
	}

	// 如果处于危险状态且启用了自动清理
	if diskInfo.Status == StatusCritical && m.config.AutoCleanupEnabled {
		if _, err := m.RunCleanup(CleanupOptions{Trigger: CleanupTriggerAuto, ProtectWatching: m.config.CleanupProtectWatching}); err != nil {
			logger.Error("Auto cleanup failed", "error", err.Error())
		}
	}
}

// GetDiskInfo 获取磁盘信息
func (m *Monitor) GetDiskInfo(path string) (*DiskInfo, error) {
	// 确保路径存在
	if _, err := os.Stat(path); err != nil {
		// 尝试创建目录
		if err := os.MkdirAll(path, 0755); err != nil {
			return nil, fmt.Errorf("path does not exist and cannot be created: %w", err)
		}
	}

	// 获取文件系统统计信息
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return nil, fmt.Errorf("failed to stat filesystem: %w", err)
	}

	// 计算磁盘信息
	// stat.Bsize 是块大小，stat.Blocks 是总块数，stat.Bavail 是可用块数
	blockSize := uint64(stat.Bsize)
	totalBytes := blockSize * stat.Blocks
	freeBytes := blockSize * stat.Bavail
	usedBytes := totalBytes - freeBytes

	totalGB := float64(totalBytes) / (1024 * 1024 * 1024)
	freeGB := float64(freeBytes) / (1024 * 1024 * 1024)
	usedGB := float64(usedBytes) / (1024 * 1024 * 1024)
	usagePercent := float64(usedBytes) / float64(totalBytes) * 100

	// 确定状态
	status := StatusHealthy
	if freeGB < float64(m.config.CriticalThresholdGB) {
		status = StatusCritical
	} else if freeGB < float64(m.config.WarningThresholdGB) {
		status = StatusWarning
	}

	return &DiskInfo{
		Path:         path,
		TotalGB:      totalGB,
		UsedGB:       usedGB,
		FreeGB:       freeGB,
		TotalBytes:   int64(totalBytes),
		UsedBytes:    int64(usedBytes),
		FreeBytes:    int64(freeBytes),
		UsagePercent: usagePercent,
		Status:       status,
	}, nil
}

// RecordDiskSample persists a disk reading for trend history.
func (m *Monitor) RecordDiskSample(downloadPath string, info *DiskInfo) error {
	if m.db == nil || info == nil {
		return nil
	}
	return m.db.Create(&model.DiskSample{
		Path:         info.Path,
		DownloadPath: downloadPath,
		TotalBytes:   info.TotalBytes,
		UsedBytes:    info.UsedBytes,
		FreeBytes:    info.FreeBytes,
		UsagePercent: info.UsagePercent,
		Status:       info.Status,
		CreatedAt:    time.Now(),
	}).Error
}

// ListDiskSamples returns recent disk samples, newest first unless callers reorder them.
func (m *Monitor) ListDiskSamples(limit int) ([]model.DiskSample, error) {
	if m.db == nil {
		return nil, nil
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var samples []model.DiskSample
	err := m.db.Order("created_at DESC").Limit(limit).Find(&samples).Error
	return samples, err
}

// ListCleanupRecords returns recent cleanup summaries.
func (m *Monitor) ListCleanupRecords(offset, limit int) ([]model.DiskCleanupRecord, int64, error) {
	if m.db == nil {
		return nil, 0, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	var records []model.DiskCleanupRecord
	var total int64
	query := m.db.Model(&model.DiskCleanupRecord{})
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&records).Error
	return records, total, err
}

// handleStatusChange 处理状态变化
func (m *Monitor) handleStatusChange(info *DiskInfo) {
	switch info.Status {
	case StatusWarning:
		logger.Warn("Disk space warning",
			"free_gb", fmt.Sprintf("%.2f", info.FreeGB),
			"threshold_gb", m.config.WarningThresholdGB)
		m.sendWarningNotification(info)

	case StatusCritical:
		logger.Error("Disk space critical",
			"free_gb", fmt.Sprintf("%.2f", info.FreeGB),
			"threshold_gb", m.config.CriticalThresholdGB)
		m.sendCriticalNotification(info)

		// 如果配置了危险时暂停下载
		if m.config.PauseOnCritical {
			m.pauseDownloads()
		}

	case StatusHealthy:
		// 从警告/危险状态恢复到健康
		if m.lastStatus != "" && m.lastStatus != StatusHealthy {
			logger.Info("Disk space recovered",
				"free_gb", fmt.Sprintf("%.2f", info.FreeGB))
			m.sendRecoveryNotification(info)

			// 如果之前暂停了下载，恢复下载
			if m.config.PauseOnCritical {
				m.resumeDownloads()
			}
		}
	}
}

// RunCleanup executes cleanup and persists a summary record.
func (m *Monitor) RunCleanup(opts CleanupOptions) (*CleanupResult, error) {
	if opts.Trigger == "" {
		opts.Trigger = CleanupTriggerManual
	}
	if opts.Strategy == "" {
		opts.Strategy = m.config.CleanupStrategy
	}
	if opts.KeepDays <= 0 {
		opts.KeepDays = m.config.CleanupKeepDays
	}
	if opts.KeepGB <= 0 {
		opts.KeepGB = m.config.CleanupKeepGB
	}
	if opts.DownloadPath == "" {
		opts.DownloadPath = m.getDownloadPath()
	}
	beforeInfo, err := m.GetDiskInfo(opts.DownloadPath)
	if err != nil {
		return nil, err
	}

	mediaState := m.checkMediaLibrary()
	result := &CleanupResult{
		BeforeFreeGB:       beforeInfo.FreeGB,
		BeforeFreeBytes:    beforeInfo.FreeBytes,
		MediaLibraryStatus: mediaState.Status,
	}

	logger.Info("Starting disk cleanup", "trigger", opts.Trigger, "strategy", opts.Strategy, "download_path", opts.DownloadPath)

	downloads, _, err := m.downloadRepo.List(0, 1000, model.DownloadStatusCompleted)
	if err != nil {
		return nil, err
	}
	m.sortCleanupCandidates(downloads)

	cutoff := time.Now().AddDate(0, 0, -opts.KeepDays)
	targetFreeBytes := opts.KeepGB * 1024 * 1024 * 1024
	needSpaceCleanup := opts.Strategy == CleanupBySpace || opts.Strategy == CleanupHybrid

	for i := range downloads {
		download := downloads[i]
		if !m.shouldConsiderForStrategy(&download, opts.Strategy, cutoff, result.FreedBytes, targetFreeBytes, beforeInfo.FreeBytes, needSpaceCleanup) {
			continue
		}

		item := CleanupItem{DownloadID: download.ID, Path: download.FilePath}
		if opts.ProtectWatching && mediaState.ConservativeSkipAll {
			item.Action = "skipped"
			item.Reason = mediaState.Message
			result.SkippedCount++
			result.Items = append(result.Items, item)
			continue
		}
		if opts.ProtectWatching && mediaState.IsProtected(download) {
			item.Action = "skipped"
			item.Reason = "media is currently watched or recently played"
			result.SkippedCount++
			result.Items = append(result.Items, item)
			continue
		}

		freed, err := m.deleteDownloadSafely(&download, opts.DownloadPath)
		if err != nil {
			item.Action = "skipped"
			item.Reason = err.Error()
			result.SkippedCount++
			result.Items = append(result.Items, item)
			continue
		}
		item.Action = "deleted"
		item.FreedBytes = freed
		result.DeletedCount++
		result.FreedBytes += freed
		result.Items = append(result.Items, item)
	}

	afterInfo, err := m.GetDiskInfo(opts.DownloadPath)
	if err == nil {
		result.AfterFreeGB = afterInfo.FreeGB
		result.AfterFreeBytes = afterInfo.FreeBytes
	}
	result.Cleaned = result.DeletedCount > 0

	if err := m.recordCleanup(opts, result); err != nil {
		logger.Warn("Failed to persist cleanup record", "error", err.Error())
	}

	logger.Info("Disk cleanup completed", "deleted_count", result.DeletedCount, "skipped_count", result.SkippedCount, "freed_bytes", result.FreedBytes)
	if opts.Trigger == CleanupTriggerAuto && result.DeletedCount > 0 && m.notificationSvc != nil {
		m.sendCleanupNotification(result.DeletedCount, float64(result.FreedBytes)/(1024*1024*1024))
	}

	return result, nil
}

func (m *Monitor) sortCleanupCandidates(downloads []model.Download) {
	sort.Slice(downloads, func(i, j int) bool {
		left := downloads[i].CreatedAt
		right := downloads[j].CreatedAt
		if downloads[i].DownloadedAt != nil {
			left = *downloads[i].DownloadedAt
		}
		if downloads[j].DownloadedAt != nil {
			right = *downloads[j].DownloadedAt
		}
		return left.Before(right)
	})
}

func (m *Monitor) shouldConsiderForStrategy(download *model.Download, strategy CleanupStrategy, cutoff time.Time, freedBytes, targetFreeBytes, beforeFreeBytes int64, needSpaceCleanup bool) bool {
	ageEligible := download.DownloadedAt != nil && download.DownloadedAt.Before(cutoff)
	spaceStillNeeded := needSpaceCleanup && beforeFreeBytes+freedBytes < targetFreeBytes

	switch strategy {
	case CleanupByAge:
		return ageEligible
	case CleanupBySpace:
		return spaceStillNeeded
	case CleanupHybrid:
		return ageEligible || spaceStillNeeded
	default:
		return ageEligible
	}
}

func (m *Monitor) recordCleanup(opts CleanupOptions, result *CleanupResult) error {
	if m.db == nil || result == nil {
		return nil
	}
	messageBytes, _ := json.Marshal(result.Items)
	return m.db.Create(&model.DiskCleanupRecord{
		Trigger:            opts.Trigger,
		Strategy:           string(opts.Strategy),
		DownloadPath:       opts.DownloadPath,
		DeletedCount:       result.DeletedCount,
		SkippedCount:       result.SkippedCount,
		FreedBytes:         result.FreedBytes,
		BeforeFreeBytes:    result.BeforeFreeBytes,
		AfterFreeBytes:     result.AfterFreeBytes,
		MediaLibraryStatus: result.MediaLibraryStatus,
		Message:            string(messageBytes),
		CreatedAt:          time.Now(),
	}).Error
}

// CheckMediaLibrary returns the current Plex/Jellyfin connection status.
func (m *Monitor) CheckMediaLibrary() (string, string) {
	state := m.checkMediaLibrary()
	return state.Status, state.Message
}

func (m *Monitor) checkMediaLibrary() mediaLibraryState {
	serverType := strings.ToLower(strings.TrimSpace(m.getConfigValue("media_library.type")))
	baseURL := strings.TrimRight(strings.TrimSpace(m.getConfigValue("media_library.url")), "/")
	token := strings.TrimSpace(m.getConfigValue("media_library.token"))
	userID := strings.TrimSpace(m.getConfigValue("media_library.user_id"))
	recentHours := m.getConfigInt("media_library.recent_play_hours", 24)

	if serverType == "" && baseURL == "" && token == "" {
		return mediaLibraryState{Status: MediaLibraryStatusUnconfigured, Message: "media library is not configured", ConservativeSkipAll: true}
	}
	if serverType == "" || baseURL == "" || token == "" {
		return mediaLibraryState{Status: MediaLibraryStatusFailed, Message: "media library configuration is incomplete", ConservativeSkipAll: true}
	}

	var paths []string
	var err error
	switch serverType {
	case "jellyfin", "emby":
		paths, err = fetchJellyfinProtectedPaths(baseURL, token, userID, recentHours)
	case "plex":
		paths, err = fetchPlexProtectedPaths(baseURL, token, recentHours)
	default:
		err = fmt.Errorf("unsupported media library type %q", serverType)
	}
	if err != nil {
		return mediaLibraryState{Status: MediaLibraryStatusFailed, Message: err.Error(), ConservativeSkipAll: true}
	}

	protected := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		if realPath, err := filepath.EvalSymlinks(abs); err == nil {
			abs = realPath
		}
		protected[abs] = struct{}{}
	}

	return mediaLibraryState{Status: MediaLibraryStatusConnected, Message: "media library connected", ProtectedPaths: protected}
}

func fetchJellyfinProtectedPaths(baseURL, token, userID string, recentHours int) ([]string, error) {
	endpoint := baseURL + "/Sessions"
	var sessions []struct {
		NowPlayingItem *struct {
			Path string `json:"Path"`
		} `json:"NowPlayingItem"`
	}
	if err := getJSON(endpoint, map[string]string{"X-Emby-Token": token}, &sessions); err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(sessions))
	for _, session := range sessions {
		if session.NowPlayingItem != nil && session.NowPlayingItem.Path != "" {
			paths = append(paths, session.NowPlayingItem.Path)
		}
	}
	if userID != "" {
		recent, err := fetchJellyfinRecentPaths(baseURL, token, userID, recentHours)
		if err != nil {
			return nil, err
		}
		paths = append(paths, recent...)
	}
	return paths, nil
}

func fetchJellyfinRecentPaths(baseURL, token, userID string, recentHours int) ([]string, error) {
	if recentHours <= 0 {
		recentHours = 24
	}
	cutoff := time.Now().Add(-time.Duration(recentHours) * time.Hour)
	endpoint := fmt.Sprintf("%s/Users/%s/Items?Recursive=true&Filters=IsPlayed&Fields=Path,DatePlayed&SortBy=DatePlayed&SortOrder=Descending&Limit=50", baseURL, url.PathEscape(userID))
	var response struct {
		Items []struct {
			Path       string `json:"Path"`
			DatePlayed string `json:"DatePlayed"`
		} `json:"Items"`
	}
	if err := getJSON(endpoint, map[string]string{"X-Emby-Token": token}, &response); err != nil {
		return nil, err
	}
	var paths []string
	for _, item := range response.Items {
		if item.Path == "" || item.DatePlayed == "" {
			continue
		}
		playedAt, err := time.Parse(time.RFC3339, item.DatePlayed)
		if err != nil || playedAt.Before(cutoff) {
			continue
		}
		paths = append(paths, item.Path)
	}
	return paths, nil
}

func fetchPlexProtectedPaths(baseURL, token string, recentHours int) ([]string, error) {
	endpoint := baseURL + "/status/sessions?X-Plex-Token=" + url.QueryEscape(token)
	paths, err := fetchPlexPathsFromEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	recent, err := fetchPlexRecentPaths(baseURL, token, recentHours)
	if err != nil {
		return nil, err
	}
	return append(paths, recent...), nil
}

func fetchPlexRecentPaths(baseURL, token string, recentHours int) ([]string, error) {
	if recentHours <= 0 {
		recentHours = 24
	}
	endpoint := fmt.Sprintf("%s/status/sessions/history/all?X-Plex-Token=%s&sort=viewedAt:desc", baseURL, url.QueryEscape(token))
	type historyContainer struct {
		Videos []struct {
			ViewedAt int64 `xml:"viewedAt,attr"`
			Media    []struct {
				Parts []struct {
					File string `xml:"file,attr"`
				} `xml:"Part"`
			} `xml:"Media"`
		} `xml:"Video"`
	}
	var container historyContainer
	if err := getXML(endpoint, nil, &container); err != nil {
		return nil, err
	}
	cutoff := time.Now().Add(-time.Duration(recentHours) * time.Hour)
	var paths []string
	for _, video := range container.Videos {
		if video.ViewedAt > 0 && time.Unix(video.ViewedAt, 0).Before(cutoff) {
			continue
		}
		for _, media := range video.Media {
			for _, part := range media.Parts {
				if part.File != "" {
					paths = append(paths, part.File)
				}
			}
		}
	}
	return paths, nil
}

func fetchPlexPathsFromEndpoint(endpoint string) ([]string, error) {
	type mediaContainer struct {
		Videos []struct {
			Media []struct {
				Parts []struct {
					File string `xml:"file,attr"`
				} `xml:"Part"`
			} `xml:"Media"`
		} `xml:"Video"`
	}
	var container mediaContainer
	if err := getXML(endpoint, nil, &container); err != nil {
		return nil, err
	}
	var paths []string
	for _, video := range container.Videos {
		for _, media := range video.Media {
			for _, part := range media.Parts {
				if part.File != "" {
					paths = append(paths, part.File)
				}
			}
		}
	}
	return paths, nil
}

func getJSON(endpoint string, headers map[string]string, target any) error {
	body, err := getHTTPBody(endpoint, headers)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, target)
}

func getXML(endpoint string, headers map[string]string, target any) error {
	body, err := getHTTPBody(endpoint, headers)
	if err != nil {
		return err
	}
	return xml.Unmarshal(body, target)
}

func getHTTPBody(endpoint string, headers map[string]string) ([]byte, error) {
	client := http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("media library returned status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// deleteDownloadSafely removes a file or directory and only then deletes the DB row.
func (m *Monitor) deleteDownloadSafely(download *model.Download, downloadRoot string) (int64, error) {
	deletePath, err := validateDeletePath(download.FilePath, downloadRoot)
	if err != nil {
		return 0, err
	}

	size := m.getFileSize(deletePath)
	if err := os.RemoveAll(deletePath); err != nil {
		return 0, fmt.Errorf("failed to delete file: %w", err)
	}
	if err := m.downloadRepo.Delete(download.ID); err != nil {
		return 0, fmt.Errorf("failed to delete download record: %w", err)
	}

	logger.Info("Deleted download", "download_id", download.ID, "title", download.Title, "path", deletePath, "freed_bytes", size)
	return size, nil
}

func validateDeletePath(rawPath, rawRoot string) (string, error) {
	if strings.TrimSpace(rawPath) == "" {
		return "", errors.New("empty path")
	}
	if strings.TrimSpace(rawRoot) == "" {
		return "", errors.New("empty download root")
	}

	rootAbs, err := filepath.Abs(rawRoot)
	if err != nil {
		return "", fmt.Errorf("invalid download root: %w", err)
	}
	pathAbs, err := filepath.Abs(rawPath)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}

	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", fmt.Errorf("invalid download root: %w", err)
	}
	pathReal, err := filepath.EvalSymlinks(pathAbs)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}

	if pathReal == rootReal {
		return "", errors.New("refusing to delete download root")
	}
	rel, err := filepath.Rel(rootReal, pathReal)
	if err != nil {
		return "", fmt.Errorf("invalid relative path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errors.New("path is outside download root")
	}

	return pathReal, nil
}

func isSameOrChild(path, root string) bool {
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// getFileSize 获取文件大小
func (m *Monitor) getFileSize(path string) int64 {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	if err != nil {
		return 0
	}
	return size
}

// getDownloadPath 获取下载路径
func (m *Monitor) getDownloadPath() string {
	// 从配置获取
	if value := m.getConfigValue("download_path"); value != "" {
		return value
	}
	// 默认路径
	return "/downloads"
}

func (m *Monitor) getConfigValue(key string) string {
	if m.configRepo == nil {
		return ""
	}
	cfg, err := m.configRepo.Get(key)
	if err != nil || cfg == nil {
		return ""
	}
	return cfg.Value
}

func (m *Monitor) getConfigInt(key string, defaultValue int) int {
	value := m.getConfigValue(key)
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return parsed
}

// pauseDownloads 暂停新下载
func (m *Monitor) pauseDownloads() {
	if downloadPaused.CompareAndSwap(false, true) {
		logger.Info("Pausing new downloads due to critical disk space")
	}
}

// resumeDownloads 恢复下载
func (m *Monitor) resumeDownloads() {
	if downloadPaused.CompareAndSwap(true, false) {
		logger.Info("Resuming downloads after disk space recovered")
	}
}

// IsDownloadsPaused 检查下载是否被暂停
func IsDownloadsPaused() bool {
	return downloadPaused.Load()
}

// 通知方法

func (m *Monitor) sendWarningNotification(info *DiskInfo) {
	if m.notificationSvc == nil {
		return
	}
	m.notificationSvc.Send(model.NotificationPayload{
		Event:   model.EventDiskWarning,
		Title:   "⚠️ 磁盘空间警告",
		Message: fmt.Sprintf("磁盘剩余空间不足 %.0f GB（当前剩余 %.2f GB）\n建议清理旧文件或扩容存储", float64(m.config.WarningThresholdGB), info.FreeGB),
		Data: map[string]any{
			"free_gb":       info.FreeGB,
			"threshold_gb":  m.config.WarningThresholdGB,
			"usage_percent": info.UsagePercent,
			"status":        "warning",
		},
		Timestamp: time.Now(),
	})
}

func (m *Monitor) sendCriticalNotification(info *DiskInfo) {
	if m.notificationSvc == nil {
		return
	}
	m.notificationSvc.Send(model.NotificationPayload{
		Event:   model.EventDiskCritical,
		Title:   "🚨 磁盘空间危险",
		Message: fmt.Sprintf("磁盘剩余空间严重不足 %.0f GB（当前剩余 %.2f GB）\n新下载任务已暂停，请立即清理", float64(m.config.CriticalThresholdGB), info.FreeGB),
		Data: map[string]any{
			"free_gb":       info.FreeGB,
			"threshold_gb":  m.config.CriticalThresholdGB,
			"usage_percent": info.UsagePercent,
			"status":        "critical",
		},
		Timestamp: time.Now(),
	})
}

func (m *Monitor) sendRecoveryNotification(info *DiskInfo) {
	if m.notificationSvc == nil {
		return
	}
	m.notificationSvc.Send(model.NotificationPayload{
		Event:   model.EventDiskRecovered,
		Title:   "✅ 磁盘空间恢复",
		Message: fmt.Sprintf("磁盘空间已恢复正常（剩余 %.2f GB）\n下载任务已恢复", info.FreeGB),
		Data: map[string]any{
			"free_gb":       info.FreeGB,
			"usage_percent": info.UsagePercent,
			"status":        "healthy",
		},
		Timestamp: time.Now(),
	})
}

func (m *Monitor) sendCleanupNotification(count int, cleanedGB float64) {
	if m.notificationSvc == nil {
		return
	}
	m.notificationSvc.Send(model.NotificationPayload{
		Event:   model.EventAutoCleanup,
		Title:   "🧹 自动清理完成",
		Message: fmt.Sprintf("已自动清理 %d 个文件，释放 %.2f GB 空间", count, cleanedGB),
		Data: map[string]any{
			"cleaned_count": count,
			"cleaned_gb":    cleanedGB,
		},
		Timestamp: time.Now(),
	})
}
