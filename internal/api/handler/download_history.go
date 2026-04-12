package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/WormW/auto-rss/internal/repository"
	"github.com/gin-gonic/gin"
)

// DownloadHistoryHandler 下载历史处理器
type DownloadHistoryHandler struct {
	downloadRepo repository.DownloadRepository
}

// NewDownloadHistoryHandler 创建下载历史处理器实例
func NewDownloadHistoryHandler(downloadRepo repository.DownloadRepository) *DownloadHistoryHandler {
	return &DownloadHistoryHandler{downloadRepo: downloadRepo}
}

// GetHistory 获取下载历史记录
// GET /api/v1/downloads/history
func (h *DownloadHistoryHandler) GetHistory(c *gin.Context) {
	// 解析分页参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	// 构建筛选条件
	filter := &repository.DownloadHistoryFilter{}

	// 解析 subscription_id
	if subIDStr := c.Query("subscription_id"); subIDStr != "" {
		if subID, err := strconv.ParseUint(subIDStr, 10, 32); err == nil {
			subIDUint := uint(subID)
			filter.SubscriptionID = &subIDUint
		}
	}

	// 解析 status
	if status := c.Query("status"); status != "" {
		filter.Status = status
	}

	// 解析日期范围
	const dateFormat = "2006-01-02"

	if startDateStr := c.Query("start_date"); startDateStr != "" {
		if startDate, err := time.Parse(dateFormat, startDateStr); err == nil {
			// 设置为当天开始时间 00:00:00
			startDate = time.Date(startDate.Year(), startDate.Month(), startDate.Day(), 0, 0, 0, 0, startDate.Location())
			filter.StartDate = &startDate
		} else {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "Invalid start_date format, expected YYYY-MM-DD",
			})
			return
		}
	}

	if endDateStr := c.Query("end_date"); endDateStr != "" {
		if endDate, err := time.Parse(dateFormat, endDateStr); err == nil {
			// 设置为当天结束时间 23:59:59
			endDate = time.Date(endDate.Year(), endDate.Month(), endDate.Day(), 23, 59, 59, 0, endDate.Location())
			filter.EndDate = &endDate
		} else {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "Invalid end_date format, expected YYYY-MM-DD",
			})
			return
		}
	}

	offset := (page - 1) * pageSize

	// 查询下载历史
	downloads, total, err := h.downloadRepo.GetDownloadHistory(filter, offset, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to get download history",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data": gin.H{
			"list":      downloads,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		},
	})
}

// GetStatistics 获取下载统计数据
// GET /api/v1/downloads/statistics
func (h *DownloadHistoryHandler) GetStatistics(c *gin.Context) {
	// 解析 days 参数，默认 7 天
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	if days < 1 {
		days = 7
	}
	if days > 365 {
		days = 365
	}

	// 获取统计数据
	stats, err := h.downloadRepo.GetDownloadStatistics(days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to get download statistics",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data":    stats,
	})
}
