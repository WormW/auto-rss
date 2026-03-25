package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/service/notification"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// NotificationHandler 通知处理器
type NotificationHandler struct {
	db              *gorm.DB
	notificationSvc notification.Service
	wsHub           *notification.WebSocketHub
}

// NewNotificationHandler 创建通知处理器实例
func NewNotificationHandler(db *gorm.DB, svc notification.Service, wsHub *notification.WebSocketHub) *NotificationHandler {
	return &NotificationHandler{
		db:              db,
		notificationSvc: svc,
		wsHub:           wsHub,
	}
}

// GetSettings 获取通知渠道配置列表
func (h *NotificationHandler) GetSettings(c *gin.Context) {
	var settings []model.NotificationSetting
	if err := h.db.Find(&settings).Error; err != nil {
		logger.Error("Failed to get notification settings", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取通知配置失败",
		})
		return
	}
	if settings == nil {
		settings = []model.NotificationSetting{}
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data":    settings,
	})
}

// GetSetting 获取单个通知渠道配置
func (h *NotificationHandler) GetSetting(c *gin.Context) {
	channel := c.Param("channel")
	if channel == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Channel is required",
		})
		return
	}
	var setting model.NotificationSetting
	if err := h.db.Where("channel = ?", channel).First(&setting).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "配置不存在",
			})
			return
		}
		logger.Error("Failed to get notification setting", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取配置失败",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data":    setting,
	})
}

// UpdateSetting 更新通知渠道配置
func (h *NotificationHandler) UpdateSetting(c *gin.Context) {
	var req struct {
		Channel string          `json:"channel" binding:"required"`
		Enabled bool            `json:"enabled"`
		Config  json.RawMessage `json:"config"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误: " + err.Error(),
		})
		return
	}
	if req.Channel != "telegram" && req.Channel != "email" && req.Channel != "webhook" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "不支持的渠道类型",
		})
		return
	}
	if len(req.Config) > 0 {
		var configMap map[string]interface{}
		if err := json.Unmarshal(req.Config, &configMap); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "配置格式错误，必须是有效的 JSON",
			})
			return
		}
	}
	setting := &model.NotificationSetting{
		Channel: req.Channel,
		Enabled: req.Enabled,
		Config:  string(req.Config),
	}
	result := h.db.Where("channel = ?", setting.Channel).Assign(setting).FirstOrCreate(setting)
	if result.Error != nil {
		logger.Error("Failed to save notification setting", "error", result.Error)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "保存配置失败",
		})
		return
	}
	logger.Info("Notification setting saved", "channel", req.Channel, "enabled", req.Enabled)
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "保存成功",
		"data":    setting,
	})
}

// DeleteSetting 删除通知渠道配置
func (h *NotificationHandler) DeleteSetting(c *gin.Context) {
	channel := c.Param("channel")
	if channel == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Channel is required",
		})
		return
	}
	if err := h.db.Where("channel = ?", channel).Delete(&model.NotificationSetting{}).Error; err != nil {
		logger.Error("Failed to delete notification setting", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "删除配置失败",
		})
		return
	}
	logger.Info("Notification setting deleted", "channel", channel)
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "删除成功",
	})
}

// TestChannel 测试通知渠道
func (h *NotificationHandler) TestChannel(c *gin.Context) {
	var req struct {
		Channel string          `json:"channel" binding:"required"`
		Config  json.RawMessage `json:"config"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误: " + err.Error(),
		})
		return
	}
	var err error
	switch req.Channel {
	case "telegram":
		config := &notification.TelegramConfig{}
		if err = json.Unmarshal(req.Config, config); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "Telegram 配置格式错误",
			})
			return
		}
		err = notification.TestTelegramConfig(config)
	case "webhook":
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Webhook 渠道尚未实现",
		})
		return
	case "email":
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Email 渠道尚未实现",
		})
		return
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "不支持的渠道类型",
		})
		return
	}
	if err != nil {
		logger.Error("Notification test failed", "channel", req.Channel, "error", err)
		c.JSON(http.StatusOK, gin.H{
			"code":    1,
			"message": "测试失败: " + err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "测试消息已发送，请检查您的设备",
	})
}

// ListNotifications 获取通知历史记录
func (h *NotificationHandler) ListNotifications(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	status := c.Query("status")
	channel := c.Query("channel")
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	var notifications []model.Notification
	var total int64
	query := h.db.Model(&model.Notification{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if channel != "" {
		query = query.Where("type = ?", channel)
	}
	if err := query.Count(&total).Error; err != nil {
		logger.Error("Failed to count notifications", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取通知列表失败",
		})
		return
	}
	offset := (page - 1) * pageSize
	if err := query.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&notifications).Error; err != nil {
		logger.Error("Failed to get notifications", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取通知列表失败",
		})
		return
	}
	if notifications == nil {
		notifications = []model.Notification{}
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data": gin.H{
			"list":  notifications,
			"total": total,
			"page":  page,
		},
	})
}

// WebSocketHandler 处理 WebSocket 连接
func (h *NotificationHandler) WebSocketHandler(c *gin.Context) {
	if h.wsHub == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"code":    503,
			"message": "WebSocket 服务未就绪",
		})
		return
	}
	notification.HandleWebSocket(h.wsHub)(c)
}

// GetWebSocketStatus 获取 WebSocket 连接状态
func (h *NotificationHandler) GetWebSocketStatus(c *gin.Context) {
	if h.wsHub == nil {
		c.JSON(http.StatusOK, gin.H{
			"code":    0,
			"message": "Success",
			"data": gin.H{
				"connected_clients": 0,
				"enabled":           false,
			},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data": gin.H{
			"connected_clients": h.wsHub.GetClientCount(),
			"enabled":           true,
		},
	})
}
