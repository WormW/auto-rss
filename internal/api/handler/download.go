package handler

import (
	"net/http"
	"strconv"

	"github.com/WormW/auto-rss/internal/pkg/constants"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/pkg/utils"
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
		if err := h.qbClient.DeleteTorrent(download.TorrentHash, true); err != nil {
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
			"hash", download.TorrentHash)
		if err := h.qbClient.DeleteTorrent(download.TorrentHash, true); err != nil {
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

	// 5. 如果 qbClient 可用，立即添加新种子
	if h.qbClient != nil {
		// 获取下载路径配置
		basePath := constants.DefaultDownloadPath
		if h.configRepo != nil {
			if config, err := h.configRepo.Get("download_path"); err == nil && config.Value != "" {
				basePath = config.Value
			}
		}

		// 生成下载路径（包含番剧名子目录）
		downloadPath := basePath
		if download.Subscription.Name != "" {
			downloadPath = utils.GenerateDownloadPath(basePath, download.Subscription.Name)
		}

		// 添加种子到 qBittorrent
		torrentHash, err := h.qbClient.AddTorrent(download.TorrentURL, downloadPath, "")
		if err != nil {
			logger.Error("Failed to add torrent for retry",
				"download_id", id,
				"torrent_url", download.TorrentURL,
				"error", err.Error())
			download.Status = "failed"
			download.LastError = err.Error()
			h.repo.Update(download)
			c.JSON(http.StatusOK, gin.H{
				"code":    500,
				"message": "Failed to add torrent: " + err.Error(),
			})
			return
		}

		// 更新为下载中状态
		download.Status = "downloading"
		download.TorrentHash = torrentHash
		if err := h.repo.Update(download); err != nil {
			logger.Error("Failed to update download status after retry",
				"download_id", id,
				"error", err.Error())
		}

		logger.Info("Retry successful - torrent added",
			"download_id", id,
			"new_hash", torrentHash)
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
		}
	}

	// 从 qBittorrent 删除种子
	if len(hashes) > 0 && h.qbClient != nil {
		for _, hash := range hashes {
			if err := h.qbClient.DeleteTorrent(hash, true); err != nil {
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
				if err := h.qbClient.DeleteTorrent(download.TorrentHash, true); err != nil {
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
