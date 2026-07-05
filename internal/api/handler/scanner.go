package handler

import (
	"net/http"
	"strconv"

	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/scanner"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ScannerHandler 文件夹扫描处理器
type ScannerHandler struct {
	db               *gorm.DB
	subscriptionRepo repository.SubscriptionRepository
	downloadRepo     repository.DownloadRepository
	configRepo       repository.ConfigRepository
	downloadPath     string
}

// NewScannerHandler 创建扫描处理器实例
func NewScannerHandler(
	db *gorm.DB,
	subscriptionRepo repository.SubscriptionRepository,
	downloadRepo repository.DownloadRepository,
	configRepo repository.ConfigRepository,
	downloadPath string,
) *ScannerHandler {
	return &ScannerHandler{
		db:               db,
		subscriptionRepo: subscriptionRepo,
		downloadRepo:     downloadRepo,
		configRepo:       configRepo,
		downloadPath:     downloadPath,
	}
}

// ScanFolder 扫描指定订阅的文件夹
func (h *ScannerHandler) ScanFolder(c *gin.Context) {
	// 解析订阅 ID
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的订阅ID",
		})
		return
	}

	// 加载订阅
	sub, err := h.subscriptionRepo.GetByID(uint(id))
	if err != nil || sub == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "订阅不存在",
		})
		return
	}

	// 解析请求体
	var req scanner.Request
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误: " + err.Error(),
		})
		return
	}

	if req.FolderPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "缺少 folder_path 参数",
		})
		return
	}

	// 执行扫描
	svc := scanner.NewScanner(h.db, h.subscriptionRepo, h.downloadRepo, h.configRepo, h.downloadPath)
	result, err := svc.Scan(sub, &req)
	if err != nil {
		logger.Error("Folder scan failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "文件夹扫描失败: " + err.Error(),
		})
		return
	}

	msg := "扫描完成（预览模式）"
	if !req.DryRun {
		if req.RenameFiles && result.RenamedCount > 0 {
			msg = "扫描完成，已重命名 " + strconv.Itoa(result.RenamedCount) + " 个文件并更新数据库"
		} else {
			msg = "扫描完成，已更新数据库"
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": msg,
		"data":    result,
	})
}
