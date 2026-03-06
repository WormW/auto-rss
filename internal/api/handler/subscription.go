package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	"github.com/WormW/auto-rss/internal/service/task"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SubscriptionHandler 订阅处理器
type SubscriptionHandler struct {
	repo           repository.SubscriptionRepository
	downloadRepo   repository.DownloadRepository
	configRepo     repository.ConfigRepository
	bangumiService *bangumi.BangumiService
	imageService   *bangumi.ImageService
	mikanService   *mikan.MikanService
	rssParser      rss.Parser
	qbClient       downloader.QBittorrentClient
	downloadPath   string
}

// NewSubscriptionHandler 创建订阅处理器实例
func NewSubscriptionHandler(
	repo repository.SubscriptionRepository,
	downloadRepo repository.DownloadRepository,
	configRepo repository.ConfigRepository,
	qbClient downloader.QBittorrentClient,
	downloadPath string,
) *SubscriptionHandler {
	return &SubscriptionHandler{
		repo:           repo,
		downloadRepo:   downloadRepo,
		configRepo:     configRepo,
		bangumiService: bangumi.NewBangumiService(),
		imageService:   bangumi.NewImageService("./data/covers"), // 封面保存路径
		mikanService:   mikan.NewMikanService(""),
		rssParser:      rss.NewParser(),
		qbClient:       qbClient,
		downloadPath:   downloadPath,
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
	// 如果已经有Bangumi ID且不是强制刷新，跳过
	if subscription.BangumiID > 0 && !force {
		logger.Debug("Subscription already has Bangumi data, skipping enrichment",
			"subscription_id", subscription.ID,
			"subscription_name", subscription.Name,
			"bangumi_id", subscription.BangumiID)
		return
	}

	logger.Info("Starting Bangumi data enrichment",
		"subscription_id", subscription.ID,
		"subscription_name", subscription.Name)

	// 设置代理
	h.setProxy()

	var subject *bangumi.Subject
	var err error

	// 如果已有 Bangumi ID，直接通过 ID 获取详细信息
	if subscription.BangumiID > 0 {
		logger.Info("Using existing Bangumi ID to fetch data",
			"subscription_id", subscription.ID,
			"subscription_name", subscription.Name,
			"bangumi_id", subscription.BangumiID)

		subject, err = h.bangumiService.GetSubject(subscription.BangumiID)
		if err != nil {
			logger.Warn("Failed to fetch Bangumi data by ID",
				"subscription_name", subscription.Name,
				"bangumi_id", subscription.BangumiID,
				"error", err.Error())
			return
		}
	} else {
		// 没有 Bangumi ID，通过番剧名称搜索
		logger.Info("Searching Bangumi by name",
			"subscription_id", subscription.ID,
			"subscription_name", subscription.Name)

		subject, err = h.bangumiService.SearchByName(subscription.Name)
		if err != nil {
			logger.Warn("Failed to fetch Bangumi data by name",
				"subscription_name", subscription.Name,
				"error", err.Error())
			return
		}
	}

	logger.Info("Bangumi data fetched successfully",
		"subscription_name", subscription.Name,
		"bangumi_id", subject.ID,
		"bangumi_score", subject.Score)

	// 填充Bangumi数据
	subscription.BangumiID = subject.ID
	subscription.BangumiScore = subject.Score
	subscription.BangumiSummary = subject.Summary

	// 使用 name_cn 作为番剧名称（如果有的话）
	if subject.NameCN != "" {
		logger.Info("Using Bangumi name_cn as subscription name",
			"original_name", subscription.Name,
			"name_cn", subject.NameCN)
		subscription.Name = subject.NameCN
	}
	if subject.Images != nil {
		subscription.BangumiCover = subject.Images.Large

		// 下载封面到本地
		if subject.Images.Large != "" {
			logger.Debug("Downloading cover image",
				"subscription_name", subscription.Name,
				"cover_url", subject.Images.Large)

			localPath, err := h.imageService.DownloadCover(subject.Images.Large, subject.ID)
			if err != nil {
				logger.Error("Failed to download cover",
					"subscription_name", subscription.Name,
					"cover_url", subject.Images.Large,
					"error", err.Error())
			} else {
				subscription.BangumiCoverLocal = localPath
				logger.Info("Cover downloaded successfully",
					"subscription_name", subscription.Name,
					"local_path", localPath)
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

	// 获取最新已播出的集数
	if subject.ID > 0 && subject.TotalEps > 0 {
		// 先尝试通过API获取精确的已播出集数
		latestEp, err := h.bangumiService.GetLatestEpisode(subject.ID)
		if err != nil || latestEp == 0 {
			// API不可用时，使用总集数作为参考
			// 对于已完结番剧，这就是最终集数
			// 对于正在播出的，RSS会提供更准确的信息
			subscription.LatestEpisode = subject.TotalEps
			logger.Info("Using total episodes as latest episode reference",
				"subscription_name", subscription.Name,
				"total_episodes", subject.TotalEps,
				"reason", "Bangumi episodes API unavailable")
		} else {
			subscription.LatestEpisode = latestEp
			logger.Info("Got latest episode from Bangumi API",
				"subscription_name", subscription.Name,
				"latest_episode", latestEp)
		}
	}

	// 如果更新日期为空，尝试从Bangumi获取
	if subscription.UpdateDay == "" && subject.AirWeekday >= 0 {
		subscription.UpdateDay = strconv.Itoa(subject.AirWeekday)
	}

	// 提取开播日期和年份
	if subject.AirDate != "" {
		subscription.AirDate = subject.AirDate
		// 从日期字符串提取年份 (格式: YYYY-MM-DD)
		if len(subject.AirDate) >= 4 {
			if year, err := strconv.Atoi(subject.AirDate[:4]); err == nil {
				subscription.AirYear = year
			}
		}
	}

	logger.Info("Subscription enriched with Bangumi data",
		"subscription_name", subscription.Name,
		"bangumi_id", subscription.BangumiID,
		"bangumi_score", subscription.BangumiScore,
		"season", subscription.Season,
		"total_episodes", subscription.TotalEpisodes,
		"air_date", subscription.AirDate,
		"air_year", subscription.AirYear,
		"has_cover", subscription.BangumiCoverLocal != "")
}

// downloadCollectionTorrent 下载合集种子
func (h *SubscriptionHandler) downloadCollectionTorrent(subscription *model.Subscription) {
	if subscription.CollectionTorrent == "" {
		return
	}

	if h.qbClient == nil {
		logger.Warn("qBittorrent client not configured, skipping collection torrent download",
			"subscription_id", subscription.ID,
			"subscription_name", subscription.Name)
		return
	}

	logger.Info("Starting collection torrent download",
		"subscription_id", subscription.ID,
		"subscription_name", subscription.Name,
		"collection_torrent", subscription.CollectionTorrent)

	// 使用系统配置的下载路径
	savePath := h.downloadPath

	// 生成带番剧名的下载路径
	downloadPath := utils.GenerateDownloadPath(savePath, subscription.Name)

	var torrentHash string
	var err error

	// 检查是否是 .torrent URL，需要通过代理下载
	torrentURL := subscription.CollectionTorrent
	if strings.HasSuffix(strings.ToLower(torrentURL), ".torrent") ||
		strings.Contains(torrentURL, "/Download/") {
		// 设置代理
		if h.configRepo != nil {
			if proxyConfig, err := h.configRepo.Get("system_proxy"); err == nil && proxyConfig != nil && proxyConfig.Value != "" {
				h.qbClient.SetProxy(proxyConfig.Value)
			}
		}

		// 先下载种子文件
		if qbDownloader, ok := h.qbClient.(interface {
			DownloadTorrentFile(url string) ([]byte, error)
		}); ok {
			fileContent, downloadErr := qbDownloader.DownloadTorrentFile(torrentURL)
			if downloadErr != nil {
				logger.Error("Failed to download collection torrent file",
					"subscription_id", subscription.ID,
					"torrent_url", torrentURL,
					"error", downloadErr.Error())
				return
			}

			// 通过文件内容添加种子
			torrentHash, err = h.qbClient.AddTorrentFile(
				"collection.torrent",
				fileContent,
				downloadPath,
				downloader.AutoRssCategory,
			)
		} else {
			// 回退到 URL 方式
			torrentHash, err = h.qbClient.AddTorrent(
				torrentURL,
				downloadPath,
				downloader.AutoRssCategory,
			)
		}
	} else {
		// magnet 链接或其他，直接添加
		torrentHash, err = h.qbClient.AddTorrent(
			torrentURL,
			downloadPath,
			downloader.AutoRssCategory,
		)
	}

	if err != nil {
		logger.Error("Failed to add collection torrent to qBittorrent",
			"subscription_id", subscription.ID,
			"subscription_name", subscription.Name,
			"torrent_url", torrentURL,
			"download_path", downloadPath,
			"error", err.Error())
		return
	}

	// 如果没有获取到 hash（种子可能已存在），尝试通过 savePath 查找
	if torrentHash == "" {
		logger.Info("Torrent hash empty, searching for existing torrent by savePath",
			"subscription_id", subscription.ID,
			"download_path", downloadPath)

		torrents, listErr := h.qbClient.GetTorrentsByCategory(downloader.AutoRssCategory)
		if listErr == nil {
			for _, t := range torrents {
				// 匹配 savePath（可能完全匹配或以 downloadPath 开头）
				if t.SavePath == downloadPath || strings.HasPrefix(t.SavePath, downloadPath) {
					torrentHash = t.Hash
					logger.Info("Found existing torrent by savePath",
						"subscription_id", subscription.ID,
						"torrent_hash", torrentHash,
						"torrent_name", t.Name,
						"save_path", t.SavePath)
					break
				}
			}
		}
	}

	logger.Info("Collection torrent added successfully",
		"subscription_id", subscription.ID,
		"subscription_name", subscription.Name,
		"torrent_hash", torrentHash,
		"download_path", downloadPath)

	// 创建 Download 记录以支持自动重命名
	// Episode 设为 0 表示合集种子
	if torrentHash != "" && h.downloadRepo != nil {
		// 先检查是否已存在相同 hash 的记录
		existing, _ := h.downloadRepo.GetByHash(torrentHash)
		if existing != nil {
			logger.Info("Download record already exists for collection torrent",
				"subscription_id", subscription.ID,
				"torrent_hash", torrentHash)
			return
		}

		download := &model.Download{
			SubscriptionID: subscription.ID,
			Title:          subscription.Name + " [合集]",
			Episode:        0, // 0 表示合集
			Fansub:         subscription.Fansub,
			TorrentURL:     torrentURL,
			TorrentHash:    torrentHash,
			Status:         "downloading",
		}

		if err := h.downloadRepo.Create(download); err != nil {
			logger.Error("Failed to create download record for collection torrent",
				"subscription_id", subscription.ID,
				"torrent_hash", torrentHash,
				"error", err.Error())
		} else {
			logger.Info("Download record created for collection torrent",
				"subscription_id", subscription.ID,
				"download_id", download.ID,
				"torrent_hash", torrentHash)
		}
	}
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

	// 将标题中的“第X季/Season X”规范到 season 字段，避免把季信息写进标题目录。
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

	// 先获取现有订阅
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

	// 保存原始值用于检测变化
	originalName := existing.Name
	originalSeason := existing.Season

	// 绑定更新数据到 map，只更新提供的字段
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

	// 处理合集种子地址，记录是否需要触发下载
	var shouldDownloadCollection bool
	if collectionTorrent, ok := updates["collection_torrent"].(string); ok {
		// 只有当合集种子地址发生变化且新值不为空时才触发下载
		if collectionTorrent != existing.CollectionTorrent && collectionTorrent != "" {
			shouldDownloadCollection = true
		}
		existing.CollectionTorrent = collectionTorrent
	}

	// 如果没有封面,尝试获取Bangumi数据
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

	// 如果合集种子地址发生变化，自动触发下载
	if shouldDownloadCollection {
		go h.downloadCollectionTorrent(existing)
	}

	// 如果名称或季度发生变化，自动触发文件重命名
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

	// 切换启用状态
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

	// 保存原始名称，用于检测是否发生变化
	originalName := subscription.Name

	// 执行Bangumi数据补全（强制刷新）
	h.enrichWithBangumiInternal(subscription, true)

	// 检测名称是否发生变化
	nameChanged := originalName != subscription.Name

	// 更新到数据库
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

	// 如果名称发生变化，自动触发文件重命名
	if nameChanged {
		logger.Info("Subscription name changed, triggering automatic file rename",
			"subscription_id", id,
			"old_name", originalName,
			"new_name", subscription.Name)

		// 从数据库重新加载 subscription，避免闭包捕获指针问题
		subscriptionCopy, err := h.repo.GetByID(uint(id))
		if err != nil {
			logger.Error("Failed to reload subscription for rename task",
				"subscription_id", id,
				"error", err.Error())
		} else {
			// 启动异步重命名任务
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

	// 异步执行下载
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
	subscriptions, _, err := h.repo.List(0, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to get subscription list",
		})
		return
	}

	// 为每个订阅添加下载统计信息
	type SubscriptionWithStats struct {
		model.Subscription
		DownloadingCount int `json:"downloading_count"`
	}

	subscriptionsWithStats := make([]SubscriptionWithStats, 0, len(subscriptions))
	for _, sub := range subscriptions {
		// 查询该订阅下正在下载的任务数量
		downloads, _, err := h.downloadRepo.List(0, 9999, "downloading")
		downloadingCount := 0
		if err == nil {
			for _, download := range downloads {
				if download.SubscriptionID == sub.ID {
					downloadingCount++
				}
			}
		}

		subscriptionsWithStats = append(subscriptionsWithStats, SubscriptionWithStats{
			Subscription:     sub,
			DownloadingCount: downloadingCount,
		})
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

	// 启动异步任务
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

	// 检查取消
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	manager.UpdateProgress(5, "设置代理...")
	// 设置代理
	h.setProxy()

	// 检查取消
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	manager.UpdateProgress(10, "获取RSS订阅...")
	// 解析RSS获取剧集列表
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

	// 检查取消
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	manager.UpdateProgress(20, "获取已有下载记录...")
	// 获取已有的下载记录
	existingDownloads, err := h.downloadRepo.ListBySubscriptionID(id)
	if err != nil {
		logger.Error("Failed to get existing downloads",
			"subscription_id", id,
			"error", err.Error())
		return fmt.Errorf("获取下载记录失败: %s", err.Error())
	}

	// 按集数分组现有的下载任务 (每个集数保留最新的一个)
	episodeMap := make(map[int]*model.Download)
	hashSet := make(map[string]bool) // 用于快速检查 hash 是否已存在

	for i := range existingDownloads {
		download := &existingDownloads[i]
		hashSet[download.TorrentHash] = true
		episodeMap[download.Episode] = download
	}

	// 收集新的剧集和需要删除的旧任务
	var newDownloads []model.Download
	var deletedCount int

	// RSS items 是按发布时间降序排列的（最新的在前）
	// 所以我们遍历时，先遇到的就是最新的版本
	processedEpisodes := make(map[int]bool)
	maxPubTime := subscription.LastRSSPubTime

	manager.UpdateProgress(25, "分析RSS条目...")

	// 计算需要处理的条目数量
	totalItems := len(items)
	processedItems := 0

	for _, item := range items {
		// 检查取消
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		processedItems++
		progress := 25 + (processedItems * 60 / totalItems) // 25-85%
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

		// 计算相对集数（考虑偏移）
		offset := subscription.EpisodeOffset
		relativeEpisode := item.Episode
		if offset > 0 {
			relativeEpisode = item.Episode - offset
			// 如果相对集数 <= 0，说明这集在偏移之前，跳过
			if relativeEpisode <= 0 {
				logger.Debug("Skipping episode before offset",
					"episode", item.Episode,
					"offset", offset,
					"relative_episode", relativeEpisode,
					"title", item.Title)
				continue
			}
		}

		// 如果设置了总集数，只收集在范围内的
		if subscription.TotalEpisodes > 0 && relativeEpisode > subscription.TotalEpisodes {
			logger.Debug("Skipping episode beyond total",
				"episode", item.Episode,
				"relative_episode", relativeEpisode,
				"total_episodes", subscription.TotalEpisodes,
				"title", item.Title)
			continue
		}

		// 如果 hash 已存在，跳过
		if hashSet[item.TorrentHash] {
			continue
		}

		// 检查是否已有相同集数的任务
		existingDownload, exists := episodeMap[item.Episode]

		if exists && !processedEpisodes[item.Episode] {
			// 该集数已有任务，且是不同的 hash（不同版本）
			// RSS feed 中靠前的是更新的版本，删除旧版本
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

			// 删除旧任务
			if err := h.downloadRepo.Delete(existingDownload.ID); err != nil {
				logger.Error("Failed to delete old download task",
					"download_id", existingDownload.ID,
					"error", err.Error())
				continue
			}
			deletedCount++
		} else if processedEpisodes[item.Episode] {
			// 该集数在本次RSS中已经处理过（保留了更新的版本），跳过旧版本
			logger.Debug("Skipping older version in RSS feed",
				"episode", item.Episode,
				"title", item.Title)
			continue
		}

		// 标记该集数已处理
		processedEpisodes[item.Episode] = true

		// 创建下载任务
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

		// 调用 qBittorrent API 添加下载任务（使用 AutoRss 分类）
		if h.qbClient != nil {
			// 使用系统配置的下载路径
			savePath := h.downloadPath

			// 生成带番剧名的下载路径
			downloadPath := utils.GenerateDownloadPath(savePath, subscription.Name)

			var torrentHash string
			var err error

			// 检查是否是.torrent URL，需要通过代理下载
			if strings.HasSuffix(strings.ToLower(item.TorrentURL), ".torrent") ||
				strings.Contains(item.TorrentURL, "/Download/") {
				// 设置代理
				if h.configRepo != nil {
					if proxyConfig, err := h.configRepo.Get("system_proxy"); err == nil && proxyConfig != nil && proxyConfig.Value != "" {
						h.qbClient.SetProxy(proxyConfig.Value)
					}
				}

				// 先下载种子文件
				if qbDownloader, ok := h.qbClient.(interface {
					DownloadTorrentFile(url string) ([]byte, error)
				}); ok {
					fileContent, downloadErr := qbDownloader.DownloadTorrentFile(item.TorrentURL)
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

					// 通过文件内容添加种子
					torrentHash, err = h.qbClient.AddTorrentFile(
						"torrent.torrent",
						fileContent,
						downloadPath,
						downloader.AutoRssCategory,
					)
				} else {
					// 回退到URL方式
					torrentHash, err = h.qbClient.AddTorrent(
						item.TorrentURL,
						downloadPath,
						downloader.AutoRssCategory,
					)
				}
			} else {
				// magnet链接或其他，直接添加
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
				// 更新下载状态为失败
				download.Status = "failed"
				h.downloadRepo.Update(&download)
				continue
			}

			// 更新下载记录的 hash 和状态
			// 只有当 qBittorrent 返回了有效的 hash 且与原来不同时才更新
			if torrentHash != "" && torrentHash != download.TorrentHash {
				// 检查这个 hash 是否已经存在于其他记录中
				existingByHash, _ := h.downloadRepo.GetByHash(torrentHash)
				if existingByHash != nil && existingByHash.ID != download.ID {
					logger.Warn("Torrent hash already exists in another download record",
						"download_id", download.ID,
						"existing_download_id", existingByHash.ID,
						"hash", torrentHash)
					// 删除当前重复的记录，保留已有的
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

	// 检查取消
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	manager.UpdateProgress(90, "更新集数信息...")

	// 找出RSS中的最新集数
	maxEpisodeInRSS := 0
	for _, item := range items {
		if item.Episode > maxEpisodeInRSS {
			maxEpisodeInRSS = item.Episode
		}
	}

	// 从Bangumi获取最新集数（如果有Bangumi ID）
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

	// 取RSS和Bangumi中的最大值作为最新集数
	latestEpisode := maxEpisodeInRSS
	if maxEpisodeFromBangumi > latestEpisode {
		latestEpisode = maxEpisodeFromBangumi
	}

	// 计算本地已收集的最大集数
	maxCollectedEpisode := 0
	for _, download := range existingDownloads {
		if download.Episode > maxCollectedEpisode && download.Status != "failed" {
			maxCollectedEpisode = download.Episode
		}
	}
	// 考虑本次新增的下载
	for _, download := range newDownloads {
		if download.Episode > maxCollectedEpisode {
			maxCollectedEpisode = download.Episode
		}
	}

	// 更新订阅的集数信息
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

	// 设置任务结果
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
	Title      string `json:"title" binding:"required"` // 番剧名称
	Fansub     string `json:"fansub"`                   // 字幕组
	RssURL     string `json:"rss_url"`                  // RSS URL
	SourceID   uint   `json:"source_id"`                // RSS源ID
	SourceName string `json:"source_name"`              // RSS源名称
}

// BatchImportFromRSS 从RSS批量导入订阅
// 通过Mikan搜索接口将RSS番剧信息转换为合法的订阅
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

	// 设置代理
	h.setProxy()

	// 批量导入结果
	type ImportResult struct {
		Title        string              `json:"title"`
		Success      bool                `json:"success"`
		Message      string              `json:"message"`
		Skipped      bool                `json:"skipped"` // 是否跳过(已存在)
		Subscription *model.Subscription `json:"subscription,omitempty"`
	}

	results := make([]ImportResult, 0, len(req.Items))
	successCount := 0
	skippedCount := 0
	failedCount := 0

	// 获取已有订阅用于去重
	existingSubs, _, err := h.repo.List(1, 9999)
	if err != nil {
		logger.Error("Failed to get existing subscriptions", "error", err)
	}
	existingNames := make(map[string]bool)
	for _, sub := range existingSubs {
		existingNames[sub.Name] = true
	}

	for _, item := range req.Items {
		result := ImportResult{
			Title:   item.Title,
			Success: false,
		}

		// 检查是否已存在
		if existingNames[item.Title] {
			result.Skipped = true
			result.Success = true
			result.Message = "已存在,跳过"
			results = append(results, result)
			skippedCount++
			continue
		}

		// 通过Mikan搜索找到对应的番剧
		searchResult, err := h.mikanService.Search(item.Title)
		if err != nil {
			result.Message = "搜索失败: " + err.Error()
			results = append(results, result)
			failedCount++
			logger.Error("Mikan search failed", "title", item.Title, "error", err)
			continue
		}

		// 查找匹配的番剧
		var foundAnime *mikan.AnimeItem

		// 如果搜索结果为空或没有分组,尝试使用第一个结果
		if searchResult == nil || len(searchResult.Groups) == 0 {
			result.Message = "Mikan搜索无结果"
			results = append(results, result)
			failedCount++
			logger.Warn("Mikan search returned no results", "title", item.Title)
			continue
		}

		// 先尝试精确匹配
		for _, group := range searchResult.Groups {
			for _, anime := range group.Items {
				if anime.Title == item.Title {
					foundAnime = anime
					break
				}
			}
			if foundAnime != nil {
				break
			}
		}

		// 如果精确匹配失败,使用第一个搜索结果
		if foundAnime == nil {
			if len(searchResult.Groups) > 0 && len(searchResult.Groups[0].Items) > 0 {
				foundAnime = searchResult.Groups[0].Items[0]
				logger.Info("Using first search result as fallback",
					"original_title", item.Title,
					"matched_title", foundAnime.Title)
			} else {
				result.Message = "未找到匹配的番剧"
				results = append(results, result)
				failedCount++
				logger.Warn("No matching anime found in Mikan", "title", item.Title)
				continue
			}
		}

		// 获取字幕组列表
		fansubGroups, err := h.mikanService.GetFansubGroups(foundAnime.URL)
		if err != nil {
			result.Message = "获取字幕组失败: " + err.Error()
			results = append(results, result)
			failedCount++
			logger.Error("Failed to get fansub groups", "title", item.Title, "error", err)
			continue
		}

		// 查找匹配的字幕组
		var selectedGroup *mikan.FansubGroup
		if item.Fansub != "" {
			for i := range fansubGroups {
				if fansubGroups[i].Name == item.Fansub {
					selectedGroup = fansubGroups[i]
					break
				}
			}
		}

		// 如果没有匹配的字幕组,使用第一个
		if selectedGroup == nil && len(fansubGroups) > 0 {
			selectedGroup = fansubGroups[0]
		}

		if selectedGroup == nil {
			result.Message = "未找到可用的字幕组"
			results = append(results, result)
			failedCount++
			logger.Warn("No fansub group available", "title", item.Title)
			continue
		}

		// 创建订阅
		subscription := &model.Subscription{
			Name:          item.Title,
			RssURL:        selectedGroup.RSS,
			Season:        1,
			Status:        "active",
			Enabled:       true,
			Fansub:        selectedGroup.Name,
			RenameEnabled: true,
			// DownloadPath 不设置，统一使用系统配置
		}

		// 先从 Bangumi 获取数据
		logger.Info("Fetching Bangumi data before creating subscription",
			"title", item.Title)
		h.enrichWithBangumi(subscription)

		// 保存订阅
		if err := h.repo.Create(subscription); err != nil {
			result.Message = "创建订阅失败: " + err.Error()
			results = append(results, result)
			failedCount++
			logger.Error("Failed to create subscription", "title", item.Title, "error", err)
			continue
		}

		// 如果 enrichWithBangumi 没有在创建前完成，再次尝试更新
		if subscription.BangumiID == 0 {
			h.enrichWithBangumi(subscription)
			// 保存Bangumi数据
			if err := h.repo.Update(subscription); err != nil {
				logger.Error("Failed to update subscription with Bangumi data", "subscription_id", subscription.ID, "error", err)
			}
		}

		result.Success = true
		result.Message = "导入成功"
		result.Subscription = subscription
		results = append(results, result)
		successCount++
		existingNames[item.Title] = true // 防止重复导入

		logger.Info("Subscription imported successfully",
			"title", item.Title,
			"fansub", selectedGroup.Name,
			"subscription_id", subscription.ID)
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
// 根据当前订阅的名称和季度设置，移动和重命名已下载的文件
func (h *SubscriptionHandler) ReorganizeFiles(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid subscription ID",
		})
		return
	}

	// 获取订阅信息
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

	// 启动异步任务
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

// doReorganizeFiles 执行文件重组织的核心逻辑（通过 qBittorrent API）
func (h *SubscriptionHandler) doReorganizeFiles(ctx context.Context, t *task.Task, subscription *model.Subscription) error {
	manager := task.GetManager()
	id := subscription.ID

	manager.UpdateProgress(5, "获取已下载文件列表...")

	// 获取该订阅的所有已完成下载
	downloads, err := h.downloadRepo.ListBySubscriptionID(id)
	if err != nil {
		return fmt.Errorf("获取下载记录失败: %s", err.Error())
	}

	// 过滤出已完成的下载
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

	// 获取重命名模板
	renameTemplate := "${title}/Season ${season}/${title} S${seasonFormat}E${episodeFormat}"
	if h.configRepo != nil {
		if templateConfig, err := h.configRepo.Get("rename_template"); err == nil && templateConfig != nil && templateConfig.Value != "" {
			renameTemplate = templateConfig.Value
		}
	}
	renameService := downloader.NewRenameService(renameTemplate)

	// 获取基础下载路径
	basePath := h.downloadPath
	if h.configRepo != nil {
		if pathConfig, err := h.configRepo.Get("download_path"); err == nil && pathConfig != nil && pathConfig.Value != "" {
			basePath = pathConfig.Value
		}
	}

	manager.UpdateProgress(10, "开始整理文件...")

	totalFiles := len(completedDownloads)
	movedCount := 0
	renamedCount := 0
	errorCount := 0

	for i, download := range completedDownloads {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		progress := 10 + (i * 80 / totalFiles)
		manager.UpdateProgress(progress, fmt.Sprintf("处理第 %d/%d 个文件...", i+1, totalFiles))

		// 获取 qBittorrent 中的种子信息
		torrentInfo, err := h.qbClient.GetTorrentInfo(download.TorrentHash)
		if err != nil {
			logger.Warn("Failed to get torrent info",
				"hash", download.TorrentHash,
				"error", err.Error())
			errorCount++
			continue
		}

		// 获取种子文件列表
		files, err := h.qbClient.GetTorrentFiles(download.TorrentHash)
		if err != nil {
			logger.Warn("Failed to get torrent files",
				"hash", download.TorrentHash,
				"error", err.Error())
			errorCount++
			continue
		}

		if len(files) == 0 {
			logger.Warn("No files found in torrent",
				"hash", download.TorrentHash)
			errorCount++
			continue
		}

		// 找到主视频文件
		var mainVideoFile *downloader.TorrentFile
		for j := range files {
			ext := strings.ToLower(filepath.Ext(files[j].Name))
			if isVideoFile(ext) {
				if mainVideoFile == nil || files[j].Size > mainVideoFile.Size {
					mainVideoFile = &files[j]
				}
			}
		}

		if mainVideoFile == nil {
			logger.Warn("No video file found in torrent",
				"hash", download.TorrentHash)
			errorCount++
			continue
		}

		ext := strings.ToLower(filepath.Ext(mainVideoFile.Name))

		// 生成新的文件名（带目录结构）
		// 注意：这里使用原始集数，不应用偏移，因为文件名应该保持原始集数
		renameCtx := &downloader.RenameContext{
			Subscription: subscription,
			Download: &model.Download{
				Episode: download.Episode,
			},
			OriginalName: mainVideoFile.Name,
			Extension:    ext,
		}
		newRelativePath := renameService.GenerateFileName(renameCtx)

		// 分离目录和文件名
		newDir := filepath.Dir(newRelativePath)
		newFileName := filepath.Base(newRelativePath)
		targetLocation := filepath.Join(basePath, newDir)

		// 当前位置
		currentLocation := torrentInfo.SavePath

		// Step 1: 移动种子到新位置（如果需要）
		if currentLocation != targetLocation {
			logger.Info("Moving torrent via qBittorrent API",
				"hash", download.TorrentHash,
				"from", currentLocation,
				"to", targetLocation)

			if err := h.qbClient.SetLocation(download.TorrentHash, targetLocation); err != nil {
				logger.Error("Failed to move torrent",
					"hash", download.TorrentHash,
					"target", targetLocation,
					"error", err.Error())
				errorCount++
				continue
			}
			movedCount++
			logger.Info("Torrent moved successfully",
				"hash", download.TorrentHash,
				"new_location", targetLocation)
		}

		// Step 2: 重命名文件（如果需要）
		oldFileName := mainVideoFile.Name
		// 处理文件在子目录中的情况（oldFileName 可能包含路径）
		oldFileBaseName := filepath.Base(oldFileName)

		if oldFileBaseName != newFileName {
			// 构建新的相对路径（在种子内部）
			newFilePath := newFileName
			if strings.Contains(oldFileName, string(filepath.Separator)) {
				// 如果原文件在子目录中，保持目录结构但改变文件名
				oldDir := filepath.Dir(oldFileName)
				newFilePath = filepath.Join(oldDir, newFileName)
			}

			logger.Info("Renaming file via qBittorrent API",
				"hash", download.TorrentHash,
				"from", oldFileName,
				"to", newFilePath)

			if err := h.qbClient.RenameTorrentFile(download.TorrentHash, oldFileName, newFilePath); err != nil {
				logger.Warn("Failed to rename file (may not be supported for multi-file torrents)",
					"hash", download.TorrentHash,
					"error", err.Error())
				// 重命名失败不算严重错误，继续处理
			} else {
				renamedCount++
				logger.Info("File renamed successfully",
					"hash", download.TorrentHash,
					"new_name", newFilePath)
			}
		}
	}

	manager.UpdateProgress(100, "整理完成")

	logger.Info("File reorganization completed",
		"subscription_id", id,
		"total_downloads", totalFiles,
		"moved", movedCount,
		"renamed", renamedCount,
		"errors", errorCount)

	if errorCount > 0 && movedCount == 0 && renamedCount == 0 {
		return fmt.Errorf("整理失败，%d 个错误", errorCount)
	}

	manager.SetResult(map[string]interface{}{
		"moved":   movedCount,
		"renamed": renamedCount,
		"errors":  errorCount,
	})

	return nil
}

// isVideoFile 检查是否是视频文件
func isVideoFile(ext string) bool {
	videoExts := map[string]bool{
		".mp4": true, ".mkv": true, ".avi": true, ".wmv": true,
		".mov": true, ".flv": true, ".webm": true, ".m4v": true,
		".ts": true, ".m2ts": true,
	}
	return videoExts[ext]
}

// moveFile 移动文件（支持跨分区）
func moveFile(src, dest string) error {
	// 如果目标文件已存在，添加时间戳后缀
	if _, err := os.Stat(dest); err == nil {
		ext := filepath.Ext(dest)
		base := strings.TrimSuffix(dest, ext)
		timestamp := time.Now().Format("20060102_150405")
		dest = fmt.Sprintf("%s_%s%s", base, timestamp, ext)
		logger.Warn("Target file already exists, using new name", "new_path", dest)
	}

	// 尝试重命名（同一文件系统）
	err := os.Rename(src, dest)
	if err == nil {
		return nil
	}

	// 跨文件系统，复制后删除
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	destFile, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, srcFile); err != nil {
		return err
	}

	// 复制成功后删除源文件
	srcFile.Close()
	return os.Remove(src)
}

// cleanEmptyDirs 递归清理空目录
func cleanEmptyDirs(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			subDir := filepath.Join(dir, entry.Name())
			cleanEmptyDirs(subDir)
		}
	}

	// 重新读取，检查是否为空
	entries, _ = os.ReadDir(dir)
	if len(entries) == 0 {
		os.Remove(dir)
		logger.Debug("Removed empty directory", "dir", dir)
	}
}

// RenameFiles 批量重命名订阅的已下载文件
// POST /api/subscriptions/:id/rename-files
func (h *SubscriptionHandler) RenameFiles(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid subscription ID",
		})
		return
	}

	// 获取订阅信息
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

	// 启动异步任务
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

	// 获取所有已完成的下载记录
	completedDownloads, _, err := h.downloadRepo.List(0, 1000, "completed")
	if err != nil {
		return fmt.Errorf("failed to get downloads: %w", err)
	}

	// 过滤出属于当前订阅的下载
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

	// 获取重命名模板
	renameTemplate := "${title}/Season ${season}/${title} S${seasonFormat}E${episodeFormat}"
	if h.configRepo != nil {
		if templateConfig, err := h.configRepo.Get("rename_template"); err == nil && templateConfig != nil && templateConfig.Value != "" {
			renameTemplate = templateConfig.Value
		}
	}
	renameService := downloader.NewRenameService(renameTemplate)

	// 获取基础下载路径
	basePath := h.downloadPath
	if h.configRepo != nil {
		if pathConfig, err := h.configRepo.Get("download_path"); err == nil && pathConfig != nil && pathConfig.Value != "" {
			basePath = pathConfig.Value
		}
	}

	totalFiles := len(downloads)
	renamedCount := 0
	movedCount := 0
	errorCount := 0

	for i, download := range downloads {
		// 检查取消
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		progress := int((float64(i) / float64(totalFiles)) * 100)
		manager.UpdateProgress(progress, fmt.Sprintf("正在处理 %d/%d: 第%d集", i+1, totalFiles, download.Episode))

		// 获取种子信息
		if download.TorrentHash == "" {
			logger.Warn("Download has no torrent hash, skipping",
				"download_id", download.ID,
				"episode", download.Episode)
			errorCount++
			continue
		}

		torrentInfo, err := h.qbClient.GetTorrentInfo(download.TorrentHash)
		if err != nil {
			logger.Warn("Failed to get torrent info, skipping",
				"download_id", download.ID,
				"hash", download.TorrentHash,
				"error", err.Error())
			errorCount++
			continue
		}

		// 获取种子文件列表
		files, err := h.qbClient.GetTorrentFiles(download.TorrentHash)
		if err != nil {
			logger.Warn("Failed to get torrent files, skipping",
				"download_id", download.ID,
				"hash", download.TorrentHash,
				"error", err.Error())
			errorCount++
			continue
		}

		if len(files) == 0 {
			logger.Warn("No files found in torrent, skipping",
				"download_id", download.ID,
				"hash", download.TorrentHash)
			errorCount++
			continue
		}

		// 提取主视频文件
		mainVideoFile := downloader.ExtractFileInfo(files)
		if mainVideoFile == nil {
			logger.Warn("No video file found in torrent, skipping",
				"download_id", download.ID,
				"hash", download.TorrentHash)
			errorCount++
			continue
		}

		// 生成新的文件名和路径
		ext := strings.ToLower(filepath.Ext(mainVideoFile.Name))
		renameCtx := &downloader.RenameContext{
			Subscription: subscription,
			Download: &model.Download{
				Episode: download.Episode,
			},
			OriginalName: mainVideoFile.Name,
			Extension:    ext,
		}
		newRelativePath := renameService.GenerateFileName(renameCtx)

		// 分离目录和文件名
		newDir := filepath.Dir(newRelativePath)
		newFileName := filepath.Base(newRelativePath)
		targetLocation := filepath.Join(basePath, newDir)

		// 当前位置
		currentLocation := torrentInfo.SavePath

		// Step 1: 移动种子到新位置（如果需要）
		if currentLocation != targetLocation {
			logger.Info("Moving torrent",
				"hash", download.TorrentHash,
				"from", currentLocation,
				"to", targetLocation)

			if err := h.qbClient.SetLocation(download.TorrentHash, targetLocation); err != nil {
				logger.Error("Failed to move torrent",
					"hash", download.TorrentHash,
					"target", targetLocation,
					"error", err.Error())
				errorCount++
				continue
			}
			movedCount++
			logger.Info("Torrent moved successfully",
				"hash", download.TorrentHash,
				"new_location", targetLocation)
		}

		// Step 2: 重命名文件（如果需要）
		oldFileName := mainVideoFile.Name
		oldFileBaseName := filepath.Base(oldFileName)

		if oldFileBaseName != newFileName {
			// 构建新的相对路径（在种子内部）
			newFilePath := newFileName
			if strings.Contains(oldFileName, string(filepath.Separator)) {
				// 如果原文件在子目录中，保持目录结构但改变文件名
				oldDir := filepath.Dir(oldFileName)
				newFilePath = filepath.Join(oldDir, newFileName)
			}

			logger.Info("Renaming file",
				"hash", download.TorrentHash,
				"from", oldFileName,
				"to", newFilePath)

			if err := h.qbClient.RenameTorrentFile(download.TorrentHash, oldFileName, newFilePath); err != nil {
				logger.Warn("Failed to rename file",
					"hash", download.TorrentHash,
					"error", err.Error())
				errorCount++
				continue
			}
			renamedCount++
			logger.Info("File renamed successfully",
				"hash", download.TorrentHash,
				"new_name", newFilePath)
		}

		// 更新数据库中的renamed_path
		download.RenamedPath = filepath.Join(targetLocation, newFileName)
		if err := h.downloadRepo.Update(&download); err != nil {
			logger.Warn("Failed to update download record",
				"download_id", download.ID,
				"error", err.Error())
			// 不算作错误，因为文件操作已成功
		}
	}

	manager.UpdateProgress(100, "重命名完成")

	logger.Info("Batch rename completed",
		"subscription_id", subscription.ID,
		"total_downloads", totalFiles,
		"moved", movedCount,
		"renamed", renamedCount,
		"errors", errorCount)

	if errorCount > 0 && movedCount == 0 && renamedCount == 0 {
		return fmt.Errorf("重命名失败，%d 个错误", errorCount)
	}

	manager.SetResult(map[string]interface{}{
		"moved":   movedCount,
		"renamed": renamedCount,
		"errors":  errorCount,
	})

	return nil
}
