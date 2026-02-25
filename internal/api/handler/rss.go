package handler

import (
	"net/http"

	"github.com/WormW/auto-rss/internal/service/scheduler"
	"github.com/gin-gonic/gin"
)

// RSSHandler RSS 处理器
type RSSHandler struct {
	scheduler scheduler.Scheduler
}

// NewRSSHandler 创建 RSS 处理器实例
func NewRSSHandler(s scheduler.Scheduler) *RSSHandler {
	return &RSSHandler{scheduler: s}
}

// Refresh 手动刷新 RSS
func (h *RSSHandler) Refresh(c *gin.Context) {
	if h.scheduler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"code":    503,
			"message": "scheduler not initialized",
		})
		return
	}

	if err := h.scheduler.RunRSSCheckNow(); err != nil {
		c.JSON(http.StatusConflict, gin.H{
			"code":    409,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "RSS refresh started",
	})
}
