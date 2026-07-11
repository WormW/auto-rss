package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/pkg/utils"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/bangumi"
	"github.com/WormW/auto-rss/internal/service/downloader"
	"github.com/WormW/auto-rss/internal/service/mikan"
	"github.com/WormW/auto-rss/internal/service/rss"
	"github.com/WormW/auto-rss/internal/service/scheduler"
	"github.com/WormW/auto-rss/internal/service/subscription"
	"github.com/WormW/auto-rss/internal/service/task"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SubscriptionHandler 订阅处理器
type SubscriptionHandler struct {
	repo           repository.SubscriptionRepository
	downloadRepo   repository.DownloadRepository
	episodeRepo    repository.EpisodeRepository
	configRepo     repository.ConfigRepository
	bangumiService *bangumi.BangumiService
	imageService   *bangumi.ImageService
	mikanService   mikan.Service
	rssParser      rss.Parser
	qbClient       downloader.QBittorrentClient
	downloadPath   string
	// New service interfaces
	bangumiEnricher      bangumi.Enricher
	batchImporter        subscription.BatchImporter
	collectionDownloader subscription.CollectionDownloader
}

type SubscriptionPreviewRequest struct {
	ID                 uint   `json:"id"`
	Name               string `json:"name"`
	RssURL             string `json:"rss_url" binding:"required"`
	Season             int    `json:"season"`
	Fansub             string `json:"fansub"`
	Language           string `json:"language"`
	LanguagePreference string `json:"language_preference"`
	TotalEpisodes      int    `json:"total_episodes"`
	EpisodeOffset      int    `json:"episode_offset"`
	FilterKeywords     string `json:"filter_keywords"`
	ExcludeKeywords    string `json:"exclude_keywords"`
	FilterRules        string `json:"filter_rules"`
	Limit              int    `json:"limit"`
}

type SubscriptionPreviewItem struct {
	Title              string `json:"title"`
	Episode            int    `json:"episode"`
	RelativeEpisode    int    `json:"relative_episode"`
	Fansub             string `json:"fansub"`
	Language           string `json:"language"`
	LanguageKeyword    string `json:"language_keyword"`
	PubDate            string `json:"pub_date"`
	TorrentURL         string `json:"torrent_url"`
	TorrentHash        string `json:"torrent_hash"`
	Action             string `json:"action"`
	Reason             string `json:"reason"`
	ExistingDownloadID uint   `json:"existing_download_id,omitempty"`
	DownloadPath       string `json:"download_path"`
	RenamePreview      string `json:"rename_preview"`
}

type SubscriptionSmartFetchStatus struct {
	SubscriptionID     uint      `json:"subscription_id"`
	ShouldFetch        bool      `json:"should_fetch"`
	Reason             string    `json:"reason"`
	Explanation        string    `json:"explanation"`
	NextFetchIn        string    `json:"next_fetch_in"`
	NextFetchSeconds   int64     `json:"next_fetch_seconds"`
	NextFetchAt        time.Time `json:"next_fetch_at"`
	MissingEpisodes    []int     `json:"missing_episodes"`
	IsInActiveWindow   bool      `json:"is_in_active_window"`
	IsCompleted        bool      `json:"is_completed"`
	SmartFetchEnabled  bool      `json:"smart_fetch_enabled"`
	SmartFetchOverride string    `json:"smart_fetch_override"`
}

// NewSubscriptionHandler 创建订阅处理器实例
func NewSubscriptionHandler(
	repo repository.SubscriptionRepository,
	downloadRepo repository.DownloadRepository,
	configRepo repository.ConfigRepository,
	qbClient downloader.QBittorrentClient,
	downloadPath string,
	episodeRepos ...repository.EpisodeRepository,
) *SubscriptionHandler {
	// Create internal services
	bgService := bangumi.NewBangumiService()
	imgService := bangumi.NewImageService(utils.GetCoverPath())
	mikanService := mikan.NewMikanService("")
	rssParser := rss.NewParser()

	// Create new service instances
	enricher := bangumi.NewEnricher(bgService, imgService, configRepo)
	batchImporter := subscription.NewBatchImporter(mikanService, enricher, repo, configRepo)
	collectionDownloader := subscription.NewCollectionDownloader(qbClient, downloadRepo, configRepo, downloadPath)

	var episodeRepo repository.EpisodeRepository
	if len(episodeRepos) > 0 {
		episodeRepo = episodeRepos[0]
	}

	return &SubscriptionHandler{
		repo:                 repo,
		downloadRepo:         downloadRepo,
		episodeRepo:          episodeRepo,
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

func (h *SubscriptionHandler) resolveDownloadPath(subscriptionName string) string {
	basePath := h.downloadPath
	if h.configRepo != nil {
		if pathConfig, err := h.configRepo.Get("download_path"); err == nil && pathConfig != nil && pathConfig.Value != "" {
			basePath = pathConfig.Value
		}
	}
	return utils.GenerateDownloadPath(basePath, subscriptionName)
}

func (h *SubscriptionHandler) resolveRenameTemplate() string {
	if h.configRepo == nil {
		return ""
	}
	if templateConfig, err := h.configRepo.Get("rename_template"); err == nil && templateConfig != nil && templateConfig.Value != "" {
		return templateConfig.Value
	}
	return ""
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

func splitRuleTokens(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == ';' || r == '，' || r == '；'
	})
}

func parseRuleToken(token string) (string, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", false
	}

	lower := strings.ToLower(token)
	excluded := false
	for _, prefix := range []string{"exclude:", "exclude=", "排除:", "排除=", "!", "-"} {
		if strings.HasPrefix(lower, prefix) {
			token = strings.TrimSpace(token[len(prefix):])
			excluded = true
			return token, excluded
		}
	}
	for _, prefix := range []string{"include:", "include=", "包含:", "包含=", "+"} {
		if strings.HasPrefix(lower, prefix) {
			token = strings.TrimSpace(token[len(prefix):])
			return token, false
		}
	}

	return token, false
}

func matchesSubscriptionFilters(sub *model.Subscription, title string) (bool, string) {
	titleLower := strings.ToLower(title)
	var include []string
	var exclude []string

	for _, token := range splitRuleTokens(sub.FilterKeywords) {
		if keyword := strings.TrimSpace(token); keyword != "" {
			include = append(include, keyword)
		}
	}
	for _, token := range splitRuleTokens(sub.ExcludeKeywords) {
		if keyword := strings.TrimSpace(token); keyword != "" {
			exclude = append(exclude, keyword)
		}
	}
	for _, token := range splitRuleTokens(sub.FilterRules) {
		keyword, excluded := parseRuleToken(token)
		if keyword == "" {
			continue
		}
		if excluded {
			exclude = append(exclude, keyword)
		} else {
			include = append(include, keyword)
		}
	}

	for _, keyword := range exclude {
		if strings.Contains(titleLower, strings.ToLower(keyword)) {
			return false, "排除关键词: " + keyword
		}
	}

	if len(include) == 0 {
		return true, ""
	}

	for _, keyword := range include {
		if strings.Contains(titleLower, strings.ToLower(keyword)) {
			return true, ""
		}
	}

	return false, "未命中过滤关键词"
}

// enrichWithBangumi 自动获取Bangumi数据
func (h *SubscriptionHandler) enrichWithBangumi(subscription *model.Subscription) {
	h.enrichWithBangumiInternal(subscription, false)
}

// enrichWithBangumiInternal 内部实现，支持强制刷新
func (h *SubscriptionHandler) enrichWithBangumiInternal(subscription *model.Subscription, force bool) {
	if h.bangumiEnricher == nil {
		return
	}
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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func normalizeSubscriptionSource(subscription *model.Subscription) {
	subscription.RssURL = strings.TrimSpace(subscription.RssURL)
	subscription.CollectionTorrent = strings.TrimSpace(subscription.CollectionTorrent)
	subscription.SourceType = strings.TrimSpace(subscription.SourceType)

	if subscription.SourceType == "" {
		subscription.SourceType = "manual"
	}

	if subscription.RssURL == "" && subscription.CollectionTorrent == "" {
		subscription.SourceType = "calendar"
		subscription.RSSSourceID = nil
		subscription.RSSBaselinePending = false
		subscription.SmartFetchOverride = "never"
		subscription.SmartFetchEnabled = nil
		return
	}

	if subscription.SourceType == "calendar" {
		subscription.SourceType = "manual"
	}
}

// Preview 预览订阅规则会匹配到的 RSS 条目
func (h *SubscriptionHandler) Preview(c *gin.Context) {
	var req SubscriptionPreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid request body",
		})
		return
	}

	req.RssURL = strings.TrimSpace(req.RssURL)
	if req.RssURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "RSS URL is required",
		})
		return
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 30
	}
	if limit > 100 {
		limit = 100
	}

	sub := &model.Subscription{
		ID:                 req.ID,
		Name:               strings.TrimSpace(req.Name),
		RssURL:             req.RssURL,
		Season:             req.Season,
		Fansub:             strings.TrimSpace(req.Fansub),
		Language:           strings.TrimSpace(req.Language),
		LanguagePreference: strings.TrimSpace(req.LanguagePreference),
		TotalEpisodes:      req.TotalEpisodes,
		EpisodeOffset:      req.EpisodeOffset,
		FilterKeywords:     req.FilterKeywords,
		ExcludeKeywords:    req.ExcludeKeywords,
		FilterRules:        req.FilterRules,
	}
	if sub.Name == "" {
		sub.Name = "未命名订阅"
	}
	sub.Name, sub.Season = utils.NormalizeMediaTitleAndSeason(sub.Name, sub.Season)

	h.setProxy()
	items, err := h.rssParser.FetchAndParseWithTimeout(req.RssURL, 15*time.Second)
	if err != nil {
		logger.Warn("Failed to preview RSS feed",
			"rss_url", req.RssURL,
			"error", err.Error(),
			"client_ip", c.ClientIP())
		c.JSON(http.StatusBadGateway, gin.H{
			"code":    502,
			"message": "Failed to fetch RSS feed: " + err.Error(),
		})
		return
	}

	downloadPath := h.resolveDownloadPath(sub.Name)
	renameService := downloader.NewRenameService(h.resolveRenameTemplate())
	existingByEpisode := map[int]model.Download{}
	existingHashes := map[string]model.Download{}
	if h.downloadRepo != nil && sub.ID > 0 {
		if downloads, err := h.downloadRepo.ListBySubscriptionID(sub.ID); err == nil {
			for _, download := range downloads {
				if download.Episode > 0 {
					existingByEpisode[download.Episode] = download
				}
				if download.TorrentHash != "" {
					existingHashes[strings.ToLower(download.TorrentHash)] = download
				}
			}
		}
	}

	previewItems := make([]SubscriptionPreviewItem, 0, minInt(len(items), limit))
	processedEpisodes := map[int]bool{}
	totalDownload := 0
	totalReplace := 0
	totalSkipped := 0
	totalDuplicate := 0
	latestEpisode := 0

	for index, item := range items {
		if item.Episode > latestEpisode {
			latestEpisode = item.Episode
		}

		action := "download"
		reason := "将创建下载任务"
		var existingID uint

		relativeEpisode := item.Episode
		if sub.EpisodeOffset > 0 {
			relativeEpisode = item.Episode - sub.EpisodeOffset
			if relativeEpisode <= 0 {
				action = "skip"
				reason = "集数在偏移范围前"
			}
		}
		if action == "download" && sub.TotalEpisodes > 0 && relativeEpisode > sub.TotalEpisodes {
			action = "skip"
			reason = "超过总集数"
		}
		if action == "download" {
			if matched, filterReason := matchesSubscriptionFilters(sub, item.Title); !matched {
				action = "skip"
				reason = filterReason
			}
		}
		if action == "download" && item.TorrentURL == "" {
			action = "skip"
			reason = "缺少种子链接"
		}
		if action == "download" && item.TorrentHash != "" {
			hashKey := strings.ToLower(item.TorrentHash)
			if existing, ok := existingHashes[hashKey]; ok {
				action = "duplicate"
				reason = "种子哈希已存在"
				existingID = existing.ID
			} else if h.downloadRepo != nil {
				if existing, err := h.downloadRepo.GetByHash(item.TorrentHash); err == nil && existing != nil {
					action = "duplicate"
					reason = "种子哈希已存在"
					existingID = existing.ID
				}
			}
		}
		if action == "download" && item.Episode > 0 {
			if processedEpisodes[item.Episode] {
				action = "skip"
				reason = "同一集已有更靠前的 RSS 条目"
			} else if existing, ok := existingByEpisode[item.Episode]; ok {
				action = "replace"
				reason = "会替换同集旧任务"
				existingID = existing.ID
			}
		}
		if action == "download" || action == "replace" {
			processedEpisodes[item.Episode] = true
		}

		switch action {
		case "download":
			totalDownload++
		case "replace":
			totalReplace++
		case "duplicate":
			totalDuplicate++
		default:
			totalSkipped++
		}

		if index >= limit {
			continue
		}

		download := &model.Download{
			Title:       item.Title,
			Episode:     item.Episode,
			Fansub:      item.Fansub,
			Language:    string(item.Language),
			TorrentURL:  item.TorrentURL,
			TorrentHash: item.TorrentHash,
		}
		extension := filepath.Ext(item.Title)
		if extension == "" {
			extension = ".mkv"
		}
		renamePreview := renameService.GenerateFileName(&downloader.RenameContext{
			Subscription: sub,
			Download:     download,
			OriginalName: item.Title,
			Extension:    extension,
		})

		previewItems = append(previewItems, SubscriptionPreviewItem{
			Title:              item.Title,
			Episode:            item.Episode,
			RelativeEpisode:    relativeEpisode,
			Fansub:             item.Fansub,
			Language:           string(item.Language),
			LanguageKeyword:    item.LangKeyword,
			PubDate:            item.PubDate,
			TorrentURL:         item.TorrentURL,
			TorrentHash:        item.TorrentHash,
			Action:             action,
			Reason:             reason,
			ExistingDownloadID: existingID,
			DownloadPath:       downloadPath,
			RenamePreview:      renamePreview,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data": gin.H{
			"summary": gin.H{
				"total_items":       len(items),
				"previewed_items":   len(previewItems),
				"download_items":    totalDownload,
				"replace_items":     totalReplace,
				"skipped_items":     totalSkipped,
				"duplicate_items":   totalDuplicate,
				"latest_episode":    latestEpisode,
				"download_path":     downloadPath,
				"subscription_name": sub.Name,
				"season":            sub.Season,
				"limited":           len(items) > limit,
			},
			"items": previewItems,
		},
	})
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

	normalizeSubscriptionSource(&subscription)
	subscription.RSSBaselinePending = subscription.RssURL != ""

	// 自动获取Bangumi数据
	h.enrichWithBangumi(&subscription)

	// 将标题中的"第X季/Season X"规范到 season 字段
	// Keep Subscription.Name as the series title; Season carries the season number.
	subscription.Name, subscription.Season = utils.NormalizeMediaTitleAndSeason(subscription.Name, subscription.Season)

	if subscription.RssURL != "" {
		existing, err := h.repo.GetByRSSURLAndSeason(subscription.RssURL, subscription.Season)
		if err == nil {
			c.JSON(http.StatusConflict, gin.H{
				"code":    409,
				"message": "Subscription already exists",
				"data":    existing,
			})
			return
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			logger.Error("Failed to check existing subscription by RSS URL and season",
				"rss_url", subscription.RssURL,
				"season", subscription.Season,
				"error", err.Error())
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "Failed to check existing subscription",
			})
			return
		}
	}

	var createErr error
	if h.episodeRepo != nil {
		createErr = h.episodeRepo.RunInTransaction(func(tx *gorm.DB) error {
			if err := h.repo.CreateInTx(tx, &subscription); err != nil {
				return err
			}
			return h.episodeRepo.EnsureRangeInTx(tx, subscription.ID, subscription.TotalEpisodes)
		})
	} else {
		createErr = h.repo.Create(&subscription)
	}
	if createErr != nil {
		logger.Error("Failed to create subscription",
			"name", subscription.Name,
			"error", createErr.Error())
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
	originalRSSURL := strings.TrimSpace(existing.RssURL)

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
	rssURLUpdated := false
	if rssURL, ok := updates["rss_url"].(string); ok {
		rssURLUpdated = true
		existing.RssURL = strings.TrimSpace(rssURL)
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
	if _, exists := updates["smart_fetch_enabled"]; exists {
		if updates["smart_fetch_enabled"] == nil {
			existing.SmartFetchEnabled = nil
		} else if enabled, ok := updates["smart_fetch_enabled"].(bool); ok {
			existing.SmartFetchEnabled = &enabled
		}
	}
	if override, ok := updates["smart_fetch_override"].(string); ok {
		switch override {
		case "", "follow", "always", "never":
			existing.SmartFetchOverride = override
			existing.SmartFetchEnabled = nil
		default:
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "Invalid smart_fetch_override",
			})
			return
		}
	}

	existing.Name, existing.Season = utils.NormalizeMediaTitleAndSeason(existing.Name, existing.Season)

	var shouldDownloadCollection bool
	if collectionTorrent, ok := updates["collection_torrent"].(string); ok {
		if collectionTorrent != existing.CollectionTorrent && collectionTorrent != "" {
			shouldDownloadCollection = true
		}
		existing.CollectionTorrent = strings.TrimSpace(collectionTorrent)
	}
	if sourceType, ok := updates["source_type"].(string); ok {
		existing.SourceType = sourceType
	}
	if _, exists := updates["rss_source_id"]; exists && updates["rss_source_id"] == nil {
		existing.RSSSourceID = nil
	}

	normalizeSubscriptionSource(existing)
	if rssURLUpdated && existing.RssURL != originalRSSURL {
		existing.RSSBaselinePending = existing.RssURL != ""
	}
	shouldDownloadCollection = shouldDownloadCollection && !existing.IsCalendarOnly()

	if existing.BangumiCoverLocal == "" {
		logger.Debug("No cover found, attempting to fetch Bangumi data",
			"id", id,
			"name", existing.Name)
		h.enrichWithBangumi(existing)
	}

	var updateErr error
	if h.episodeRepo != nil {
		updateErr = h.episodeRepo.RunInTransaction(func(tx *gorm.DB) error {
			if err := h.repo.UpdateInTx(tx, existing); err != nil {
				return err
			}
			return h.episodeRepo.EnsureRangeInTx(tx, existing.ID, existing.TotalEpisodes)
		})
	} else {
		updateErr = h.repo.Update(existing)
	}
	if updateErr != nil {
		logger.Error("Failed to update subscription",
			"id", id,
			"name", existing.Name,
			"error", updateErr.Error())
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

	// 异步补下载缺失的封面
	go h.repairMissingCovers(subscriptionsWithStats)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data": gin.H{
			"list": subscriptionsWithStats,
		},
	})
}

// ListSmartFetchStatus 返回每个订阅的智能拉取决策摘要
func (h *SubscriptionHandler) ListSmartFetchStatus(c *gin.Context) {
	subscriptionsWithStats, err := h.repo.GetSubscriptionsWithDownloadCount()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to get subscription list",
		})
		return
	}

	filter := scheduler.NewSmartFetchFilter(h.downloadRepo, h.episodeRepo)
	filter.LoadConfigFromDB(h.configRepo)
	now := time.Now()
	items := make([]SubscriptionSmartFetchStatus, 0, len(subscriptionsWithStats))

	for i := range subscriptionsWithStats {
		status, _ := filter.EvaluateSubscription(&subscriptionsWithStats[i].Subscription)
		items = append(items, smartFetchStatusResponse(status, now))
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data": gin.H{
			"evaluated_at": now,
			"list":         items,
		},
	})
}

func smartFetchStatusResponse(status scheduler.SubscriptionFetchStatus, now time.Time) SubscriptionSmartFetchStatus {
	missingEpisodes := status.MissingEpisodes
	if missingEpisodes == nil {
		missingEpisodes = []int{}
	}

	nextFetch := status.NextFetchInterval
	if nextFetch < 0 {
		nextFetch = 0
	}

	override := ""
	if status.Subscription != nil {
		override = status.Subscription.SmartFetchOverride
	}

	return SubscriptionSmartFetchStatus{
		SubscriptionID:     status.Subscription.ID,
		ShouldFetch:        status.ShouldFetch,
		Reason:             status.FetchReason,
		Explanation:        status.Explanation,
		NextFetchIn:        nextFetch.Round(time.Second).String(),
		NextFetchSeconds:   int64(nextFetch.Round(time.Second).Seconds()),
		NextFetchAt:        now.Add(nextFetch),
		MissingEpisodes:    missingEpisodes,
		IsInActiveWindow:   status.IsInActiveWindow,
		IsCompleted:        status.IsCompleted,
		SmartFetchEnabled:  status.SmartFetchEnabled,
		SmartFetchOverride: override,
	}
}

// repairMissingCovers 检查并补下载磁盘上缺失的封面
func (h *SubscriptionHandler) repairMissingCovers(subscriptions []repository.SubscriptionWithStats) {
	coverPath := utils.GetCoverPath()
	h.setProxy()
	for _, sub := range subscriptions {
		if sub.BangumiCoverLocal == "" || sub.BangumiCover == "" || sub.BangumiID <= 0 {
			continue
		}
		localPath := filepath.Join(coverPath, sub.BangumiCoverLocal)
		if _, err := os.Stat(localPath); err == nil {
			continue
		}
		if _, err := h.imageService.DownloadCover(sub.BangumiCover, sub.BangumiID); err != nil {
			logger.Error("Failed to repair missing cover",
				"subscription_id", sub.ID,
				"filename", sub.BangumiCoverLocal,
				"error", err)
		} else {
			logger.Info("Repaired missing cover",
				"subscription_id", sub.ID,
				"filename", sub.BangumiCoverLocal)
		}
	}
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

	if subscription.IsCalendarOnly() {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Calendar-only subscriptions do not have RSS collection tasks",
		})
		return
	}

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

	if subscription.IsCalendarOnly() {
		manager.SetResult(gin.H{
			"collected":       0,
			"deleted":         0,
			"total_rss_items": 0,
			"skipped_reason":  "calendar_only",
		})
		return nil
	}

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

		if matched, reason := matchesSubscriptionFilters(subscription, item.Title); !matched {
			logger.Debug("Skipping RSS item based on subscription filters",
				"subscription_id", id,
				"title", item.Title,
				"reason", reason)
			continue
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

		minSizeBytes := scheduler.MinTorrentSizeBytes(h.configRepo)
		if scheduler.ShouldSkipSmallTorrent(&item, minSizeBytes) {
			logger.Info("Skipping small torrent from manual collection",
				"subscription", subscription.Name,
				"subscription_id", id,
				"episode", item.Episode,
				"title", item.Title,
				"torrent_url", item.TorrentURL,
				"reason", scheduler.SmallTorrentSkipMessage(&item, minSizeBytes),
				"size_bytes", item.SizeBytes,
				"min_size_bytes", minSizeBytes,
				"trigger_context", "manual_collect")
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
	Season     int    `json:"season"`
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
		season := item.Season
		if season <= 0 {
			season = 1
		}
		importItems[i] = subscription.ImportItem{
			Title:      item.Title,
			Fansub:     item.Fansub,
			RssURL:     item.RssURL,
			Season:     season,
			SourceID:   item.SourceID,
			SourceName: item.SourceName,
		}
	}

	manager := task.GetManager()
	taskName := fmt.Sprintf("批量导入 %d 个订阅", len(req.Items))
	if len(req.Items) == 1 {
		taskName = fmt.Sprintf("导入订阅: %s", req.Items[0].Title)
	}

	newTask, err := manager.StartTask(task.TaskTypeImport, 0, taskName, func(ctx context.Context, t *task.Task) error {
		total := len(importItems)
		manager.UpdateProgress(5, "开始导入...")

		results, importErr := h.batchImporter.Import(importItems)
		if importErr != nil {
			logger.Error("Batch import failed", "error", importErr)
			return importErr
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

		manager.UpdateProgress(100, "导入完成")
		manager.SetResult(gin.H{
			"total":   total,
			"success": successCount,
			"skipped": skippedCount,
			"failed":  failedCount,
			"results": results,
		})
		return nil
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
		"message": "导入任务已提交",
		"data": gin.H{
			"task_id": newTask.ID,
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

// ==================== 批量操作 API ====================

// BatchUpdateEnabledRequest 批量更新启用状态请求
type BatchUpdateEnabledRequest struct {
	IDs     []uint `json:"ids" binding:"required"`
	Enabled bool   `json:"enabled" binding:"required"`
}

// BatchUpdateEnabled 批量更新订阅启用状态
func (h *SubscriptionHandler) BatchUpdateEnabled(c *gin.Context) {
	var req BatchUpdateEnabledRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid request body",
		})
		return
	}

	if len(req.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Subscription IDs are required",
		})
		return
	}

	logger.Info("Batch updating subscription enabled status",
		"count", len(req.IDs),
		"enabled", req.Enabled,
		"client_ip", c.ClientIP())

	if err := h.repo.BatchUpdateEnabled(req.IDs, req.Enabled); err != nil {
		logger.Error("Failed to batch update enabled status", "error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to update subscriptions",
		})
		return
	}

	logger.Info("Batch update enabled status completed", "count", len(req.IDs))
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data": gin.H{
			"updated_count": len(req.IDs),
			"enabled":       req.Enabled,
		},
	})
}

// BatchDeleteRequest 批量删除请求
type BatchDeleteRequest struct {
	IDs []uint `json:"ids" binding:"required"`
}

// BatchDelete 批量删除订阅
func (h *SubscriptionHandler) BatchDelete(c *gin.Context) {
	var req BatchDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid request body",
		})
		return
	}

	if len(req.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Subscription IDs are required",
		})
		return
	}

	logger.Info("Batch deleting subscriptions",
		"count", len(req.IDs),
		"client_ip", c.ClientIP())

	if err := h.repo.BatchDelete(req.IDs); err != nil {
		logger.Error("Failed to batch delete subscriptions", "error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to delete subscriptions",
		})
		return
	}

	logger.Info("Batch delete completed", "count", len(req.IDs))
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data": gin.H{
			"deleted_count": len(req.IDs),
		},
	})
}

// BatchUpdateGroupRequest 批量更新分组请求
type BatchUpdateGroupRequest struct {
	IDs     []uint `json:"ids" binding:"required"`
	GroupID *uint  `json:"group_id"`
}

// BatchUpdateGroup 批量更新订阅分组
func (h *SubscriptionHandler) BatchUpdateGroup(c *gin.Context) {
	var req BatchUpdateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid request body",
		})
		return
	}

	if len(req.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Subscription IDs are required",
		})
		return
	}

	// 如果指定了分组ID，检查分组是否存在
	if req.GroupID != nil {
		if _, err := h.repo.GetGroupByID(*req.GroupID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "Group not found",
			})
			return
		}
	}

	logger.Info("Batch updating subscription group",
		"count", len(req.IDs),
		"group_id", req.GroupID,
		"client_ip", c.ClientIP())

	if err := h.repo.BatchUpdateGroup(req.IDs, req.GroupID); err != nil {
		logger.Error("Failed to batch update group", "error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to update subscriptions",
		})
		return
	}

	logger.Info("Batch update group completed", "count", len(req.IDs))
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data": gin.H{
			"updated_count": len(req.IDs),
			"group_id":      req.GroupID,
		},
	})
}

// ==================== 分组管理 API ====================

// CreateGroup 创建订阅分组
func (h *SubscriptionHandler) CreateGroup(c *gin.Context) {
	var group model.SubscriptionGroup
	if err := c.ShouldBindJSON(&group); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid request body",
		})
		return
	}

	if group.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Group name is required",
		})
		return
	}

	logger.Info("Creating subscription group",
		"name", group.Name,
		"client_ip", c.ClientIP())

	if err := h.repo.CreateGroup(&group); err != nil {
		logger.Error("Failed to create group", "error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to create group",
		})
		return
	}

	logger.Info("Group created successfully", "id", group.ID, "name", group.Name)
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data":    group,
	})
}

// UpdateGroup 更新订阅分组
func (h *SubscriptionHandler) UpdateGroup(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid group ID",
		})
		return
	}

	existing, err := h.repo.GetGroupByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "Group not found",
		})
		return
	}

	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid request body",
		})
		return
	}

	if name, ok := updates["name"].(string); ok && name != "" {
		existing.Name = name
	}
	if desc, ok := updates["description"].(string); ok {
		existing.Description = desc
	}
	if color, ok := updates["color"].(string); ok {
		existing.Color = color
	}
	if sortOrder, ok := updates["sort_order"].(float64); ok {
		existing.SortOrder = int(sortOrder)
	}

	logger.Info("Updating group",
		"id", id,
		"name", existing.Name,
		"client_ip", c.ClientIP())

	if err := h.repo.UpdateGroup(existing); err != nil {
		logger.Error("Failed to update group", "error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to update group",
		})
		return
	}

	logger.Info("Group updated successfully", "id", id)
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data":    existing,
	})
}

// DeleteGroup 删除订阅分组
func (h *SubscriptionHandler) DeleteGroup(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid group ID",
		})
		return
	}

	// 检查是否是默认分组
	group, err := h.repo.GetGroupByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "Group not found",
		})
		return
	}

	if group.IsDefault {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Cannot delete default group",
		})
		return
	}

	logger.Info("Deleting group",
		"id", id,
		"name", group.Name,
		"client_ip", c.ClientIP())

	if err := h.repo.DeleteGroup(uint(id)); err != nil {
		logger.Error("Failed to delete group", "error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to delete group",
		})
		return
	}

	logger.Info("Group deleted successfully", "id", id)
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
	})
}

// ListGroups 获取所有分组
func (h *SubscriptionHandler) ListGroups(c *gin.Context) {
	groups, err := h.repo.ListGroups()
	if err != nil {
		logger.Error("Failed to list groups", "error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to get groups",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data":    groups,
	})
}

// GetGroup 获取分组详情
func (h *SubscriptionHandler) GetGroup(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid group ID",
		})
		return
	}

	group, err := h.repo.GetGroupByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "Group not found",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data":    group,
	})
}

// ==================== 统计 API ====================

// GetStatistics 获取订阅统计信息
func (h *SubscriptionHandler) GetStatistics(c *gin.Context) {
	stats, err := h.repo.GetStatistics()
	if err != nil {
		logger.Error("Failed to get statistics", "error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to get statistics",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data":    stats,
	})
}

// ==================== 导入/导出 API ====================

// ExportSubscriptions 导出订阅（支持 JSON 和 OPML 格式）
func (h *SubscriptionHandler) ExportSubscriptions(c *gin.Context) {
	format := c.DefaultQuery("format", "json")

	subscriptions, err := h.repo.GetSubscriptionsWithDownloadCount()
	if err != nil {
		logger.Error("Failed to get subscriptions for export", "error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to export subscriptions",
		})
		return
	}

	switch format {
	case "opml":
		opml := h.generateOPML(subscriptions)
		c.Header("Content-Type", "application/xml")
		c.Header("Content-Disposition", "attachment; filename=subscriptions.opml")
		c.String(http.StatusOK, opml)
	case "json":
		c.JSON(http.StatusOK, gin.H{
			"code":    0,
			"message": "Success",
			"data": gin.H{
				"export_time":   time.Now().Format(time.RFC3339),
				"version":       "1.0",
				"subscriptions": subscriptions,
			},
		})
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Unsupported format. Use 'json' or 'opml'",
		})
	}
}

// generateOPML 生成 OPML 格式
func (h *SubscriptionHandler) generateOPML(subscriptions []repository.SubscriptionWithStats) string {
	var buf strings.Builder
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	buf.WriteString(`<opml version="2.0">` + "\n")
	buf.WriteString("  <head>\n")
	buf.WriteString(fmt.Sprintf("    <title>Auto-RSS Subscriptions - %s</title>\n", time.Now().Format("2006-01-02")))
	buf.WriteString("  </head>\n")
	buf.WriteString("  <body>\n")

	for _, sub := range subscriptions {
		title := html.EscapeString(sub.Name)
		rssURL := html.EscapeString(sub.RssURL)
		buf.WriteString(fmt.Sprintf(`    <outline type="rss" text="%s" title="%s" xmlUrl="%s" />`+"\n", title, title, rssURL))
	}

	buf.WriteString("  </body>\n")
	buf.WriteString("</opml>")
	return buf.String()
}

// ImportSubscriptionsRequest 导入订阅请求
type ImportSubscriptionsRequest struct {
	Data    string `json:"data" binding:"required"`   // JSON 或 OPML 数据
	Format  string `json:"format" binding:"required"` // "json" 或 "opml"
	GroupID *uint  `json:"group_id"`                  // 可选：导入到的分组
}

// ImportSubscriptions 导入订阅（支持 JSON 和 OPML 格式）
func (h *SubscriptionHandler) ImportSubscriptions(c *gin.Context) {
	var req ImportSubscriptionsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid request body",
		})
		return
	}

	logger.Info("Importing subscriptions",
		"format", req.Format,
		"group_id", req.GroupID,
		"client_ip", c.ClientIP())

	var results []subscription.ImportResult
	var importErr error

	switch req.Format {
	case "json":
		results, importErr = h.importFromJSON(req.Data, req.GroupID)
	case "opml":
		results, importErr = h.importFromOPML(req.Data, req.GroupID)
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Unsupported format. Use 'json' or 'opml'",
		})
		return
	}

	if importErr != nil {
		logger.Error("Import failed", "error", importErr.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Import failed: " + importErr.Error(),
		})
		return
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

	logger.Info("Import completed",
		"total", len(results),
		"success", successCount,
		"skipped", skippedCount,
		"failed", failedCount)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Import completed",
		"data": gin.H{
			"total":   len(results),
			"success": successCount,
			"skipped": skippedCount,
			"failed":  failedCount,
			"results": results,
		},
	})
}

// importFromJSON 从 JSON 导入
func (h *SubscriptionHandler) importFromJSON(data string, groupID *uint) ([]subscription.ImportResult, error) {
	var exportData struct {
		Subscriptions []model.Subscription `json:"subscriptions"`
	}

	if err := json.Unmarshal([]byte(data), &exportData); err != nil {
		// 尝试直接解析为订阅数组
		var subs []model.Subscription
		if err := json.Unmarshal([]byte(data), &subs); err != nil {
			return nil, fmt.Errorf("invalid JSON format: %w", err)
		}
		exportData.Subscriptions = subs
	}

	results := make([]subscription.ImportResult, 0, len(exportData.Subscriptions))

	for _, sub := range exportData.Subscriptions {
		result := subscription.ImportResult{Title: sub.Name, Success: false}

		// 检查是否已存在
		if sub.RssURL != "" {
			existing, err := h.repo.GetByRSSURLAndSeason(sub.RssURL, sub.Season)
			if err == nil && existing != nil {
				result.Skipped = true
				result.Success = true
				result.Message = "Already exists"
				results = append(results, result)
				continue
			}
		}

		// 重置 ID 和时间戳
		sub.ID = 0
		sub.CreatedAt = time.Time{}
		sub.UpdatedAt = time.Time{}
		sub.GroupID = groupID

		// 丰富 Bangumi 数据
		h.enrichWithBangumi(&sub)

		if err := h.repo.Create(&sub); err != nil {
			result.Message = "Create failed: " + err.Error()
			results = append(results, result)
			continue
		}

		result.Success = true
		result.Message = "Imported successfully"
		result.Subscription = &sub
		results = append(results, result)
	}

	return results, nil
}

// importFromOPML 从 OPML 导入
func (h *SubscriptionHandler) importFromOPML(data string, groupID *uint) ([]subscription.ImportResult, error) {
	// 简单的 OPML 解析（提取 outline 元素）
	outlineRegex := regexp.MustCompile(`<outline[^>]*text="([^"]*)"[^>]*xmlUrl="([^"]*)"[^>]*/>`)
	matches := outlineRegex.FindAllStringSubmatch(data, -1)

	results := make([]subscription.ImportResult, 0, len(matches))

	for _, match := range matches {
		if len(match) < 3 {
			continue
		}

		name := match[1]
		rssURL := match[2]

		result := subscription.ImportResult{Title: name, Success: false}

		// 检查是否已存在
		if rssURL != "" {
			existing, err := h.repo.GetByRSSURLAndSeason(rssURL, 1)
			if err == nil && existing != nil {
				result.Skipped = true
				result.Success = true
				result.Message = "Already exists"
				results = append(results, result)
				continue
			}
		}

		sub := model.Subscription{
			Name:    name,
			RssURL:  rssURL,
			Season:  1,
			Status:  "active",
			Enabled: true,
			GroupID: groupID,
		}

		// 丰富 Bangumi 数据
		h.enrichWithBangumi(&sub)

		if err := h.repo.Create(&sub); err != nil {
			result.Message = "Create failed: " + err.Error()
			results = append(results, result)
			continue
		}

		result.Success = true
		result.Message = "Imported successfully"
		result.Subscription = &sub
		results = append(results, result)
	}

	return results, nil
}
