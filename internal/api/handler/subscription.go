package handler

import (
	"log"
	"net/http"
	"strconv"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/bangumi"
	"github.com/gin-gonic/gin"
)

// SubscriptionHandler 订阅处理器
type SubscriptionHandler struct {
	repo           repository.SubscriptionRepository
	configRepo     repository.ConfigRepository
	bangumiService *bangumi.BangumiService
	imageService   *bangumi.ImageService
}

// NewSubscriptionHandler 创建订阅处理器实例
func NewSubscriptionHandler(repo repository.SubscriptionRepository, configRepo repository.ConfigRepository) *SubscriptionHandler {
	return &SubscriptionHandler{
		repo:           repo,
		configRepo:     configRepo,
		bangumiService: bangumi.NewBangumiService(),
		imageService:   bangumi.NewImageService("./data/covers"), // 封面保存路径
	}
}

// setProxy 设置代理
func (h *SubscriptionHandler) setProxy() {
	if h.configRepo != nil {
		proxyConfig, err := h.configRepo.Get("system_proxy")
		if err == nil && proxyConfig.Value != "" {
			h.bangumiService.SetProxy(proxyConfig.Value)
			h.imageService.SetProxy(proxyConfig.Value)
		}
	}
}

// enrichWithBangumi 自动获取Bangumi数据
func (h *SubscriptionHandler) enrichWithBangumi(subscription *model.Subscription) {
	// 如果已经有Bangumi ID，跳过
	if subscription.BangumiID > 0 {
		return
	}

	// 设置代理
	h.setProxy()

	// 通过番剧名称搜索
	subject, err := h.bangumiService.SearchByName(subscription.Name)
	if err != nil {
		log.Printf("Failed to fetch Bangumi data for %s: %v", subscription.Name, err)
		return
	}

	// 填充Bangumi数据
	subscription.BangumiID = subject.ID
	subscription.BangumiScore = subject.Score
	subscription.BangumiSummary = subject.Summary
	if subject.Images != nil {
		subscription.BangumiCover = subject.Images.Large

		// 下载封面到本地
		if subject.Images.Large != "" {
			localPath, err := h.imageService.DownloadCover(subject.Images.Large, subject.ID)
			if err != nil {
				log.Printf("Failed to download cover for %s: %v", subscription.Name, err)
			} else {
				subscription.BangumiCoverLocal = localPath
				log.Printf("Downloaded cover for %s: %s", subscription.Name, localPath)
			}
		}
	}
	if subject.Rating != nil {
		subscription.BangumiRank = subject.Rating.Rank
	}

	// 自动填充季度信息
	if subject.Season > 0 {
		subscription.Season = subject.Season
	}

	// 如果总集数为0，尝试从Bangumi获取
	if subscription.TotalEpisodes == 0 && subject.TotalEps > 0 {
		subscription.TotalEpisodes = subject.TotalEps
	}

	// 如果更新日期为空，尝试从Bangumi获取
	if subscription.UpdateDay == "" && subject.AirWeekday >= 0 {
		subscription.UpdateDay = strconv.Itoa(subject.AirWeekday)
	}

	log.Printf("Enriched subscription %s with Bangumi data (ID: %d, Score: %.1f, Season: %d)",
		subscription.Name, subscription.BangumiID, subscription.BangumiScore, subscription.Season)
}

// Create 创建订阅
func (h *SubscriptionHandler) Create(c *gin.Context) {
	var subscription model.Subscription
	if err := c.ShouldBindJSON(&subscription); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid request body",
		})
		return
	}

	// 自动获取Bangumi数据
	h.enrichWithBangumi(&subscription)

	if err := h.repo.Create(&subscription); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to create subscription",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data":    subscription,
	})
}

// Update 更新订阅
func (h *SubscriptionHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid subscription ID",
		})
		return
	}

	var subscription model.Subscription
	if err := c.ShouldBindJSON(&subscription); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid request body",
		})
		return
	}

	subscription.ID = uint(id)
	if err := h.repo.Update(&subscription); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to update subscription",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data":    subscription,
	})
}

// Delete 删除订阅
func (h *SubscriptionHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid subscription ID",
		})
		return
	}

	if err := h.repo.Delete(uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to delete subscription",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
	})
}

// GetByID 获取订阅详情
func (h *SubscriptionHandler) GetByID(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid subscription ID",
		})
		return
	}

	subscription, err := h.repo.GetByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "Subscription not found",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data":    subscription,
	})
}

// List 获取订阅列表
func (h *SubscriptionHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	offset := (page - 1) * pageSize
	subscriptions, total, err := h.repo.List(offset, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to get subscription list",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data": gin.H{
			"list":  subscriptions,
			"total": total,
			"page":  page,
		},
	})
}
