package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/downloader"
	"github.com/gin-gonic/gin"
)

// DownloadHandler 下载处理器
type DownloadHandler struct {
	repo       repository.DownloadRepository
	qbClient   downloader.QBittorrentClient
	configRepo repository.ConfigRepository
}

type DownloadDiagnosticAction struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Enabled bool   `json:"enabled"`
}

type DownloadDiagnostics struct {
	ID           uint                       `json:"id"`
	Status       string                     `json:"status"`
	Severity     string                     `json:"severity"`
	Category     string                     `json:"category"`
	Title        string                     `json:"title"`
	Detail       string                     `json:"detail"`
	CanRetry     bool                       `json:"can_retry"`
	RetryBlocked string                     `json:"retry_blocked,omitempty"`
	Checks       map[string]bool            `json:"checks"`
	Actions      []DownloadDiagnosticAction `json:"actions"`
}

// NewDownloadHandler 创建下载处理器实例
func NewDownloadHandler(repo repository.DownloadRepository, qbClient downloader.QBittorrentClient, configRepo repository.ConfigRepository) *DownloadHandler {
	return &DownloadHandler{repo: repo, qbClient: qbClient, configRepo: configRepo}
}

// GetByID 获取下载任务详情
func (h *DownloadHandler) GetByID(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid download ID",
		})
		return
	}

	download, err := h.repo.GetByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "Download not found",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data":    download,
	})
}

// Diagnostics 获取下载任务失败诊断信息
func (h *DownloadHandler) Diagnostics(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid download ID",
		})
		return
	}

	download, err := h.repo.GetByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "Download not found",
		})
		return
	}

	diagnostics := buildDownloadDiagnostics(download)
	if h.qbClient != nil && download.TorrentHash != "" {
		if _, err := h.qbClient.GetTorrentInfo(download.TorrentHash); err == nil {
			diagnostics.Checks["qbittorrent_task_found"] = true
		} else {
			diagnostics.Checks["qbittorrent_task_found"] = false
			if diagnostics.Category == "unknown" && download.Status != "completed" {
				diagnostics.Category = "qbittorrent"
				diagnostics.Title = "qBittorrent 中未找到任务"
				diagnostics.Detail = "数据库记录存在，但 qBittorrent 当前没有这个 hash 对应的任务。"
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data":    diagnostics,
	})
}

func buildDownloadDiagnostics(download *model.Download) DownloadDiagnostics {
	lastError := strings.TrimSpace(download.LastError)
	if lastError == "" {
		lastError = strings.TrimSpace(download.ErrorMessage)
	}

	checks := map[string]bool{
		"has_torrent_url":  download.TorrentURL != "",
		"has_torrent_hash": download.TorrentHash != "",
		"has_file_path":    download.FilePath != "",
		"has_error":        lastError != "",
		"retry_available":  download.MaxRetries <= 0 || download.RetryCount < download.MaxRetries,
	}

	diagnostics := DownloadDiagnostics{
		ID:       download.ID,
		Status:   download.Status,
		Severity: "info",
		Category: "unknown",
		Title:    "暂无异常信息",
		Detail:   "这个任务目前没有记录明确的失败原因。",
		Checks:   checks,
	}

	switch download.Status {
	case "failed":
		diagnostics.Severity = "error"
		diagnostics.Title = "下载任务失败"
	case "stalled":
		diagnostics.Severity = "warning"
		diagnostics.Category = "qbittorrent"
		diagnostics.Title = "下载停滞"
		diagnostics.Detail = "qBittorrent 任务处于停滞状态，通常和种子活跃度、网络或保存路径有关。"
	case "pending":
		diagnostics.Severity = "warning"
		diagnostics.Category = "queue"
		diagnostics.Title = "等待处理"
		diagnostics.Detail = "任务仍在等待下载器接收或后台调度处理。"
	case "completed":
		diagnostics.Severity = "success"
		diagnostics.Category = "completed"
		diagnostics.Title = "任务已完成"
		diagnostics.Detail = "下载已经完成。"
	}

	if lastError != "" {
		diagnostics.Detail = lastError
		diagnostics.Category, diagnostics.Title = classifyDownloadError(lastError)
	}

	if download.TorrentURL == "" {
		diagnostics.Category = "rss"
		diagnostics.Title = "缺少种子链接"
		diagnostics.Detail = "RSS 条目没有解析到可用的种子链接。"
	}

	diagnostics.CanRetry = diagnostics.Severity != "success" && download.TorrentURL != ""
	if download.MaxRetries > 0 && download.RetryCount >= download.MaxRetries {
		diagnostics.CanRetry = true
		diagnostics.RetryBlocked = "自动重试次数已用尽，仍可手动重试。"
	}

	diagnostics.Actions = []DownloadDiagnosticAction{
		{Key: "retry", Label: "重试", Enabled: diagnostics.CanRetry},
		{Key: "delete", Label: "删除", Enabled: true},
		{Key: "check_config", Label: "检查配置", Enabled: diagnostics.Category == "qbittorrent" || diagnostics.Category == "disk" || diagnostics.Category == "file"},
	}

	return diagnostics
}

func classifyDownloadError(message string) (string, string) {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "rss") || strings.Contains(message, "获取RSS") || strings.Contains(lower, "parse rss"):
		return "rss", "RSS 获取或解析失败"
	case strings.Contains(lower, "torrent file") || strings.Contains(message, "种子文件"):
		return "torrent", "种子文件获取失败"
	case strings.Contains(lower, "qbittorrent") || strings.Contains(lower, "add torrent") || strings.Contains(lower, "login") || strings.Contains(lower, "unauthorized"):
		return "qbittorrent", "下载器连接或添加任务失败"
	case strings.Contains(lower, "no space") || strings.Contains(message, "空间") || strings.Contains(lower, "disk"):
		return "disk", "磁盘空间或路径异常"
	case strings.Contains(lower, "rename") || strings.Contains(message, "重命名"):
		return "file", "文件重命名失败"
	case strings.Contains(lower, "permission") || strings.Contains(message, "权限") || strings.Contains(lower, "no such file"):
		return "file", "文件访问失败"
	case strings.Contains(lower, "timeout") || strings.Contains(message, "超时"):
		return "network", "网络请求超时"
	default:
		return "unknown", "失败原因未归类"
	}
}

// List 获取下载任务列表
func (h *DownloadHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	status := c.Query("status")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	offset := (page - 1) * pageSize
	downloads, total, err := h.repo.List(offset, pageSize, status)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to get download list",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data": gin.H{
			"list":  downloads,
			"total": total,
			"page":  page,
		},
	})
}

// Delete 删除下载任务
func (h *DownloadHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid download ID",
		})
		return
	}

	// 先获取下载记录以获取 torrent hash
	download, err := h.repo.GetByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "Download not found",
		})
		return
	}

	// 如果有 hash 且 qbClient 可用，从 qBittorrent 删除种子
	if download.TorrentHash != "" && h.qbClient != nil {
		logger.Info("Deleting qBittorrent torrent with payload",
			"download_id", id,
			"hash", download.TorrentHash,
			"file_path", download.FilePath,
			"renamed_path", download.RenamedPath)
		if err := h.qbClient.DeleteTorrentWithPayload(download.TorrentHash); err != nil {
			logger.Warn("Failed to delete torrent from qBittorrent",
				"download_id", id,
				"hash", download.TorrentHash,
				"error", err.Error())
			// 继续删除数据库记录，不阻塞
		} else {
			logger.Info("Torrent deleted from qBittorrent",
				"download_id", id,
				"hash", download.TorrentHash)
		}
	}

	if err := h.repo.Delete(uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to delete download",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
	})
}

// Retry 重试下载任务
func (h *DownloadHandler) Retry(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid download ID",
		})
		return
	}

	// 1. 获取下载记录
	download, err := h.repo.GetByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "Download not found",
		})
		return
	}

	// 2. 如果存在旧种子，从 qBittorrent 删除
	if download.TorrentHash != "" && h.qbClient != nil {
		logger.Info("Deleting old torrent for retry",
			"download_id", id,
			"hash", download.TorrentHash,
			"file_path", download.FilePath,
			"renamed_path", download.RenamedPath)
		if err := h.qbClient.DeleteTorrentWithPayload(download.TorrentHash); err != nil {
			logger.Warn("Failed to delete old torrent from qBittorrent (ignoring)",
				"download_id", id,
				"hash", download.TorrentHash,
				"error", err.Error())
			// 忽略删除错误，旧种子可能不存在
		}
	}

	// 3. 重置重试相关字段
	download.RetryCount = 0
	download.RetryReason = "user_retry"
	download.NextRetryAt = nil
	download.LastError = ""
	download.Status = "pending"
	download.TorrentHash = "" // 清除旧hash

	// 4. 保存重置后的状态
	if err := h.repo.Update(download); err != nil {
		logger.Error("Failed to reset download for retry",
			"download_id", id,
			"error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to retry download",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
	})
}

// BatchDelete 批量删除下载任务
func (h *DownloadHandler) BatchDelete(c *gin.Context) {
	var req struct {
		IDs []uint `json:"ids" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	if len(req.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请选择要删除的任务",
		})
		return
	}

	logger.Info("Batch deleting downloads",
		"count", len(req.IDs),
		"ids", req.IDs,
		"client_ip", c.ClientIP())

	// 先获取所有下载记录以获取 hash
	var hashes []string
	for _, id := range req.IDs {
		download, err := h.repo.GetByID(id)
		if err == nil && download.TorrentHash != "" {
			hashes = append(hashes, download.TorrentHash)
			logger.Info("Queued qBittorrent torrent payload deletion for batch delete",
				"download_id", id,
				"hash", download.TorrentHash,
				"file_path", download.FilePath,
				"renamed_path", download.RenamedPath)
		}
	}

	// 从 qBittorrent 删除种子
	if len(hashes) > 0 && h.qbClient != nil {
		for _, hash := range hashes {
			if err := h.qbClient.DeleteTorrentWithPayload(hash); err != nil {
				logger.Warn("Failed to delete torrent from qBittorrent",
					"hash", hash,
					"error", err.Error())
			}
		}
		logger.Info("Torrents deleted from qBittorrent", "count", len(hashes))
	}

	if err := h.repo.BatchDelete(req.IDs); err != nil {
		logger.Error("Failed to batch delete downloads", "error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "批量删除失败",
		})
		return
	}

	logger.Info("Batch delete completed", "count", len(req.IDs))

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data": gin.H{
			"deleted": len(req.IDs),
		},
	})
}

// Clear 清空下载任务
func (h *DownloadHandler) Clear(c *gin.Context) {
	status := c.Query("status")

	logger.Info("Clearing downloads",
		"status", status,
		"client_ip", c.ClientIP())

	// 先获取要删除的下载记录以获取 hash
	downloads, _, err := h.repo.List(0, 10000, status)
	if err != nil {
		logger.Error("Failed to get downloads for clearing", "error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取下载列表失败",
		})
		return
	}

	// 从 qBittorrent 删除种子
	if h.qbClient != nil {
		deletedCount := 0
		for _, download := range downloads {
			if download.TorrentHash != "" {
				logger.Info("Deleting qBittorrent torrent with payload during clear",
					"download_id", download.ID,
					"hash", download.TorrentHash,
					"file_path", download.FilePath,
					"renamed_path", download.RenamedPath)
				if err := h.qbClient.DeleteTorrentWithPayload(download.TorrentHash); err != nil {
					logger.Warn("Failed to delete torrent from qBittorrent",
						"hash", download.TorrentHash,
						"error", err.Error())
				} else {
					deletedCount++
				}
			}
		}
		logger.Info("Torrents deleted from qBittorrent", "count", deletedCount)
	}

	var message string
	if status != "" {
		err = h.repo.DeleteByStatus(status)
		message = "已清空状态为 " + status + " 的任务"
	} else {
		err = h.repo.DeleteAll()
		message = "已清空所有任务"
	}

	if err != nil {
		logger.Error("Failed to clear downloads", "status", status, "error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "清空失败",
		})
		return
	}

	logger.Info("Downloads cleared", "status", status)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": message,
	})
}
