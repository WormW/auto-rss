package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/pkg/utils"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/bangumi"
	"github.com/WormW/auto-rss/internal/service/downloader"
	"github.com/WormW/auto-rss/internal/service/mikan"
	"github.com/WormW/auto-rss/internal/service/rss"
	"github.com/WormW/auto-rss/internal/service/subscription"
	"github.com/WormW/auto-rss/internal/service/task"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SubscriptionHandler 订阅处理器
type SubscriptionHandler struct {
	repo                repository.SubscriptionRepository
	downloadRepo        repository.DownloadRepository
	configRepo          repository.ConfigRepository
	bangumiService      *bangumi.BangumiService
	imageService        *bangumi.ImageService
	mikanService        mikan.Service
	rssParser           rss.Parser
	qbClient            downloader.QBittorrentClient
	downloadPath        string
	// New service interfaces
	bangumiEnricher      bangumi.Enricher
	batchImporter        subscription.BatchImporter
	collectionDownloader subscription.CollectionDownloader
}

// NewSubscriptionHandler 创建订阅处理器实例
func NewSubscriptionHandler(
	repo repository.SubscriptionRepository,
	downloadRepo repository.DownloadRepository,
	configRepo repository.ConfigRepository,
	qbClient downloader.QBittorrentClient,
	downloadPath string,
) *SubscriptionHandler {
	// Create internal services
	bgService := bangumi.NewBangumiService()
	imgService := bangumi.NewImageService("./data/covers")
	mikanService := mikan.NewMikanService("")
	rssParser := rss.NewParser()

	// Create new service instances
	enricher := bangumi.NewEnricher(bgService, imgService, configRepo)
	batchImporter := subscription.NewBatchImporter(mikanService, enricher, repo, configRepo)
	collectionDownloader := subscription.NewCollectionDownloader(qbClient, downloadRepo, configRepo, downloadPath)

	return &SubscriptionHandler{
		repo:                 repo,
		downloadRepo:         downloadRepo,
		configRepo:           configRepo,
		bangumiService:       bgService,
		imageService:         imgService,
		mikanService:         mikanService,
		rssParser:            rssParser,
		qbClient:             qbClient,
		downloadPath:         downloadPath,
		bangumiEnricher:      enricher,
		batchImporter:        batchImporter,
		collectionDownloader: collectionDownloader,
	}
}

// setProxy 设置代理
func (h *SubscriptionHandler) setProxy() {
	if h.configRepo != nil {
		proxyConfig, err := h.configRepo.Get("system_proxy")
		if err == nil && proxyConfig != nil && proxyConfig.Value != "" {
			logger.Debug("Setting proxy for services", "proxy", proxyConfig.Value)
			h.bangumiService.SetProxy(proxyConfig.Value)
			h.imageService.SetProxy(proxyConfig.Value)
			h.mikanService.SetProxy(proxyConfig.Value)
			if err := h.rssParser.SetProxy(proxyConfig.Value); err != nil {
				logger.Error("Failed to set proxy for RSS parser", "proxy", proxyConfig.Value, "error", err)
			}
		} else {
			logger.Debug("No proxy configured", "err", err)
		}
	}
}

func normalizeSubscriptionNameAndSeason(name string, season int) (string, int) {
	if name == "" {
		if season <= 0 {
			return name, 1
		}
		return name, season
	}

	detectedSeason := season
	seasonPatterns := []string{
		`第([0-9一二三四五六七八九十两]+)季`,
		`第([0-9一二三四五六七八九十两]+)期`,
		`(?i)Season\s*([0-9IVX]+)`,
		`(?i)(?:^|\s|\()S([0-9]{1,2})(?:$|\s|\))`,
	}

	for _, pattern := range seasonPatterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(name); len(matches) > 1 {
			if parsed := parseSeasonToken(matches[1]); parsed > 0 {
				detectedSeason = parsed
				break
			}
		}
	}

	cleaned := name
	trimPatterns := []string{
		`\s*[\(（]?第[0-9一二三四五六七八九十两]+季[\)）]?\s*$`,
		`\s*[\(（]?第[0-9一二三四五六七八九十两]+期[\)）]?\s*$`,
		`(?i)\s*[\(（]?Season\s*[0-9IVX]+[\)）]?\s*$`,
		`(?i)\s*[\(（]?S[0-9]{1,2}[\)）]?\s*$`,
	}
	for _, pattern := range trimPatterns {
		re := regexp.MustCompile(pattern)
		cleaned = re.ReplaceAllString(cleaned, "")
	}
	cleaned = strings.TrimSpace(strings.Trim(cleaned, "-_·:："))
	if cleaned == "" {
		cleaned = name
	}

	if detectedSeason <= 0 {
		detectedSeason = 1
	}

	return cleaned, detectedSeason
}

func parseSeasonToken(token string) int {
	token = strings.TrimSpace(token)
	if token == "" {
		return 0
	}

	if n, err := strconv.Atoi(token); err == nil {
		return n
	}

	if n := romanToInt(strings.ToUpper(token)); n > 0 {
		return n
	}

	if n := chineseNumeralToInt(token); n > 0 {
		return n
	}

	return 0
}

func romanToInt(s string) int {
	if s == "" {
		return 0
	}

	vals := map[rune]int{'I': 1, 'V': 5, 'X': 10}
	total := 0
	prev := 0

	for i := len(s) - 1; i >= 0; i-- {
		v, ok := vals[rune(s[i])]
		if !ok {
			return 0
		}
		if v < prev {
			total -= v
		} else {
			total += v
		}
		prev = v
	}

	return total
}

func chineseNumeralToInt(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}

	digits := map[rune]int{
		'零': 0,
		'一': 1,
		'二': 2,
		'三': 3,
		'四': 4,
		'五': 5,
		'六': 6,
		'七': 7,
		'八': 8,
		'九': 9,
		'两': 2,
	}

	if s == "十" {
		return 10
	}

	if strings.ContainsRune(s, '十') {
		parts := strings.SplitN(s, "十", 2)
		tens := 1
		if parts[0] != "" {
			r := []rune(parts[0])
			if len(r) != 1 {
				return 0
			}
			v, ok := digits[r[0]]
			if !ok {
				return 0
			}
			tens = v
		}

		ones := 0
		if len(parts) == 2 && parts[1] != "" {
			r := []rune(parts[1])
			if len(r) != 1 {
				return 0
			}
			v, ok := digits[r[0]]
			if !ok {
				return 0
			}
			ones = v
		}
		return tens*10 + ones
	}

	r := []rune(s)
	if len(r) == 1 {
		if v, ok := digits[r[0]]; ok {
			return v
		}
	}

	return 0
}

// enrichWithBangumi 自动获取Bangumi数据
func (h *SubscriptionHandler) enrichWithBangumi(subscription *model.Subscription) {
	h.enrichWithBangumiInternal(subscription, false)
}

// enrichWithBangumiInternal 内部实现，支持强制刷新
func (h *SubscriptionHandler) enrichWithBangumiInternal(subscription *model.Subscription, force bool) {
	if err := h.bangumiEnricher.Enrich(subscription, force); err != nil {
		logger.Warn("Failed to enrich subscription with Bangumi data",
			"subscription_name", subscription.Name,
			"error", err.Error())
	}
}

// downloadCollectionTorrent 下载合集种子
func (h *SubscriptionHandler) downloadCollectionTorrent(subscription *model.Subscription) {
	h.collectionDownloader.DownloadAsync(subscription)
}

// Create 创建订阅
func (h *SubscriptionHandler) Create(c *gin.Context) {
	var subscription model.Subscription
	if err := c.ShouldBindJSON(&subscription); err != nil {
		logger.Warn("Invalid subscription create request",
			"error", err.Error(),
			"client_ip", c.ClientIP())
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid request body",
		})
		return
	}

	logger.Info("Creating new subscription",
		"name", subscription.Name,
		"rss_url", subscription.RssURL,
		"client_ip", c.ClientIP())

	if subscription.RssURL != "" {
		existing, err := h.repo.GetByRSSURL(subscription.RssURL)
		if err == nil {
			c.JSON(http.StatusConflict, gin.H{
				"code":    409,
				"message": "Subscription already exists",
				"data":    existing,
			})
			return
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			logger.Error("Failed to check existing subscription by RSS URL",
				"rss_url", subscription.RssURL,
				"error", err.Error())
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "Failed to check existing subscription",
			})
			return
		}
	}

	// 自动获取Bangumi数据
	h.enrichWithBangumi(&subscription)

	// 将标题中的"第X季/Season X"规范到 season 字段
	subscription.Name, subscription.Season = normalizeSubscriptionNameAndSeason(subscription.Name, subscription.Season)

	if err := h.repo.Create(&subscription); err != nil {
		logger.Error("Failed to create subscription",
			"name", subscription.Name,
			"error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to create subscription",
		})
		return
	}

	logger.Info("Subscription created successfully",
		"id", subscription.ID,
		"name", subscription.Name,
		"bangumi_id", subscription.BangumiID,
		"has_cover", subscription.BangumiCoverLocal != "")

	// 如果提供了合集种子地址，自动触发下载
	go h.downloadCollectionTorrent(&subscription)

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
		logger.Warn("Invalid subscription ID in update request",
			"id_param", c.Param("id"),
			"error", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid subscription ID",
		})
		return
	}

	existing, err := h.repo.GetByID(uint(id))
	if err != nil {
		logger.Error("Failed to get existing subscription",
			"id", id,
			"error", err.Error())
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "Subscription not found",
		})
		return
	}

	originalName := existing.Name
	originalSeason := existing.Season

	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		logger.Warn("Invalid subscription update request",
			"id", id,
			"error", err.Error(),
			"client_ip", c.ClientIP())
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid request body",
		})
		return
	}

	logger.Info("Updating subscription",
		"id", id,
		"updates", updates,
		"client_ip", c.ClientIP())

	// 应用更新到现有订阅
	if name, ok := updates["name"].(string); ok {
		existing.Name = name
	}
	if rssURL, ok := updates["rss_url"].(string); ok {
		existing.RssURL = rssURL
	}
	if fansub, ok := updates["fansub"].(string); ok {
		existing.Fansub = fansub
	}
	if language, ok := updates["language"].(string); ok {
		existing.Language = language
	}
	if updateDay, ok := updates["update_day"].(string); ok {
		existing.UpdateDay = updateDay
	}
	if season, ok := updates["season"].(float64); ok {
		existing.Season = int(season)
	}
	if bangumiID, ok := updates["bangumi_id"].(float64); ok {
		existing.BangumiID = int(bangumiID)
	}
	if totalEps, ok := updates["total_episodes"].(float64); ok {
		existing.TotalEpisodes = int(totalEps)
	}
	if epOffset, ok := updates["episode_offset"].(float64); ok {
		existing.EpisodeOffset = int(epOffset)
	}
	if filterRules, ok := updates["filter_rules"].(string); ok {
		existing.FilterRules = filterRules
	}
	if enabled, ok := updates["enabled"].(bool); ok {
		existing.Enabled = enabled
	}
	if renameEnabled, ok := updates["rename_enabled"].(bool); ok {
		existing.RenameEnabled = renameEnabled
	}

	existing.Name, existing.Season = normalizeSubscriptionNameAndSeason(existing.Name, existing.Season)

	var shouldDownloadCollection bool
	if collectionTorrent, ok := updates["collection_torrent"].(string); ok {
		if collectionTorrent != existing.CollectionTorrent && collectionTorrent != "" {
			shouldDownloadCollection = true
		}
		existing.CollectionTorrent = collectionTorrent
	}

	if existing.BangumiCoverLocal == "" {
		logger.Debug("No cover found, attempting to fetch Bangumi data",
			"id", id,
			"name", existing.Name)
		h.enrichWithBangumi(existing)
	}

	if err := h.repo.Update(existing); err != nil {
		logger.Error("Failed to update subscription",
			"id", id,
			"name", existing.Name,
			"error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to update subscription",
		})
		return
	}

	logger.Info("Subscription updated successfully",
		"id", id,
		"name", existing.Name,
		"bangumi_id", existing.BangumiID)

	if shouldDownloadCollection {
		go h.downloadCollectionTorrent(existing)
	}

	if existing.Name != originalName || existing.Season != originalSeason {
		logger.Info("Subscription name or season changed, triggering automatic file rename",
			"subscription_id", id,
			"old_name", originalName,
			"new_name", existing.Name,
			"old_season", originalSeason,
			"new_season", existing.Season)

		go func(sub *model.Subscription) {
			manager := task.GetManager()
			taskName := fmt.Sprintf("自动重命名文件: %s", sub.Name)
			_, err := manager.StartTask(task.TaskTypeCollect, sub.ID, taskName, func(ctx context.Context, t *task.Task) error {
				return h.doRenameFiles(ctx, t, sub)
			})
			if err != nil {
				logger.Error("Failed to start automatic rename task",
					"subscription_id", sub.ID,
					"error", err.Error())
			}
		}(existing)
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data":    existing,
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

// Toggle 切换订阅启用状态
func (h *SubscriptionHandler) Toggle(c *gin.Context) {
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

	subscription.Enabled = !subscription.Enabled

	logger.Info("Toggling subscription status",
		"id", id,
		"name", subscription.Name,
		"enabled", subscription.Enabled,
		"client_ip", c.ClientIP())

	if err := h.repo.Update(subscription); err != nil {
		logger.Error("Failed to toggle subscription",
			"id", id,
			"error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to toggle subscription",
		})
		return
	}

	logger.Info("Subscription toggled successfully",
		"id", id,
		"enabled", subscription.Enabled)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data":    subscription,
	})
}

// EnrichBangumi 手动补全Bangumi数据
func (h *SubscriptionHandler) EnrichBangumi(c *gin.Context) {
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

	logger.Info("Manual Bangumi enrichment requested",
		"id", id,
		"name", subscription.Name,
		"client_ip", c.ClientIP())

	originalName := subscription.Name

	h.enrichWithBangumiInternal(subscription, true)

	nameChanged := originalName != subscription.Name

	if err := h.repo.Update(subscription); err != nil {
		logger.Error("Failed to update subscription after enrichment",
			"id", id,
			"error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to update subscription",
		})
		return
	}

	logger.Info("Manual Bangumi enrichment completed",
		"id", id,
		"bangumi_id", subscription.BangumiID,
		"has_cover", subscription.BangumiCoverLocal != "",
		"name_changed", nameChanged,
		"original_name", originalName,
		"new_name", subscription.Name)

	if nameChanged {
		logger.Info("Subscription name changed, triggering automatic file rename",
			"subscription_id", id,
			"old_name", originalName,
			"new_name", subscription.Name)

		subscriptionCopy, err := h.repo.GetByID(uint(id))
		if err != nil {
			logger.Error("Failed to reload subscription for rename task",
				"subscription_id", id,
				"error", err.Error())
		} else {
			go func(sub *model.Subscription) {
				manager := task.GetManager()
				taskName := fmt.Sprintf("自动重命名文件: %s", sub.Name)

				_, err := manager.StartTask(task.TaskTypeCollect, sub.ID, taskName, func(ctx context.Context, t *task.Task) error {
					return h.doRenameFiles(ctx, t, sub)
				})

				if err != nil {
					logger.Error("Failed to start automatic rename task",
						"subscription_id", sub.ID,
						"error", err.Error())
				} else {
					logger.Info("Automatic rename task started",
						"subscription_id", sub.ID,
						"task_name", taskName)
				}
			}(subscriptionCopy)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data":    subscription,
	})
}

// DownloadCollection 手动触发合集种子下载
func (h *SubscriptionHandler) DownloadCollection(c *gin.Context) {
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

	if subscription.CollectionTorrent == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "No collection torrent configured for this subscription",
		})
		return
	}

	logger.Info("Manual collection torrent download requested",
		"id", id,
		"name", subscription.Name,
		"collection_torrent", subscription.CollectionTorrent,
		"client_ip", c.ClientIP())

	go h.downloadCollectionTorrent(subscription)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Collection torrent download started",
		"data": gin.H{
			"subscription_id":    subscription.ID,
			"collection_torrent": subscription.CollectionTorrent,
		},
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
	subscriptionsWithStats, err := h.repo.GetSubscriptionsWithDownloadCount()
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
			"list": subscriptionsWithStats,
		},
	})
}

// CollectEpisodes 手动收集缺失的剧集（异步执行）
func (h *SubscriptionHandler) CollectEpisodes(c *gin.Context) {
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

	logger.Info("Manual episode collection requested",
		"id", id,
		"name", subscription.Name,
		"client_ip", c.ClientIP())

	manager := task.GetManager()
	taskName := fmt.Sprintf("采集: %s", subscription.Name)

	newTask, err := manager.StartTask(task.TaskTypeCollect, uint(id), taskName, func(ctx context.Context, t *task.Task) error {
		return h.doCollectEpisodes(ctx, t, subscription)
	})

	if err != nil {
		c.JSON(http.StatusConflict, gin.H{
			"code":    409,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "采集任务已启动",
		"data": gin.H{
			"task": newTask,
		},
	})
}

// doCollectEpisodes 执行采集任务的核心逻辑
func (h *SubscriptionHandler) doCollectEpisodes(ctx context.Context, t *task.Task, subscription *model.Subscription) error {
	manager := task.GetManager()
	id := subscription.ID

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	manager.UpdateProgress(5, "设置代理...")
	h.setProxy()

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	manager.UpdateProgress(10, "获取RSS订阅...")
	items, err := h.rssParser.FetchAndParse(subscription.RssURL)
	if err != nil {
		logger.Error("Failed to fetch RSS feed",
			"subscription_id", id,
			"rss_url", subscription.RssURL,
			"error", err.Error())
		return fmt.Errorf("获取RSS失败: %s", err.Error())
	}

	logger.Info("RSS feed fetched successfully",
		"subscription_id", id,
		"items_count", len(items))

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	manager.UpdateProgress(20, "获取已有下载记录...")
	existingDownloads, err := h.downloadRepo.ListBySubscriptionID(id)
	if err != nil {
		logger.Error("Failed to get existing downloads",
			"subscription_id", id,
			"error", err.Error())
		return fmt.Errorf("获取下载记录失败: %s", err.Error())
	}

	episodeMap := make(map[int]*model.Download)
	hashSet := make(map[string]bool)

	for i := range existingDownloads {
		download := &existingDownloads[i]
		hashSet[download.TorrentHash] = true
		episodeMap[download.Episode] = download
	}

	var newDownloads []model.Download
	var deletedCount int

	processedEpisodes := make(map[int]bool)
	maxPubTime := subscription.LastRSSPubTime

	manager.UpdateProgress(25, "分析RSS条目...")

	totalItems := len(items)
	processedItems := 0

	for _, item := range items {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		processedItems++
		progress := 25 + (processedItems * 60 / totalItems)
		manager.UpdateProgress(progress, fmt.Sprintf("处理第 %d/%d 个条目...", processedItems, totalItems))

		if !item.PubTime.IsZero() {
			if maxPubTime == nil || item.PubTime.After(*maxPubTime) {
				pubCopy := item.PubTime
				maxPubTime = &pubCopy
			}
			if subscription.LastRSSPubTime != nil && !item.PubTime.After(*subscription.LastRSSPubTime) {
				continue
			}
		}

		offset := subscription.EpisodeOffset
		relativeEpisode := item.Episode
		if offset > 0 {
			relativeEpisode = item.Episode - offset
			if relativeEpisode <= 0 {
				logger.Debug("Skipping episode before offset",
					"episode", item.Episode,
					"offset", offset,
					"relative_episode", relativeEpisode,
					"title", item.Title)
				continue
			}
		}

		if subscription.TotalEpisodes > 0 && relativeEpisode > subscription.TotalEpisodes {
			logger.Debug("Skipping episode beyond total",
				"episode", item.Episode,
				"relative_episode", relativeEpisode,
				"total_episodes", subscription.TotalEpisodes,
				"title", item.Title)
			continue
		}

		if hashSet[item.TorrentHash] {
			continue
		}

		existingDownload, exists := episodeMap[item.Episode]

		if exists && !processedEpisodes[item.Episode] {
			logger.Info("Found newer version of episode, replacing old task",
				"subscription", subscription.Name,
				"subscription_id", id,
				"episode", item.Episode,
				"old_download_id", existingDownload.ID,
				"old_title", existingDownload.Title,
				"old_hash", existingDownload.TorrentHash,
				"new_title", item.Title,
				"new_hash", item.TorrentHash,
				"trigger_context", "manual_collect")

			if err := h.downloadRepo.Delete(existingDownload.ID); err != nil {
				logger.Error("Failed to delete old download task",
					"download_id", existingDownload.ID,
					"error", err.Error())
				continue
			}
			deletedCount++
		} else if processedEpisodes[item.Episode] {
			logger.Debug("Skipping older version in RSS feed",
				"episode", item.Episode,
				"title", item.Title)
			continue
		}

		processedEpisodes[item.Episode] = true

		download := model.Download{
			SubscriptionID: id,
			Title:          item.Title,
			Episode:        item.Episode,
			Fansub:         item.Fansub,
			TorrentURL:     item.TorrentURL,
			TorrentHash:    item.TorrentHash,
			Status:         "pending",
		}

		if err := h.downloadRepo.Create(&download); err != nil {
			logger.Error("Failed to create download task",
				"subscription_id", id,
				"episode", item.Episode,
				"title", item.Title,
				"error", err.Error())
			continue
		}

		if h.qbClient != nil {
			savePath := h.downloadPath
			downloadPath := utils.GenerateDownloadPath(savePath, subscription.Name)

			var torrentHash string
			var err error

			if strings.HasSuffix(strings.ToLower(item.TorrentURL), ".torrent") ||
				strings.Contains(item.TorrentURL, "/Download/") {
				if h.configRepo != nil {
					if proxyConfig, err := h.configRepo.Get("system_proxy"); err == nil && proxyConfig != nil && proxyConfig.Value != "" {
						h.qbClient.SetProxy(proxyConfig.Value)
					}
				}

				fileContent, downloadErr := h.qbClient.DownloadTorrentFile(item.TorrentURL)
				if downloadErr != nil {
					logger.Error("Failed to download torrent file",
						"subscription_id", id,
						"episode", item.Episode,
						"torrent_url", item.TorrentURL,
						"error", downloadErr.Error())
					download.Status = "failed"
					h.downloadRepo.Update(&download)
					continue
				}

				torrentHash, err = h.qbClient.AddTorrentFile(
					"torrent.torrent",
					fileContent,
					downloadPath,
					downloader.AutoRssCategory,
				)
			} else {
				torrentHash, err = h.qbClient.AddTorrent(
					item.TorrentURL,
					downloadPath,
					downloader.AutoRssCategory,
				)
			}

			if err != nil {
				logger.Error("Failed to add torrent to qBittorrent",
					"subscription_id", id,
					"episode", item.Episode,
					"title", item.Title,
					"torrent_url", item.TorrentURL,
					"download_path", downloadPath,
					"error", err.Error())
				download.Status = "failed"
				h.downloadRepo.Update(&download)
				continue
			}

			if torrentHash != "" && torrentHash != download.TorrentHash {
				existingByHash, _ := h.downloadRepo.GetByHash(torrentHash)
				if existingByHash != nil && existingByHash.ID != download.ID {
					logger.Warn("Torrent hash already exists in another download record",
						"download_id", download.ID,
						"existing_download_id", existingByHash.ID,
						"hash", torrentHash)
					h.downloadRepo.Delete(download.ID)
					continue
				}
				download.TorrentHash = torrentHash
			}
			download.Status = "downloading"
			if err := h.downloadRepo.Update(&download); err != nil {
				logger.Error("Failed to update download status",
					"download_id", download.ID,
					"error", err.Error())
			}

			logger.Info("Torrent added to qBittorrent successfully",
				"subscription_id", id,
				"subscription_name", subscription.Name,
				"episode", item.Episode,
				"title", item.Title,
				"hash", torrentHash,
				"download_path", downloadPath,
				"category", downloader.AutoRssCategory)
		}

		newDownloads = append(newDownloads, download)
		logger.Info("Download task created",
			"subscription_id", id,
			"episode", item.Episode,
			"title", item.Title)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	manager.UpdateProgress(90, "更新集数信息...")

	maxEpisodeInRSS := 0
	for _, item := range items {
		if item.Episode > maxEpisodeInRSS {
			maxEpisodeInRSS = item.Episode
		}
	}

	maxEpisodeFromBangumi := 0
	if subscription.BangumiID > 0 {
		latestEp, err := h.bangumiService.GetLatestEpisode(subscription.BangumiID)
		if err != nil {
			logger.Warn("Failed to get latest episode from Bangumi",
				"subscription_id", id,
				"bangumi_id", subscription.BangumiID,
				"error", err.Error())
		} else if latestEp > 0 {
			maxEpisodeFromBangumi = latestEp
			logger.Info("Got latest episode from Bangumi",
				"subscription_id", id,
				"latest_episode", latestEp)
		}
	}

	latestEpisode := maxEpisodeInRSS
	if maxEpisodeFromBangumi > latestEpisode {
		latestEpisode = maxEpisodeFromBangumi
	}

	maxCollectedEpisode := 0
	for _, download := range existingDownloads {
		if download.Episode > maxCollectedEpisode && download.Status != "failed" {
			maxCollectedEpisode = download.Episode
		}
	}
	for _, download := range newDownloads {
		if download.Episode > maxCollectedEpisode {
			maxCollectedEpisode = download.Episode
		}
	}

	if latestEpisode > 0 || maxCollectedEpisode > 0 {
		subscription.LatestEpisode = latestEpisode
		subscription.CurrentEpisode = maxCollectedEpisode
		if err := h.repo.Update(subscription); err != nil {
			logger.Error("Failed to update subscription episode info",
				"subscription_id", id,
				"latest_episode", latestEpisode,
				"current_episode", maxCollectedEpisode,
				"error", err.Error())
		} else {
			logger.Info("Updated subscription episode info",
				"subscription_id", id,
				"latest_episode", latestEpisode,
				"current_episode", maxCollectedEpisode,
				"rss_episodes", maxEpisodeInRSS,
				"bangumi_episodes", maxEpisodeFromBangumi)
		}
	}

	if maxPubTime != nil {
		pubCopy := *maxPubTime
		subscription.LastRSSPubTime = &pubCopy
		if err := h.repo.Update(subscription); err != nil {
			logger.Warn("Failed to persist last RSS pub time", "subscription_id", id, "error", err.Error())
		}
	}

	logger.Info("Episode collection completed",
		"subscription_id", id,
		"new_downloads", len(newDownloads),
		"deleted_old_tasks", deletedCount,
		"total_rss_items", len(items),
		"latest_episode", latestEpisode,
		"current_episode", maxCollectedEpisode,
		"last_rss_pub_time", subscription.LastRSSPubTime)

	manager.SetResult(gin.H{
		"collected":       len(newDownloads),
		"deleted":         deletedCount,
		"total_rss_items": len(items),
	})

	return nil
}

// BatchImportFromRSSRequest 从RSS批量导入请求
type BatchImportFromRSSRequest struct {
	Items []RSSAnimeImportItem `json:"items" binding:"required,dive"`
}

// RSSAnimeImportItem RSS番剧导入项
type RSSAnimeImportItem struct {
	Title      string `json:"title" binding:"required"`
	Fansub     string `json:"fansub"`
	RssURL     string `json:"rss_url"`
	SourceID   uint   `json:"source_id"`
	SourceName string `json:"source_name"`
}

// BatchImportFromRSS 从RSS批量导入订阅
func (h *SubscriptionHandler) BatchImportFromRSS(c *gin.Context) {
	var req BatchImportFromRSSRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    1,
			"message": "参数错误: " + err.Error(),
		})
		return
	}

	if len(req.Items) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    1,
			"message": "导入列表不能为空",
		})
		return
	}

	h.setProxy()

	// Convert request items to import items
	importItems := make([]subscription.ImportItem, len(req.Items))
	for i, item := range req.Items {
		importItems[i] = subscription.ImportItem{
			Title:      item.Title,
			Fansub:     item.Fansub,
			RssURL:     item.RssURL,
			SourceID:   item.SourceID,
			SourceName: item.SourceName,
		}
	}

	results, err := h.batchImporter.Import(importItems)
	if err != nil {
		logger.Error("Batch import failed", "error", err)
	}

	successCount := 0
	skippedCount := 0
	failedCount := 0
	for _, r := range results {
		if r.Skipped {
			skippedCount++
		} else if r.Success {
			successCount++
		} else {
			failedCount++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "批量导入完成",
		"data": gin.H{
			"total":   len(req.Items),
			"success": successCount,
			"skipped": skippedCount,
			"failed":  failedCount,
			"results": results,
		},
	})
}

// ReorganizeFiles 重新组织订阅的已下载文件
func (h *SubscriptionHandler) ReorganizeFiles(c *gin.Context) {
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

	logger.Info("Reorganizing files for subscription",
		"id", id,
		"name", subscription.Name,
		"season", subscription.Season,
		"client_ip", c.ClientIP())

	manager := task.GetManager()
	taskName := fmt.Sprintf("整理文件: %s", subscription.Name)

	newTask, err := manager.StartTask(task.TaskTypeCollect, uint(id), taskName, func(ctx context.Context, t *task.Task) error {
		return h.doReorganizeFiles(ctx, t, subscription)
	})

	if err != nil {
		c.JSON(http.StatusConflict, gin.H{
			"code":    409,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "文件整理任务已启动",
		"data": gin.H{
			"task": newTask,
		},
	})
}

// doReorganizeFiles 执行文件重组织的核心逻辑
func (h *SubscriptionHandler) doReorganizeFiles(ctx context.Context, t *task.Task, subscription *model.Subscription) error {
	manager := task.GetManager()
	id := subscription.ID

	manager.UpdateProgress(5, "获取已下载文件列表...")

	downloads, err := h.downloadRepo.ListBySubscriptionID(id)
	if err != nil {
		return fmt.Errorf("获取下载记录失败: %s", err.Error())
	}

	var completedDownloads []model.Download
	for _, d := range downloads {
		if d.Status == "completed" && d.TorrentHash != "" {
			completedDownloads = append(completedDownloads, d)
		}
	}

	if len(completedDownloads) == 0 {
		return fmt.Errorf("没有已完成的下载任务")
	}

	logger.Info("Found completed downloads",
		"subscription_id", id,
		"count", len(completedDownloads))

	renameTemplate := "${title}/Season ${season}/${title} S${seasonFormat}E${episodeFormat}"
	if h.configRepo != nil {
		if templateConfig, err := h.configRepo.Get("rename_template"); err == nil && templateConfig != nil && templateConfig.Value != "" {
			renameTemplate = templateConfig.Value
		}
	}
	renameService := downloader.NewRenameService(renameTemplate)

	basePath := h.downloadPath
	if h.configRepo != nil {
		if pathConfig, err := h.configRepo.Get("download_path"); err == nil && pathConfig != nil && pathConfig.Value != "" {
			basePath = pathConfig.Value
		}
	}

	result, err := renameService.ReorganizeSubscriptionFiles(ctx, subscription, completedDownloads, h.qbClient, h.configRepo, basePath)
	if err != nil {
		return err
	}

	manager.UpdateProgress(100, "整理完成")

	logger.Info("File reorganization completed",
		"subscription_id", id,
		"result", result)

	if result["errors"].(int) > 0 && result["moved"].(int) == 0 && result["renamed"].(int) == 0 {
		return fmt.Errorf("整理失败，%d 个错误", result["errors"])
	}

	manager.SetResult(result)
	return nil
}

// RenameFiles 批量重命名订阅的已下载文件
func (h *SubscriptionHandler) RenameFiles(c *gin.Context) {
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

	logger.Info("Starting batch rename for subscription",
		"subscription_id", id,
		"name", subscription.Name,
		"client_ip", c.ClientIP())

	manager := task.GetManager()
	taskName := fmt.Sprintf("重命名文件: %s", subscription.Name)

	newTask, err := manager.StartTask(task.TaskTypeCollect, uint(id), taskName, func(ctx context.Context, t *task.Task) error {
		return h.doRenameFiles(ctx, t, subscription)
	})

	if err != nil {
		c.JSON(http.StatusConflict, gin.H{
			"code":    409,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "重命名任务已启动",
		"data": gin.H{
			"task": newTask,
		},
	})
}

// doRenameFiles 执行批量重命名
func (h *SubscriptionHandler) doRenameFiles(ctx context.Context, t *task.Task, subscription *model.Subscription) error {
	manager := task.GetManager()
	manager.UpdateProgress(0, "正在查询已下载文件...")

	completedDownloads, _, err := h.downloadRepo.List(0, 1000, "completed")
	if err != nil {
		return fmt.Errorf("failed to get downloads: %w", err)
	}

	var downloads []model.Download
	for _, download := range completedDownloads {
		if download.SubscriptionID == subscription.ID {
			downloads = append(downloads, download)
		}
	}

	if len(downloads) == 0 {
		manager.UpdateProgress(100, "没有需要重命名的文件")
		return nil
	}

	logger.Info("Found completed downloads",
		"subscription_id", subscription.ID,
		"count", len(downloads))

	renameTemplate := "${title}/Season ${season}/${title} S${seasonFormat}E${episodeFormat}"
	if h.configRepo != nil {
		if templateConfig, err := h.configRepo.Get("rename_template"); err == nil && templateConfig != nil && templateConfig.Value != "" {
			renameTemplate = templateConfig.Value
		}
	}
	renameService := downloader.NewRenameService(renameTemplate)

	basePath := h.downloadPath
	if h.configRepo != nil {
		if pathConfig, err := h.configRepo.Get("download_path"); err == nil && pathConfig != nil && pathConfig.Value != "" {
			basePath = pathConfig.Value
		}
	}

	result, err := renameService.RenameSubscriptionFiles(ctx, subscription, downloads, h.qbClient, h.configRepo, h.downloadRepo, basePath)
	if err != nil {
		return err
	}

	manager.UpdateProgress(100, "重命名完成")

	logger.Info("Batch rename completed",
		"subscription_id", subscription.ID,
		"result", result)

	if result["errors"].(int) > 0 && result["moved"].(int) == 0 && result["renamed"].(int) == 0 {
		return fmt.Errorf("重命名失败，%d 个错误", result["errors"])
	}

	manager.SetResult(result)
	return nil
}
