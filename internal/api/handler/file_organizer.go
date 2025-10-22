package handler

import (
	"net/http"

	"github.com/WormW/auto-rss/internal/app"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/gin-gonic/gin"
)

// FileOrganizerHandler 文件整理处理器
type FileOrganizerHandler struct {
	appCtx *app.Context
}

// NewFileOrganizerHandler 创建文件整理处理器实例
func NewFileOrganizerHandler(appCtx *app.Context) *FileOrganizerHandler {
	return &FileOrganizerHandler{
		appCtx: appCtx,
	}
}

// TriggerScan 手动触发文件扫描
func (h *FileOrganizerHandler) TriggerScan(c *gin.Context) {
	organizer := h.appCtx.GetFileOrganizer()
	if organizer == nil {
		logger.Warn("File organizer not initialized")
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "文件整理服务未启用",
		})
		return
	}

	organizer.TriggerScan()

	logger.Info("Manual file scan triggered via API")

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "文件整理任务已触发",
	})
}

// ReloadConfig 重新加载文件整理配置
func (h *FileOrganizerHandler) ReloadConfig(c *gin.Context) {
	logger.Info("Reloading file organizer configuration")

	if err := h.appCtx.ReloadFileOrganizer(); err != nil {
		logger.Error("Failed to reload file organizer", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "重新加载文件整理配置失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "文件整理配置已重新加载",
	})
}
