package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/service/backup"
	"github.com/gin-gonic/gin"
)

type BackupHandler struct {
	service *backup.Service
}

func NewBackupHandler(service *backup.Service) *BackupHandler {
	return &BackupHandler{service: service}
}

type backupImportRequest struct {
	Data         json.RawMessage `json:"data" binding:"required"`
	SourceFormat string          `json:"source_format"`
	Strategy     string          `json:"strategy"`
}

func (h *BackupHandler) Export(c *gin.Context) {
	includeSensitive := c.Query("include_sensitive") == "true"

	pkg, err := h.service.Export(includeSensitive)
	if err != nil {
		logger.Error("Failed to export backup", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "导出备份失败",
		})
		return
	}

	filename := fmt.Sprintf("auto-rss-backup-%s.json", time.Now().Format("20060102-150405"))
	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data":    pkg,
	})
}

func (h *BackupHandler) Preview(c *gin.Context) {
	var req backupImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误: " + err.Error(),
		})
		return
	}

	plan, err := h.service.Preview(req.Data, req.SourceFormat, req.Strategy)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "预览导入失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data":    plan,
	})
}

func (h *BackupHandler) Import(c *gin.Context) {
	var req backupImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误: " + err.Error(),
		})
		return
	}

	plan, err := h.service.Import(req.Data, req.SourceFormat, req.Strategy)
	if err != nil {
		logger.Error("Failed to import backup", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "导入备份失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "导入完成",
		"data":    plan,
	})
}
