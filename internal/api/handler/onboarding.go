package handler

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/WormW/auto-rss/internal/config"
	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	onboardingCompletedKey = "onboarding_completed"
	onboardingSkippedKey   = "onboarding_skipped"
)

type OnboardingHandler struct {
	configRepo       repository.ConfigRepository
	rssSourceRepo    repository.RSSSourceRepository
	subscriptionRepo repository.SubscriptionRepository
	db               *gorm.DB
	cfg              *config.Config
}

type onboardingStep struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Complete bool   `json:"complete"`
	Message  string `json:"message"`
}

type downloadPathStatus struct {
	Path     string `json:"path"`
	Set      bool   `json:"set"`
	Exists   bool   `json:"exists"`
	IsDir    bool   `json:"is_dir"`
	Writable bool   `json:"writable"`
	Error    string `json:"error,omitempty"`
}

func NewOnboardingHandler(
	configRepo repository.ConfigRepository,
	rssSourceRepo repository.RSSSourceRepository,
	subscriptionRepo repository.SubscriptionRepository,
	db *gorm.DB,
	cfg *config.Config,
) *OnboardingHandler {
	return &OnboardingHandler{
		configRepo:       configRepo,
		rssSourceRepo:    rssSourceRepo,
		subscriptionRepo: subscriptionRepo,
		db:               db,
		cfg:              cfg,
	}
}

func (h *OnboardingHandler) Status(c *gin.Context) {
	completed := h.boolConfig(onboardingCompletedKey)
	skipped := h.boolConfig(onboardingSkippedKey)

	qb := h.qbittorrentStatus()
	downloadPath := h.downloadPathStatus(false)
	rename := h.renameTemplateStatus()
	rssSourceCount := h.rssSourceCount()
	subscriptionCount := h.subscriptionCount()
	notificationCount := h.notificationCount()

	steps := []onboardingStep{
		{
			Key:      "qbittorrent",
			Label:    "qBittorrent",
			Complete: qb["configured"].(bool),
			Message:  qb["message"].(string),
		},
		{
			Key:      "download_path",
			Label:    "下载目录",
			Complete: downloadPath.Set && downloadPath.Exists && downloadPath.IsDir,
			Message:  downloadPath.Message(),
		},
		{
			Key:      "rss_source",
			Label:    "RSS 源",
			Complete: rssSourceCount > 0,
			Message:  countMessage(rssSourceCount, "RSS 源"),
		},
		{
			Key:      "subscription",
			Label:    "订阅",
			Complete: subscriptionCount > 0,
			Message:  countMessage(subscriptionCount, "订阅"),
		},
		{
			Key:      "rename_template",
			Label:    "重命名模板",
			Complete: rename["configured"].(bool),
			Message:  rename["message"].(string),
		},
	}

	missing := make([]string, 0)
	for _, step := range steps {
		if !step.Complete {
			missing = append(missing, step.Key)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data": gin.H{
			"completed":          completed,
			"skipped":            skipped,
			"should_show":        !completed && !skipped && len(missing) > 0,
			"missing":            missing,
			"steps":              steps,
			"qbittorrent":        qb,
			"download_path":      downloadPath,
			"rename_template":    rename,
			"rss_source_count":   rssSourceCount,
			"subscription_count": subscriptionCount,
			"notification_count": notificationCount,
		},
	})
}

func (h *OnboardingHandler) Skip(c *gin.Context) {
	if err := h.configRepo.Set(onboardingSkippedKey, "true"); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "保存向导状态失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "Success"})
}

func (h *OnboardingHandler) Complete(c *gin.Context) {
	if err := h.configRepo.Set(onboardingCompletedKey, "true"); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "保存向导完成状态失败"})
		return
	}
	if err := h.configRepo.Set(onboardingSkippedKey, "false"); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "保存向导状态失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "Success"})
}

func (h *OnboardingHandler) ValidateDownloadPath(c *gin.Context) {
	var req struct {
		Path string `json:"path" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "请求参数错误"})
		return
	}

	status := inspectDownloadPath(strings.TrimSpace(req.Path), true)
	if !status.Set || !status.Exists || !status.IsDir || !status.Writable {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"code":    422,
			"message": status.Message(),
			"data":    status,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "目录可用",
		"data":    status,
	})
}

func (h *OnboardingHandler) boolConfig(key string) bool {
	cfg, err := h.configRepo.Get(key)
	if err != nil || cfg == nil {
		return false
	}
	value := strings.TrimSpace(strings.ToLower(cfg.Value))
	return value == "true" || value == "1" || value == "yes"
}

func (h *OnboardingHandler) stringConfig(keys ...string) string {
	for _, key := range keys {
		cfg, err := h.configRepo.Get(key)
		if err == nil && cfg != nil && strings.TrimSpace(cfg.Value) != "" {
			return strings.TrimSpace(cfg.Value)
		}
	}
	return ""
}

func (h *OnboardingHandler) qbittorrentStatus() gin.H {
	host := h.stringConfig("qbittorrent_host", "qb_host")
	username := h.stringConfig("qbittorrent_username", "qb_username")
	password := h.stringConfig("qbittorrent_password", "qb_password")

	if host == "" && h.cfg != nil {
		host = strings.TrimSpace(h.cfg.QBHost)
	}
	if username == "" && h.cfg != nil {
		username = strings.TrimSpace(h.cfg.QBUsername)
	}
	if password == "" && h.cfg != nil {
		password = strings.TrimSpace(h.cfg.QBPassword)
	}

	configured := host != "" && username != "" && password != ""
	message := "缺少 qBittorrent 地址、用户名或密码"
	if configured {
		message = "qBittorrent 配置已填写"
	}

	return gin.H{
		"configured": configured,
		"host":       host,
		"username":   username,
		"message":    message,
	}
}

func (h *OnboardingHandler) downloadPathStatus(testWrite bool) downloadPathStatus {
	path := h.stringConfig("download_path")
	if path == "" && h.cfg != nil {
		path = strings.TrimSpace(h.cfg.DownloadPath)
	}
	return inspectDownloadPath(path, testWrite)
}

func (h *OnboardingHandler) renameTemplateStatus() gin.H {
	template := h.stringConfig("rename_template")
	configured := template != ""
	message := "尚未设置重命名模板"
	if configured {
		message = "重命名模板已设置"
	}

	return gin.H{
		"configured": configured,
		"template":   template,
		"message":    message,
	}
}

func (h *OnboardingHandler) rssSourceCount() int64 {
	if h.rssSourceRepo == nil {
		return 0
	}
	_, total, err := h.rssSourceRepo.List(1, 1, nil)
	if err != nil {
		return 0
	}
	return total
}

func (h *OnboardingHandler) subscriptionCount() int64 {
	if h.subscriptionRepo == nil {
		return 0
	}
	_, total, err := h.subscriptionRepo.List(0, 1)
	if err != nil {
		return 0
	}
	return total
}

func (h *OnboardingHandler) notificationCount() int64 {
	if h.db == nil {
		return 0
	}
	var total int64
	if err := h.db.Model(&model.NotificationSetting{}).Where("enabled = ?", true).Count(&total).Error; err != nil {
		return 0
	}
	return total
}

func inspectDownloadPath(path string, testWrite bool) downloadPathStatus {
	status := downloadPathStatus{
		Path: path,
		Set:  strings.TrimSpace(path) != "",
	}
	if !status.Set {
		status.Error = "下载目录不能为空"
		return status
	}

	cleanPath := filepath.Clean(path)
	status.Path = cleanPath

	info, err := os.Stat(cleanPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			status.Error = "目录不存在"
		} else {
			status.Error = err.Error()
		}
		return status
	}

	status.Exists = true
	status.IsDir = info.IsDir()
	if !status.IsDir {
		status.Error = "路径不是目录"
		return status
	}

	if testWrite {
		file, err := os.CreateTemp(cleanPath, ".auto-rss-write-test-*")
		if err != nil {
			status.Error = "目录不可写: " + err.Error()
			return status
		}
		name := file.Name()
		_ = file.Close()
		_ = os.Remove(name)
	}

	status.Writable = true
	return status
}

func (s downloadPathStatus) Message() string {
	switch {
	case !s.Set:
		return "尚未设置下载目录"
	case !s.Exists:
		return "下载目录不存在"
	case !s.IsDir:
		return "下载路径不是目录"
	case !s.Writable && s.Error != "":
		if s.Error != "" {
			return s.Error
		}
		return "下载目录尚未验证写入权限"
	default:
		return "下载目录可用"
	}
}

func countMessage(count int64, noun string) string {
	if count == 0 {
		return "尚未添加" + noun
	}
	return "已有 " + strconv.FormatInt(count, 10) + " 个" + noun
}
