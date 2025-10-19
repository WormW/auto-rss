package handler

import (
	"net/http"

	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/bangumi"
	"github.com/gin-gonic/gin"
)

// BangumiHandler Bangumi API处理器
type BangumiHandler struct {
	bangumiService *bangumi.BangumiService
	configRepo     repository.ConfigRepository
}

// NewBangumiHandler 创建Bangumi处理器
func NewBangumiHandler(configRepo repository.ConfigRepository) *BangumiHandler {
	return &BangumiHandler{
		bangumiService: bangumi.NewBangumiService(),
		configRepo:     configRepo,
	}
}

// setProxy 设置代理
func (h *BangumiHandler) setProxy() {
	proxyConfig, err := h.configRepo.Get("system_proxy")
	if err == nil && proxyConfig.Value != "" {
		h.bangumiService.SetProxy(proxyConfig.Value)
	}
}

// BangumiSearchRequest 搜索请求
type BangumiSearchRequest struct {
	Keyword string `form:"keyword" binding:"required,min=1"`
}

// BangumiSearchByNameRequest 按名称搜索请求
type BangumiSearchByNameRequest struct {
	Name string `form:"name" binding:"required,min=1"`
}

// BangumiGetSubjectRequest 获取详情请求
type BangumiGetSubjectRequest struct {
	ID int `uri:"id" binding:"required,min=1"`
}

// Search 搜索番剧
// GET /api/v1/bangumi/search?keyword={keyword}
func (h *BangumiHandler) Search(c *gin.Context) {
	var req BangumiSearchRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误: " + err.Error()})
		return
	}

	h.setProxy()

	result, err := h.bangumiService.Search(req.Keyword, bangumi.SubjectTypeAnime)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "搜索失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

// SearchByName 通过名称搜索番剧(返回最佳匹配)
// GET /api/v1/bangumi/search-by-name?name={name}
func (h *BangumiHandler) SearchByName(c *gin.Context) {
	var req BangumiSearchByNameRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误: " + err.Error()})
		return
	}

	h.setProxy()

	subject, err := h.bangumiService.SearchByName(req.Name)
	if err != nil {
		// 如果找不到结果,返回空而不是错误
		c.JSON(http.StatusOK, gin.H{"data": nil})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": subject})
}

// GetSubject 获取番剧详情
// GET /api/v1/bangumi/subjects/:id
func (h *BangumiHandler) GetSubject(c *gin.Context) {
	var req BangumiGetSubjectRequest
	if err := c.ShouldBindUri(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误: " + err.Error()})
		return
	}

	h.setProxy()

	subject, err := h.bangumiService.GetSubject(req.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取详情失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": subject})
}
