package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/service/backup"
	"github.com/gin-gonic/gin"
)

// The request includes a JSON wrapper around the raw package. Keep one MiB of
// envelope headroom while still bounding work before JSON binding.
const maxBackupRequestBytes int64 = backup.MaxPackageBytes + (1 << 20)

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
	if !bindBackupImportRequest(c, &req) {
		return
	}

	plan, err := h.service.Preview(req.Data, req.SourceFormat, req.Strategy)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, backup.ErrPackageTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		c.JSON(status, gin.H{
			"code":    status,
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
	if !bindBackupImportRequest(c, &req) {
		return
	}

	plan, err := h.service.Import(req.Data, req.SourceFormat, req.Strategy)
	if err != nil {
		logger.Error("Failed to import backup", "error", err)
		status := http.StatusInternalServerError
		if errors.Is(err, backup.ErrPackageTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		c.JSON(status, gin.H{
			"code":    status,
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

func bindBackupImportRequest(c *gin.Context, req *backupImportRequest) bool {
	if c.Request.ContentLength > maxBackupRequestBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{
			"code":    http.StatusRequestEntityTooLarge,
			"message": "请求参数错误: http: request body too large",
		})
		return false
	}

	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxBackupRequestBytes))
	if err != nil {
		status := http.StatusBadRequest
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			status = http.StatusRequestEntityTooLarge
		}
		c.JSON(status, gin.H{
			"code":    status,
			"message": "请求参数错误: " + err.Error(),
		})
		return false
	}
	if int64(len(body)) > maxBackupRequestBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{
			"code":    http.StatusRequestEntityTooLarge,
			"message": "请求参数错误: http: request body too large",
		})
		return false
	}
	if err := json.Unmarshal(body, req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    http.StatusBadRequest,
			"message": "请求参数错误: " + err.Error(),
		})
		return false
	}
	if len(req.Data) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    http.StatusBadRequest,
			"message": "请求参数错误: data is required",
		})
		return false
	}
	return true
}
