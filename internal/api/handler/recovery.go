package handler

import (
	"net/http"

	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/bangumi"
	"github.com/WormW/auto-rss/internal/service/recovery"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// RecoveryHandler 扫描恢复处理器
type RecoveryHandler struct {
	db               *gorm.DB
	subscriptionRepo repository.SubscriptionRepository
	downloadRepo     repository.DownloadRepository
	configRepo       repository.ConfigRepository
	bangumiService   *bangumi.BangumiService
}

// NewRecoveryHandler 创建恢复处理器实例
func NewRecoveryHandler(
	db *gorm.DB,
	subscriptionRepo repository.SubscriptionRepository,
	downloadRepo repository.DownloadRepository,
	configRepo repository.ConfigRepository,
	bangumiService *bangumi.BangumiService,
) *RecoveryHandler {
	return &RecoveryHandler{
		db:               db,
		subscriptionRepo: subscriptionRepo,
		downloadRepo:     downloadRepo,
		configRepo:       configRepo,
		bangumiService:   bangumiService,
	}
}

// ScanRequest 扫描请求
type ScanRequest struct {
	DryRun         bool   `json:"dry_run"`
	SubscriptionID *uint  `json:"subscription_id,omitempty"`
}

// Scan 执行扫描恢复
func (h *RecoveryHandler) Scan(c *gin.Context) {
	var req ScanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误: " + err.Error(),
		})
		return
	}

	scanner := recovery.NewScanner(h.db, h.subscriptionRepo, h.downloadRepo, h.configRepo, h.bangumiService)
	result, err := scanner.Scan(&recovery.ScanRequest{
		DryRun:         req.DryRun,
		SubscriptionID: req.SubscriptionID,
	})
	if err != nil {
		logger.Error("Recovery scan failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "扫描恢复失败: " + err.Error(),
		})
		return
	}

	msg := "扫描完成"
	if result.Applied {
		msg = "扫描并修正完成"
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": msg,
		"data":    result,
	})
}
