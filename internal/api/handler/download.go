package handler

import (
	"net/http"
	"strconv"

	"github.com/WormW/auto-rss/internal/repository"
	"github.com/gin-gonic/gin"
)

// DownloadHandler 下载处理器
type DownloadHandler struct {
	repo repository.DownloadRepository
}

// NewDownloadHandler 创建下载处理器实例
func NewDownloadHandler(repo repository.DownloadRepository) *DownloadHandler {
	return &DownloadHandler{repo: repo}
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
