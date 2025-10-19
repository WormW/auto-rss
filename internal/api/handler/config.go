package handler

import (
	"net/http"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/downloader"
	"github.com/gin-gonic/gin"
)

// ConfigHandler 配置处理器
type ConfigHandler struct {
	repo      repository.ConfigRepository
	qbClient  downloader.QBittorrentClient
}

// NewConfigHandler 创建配置处理器实例
func NewConfigHandler(repo repository.ConfigRepository) *ConfigHandler {
	return &ConfigHandler{
		repo:     repo,
		qbClient: downloader.NewQBittorrentClient(),
	}
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

// TestQBittorrent 测试qBittorrent连接
func (h *ConfigHandler) TestQBittorrent(c *gin.Context) {
	var req struct {
		Host     string `json:"host" binding:"required"`
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	logger.Info("Testing qBittorrent connection",
		"host", req.Host,
		"username", req.Username,
		"client_ip", c.ClientIP())

	// 测试连接
	err := h.qbClient.TestConnection(req.Host, req.Username, req.Password)
	if err != nil {
		logger.Error("qBittorrent connection test failed",
			"host", req.Host,
			"error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": err.Error(),
		})
		return
	}

	logger.Info("qBittorrent connection test successful",
		"host", req.Host)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "连接成功",
	})
}

// SaveQBittorrentConfig 保存qBittorrent配置
func (h *ConfigHandler) SaveQBittorrentConfig(c *gin.Context) {
	var req struct {
		Host     string `json:"host" binding:"required"`
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	logger.Info("Saving qBittorrent config",
		"host", req.Host,
		"username", req.Username,
		"client_ip", c.ClientIP())

	// 保存配置
	if err := h.repo.Set("qbittorrent_host", req.Host); err != nil {
		logger.Error("Failed to save qBittorrent host", "error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "保存配置失败",
		})
		return
	}

	if err := h.repo.Set("qbittorrent_username", req.Username); err != nil {
		logger.Error("Failed to save qBittorrent username", "error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "保存配置失败",
		})
		return
	}

	if err := h.repo.Set("qbittorrent_password", req.Password); err != nil {
		logger.Error("Failed to save qBittorrent password", "error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "保存配置失败",
		})
		return
	}

	logger.Info("qBittorrent config saved successfully")

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "配置保存成功",
	})
}

// GetRenamePresets 获取重命名模板预设
func (h *ConfigHandler) GetRenamePresets(c *gin.Context) {
	presets := downloader.GetPresetTemplates()
	variables := downloader.GetTemplateVariables()

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data": gin.H{
			"presets":   presets,
			"variables": variables,
		},
	})
}

// GetRenameTemplate 获取当前重命名模板
func (h *ConfigHandler) GetRenameTemplate(c *gin.Context) {
	config, err := h.repo.Get("rename_template")
	var template string
	if err != nil {
		// 如果没有配置，返回默认模板
		template = "${title}/Season ${season}/${title} S${seasonFormat}E${episodeFormat}"
	} else {
		template = config.Value
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data": gin.H{
			"template": template,
		},
	})
}

// SaveRenameTemplate 保存重命名模板
func (h *ConfigHandler) SaveRenameTemplate(c *gin.Context) {
	var req struct {
		Template string `json:"template" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 验证模板
	if err := downloader.ValidateTemplate(req.Template); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": err.Error(),
		})
		return
	}

	// 保存模板
	if err := h.repo.Set("rename_template", req.Template); err != nil {
		logger.Error("Failed to save rename template", "error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "保存模板失败",
		})
		return
	}

	logger.Info("Rename template saved successfully", "template", req.Template)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "模板保存成功",
	})
}

// PreviewRenameTemplate 预览重命名模板效果
func (h *ConfigHandler) PreviewRenameTemplate(c *gin.Context) {
	var req struct {
		Template string `json:"template" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 验证模板
	if err := downloader.ValidateTemplate(req.Template); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": err.Error(),
		})
		return
	}

	// 创建示例数据
	sampleSubscription := &model.Subscription{
		Name:     "葬送的芙莉莲",
		Season:   1,
		Fansub:   "ANi",
		Language: "CHS",
	}

	sampleDownload := &model.Download{
		Episode: 3,
	}

	sampleCtx := &downloader.RenameContext{
		Subscription: sampleSubscription,
		Download:     sampleDownload,
		OriginalName: "[ANi] 葬送的芙莉莲 - 03 [1080p][Baha][WEB-DL][AAC AVC][CHT].mp4",
		Extension:    ".mkv",
		Resolution:   "1080p",
	}

	// 使用临时 RenameService 解析模板
	renameService := downloader.NewRenameService(req.Template)
	preview := renameService.GenerateFileName(sampleCtx)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data": gin.H{
			"preview": preview,
			"sample": gin.H{
				"title":      sampleSubscription.Name,
				"season":     sampleSubscription.Season,
				"episode":    sampleDownload.Episode,
				"fansub":     sampleSubscription.Fansub,
				"resolution": sampleCtx.Resolution,
				"language":   sampleSubscription.Language,
			},
		},
	})
}

