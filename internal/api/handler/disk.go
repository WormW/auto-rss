package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/disk"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// DiskHandler 磁盘监控处理器
type DiskHandler struct {
	db           *gorm.DB
	monitor      *disk.Monitor
	downloadRepo repository.DownloadRepository
	configRepo   repository.ConfigRepository
}

// NewDiskHandler 创建磁盘处理器实例
func NewDiskHandler(
	db *gorm.DB,
	downloadRepo repository.DownloadRepository,
	subscriptionRepo repository.SubscriptionRepository,
	configRepo repository.ConfigRepository,
) *DiskHandler {
	monitor := disk.NewMonitor(db, downloadRepo, subscriptionRepo, configRepo)

	return &DiskHandler{
		db:           db,
		monitor:      monitor,
		downloadRepo: downloadRepo,
		configRepo:   configRepo,
	}
}

// DiskStatusResponse 磁盘状态响应
type DiskStatusResponse struct {
	Path         string  `json:"path"`
	DownloadPath string  `json:"download_path"`
	Total        int64   `json:"total"`
	Free         int64   `json:"free"`
	Used         int64   `json:"used"`
	UsagePercent float64 `json:"usage_percent"`
	Status       string  `json:"status"`
}

type DiskHistoryResponse struct {
	Samples  []DiskSampleResponse        `json:"samples"`
	Cleanup  []modelDiskCleanupRecordDTO `json:"cleanup"`
	List     []modelDiskCleanupRecordDTO `json:"list"`
	Total    int64                       `json:"total"`
	Page     int                         `json:"page"`
	PageSize int                         `json:"page_size"`
}

type DiskSampleResponse struct {
	Path         string  `json:"path"`
	DownloadPath string  `json:"download_path"`
	Total        int64   `json:"total"`
	Used         int64   `json:"used"`
	Free         int64   `json:"free"`
	UsagePercent float64 `json:"usage_percent"`
	Status       string  `json:"status"`
	CreatedAt    string  `json:"created_at"`
}

type modelDiskCleanupRecordDTO struct {
	ID                 uint     `json:"id"`
	Trigger            string   `json:"trigger"`
	Strategy           string   `json:"strategy"`
	DownloadPath       string   `json:"download_path"`
	DeletedCount       int      `json:"deleted_count"`
	SkippedCount       int      `json:"skipped_count"`
	FailedCount        int      `json:"failed_count"`
	FailedPaths        []string `json:"failed_paths"`
	FreedBytes         int64    `json:"freed_bytes"`
	BeforeFreeBytes    int64    `json:"before_free_bytes"`
	AfterFreeBytes     int64    `json:"after_free_bytes"`
	MediaLibraryStatus string   `json:"media_library_status"`
	Message            string   `json:"message"`
	CreatedAt          string   `json:"created_at"`
}

// GetStatus 获取磁盘状态
func (h *DiskHandler) GetStatus(c *gin.Context) {
	downloadPath := h.getDownloadPath()

	info, err := h.monitor.GetDiskInfo(downloadPath)
	if err != nil {
		logger.Error("Failed to get disk info", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取磁盘状态失败",
		})
		return
	}

	// 转换为字节
	totalBytes := info.TotalBytes
	freeBytes := info.FreeBytes
	usedBytes := info.UsedBytes
	if err := h.monitor.RecordDiskSample(downloadPath, info); err != nil {
		logger.Warn("Failed to persist disk sample from status request", "error", err.Error())
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data": []DiskStatusResponse{
			{
				Path:         info.Path,
				DownloadPath: downloadPath,
				Total:        totalBytes,
				Free:         freeBytes,
				Used:         usedBytes,
				UsagePercent: info.UsagePercent,
				Status:       info.Status,
			},
		},
	})
}

// GetInfo 获取磁盘信息（简化版）
func (h *DiskHandler) GetInfo(c *gin.Context) {
	h.GetStatus(c)
}

// CleanupSettingsRequest 清理设置请求
type CleanupSettingsRequest struct {
	Enabled             bool   `json:"enabled"`
	Strategy            string `json:"strategy"`
	RetentionDays       int    `json:"retention_days"`
	MinFreeGB           int64  `json:"min_free_gb"`
	WarningThresholdGB  int64  `json:"warning_threshold_gb"`
	CriticalThresholdGB int64  `json:"critical_threshold_gb"`
	ProtectWatching     bool   `json:"protect_watching"`
}

// GetSettings 获取清理设置
func (h *DiskHandler) GetSettings(c *gin.Context) {
	// 从配置库读取设置
	enabled := h.getConfigBool("disk.auto_cleanup_enabled", false)
	strategy := h.getConfigString("disk.cleanup_strategy", "hybrid")
	retentionDays := h.getConfigInt("disk.cleanup_keep_days", 30)
	minFreeGB := h.getConfigInt64("disk.cleanup_keep_gb", 50)
	warningThreshold := h.getConfigInt64("disk.warning_threshold_gb", 10)
	criticalThreshold := h.getConfigInt64("disk.critical_threshold_gb", 5)
	protectWatching := h.getConfigBool("disk.cleanup_protect_watching", true)
	mediaStatus, mediaMessage := h.monitor.CheckMediaLibrary()

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data": gin.H{
			"enabled":               enabled,
			"strategy":              strategy,
			"retention_days":        retentionDays,
			"min_free_gb":           minFreeGB,
			"warning_threshold_gb":  warningThreshold,
			"critical_threshold_gb": criticalThreshold,
			"protect_watching":      protectWatching,
			"media_library_status":  mediaStatus,
			"media_library_message": mediaMessage,
		},
	})
}

// UpdateSettings 更新清理设置
func (h *DiskHandler) UpdateSettings(c *gin.Context) {
	var req CleanupSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误: " + err.Error(),
		})
		return
	}

	// 保存设置到数据库
	h.setConfig("disk.auto_cleanup_enabled", req.Enabled)
	h.setConfig("disk.cleanup_strategy", req.Strategy)
	h.setConfig("disk.cleanup_keep_days", req.RetentionDays)
	h.setConfig("disk.cleanup_keep_gb", req.MinFreeGB)
	h.setConfig("disk.warning_threshold_gb", req.WarningThresholdGB)
	h.setConfig("disk.critical_threshold_gb", req.CriticalThresholdGB)
	h.setConfig("disk.cleanup_protect_watching", req.ProtectWatching)

	logger.Info("Disk cleanup settings updated",
		"enabled", req.Enabled,
		"strategy", req.Strategy)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "设置已保存",
	})
}

// TriggerCleanupRequest 触发清理请求
type TriggerCleanupRequest struct {
	Strategy string `json:"strategy"`
	KeepDays int    `json:"keep_days"`
	KeepGB   int64  `json:"keep_gb"`
}

// TriggerCleanup 触发手动清理
func (h *DiskHandler) TriggerCleanup(c *gin.Context) {
	var req TriggerCleanupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// 使用默认策略
		req.Strategy = "hybrid"
		req.KeepDays = 30
	}

	downloadPath := h.getDownloadPath()
	strategy := disk.CleanupStrategy(req.Strategy)
	if strategy == "" {
		strategy = disk.CleanupHybrid
	}
	result, err := h.monitor.RunCleanup(disk.CleanupOptions{
		Trigger:         disk.CleanupTriggerManual,
		Strategy:        strategy,
		KeepDays:        req.KeepDays,
		KeepGB:          req.KeepGB,
		DownloadPath:    downloadPath,
		ProtectWatching: h.getConfigBool("disk.cleanup_protect_watching", true),
	})
	if err != nil {
		logger.Error("Failed to run manual cleanup", "error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "清理失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data":    result,
	})
}

// GetHistory 获取清理历史
func (h *DiskHandler) GetHistory(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	samples, err := h.monitor.ListDiskSamples(pageSize * 3)
	if err != nil {
		logger.Error("Failed to list disk samples", "error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "获取磁盘历史失败"})
		return
	}
	records, total, err := h.monitor.ListCleanupRecords((page-1)*pageSize, pageSize)
	if err != nil {
		logger.Error("Failed to list cleanup records", "error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "获取清理历史失败"})
		return
	}

	sampleDTOs := make([]DiskSampleResponse, 0, len(samples))
	for i := len(samples) - 1; i >= 0; i-- {
		sample := samples[i]
		sampleDTOs = append(sampleDTOs, DiskSampleResponse{
			Path:         sample.Path,
			DownloadPath: sample.DownloadPath,
			Total:        sample.TotalBytes,
			Used:         sample.UsedBytes,
			Free:         sample.FreeBytes,
			UsagePercent: sample.UsagePercent,
			Status:       sample.Status,
			CreatedAt:    sample.CreatedAt.Format(time.RFC3339),
		})
	}
	cleanupDTOs := make([]modelDiskCleanupRecordDTO, 0, len(records))
	for _, record := range records {
		cleanupDTOs = append(cleanupDTOs, cleanupRecordDTO(record))
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data": DiskHistoryResponse{
			Samples:  sampleDTOs,
			Cleanup:  cleanupDTOs,
			List:     cleanupDTOs,
			Total:    total,
			Page:     page,
			PageSize: pageSize,
		},
	})
}

func cleanupRecordDTO(record model.DiskCleanupRecord) modelDiskCleanupRecordDTO {
	failedPaths := decodeCleanupFailedPaths(record.FailedPaths)
	return modelDiskCleanupRecordDTO{
		ID:                 record.ID,
		Trigger:            record.Trigger,
		Strategy:           record.Strategy,
		DownloadPath:       record.DownloadPath,
		DeletedCount:       record.DeletedCount,
		SkippedCount:       record.SkippedCount,
		FailedCount:        cleanupFailedCount(record.FailedCount, failedPaths),
		FailedPaths:        failedPaths,
		FreedBytes:         record.FreedBytes,
		BeforeFreeBytes:    record.BeforeFreeBytes,
		AfterFreeBytes:     record.AfterFreeBytes,
		MediaLibraryStatus: record.MediaLibraryStatus,
		Message:            record.Message,
		CreatedAt:          record.CreatedAt.Format(time.RFC3339),
	}
}

func decodeCleanupFailedPaths(raw string) []string {
	var paths []string
	if raw == "" || json.Unmarshal([]byte(raw), &paths) != nil {
		return []string{}
	}
	return paths
}

func cleanupFailedCount(recorded int, paths []string) int {
	if recorded > 0 {
		return recorded
	}
	return len(paths)
}

// 辅助方法

func (h *DiskHandler) getDownloadPath() string {
	if h.configRepo != nil {
		if cfg, err := h.configRepo.Get("download_path"); err == nil && cfg != nil {
			if cfg.Value != "" {
				return cfg.Value
			}
		}
	}
	return "/downloads"
}

func (h *DiskHandler) getConfigBool(key string, defaultValue bool) bool {
	if h.configRepo == nil {
		return defaultValue
	}
	cfg, err := h.configRepo.Get(key)
	if err != nil || cfg == nil {
		return defaultValue
	}
	if val, err := strconv.ParseBool(cfg.Value); err == nil {
		return val
	}
	return defaultValue
}

func (h *DiskHandler) getConfigString(key string, defaultValue string) string {
	if h.configRepo == nil {
		return defaultValue
	}
	cfg, err := h.configRepo.Get(key)
	if err != nil || cfg == nil {
		return defaultValue
	}
	return cfg.Value
}

func (h *DiskHandler) getConfigInt(key string, defaultValue int) int {
	if h.configRepo == nil {
		return defaultValue
	}
	cfg, err := h.configRepo.Get(key)
	if err != nil || cfg == nil {
		return defaultValue
	}
	if val, err := strconv.Atoi(cfg.Value); err == nil {
		return val
	}
	return defaultValue
}

func (h *DiskHandler) getConfigInt64(key string, defaultValue int64) int64 {
	if h.configRepo == nil {
		return defaultValue
	}
	cfg, err := h.configRepo.Get(key)
	if err != nil || cfg == nil {
		return defaultValue
	}
	if val, err := strconv.ParseInt(cfg.Value, 10, 64); err == nil {
		return val
	}
	return defaultValue
}

func (h *DiskHandler) setConfig(key string, value interface{}) {
	if h.configRepo == nil {
		return
	}
	var strValue string
	switch v := value.(type) {
	case string:
		strValue = v
	case bool:
		strValue = strconv.FormatBool(v)
	case int:
		strValue = strconv.Itoa(v)
	case int64:
		strValue = strconv.FormatInt(v, 10)
	default:
		strValue = ""
	}
	_ = h.configRepo.Set(key, strValue)
}
