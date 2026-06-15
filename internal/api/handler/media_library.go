package handler

import (
	"net/http"
	"strconv"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/medialibrary"
	"github.com/gin-gonic/gin"
)

// MediaLibraryHandler handles media library integration APIs.
type MediaLibraryHandler struct {
	service          *medialibrary.Service
	downloadRepo     repository.DownloadRepository
	subscriptionRepo repository.SubscriptionRepository
}

// NewMediaLibraryHandler creates a media library handler.
func NewMediaLibraryHandler(
	service *medialibrary.Service,
	downloadRepo repository.DownloadRepository,
	subscriptionRepo repository.SubscriptionRepository,
) *MediaLibraryHandler {
	return &MediaLibraryHandler{
		service:          service,
		downloadRepo:     downloadRepo,
		subscriptionRepo: subscriptionRepo,
	}
}

// GetConfig returns the media library connection config without secrets.
func (h *MediaLibraryHandler) GetConfig(c *gin.Context) {
	cfg, err := h.service.GetConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "读取媒体库配置失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data":    medialibrary.PublicConfig(cfg),
	})
}

// SaveConfig saves the media library connection config.
func (h *MediaLibraryHandler) SaveConfig(c *gin.Context) {
	var req medialibrary.Config
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	if err := h.service.SaveConfig(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": err.Error(),
		})
		return
	}

	cfg, _ := h.service.GetConfig()
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "媒体库配置已保存",
		"data":    medialibrary.PublicConfig(cfg),
	})
}

// TestConnection tests a media library connection without saving it.
func (h *MediaLibraryHandler) TestConnection(c *gin.Context) {
	var req medialibrary.Config
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	if err := h.service.TestConnection(req); err != nil {
		logger.Warn("Media library connection test failed",
			"provider", req.Provider,
			"base_url", req.BaseURL,
			"error", err.Error())
		c.JSON(http.StatusBadGateway, gin.H{
			"code":    502,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "媒体库连接成功",
	})
}

// RefreshDownload manually refreshes the media library for a download.
func (h *MediaLibraryHandler) RefreshDownload(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid download ID",
		})
		return
	}

	download, err := h.downloadRepo.GetByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "Download not found",
		})
		return
	}

	result := h.service.RefreshDownload(download)
	status := http.StatusOK
	if result.Status == medialibrary.RefreshStatusFailed {
		status = http.StatusBadRequest
	}

	c.JSON(status, gin.H{
		"code":    codeForStatus(status),
		"message": result.Message,
		"data":    result,
	})
}

// GetSubscriptionStatus returns recent media library status for a subscription.
func (h *MediaLibraryHandler) GetSubscriptionStatus(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid subscription ID",
		})
		return
	}

	subscription, err := h.subscriptionRepo.GetByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "Subscription not found",
		})
		return
	}

	downloads, err := h.downloadRepo.GetRecentBySubscription(subscription.ID, 20)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "读取媒体库状态失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data": gin.H{
			"subscription": subscription,
			"items":        buildMediaStatusItems(downloads),
		},
	})
}

func buildMediaStatusItems(downloads []model.Download) []gin.H {
	items := make([]gin.H, 0, len(downloads))
	for _, item := range downloads {
		items = append(items, gin.H{
			"id":             item.ID,
			"title":          item.Title,
			"episode":        item.Episode,
			"status":         item.Status,
			"file_path":      item.FilePath,
			"renamed_path":   item.RenamedPath,
			"library_path":   item.MediaLibraryPath,
			"refresh_status": item.MediaLibraryRefreshStatus,
			"refresh_error":  item.MediaLibraryRefreshError,
			"refreshed_at":   item.MediaLibraryRefreshedAt,
			"downloaded_at":  item.DownloadedAt,
		})
	}
	return items
}

func codeForStatus(status int) int {
	if status >= 200 && status < 300 {
		return 0
	}
	return status
}
