package handler

import (
	"net/http"
	"strconv"

	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/gin-gonic/gin"
)

// LogHandler 日志处理器
type LogHandler struct {
	repo repository.LogRepository
}

// NewLogHandler 创建日志处理器实例
func NewLogHandler(repo repository.LogRepository) *LogHandler {
	return &LogHandler{repo: repo}
}

// List 获取日志列表
func (h *LogHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	level := c.Query("level")
	module := c.Query("module")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}

	logs, total, err := h.repo.List(page, pageSize, level, module)
	if err != nil {
		logger.Error("Failed to query logs",
			"page", page,
			"page_size", pageSize,
			"level", level,
			"error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to query logs",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data": gin.H{
			"list":  logs,
			"total": total,
			"page":  page,
		},
	})
}

// Clear 清空日志
func (h *LogHandler) Clear(c *gin.Context) {
	// 删除7天前的日志
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	if days < 1 {
		days = 7
	}

	err := h.repo.DeleteBefore(days)
	if err != nil {
		logger.Error("Failed to clear logs",
			"days", days,
			"error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to clear logs",
		})
		return
	}

	logger.Info("Logs cleared",
		"days", days,
		"client_ip", c.ClientIP())

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
	})
}
