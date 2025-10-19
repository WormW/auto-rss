package handler

import (
	"net/http"

	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/mikan"
	"github.com/gin-gonic/gin"
)

// MikanHandler Mikan搜索处理器
type MikanHandler struct {
	mikanService *mikan.MikanService
	configRepo   repository.ConfigRepository
	subRepo      repository.SubscriptionRepository
}

// NewMikanHandler 创建Mikan处理器
func NewMikanHandler(configRepo repository.ConfigRepository, subRepo repository.SubscriptionRepository) *MikanHandler {
	return &MikanHandler{
		mikanService: mikan.NewMikanService(""),
		configRepo:   configRepo,
		subRepo:      subRepo,
	}
}

// SearchRequest 搜索请求
type SearchRequest struct {
	Text string `json:"text" form:"text"`
}

// SeasonRequest 季度请求
type SeasonRequest struct {
	Year   int    `json:"year" form:"year" binding:"required"`
	Season string `json:"season" form:"season" binding:"required"`
}

// FansubGroupsRequest 字幕组请求
type FansubGroupsRequest struct {
	URL string `json:"url" form:"url" binding:"required,url"`
}

// Search 搜索番剧
func (h *MikanHandler) Search(c *gin.Context) {
	var req SearchRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	if req.Text == "" || len(req.Text) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "搜索关键词至少需要2个字符"})
		return
	}

	// 设置代理
	h.setProxy()

	// 搜索
	result, err := h.mikanService.Search(req.Text)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "搜索失败: " + err.Error()})
		return
	}

	// 标记已订阅的番剧
	h.markExisting(result)

	c.JSON(http.StatusOK, gin.H{"data": result})
}

// GetBySeason 按季度获取番剧
func (h *MikanHandler) GetBySeason(c *gin.Context) {
	var req SeasonRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	// 设置代理
	h.setProxy()

	// 获取季度番剧
	result, err := h.mikanService.GetBySeason(req.Year, req.Season)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取失败: " + err.Error()})
		return
	}

	// 标记已订阅的番剧
	h.markExisting(result)

	c.JSON(http.StatusOK, gin.H{"data": result})
}

// GetFansubGroups 获取字幕组列表
func (h *MikanHandler) GetFansubGroups(c *gin.Context) {
	var req FansubGroupsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	// 设置代理
	h.setProxy()

	// 获取字幕组
	groups, err := h.mikanService.GetFansubGroups(req.URL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取字幕组失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": groups})
}

// setProxy 设置代理
func (h *MikanHandler) setProxy() {
	proxyConfig, err := h.configRepo.Get("system_proxy")
	if err == nil && proxyConfig.Value != "" {
		_ = h.mikanService.SetProxy(proxyConfig.Value)
	}
}

// markExisting 标记已订阅的番剧
func (h *MikanHandler) markExisting(result *mikan.SearchResult) {
	// 获取所有订阅
	subscriptions, _, err := h.subRepo.List(1, 9999)
	if err != nil {
		return
	}

	// 创建已订阅的番剧名称集合
	existingNames := make(map[string]bool)
	for _, sub := range subscriptions {
		existingNames[sub.Name] = true
	}

	// 标记已存在的番剧
	for _, group := range result.Groups {
		for _, item := range group.Items {
			if existingNames[item.Title] {
				item.Exists = true
			}
		}
	}
}
