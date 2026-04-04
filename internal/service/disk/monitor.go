package disk

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/repository"
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
	Path       string  `json:"path"`
	TotalGB    float64 `json:"total_gb"`
	UsedGB     float64 `json:"used_gb"`
	FreeGB     float64 `json:"free_gb"`
	UsagePercent float64 `json:"usage_percent"`
	Status     string  `json:"status"`
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
	Enabled                 bool            `json:"enabled"`                  // 是否启用监控
	WarningThresholdGB      int64           `json:"warning_threshold_gb"`     // 警告阈值
	CriticalThresholdGB     int64           `json:"critical_threshold_gb"`    // 危险阈值
	AutoCleanupEnabled      bool            `json:"auto_cleanup_enabled"`     // 自动清理开关
	CleanupStrategy         CleanupStrategy `json:"cleanup_strategy"`         // 清理策略
	CleanupKeepDays         int             `json:"cleanup_keep_days"`        // 保留天数
	CleanupKeepGB           int64           `json:"cleanup_keep_gb"`          // 预留空间
	CleanupProtectWatching  bool            `json:"cleanup_protect_watching"` // 保护正在观看的
	PauseOnCritical         bool            `json:"pause_on_critical"`        // 危险时暂停下载
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
	downloadRepo repository.DownloadRepository,
	subscriptionRepo repository.SubscriptionRepository,
	configRepo repository.ConfigRepository,
) *Monitor {
	return &Monitor{
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

	// 检查状态变化
	if diskInfo.Status != m.lastStatus {
		m.handleStatusChange(diskInfo)
		m.lastStatus = diskInfo.Status
	}

	// 如果处于危险状态且启用了自动清理
	if diskInfo.Status == StatusCritical && m.config.AutoCleanupEnabled {
		m.performCleanup(diskInfo)
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
		UsagePercent: usagePercent,
		Status:       status,
	}, nil
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

// performCleanup 执行清理
func (m *Monitor) performCleanup(info *DiskInfo) {
	logger.Info("Starting auto cleanup",
		"strategy", m.config.CleanupStrategy,
		"free_gb", fmt.Sprintf("%.2f", info.FreeGB))

	var cleanedCount int
	var cleanedGB float64

	switch m.config.CleanupStrategy {
	case CleanupByAge:
		cleanedCount, cleanedGB = m.cleanupByAge()
	case CleanupBySpace:
		cleanedCount, cleanedGB = m.cleanupBySpace(info)
	case CleanupHybrid:
		cleanedCount, cleanedGB = m.cleanupHybrid(info)
	}

	logger.Info("Auto cleanup completed",
		"cleaned_count", cleanedCount,
		"cleaned_gb", fmt.Sprintf("%.2f", cleanedGB))

	// 发送清理报告
	if cleanedCount > 0 && m.notificationSvc != nil {
		m.sendCleanupNotification(cleanedCount, cleanedGB)
	}
}

// cleanupByAge 按年龄清理
func (m *Monitor) cleanupByAge() (int, float64) {
	cutoffTime := time.Now().AddDate(0, 0, -m.config.CleanupKeepDays)

	// 获取已完成的下载任务
	downloads, _, err := m.downloadRepo.List(0, 1000, "completed")
	if err != nil {
		logger.Error("Failed to list completed downloads for cleanup", "error", err.Error())
		return 0, 0
	}

	var cleanedCount int
	var cleanedBytes int64

	for _, download := range downloads {
		// 检查是否需要保护（正在观看的）
		if m.config.CleanupProtectWatching && m.isBeingWatched(&download) {
			continue
		}

		// 检查下载时间
		if download.DownloadedAt != nil && download.DownloadedAt.Before(cutoffTime) {
			if err := m.deleteDownload(&download); err != nil {
				logger.Error("Failed to delete old download",
					"download_id", download.ID,
					"error", err.Error())
				continue
			}
			cleanedCount++
			if download.FilePath != "" {
				size := m.getFileSize(download.FilePath)
				cleanedBytes += size
			}
		}
	}

	return cleanedCount, float64(cleanedBytes) / (1024 * 1024 * 1024)
}

// cleanupBySpace 按空间清理
func (m *Monitor) cleanupBySpace(info *DiskInfo) (int, float64) {
	targetFreeGB := float64(m.config.CleanupKeepGB)
	needToFreeGB := targetFreeGB - info.FreeGB

	if needToFreeGB <= 0 {
		return 0, 0
	}

	// 获取已完成的下载任务，按下载时间排序（先删除最旧的）
	downloads, _, err := m.downloadRepo.List(0, 1000, "completed")
	if err != nil {
		logger.Error("Failed to list completed downloads for cleanup", "error", err.Error())
		return 0, 0
	}

	// 按下载时间排序（最旧的在前）
	sort.Slice(downloads, func(i, j int) bool {
		if downloads[i].DownloadedAt == nil || downloads[j].DownloadedAt == nil {
			return downloads[i].CreatedAt.Before(downloads[j].CreatedAt)
		}
		return downloads[i].DownloadedAt.Before(*downloads[j].DownloadedAt)
	})

	var cleanedCount int
	var cleanedBytes int64

	for _, download := range downloads {
		if float64(cleanedBytes)/(1024*1024*1024) >= needToFreeGB {
			break
		}

		// 检查是否需要保护
		if m.config.CleanupProtectWatching && m.isBeingWatched(&download) {
			continue
		}

		if err := m.deleteDownload(&download); err != nil {
			logger.Error("Failed to delete download for space",
				"download_id", download.ID,
				"error", err.Error())
			continue
		}

		cleanedCount++
		if download.FilePath != "" {
			size := m.getFileSize(download.FilePath)
			cleanedBytes += size
		}
	}

	return cleanedCount, float64(cleanedBytes) / (1024 * 1024 * 1024)
}

// cleanupHybrid 混合清理策略
func (m *Monitor) cleanupHybrid(info *DiskInfo) (int, float64) {
	// 首先按年龄清理
	count, gb := m.cleanupByAge()

	// 如果空间仍然不足，按空间清理
	if info.FreeGB+gb < float64(m.config.CriticalThresholdGB) {
		// 更新磁盘信息
		newInfo, _ := m.GetDiskInfo(m.getDownloadPath())
		c2, g2 := m.cleanupBySpace(newInfo)
		count += c2
		gb += g2
	}

	return count, gb
}

// isBeingWatched 检查是否正在观看
// 这是一个占位实现，实际应该查询 Plex/Jellyfin API
func (m *Monitor) isBeingWatched(download *model.Download) bool {
	// TODO: 集成 Plex/Jellyfin API 检查观看状态
	// 暂时返回 false
	return false
}

// deleteDownload 删除下载文件和记录
func (m *Monitor) deleteDownload(download *model.Download) error {
	// 删除文件
	if download.FilePath != "" {
		if err := os.RemoveAll(download.FilePath); err != nil {
			logger.Warn("Failed to delete file",
				"path", download.FilePath,
				"error", err.Error())
			// 继续删除数据库记录
		}
	}

	// 删除数据库记录
	if err := m.downloadRepo.Delete(download.ID); err != nil {
		return fmt.Errorf("failed to delete download record: %w", err)
	}

	logger.Info("Deleted download",
		"download_id", download.ID,
		"title", download.Title,
		"path", download.FilePath)

	return nil
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
	if m.configRepo != nil {
		if cfg, err := m.configRepo.Get("download_path"); err == nil && cfg != nil {
			if cfg.Value != "" {
				return cfg.Value
			}
		}
	}
	// 默认路径
	return "/downloads"
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
