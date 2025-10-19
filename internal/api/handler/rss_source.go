package handler

import (
	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/rss"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type RSSSourceHandler struct {
	repo       repository.RSSSourceRepository
	configRepo repository.ConfigRepository
	rssParser  rss.Parser
}

func NewRSSSourceHandler(repo repository.RSSSourceRepository, configRepo repository.ConfigRepository, rssParser rss.Parser) *RSSSourceHandler {
	return &RSSSourceHandler{
		repo:       repo,
		configRepo: configRepo,
		rssParser:  rssParser,
	}
}

type CreateRSSSourceRequest struct {
	Name        string `json:"name" binding:"required"`
	BaseURL     string `json:"base_url" binding:"required,url"`
	Description string `json:"description"`
	Enabled     *bool  `json:"enabled"`
}

type UpdateRSSSourceRequest struct {
	Name        string `json:"name"`
	BaseURL     string `json:"base_url" binding:"omitempty,url"`
	Description string `json:"description"`
	Enabled     *bool  `json:"enabled"`
}

// Create 创建RSS源
func (h *RSSSourceHandler) Create(c *gin.Context) {
	var req CreateRSSSourceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	source := &model.RSSSource{
		Name:        req.Name,
		BaseURL:     req.BaseURL,
		Description: req.Description,
		Enabled:     enabled,
	}

	if err := h.repo.Create(source); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建RSS源失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": source})
}

// List 获取RSS源列表
func (h *RSSSourceHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	var enabled *bool
	if enabledStr := c.Query("enabled"); enabledStr != "" {
		val := enabledStr == "true"
		enabled = &val
	}

	sources, total, err := h.repo.List(page, pageSize, enabled)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取RSS源列表失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"list":  sources,
			"total": total,
		},
	})
}

// Get 获取单个RSS源
func (h *RSSSourceHandler) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的ID"})
		return
	}

	source, err := h.repo.FindByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "RSS源不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": source})
}

// Update 更新RSS源
func (h *RSSSourceHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的ID"})
		return
	}

	var req UpdateRSSSourceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	source, err := h.repo.FindByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "RSS源不存在"})
		return
	}

	// 更新字段
	if req.Name != "" {
		source.Name = req.Name
	}
	if req.BaseURL != "" {
		source.BaseURL = req.BaseURL
	}
	if req.Description != "" {
		source.Description = req.Description
	}
	if req.Enabled != nil {
		source.Enabled = *req.Enabled
	}

	if err := h.repo.Update(source); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新RSS源失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": source})
}

// Delete 删除RSS源
func (h *RSSSourceHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的ID"})
		return
	}

	if err := h.repo.Delete(uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除RSS源失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// FetchAnimes 从RSS源获取番剧列表
func (h *RSSSourceHandler) FetchAnimes(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的ID"})
		return
	}

	source, err := h.repo.FindByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "RSS源不存在"})
		return
	}

	if !source.Enabled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "RSS源已禁用"})
		return
	}

	// 获取代理配置
	proxyConfig, err := h.configRepo.Get("system_proxy")
	if err == nil && proxyConfig.Value != "" {
		if err := h.rssParser.SetProxy(proxyConfig.Value); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "设置代理失败: " + err.Error()})
			return
		}
	}

	// 从RSS源获取番剧列表
	items, err := h.rssParser.FetchAndParse(source.BaseURL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取番剧列表失败: " + err.Error()})
		return
	}

	// 按番剧名称分组（去除字幕组和集数信息）
	animeMap := make(map[string]*model.RSSAnime)
	for _, item := range items {
		// 提取纯番剧名称（去除字幕组标签和集数）
		animeName := extractAnimeName(item.Title, item.Fansub)

		anime, exists := animeMap[animeName]
		if !exists {
			anime = &model.RSSAnime{
				Title:      animeName,
				RssURL:     source.BaseURL,  // 这里后续可以改为番剧专属的 RSS URL
				Fansub:     item.Fansub,
				Episodes:   []string{},
				SourceID:   source.ID,
				SourceName: source.Name,
			}
			animeMap[animeName] = anime
		}

		// 添加集数
		if item.Episode > 0 {
			episodeStr := strconv.Itoa(item.Episode)
			// 检查是否已存在
			found := false
			for _, ep := range anime.Episodes {
				if ep == episodeStr {
					found = true
					break
				}
			}
			if !found {
				anime.Episodes = append(anime.Episodes, episodeStr)
			}
		}
	}

	// 转换为数组并排序集数
	animes := make([]model.RSSAnime, 0, len(animeMap))
	for _, anime := range animeMap {
		// 排序集数
		sort.Slice(anime.Episodes, func(i, j int) bool {
			ei, _ := strconv.Atoi(anime.Episodes[i])
			ej, _ := strconv.Atoi(anime.Episodes[j])
			return ei < ej
		})
		animes = append(animes, *anime)
	}

	c.JSON(http.StatusOK, gin.H{"data": animes})
}

// extractAnimeName 从标题中提取纯番剧名称
func extractAnimeName(title string, fansub string) string {
	name := title

	// 移除字幕组标签 [字幕组]
	if fansub != "" {
		name = strings.TrimPrefix(name, "["+fansub+"]")
		name = strings.TrimSpace(name)
	}

	// 移除常见的集数模式
	patterns := []string{
		`\s*第?\s*\d+\s*[集话話].*$`,     // 第12集
		`\s*[Ee][Pp]?\.?\s*\d+.*$`,    // E12, EP12
		`\s*Episode\s*\d+.*$`,         // Episode 12
		`\s*\[\s*\d+\s*\].*$`,         // [12]
		`\s*S\d+E\d+.*$`,              // S01E12
		`\s*-\s*\d+\s*[-\[].*$`,       // - 12 -
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		name = re.ReplaceAllString(name, "")
	}

	// 移除常见的后缀
	name = regexp.MustCompile(`\s*\[.*\]$`).ReplaceAllString(name, "")  // 移除末尾的 [...]
	name = regexp.MustCompile(`\s*【.*】$`).ReplaceAllString(name, "")  // 移除末尾的 【...】
	name = strings.TrimSpace(name)

	return name
}
