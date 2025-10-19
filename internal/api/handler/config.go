package handler

import (
	"net/http"

	"github.com/WormW/auto-rss/internal/repository"
	"github.com/gin-gonic/gin"
)

// ConfigHandler 配置处理器
type ConfigHandler struct {
	repo repository.ConfigRepository
}

// NewConfigHandler 创建配置处理器实例
func NewConfigHandler(repo repository.ConfigRepository) *ConfigHandler {
	return &ConfigHandler{repo: repo}
}

// GetAll 获取所有配置
func (h *ConfigHandler) GetAll(c *gin.Context) {
	configs, err := h.repo.GetAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to get configs",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data":    configs,
	})
}

// Update 更新配置
func (h *ConfigHandler) Update(c *gin.Context) {
	var req struct {
		Key   string `json:"key" binding:"required"`
		Value string `json:"value" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid request body",
		})
		return
	}

	if err := h.repo.Set(req.Key, req.Value); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to update config",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
	})
}
