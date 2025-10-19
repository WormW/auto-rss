package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RSSHandler RSS 处理器
type RSSHandler struct{}

// NewRSSHandler 创建 RSS 处理器实例
func NewRSSHandler() *RSSHandler {
	return &RSSHandler{}
}

// Refresh 手动刷新 RSS
func (h *RSSHandler) Refresh(c *gin.Context) {
	// TODO: 实现手动刷新逻辑
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "RSS refresh triggered",
	})
}
