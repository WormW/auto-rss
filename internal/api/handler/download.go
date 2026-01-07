package handler

import (
	"net/http"
	"strconv"

	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/downloader"
	"github.com/gin-gonic/gin"
)

// DownloadHandler 下载处理器
type DownloadHandler struct {
	repo     repository.DownloadRepository
	qbClient downloader.QBittorrentClient
}

// NewDownloadHandler 创建下载处理器实例
func NewDownloadHandler(repo repository.DownloadRepository, qbClient downloader.QBittorrentClient) *DownloadHandler {
	return &DownloadHandler{repo: repo, qbClient: qbClient}
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

	// TODO: 实现重试逻辑
	if err := h.repo.UpdateStatus(uint(id), "pending"); err != nil {
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
