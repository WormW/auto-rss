package handler

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"time"

	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// HealthChecker 健康检查器
type HealthChecker struct {
	db        *gorm.DB
	qbChecker QBittorrentChecker
	config    *HealthConfig
}

// QBittorrentChecker qBittorrent 连接检查接口
type QBittorrentChecker interface {
	GetVersion() (string, error)
}

// HealthConfig 健康检查配置
type HealthConfig struct {
	DiskCheckEnabled    bool
	DiskWarningPercent  float64
	DiskCriticalPercent float64
}

// NewHealthChecker 创建健康检查器
func NewHealthChecker(db *gorm.DB, qbChecker QBittorrentChecker) *HealthChecker {
	return &HealthChecker{
		db:        db,
		qbChecker: qbChecker,
		config: &HealthConfig{
			DiskCheckEnabled:    true,
			DiskWarningPercent:  90,
			DiskCriticalPercent: 95,
		},
	}
}

// HealthResponse 健康检查响应
type HealthResponse struct {
	Status    string                 `json:"status"`
	Timestamp string                 `json:"timestamp"`
	Version   string                 `json:"version"`
	Checks    map[string]HealthCheck `json:"checks"`
}

// HealthCheck 单项健康检查
type HealthCheck struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Details any    `json:"details,omitempty"`
}

// ReadinessResponse 就绪检查响应
type ReadinessResponse struct {
	Status    string            `json:"status"`
	Timestamp string            `json:"timestamp"`
	Checks    map[string]string `json:"checks"`
}

// SystemStatus 系统状态响应
type SystemStatus struct {
	Status    string         `json:"status"`
	Timestamp string         `json:"timestamp"`
	GoVersion string         `json:"go_version"`
	Uptime    string         `json:"uptime"`
	Memory    MemoryStatus   `json:"memory"`
	Database  DatabaseStatus `json:"database"`
	Downloads DownloadStatus `json:"downloads"`
}

// MemoryStatus 内存状态
type MemoryStatus struct {
	Alloc      string `json:"alloc"`
	TotalAlloc string `json:"total_alloc"`
	Sys        string `json:"sys"`
	NumGC      uint32 `json:"num_gc"`
}

// DatabaseStatus 数据库状态
type DatabaseStatus struct {
	Connected       bool   `json:"connected"`
	OpenConnections int    `json:"open_connections"`
	InUse           int    `json:"in_use"`
	Idle            int    `json:"idle"`
}

// DownloadStatus 下载状态统计
type DownloadStatus struct {
	Active       int64 `json:"active"`
	Pending      int64 `json:"pending"`
	Completed24h int64 `json:"completed_24h"`
	Failed24h    int64 `json:"failed_24h"`
}

const version = "0.2.0"

var startTime = time.Now()

// HealthHandler 健康检查端点
func (h *HealthChecker) HealthHandler(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	checks := make(map[string]HealthCheck)
	overallStatus := "healthy"

	// 检查数据库
	dbCheck := h.checkDatabase(ctx)
	checks["database"] = dbCheck
	if dbCheck.Status != "healthy" {
		overallStatus = "unhealthy"
	}

	// 检查 qBittorrent
	if h.qbChecker != nil {
		qbCheck := h.checkQBittorrent()
		checks["qbittorrent"] = qbCheck
		if qbCheck.Status != "healthy" && qbCheck.Status != "unknown" {
			overallStatus = "degraded"
		}
	}

	// 检查磁盘空间
	diskCheck := h.checkDiskSpace()
	checks["disk"] = diskCheck
	if diskCheck.Status == "critical" {
		overallStatus = "unhealthy"
	} else if diskCheck.Status == "warning" && overallStatus == "healthy" {
		overallStatus = "degraded"
	}

	response := HealthResponse{
		Status:    overallStatus,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Version:   version,
		Checks:    checks,
	}

	statusCode := http.StatusOK
	if overallStatus == "unhealthy" {
		statusCode = http.StatusServiceUnavailable
	}

	c.JSON(statusCode, response)
}

// ReadyHandler 就绪检查端点
func (h *HealthChecker) ReadyHandler(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	checks := make(map[string]string)
	ready := true

	// 检查数据库连接
	if h.db != nil {
		sqlDB, err := h.db.DB()
		if err != nil {
			checks["database"] = "not_initialized"
			ready = false
		} else {
			if err := sqlDB.PingContext(ctx); err != nil {
				checks["database"] = "disconnected"
				ready = false
			} else {
				checks["database"] = "connected"
			}
		}
	} else {
		checks["database"] = "not_configured"
		ready = false
	}

	// 检查 qBittorrent（可选，不影响就绪状态）
	if h.qbChecker != nil {
		if _, err := h.qbChecker.GetVersion(); err != nil {
			checks["qbittorrent"] = "disconnected"
		} else {
			checks["qbittorrent"] = "connected"
		}
	} else {
		checks["qbittorrent"] = "not_configured"
	}

	status := "ready"
	if !ready {
		status = "not_ready"
	}

	response := ReadinessResponse{
		Status:    status,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Checks:    checks,
	}

	statusCode := http.StatusOK
	if !ready {
		statusCode = http.StatusServiceUnavailable
	}

	c.JSON(statusCode, response)
}

// LiveHandler 存活检查端点
func (h *HealthChecker) LiveHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "alive",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

// SystemStatusHandler 系统状态端点
func (h *HealthChecker) SystemStatusHandler(c *gin.Context) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// 获取数据库统计
	dbStatus := DatabaseStatus{Connected: false}
	if h.db != nil {
		sqlDB, err := h.db.DB()
		if err == nil {
			if err := sqlDB.Ping(); err == nil {
				dbStatus.Connected = true
				stat := sqlDB.Stats()
				dbStatus.OpenConnections = stat.OpenConnections
				dbStatus.InUse = stat.InUse
				dbStatus.Idle = stat.Idle
			}
		}
	}

	// 获取下载统计（简化版，实际应从 repository 查询）
	downloadStatus := DownloadStatus{}

	status := SystemStatus{
		Status:    "running",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		GoVersion: runtime.Version(),
		Uptime:    time.Since(startTime).Round(time.Second).String(),
		Memory: MemoryStatus{
			Alloc:      formatBytes(m.Alloc),
			TotalAlloc: formatBytes(m.TotalAlloc),
			Sys:        formatBytes(m.Sys),
			NumGC:      m.NumGC,
		},
		Database:  dbStatus,
		Downloads: downloadStatus,
	}

	c.JSON(http.StatusOK, status)
}

// checkDatabase 检查数据库连接
func (h *HealthChecker) checkDatabase(ctx context.Context) HealthCheck {
	if h.db == nil {
		return HealthCheck{
			Status:  "unhealthy",
			Message: "database not initialized",
		}
	}

	sqlDB, err := h.db.DB()
	if err != nil {
		return HealthCheck{
			Status:  "unhealthy",
			Message: fmt.Sprintf("failed to get database instance: %v", err),
		}
	}

	if err := sqlDB.PingContext(ctx); err != nil {
		return HealthCheck{
			Status:  "unhealthy",
			Message: fmt.Sprintf("database ping failed: %v", err),
		}
	}

	stats := sqlDB.Stats()
	return HealthCheck{
		Status: "healthy",
		Details: map[string]interface{}{
			"open_connections": stats.OpenConnections,
			"in_use":           stats.InUse,
			"idle":             stats.Idle,
			"wait_count":       stats.WaitCount,
		},
	}
}

// checkQBittorrent 检查 qBittorrent 连接
func (h *HealthChecker) checkQBittorrent() HealthCheck {
	if h.qbChecker == nil {
		return HealthCheck{
			Status:  "unknown",
			Message: "qbittorrent checker not configured",
		}
	}

	version, err := h.qbChecker.GetVersion()
	if err != nil {
		return HealthCheck{
			Status:  "unhealthy",
			Message: fmt.Sprintf("connection failed: %v", err),
		}
	}

	return HealthCheck{
		Status: "healthy",
		Details: map[string]string{
			"version": version,
		},
	}
}

// checkDiskSpace 检查磁盘空间
func (h *HealthChecker) checkDiskSpace() HealthCheck {
	if !h.config.DiskCheckEnabled {
		return HealthCheck{
			Status:  "unknown",
			Message: "disk check disabled",
		}
	}

	// 这里使用简单的统计，实际可以使用 syscall.Statfs
	// 简化实现，返回 healthy，实际项目中应实现真正的磁盘检查
	return HealthCheck{
		Status: "healthy",
		Message: "disk space check placeholder - implement platform-specific disk check",
	}
}

// formatBytes 格式化字节大小
func formatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// SimpleHealthHandler 简单的健康检查（兼容旧版）
func SimpleHealthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "ok",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"version":   version,
	})
}
