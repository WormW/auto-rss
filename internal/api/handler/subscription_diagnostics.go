package handler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/downloader"
	"github.com/WormW/auto-rss/internal/service/rss"
	"github.com/gin-gonic/gin"
)

type SubscriptionDiagnosticStatus string

const (
	SubscriptionDiagnosticHealthy SubscriptionDiagnosticStatus = "healthy"
	SubscriptionDiagnosticWarning SubscriptionDiagnosticStatus = "warning"
	SubscriptionDiagnosticError   SubscriptionDiagnosticStatus = "error"
	SubscriptionDiagnosticUnknown SubscriptionDiagnosticStatus = "unknown"
)

type SubscriptionDiagnosticsHandler struct {
	subscriptionRepo repository.SubscriptionRepository
	feedRepo         rss.SubscriptionFeedReader
	downloadRepo     repository.DownloadRepository
	configRepo       repository.ConfigRepository
	qbClient         downloader.QBittorrentClient
	rssHealthChecker *rss.RSSHealthChecker
	requeueSvc       DownloadRequeueService
}

type SubscriptionDiagnosticCheck struct {
	Key     string                       `json:"key"`
	Label   string                       `json:"label"`
	Checked bool                         `json:"checked"`
	Status  SubscriptionDiagnosticStatus `json:"status"`
	Summary string                       `json:"summary"`
	Detail  string                       `json:"detail"`
}

type SubscriptionDiagnosticSummary struct {
	Overall string `json:"overall"`
	Checked int    `json:"checked"`
	Total   int    `json:"total"`
	Healthy int    `json:"healthy"`
	Warning int    `json:"warning"`
	Error   int    `json:"error"`
	Unknown int    `json:"unknown"`
}

type SubscriptionDownloadDiagnostics struct {
	Total               int                                  `json:"total"`
	Pending             int                                  `json:"pending"`
	Downloading         int                                  `json:"downloading"`
	Stalled             int                                  `json:"stalled"`
	Failed              int                                  `json:"failed"`
	Completed           int                                  `json:"completed"`
	Organizing          int                                  `json:"organizing"`
	Retryable           int                                  `json:"retryable"`
	MissingTorrentTasks int                                  `json:"missing_torrent_tasks"`
	FailedItems         []SubscriptionDownloadDiagnosticItem `json:"failed_items"`
}

type SubscriptionDownloadDiagnosticsPatch struct {
	Total               *int                                  `json:"total,omitempty"`
	Pending             *int                                  `json:"pending,omitempty"`
	Downloading         *int                                  `json:"downloading,omitempty"`
	Stalled             *int                                  `json:"stalled,omitempty"`
	Failed              *int                                  `json:"failed,omitempty"`
	Completed           *int                                  `json:"completed,omitempty"`
	Organizing          *int                                  `json:"organizing,omitempty"`
	Retryable           *int                                  `json:"retryable,omitempty"`
	MissingTorrentTasks *int                                  `json:"missing_torrent_tasks,omitempty"`
	FailedItems         *[]SubscriptionDownloadDiagnosticItem `json:"failed_items,omitempty"`
}

type SubscriptionDownloadDiagnosticItem struct {
	ID           uint   `json:"id"`
	Title        string `json:"title"`
	Episode      int    `json:"episode"`
	Status       string `json:"status"`
	Severity     string `json:"severity"`
	Category     string `json:"category"`
	Reason       string `json:"reason"`
	CanRetry     bool   `json:"can_retry"`
	RetryBlocked string `json:"retry_blocked,omitempty"`
}

type SubscriptionFileDiagnostics struct {
	RenameEnabled        bool  `json:"rename_enabled"`
	CompletedWithFile    int   `json:"completed_with_file"`
	CompletedMissingFile int   `json:"completed_missing_file"`
	MissingRenamed       int   `json:"missing_renamed"`
	MissingEpisodes      []int `json:"missing_episodes"`
}

type SubscriptionFileDiagnosticsPatch struct {
	RenameEnabled        *bool  `json:"rename_enabled,omitempty"`
	CompletedWithFile    *int   `json:"completed_with_file,omitempty"`
	CompletedMissingFile *int   `json:"completed_missing_file,omitempty"`
	MissingRenamed       *int   `json:"missing_renamed,omitempty"`
	MissingEpisodes      *[]int `json:"missing_episodes,omitempty"`
}

type SubscriptionDiagnosticAction struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Method   string `json:"method"`
	Endpoint string `json:"endpoint"`
	Enabled  bool   `json:"enabled"`
	Reason   string `json:"reason,omitempty"`
}

type SubscriptionDiagnosticsResponse struct {
	SubscriptionID uint                            `json:"subscription_id"`
	Name           string                          `json:"name"`
	Enabled        bool                            `json:"enabled"`
	CheckedAt      time.Time                       `json:"checked_at"`
	Feeds          []rss.FeedHealthCheckResult     `json:"feeds"`
	Summary        SubscriptionDiagnosticSummary   `json:"summary"`
	Checks         []SubscriptionDiagnosticCheck   `json:"checks"`
	Downloads      SubscriptionDownloadDiagnostics `json:"downloads"`
	Files          SubscriptionFileDiagnostics     `json:"files"`
	Actions        []SubscriptionDiagnosticAction  `json:"actions"`
}

type SubscriptionDiagnosticCheckResponse struct {
	Check     SubscriptionDiagnosticCheck           `json:"check"`
	Feeds     []rss.FeedHealthCheckResult           `json:"feeds,omitempty"`
	Downloads *SubscriptionDownloadDiagnosticsPatch `json:"downloads,omitempty"`
	Files     *SubscriptionFileDiagnosticsPatch     `json:"files,omitempty"`
	Actions   []SubscriptionDiagnosticAction        `json:"actions,omitempty"`
}

type SubscriptionRetryFailedResponse struct {
	SubscriptionID uint                      `json:"subscription_id"`
	Retried        int                       `json:"retried"`
	Failed         int                       `json:"failed"`
	Skipped        int                       `json:"skipped"`
	Results        []SubscriptionRetryResult `json:"results"`
}

type SubscriptionRetryResult struct {
	ID      uint   `json:"id"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func NewSubscriptionDiagnosticsHandler(
	subscriptionRepo repository.SubscriptionRepository,
	feedRepo rss.SubscriptionFeedReader,
	downloadRepo repository.DownloadRepository,
	configRepo repository.ConfigRepository,
	qbClient downloader.QBittorrentClient,
	_ string,
	requeueSvc ...DownloadRequeueService,
) *SubscriptionDiagnosticsHandler {
	var requeue DownloadRequeueService
	if len(requeueSvc) > 0 {
		requeue = requeueSvc[0]
	}
	return &SubscriptionDiagnosticsHandler{
		subscriptionRepo: subscriptionRepo,
		feedRepo:         feedRepo,
		downloadRepo:     downloadRepo,
		configRepo:       configRepo,
		qbClient:         qbClient,
		rssHealthChecker: rss.NewHealthChecker(subscriptionRepo, feedRepo),
		requeueSvc:       requeue,
	}
}

func (h *SubscriptionDiagnosticsHandler) Get(c *gin.Context) {
	subscription, ok := h.loadSubscription(c)
	if !ok {
		return
	}

	enabledFeeds, err := h.loadEnabledFeeds(subscription)
	if err != nil {
		logger.Error("Failed to list feeds for subscription diagnostics",
			"subscription_id", subscription.ID,
			"error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取订阅源失败",
		})
		return
	}

	checks := initialSubscriptionDiagnosticChecks()
	summary := summarizeSubscriptionChecks(checks)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data": SubscriptionDiagnosticsResponse{
			SubscriptionID: subscription.ID,
			Name:           subscription.Name,
			Enabled:        subscription.Enabled,
			CheckedAt:      time.Now(),
			Feeds:          feedHealthSnapshots(enabledFeeds),
			Summary:        summary,
			Checks:         checks,
			Downloads: SubscriptionDownloadDiagnostics{
				FailedItems: []SubscriptionDownloadDiagnosticItem{},
			},
			Files: SubscriptionFileDiagnostics{
				MissingEpisodes: []int{},
			},
			Actions: h.buildActions(subscription, SubscriptionDownloadDiagnostics{}, false, len(enabledFeeds) > 0),
		},
	})
}

func (h *SubscriptionDiagnosticsHandler) Check(c *gin.Context) {
	subscription, ok := h.loadSubscription(c)
	if !ok {
		return
	}

	key := c.Param("key")
	result := SubscriptionDiagnosticCheckResponse{}
	enabledFeeds, err := h.loadEnabledFeeds(subscription)
	if err != nil {
		logger.Error("Failed to list feeds for subscription diagnostic check",
			"subscription_id", subscription.ID,
			"error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取订阅源失败",
		})
		return
	}

	switch key {
	case "subscription_enabled":
		result.Check = h.buildEnabledCheck(subscription)
	case "rss_reachability":
		healthResult, check := h.runRSSReachabilityCheck(c.Request.Context(), subscription, enabledFeeds)
		result.Check = check
		if healthResult != nil {
			result.Feeds = healthResult.Feeds
		}
	case "rss_freshness":
		result.Check = h.buildRSSFreshnessCheck(enabledFeeds)
		result.Feeds = feedHealthSnapshots(enabledFeeds)
	case "episode_progress":
		pending := computeMissingEpisodes(subscription)
		result.Check = h.buildEpisodeProgressCheck(pending)
		result.Files = episodeProgressFilePatch(pending)
	case "downloads":
		downloads, ok := h.loadDownloads(c, subscription.ID)
		if !ok {
			return
		}
		summary, check := h.buildDownloadDiagnostics(downloads)
		result.Check = check
		result.Downloads = downloadDiagnosticsPatch(summary)
		result.Actions = h.buildActions(subscription, summary, true, len(enabledFeeds) > 0)
	case "qbittorrent":
		downloads, ok := h.loadDownloads(c, subscription.ID)
		if !ok {
			return
		}
		summary, _ := h.buildDownloadDiagnostics(downloads)
		result.Check = h.buildQBittorrentCheck(downloads, &summary)
		result.Downloads = qbittorrentDiagnosticsPatch(summary.MissingTorrentTasks)
	case "files":
		downloads, ok := h.loadDownloads(c, subscription.ID)
		if !ok {
			return
		}
		files, check := h.buildFileDiagnostics(subscription, downloads)
		result.Check = check
		result.Files = fileDiagnosticsPatch(files)
		result.Actions = h.buildActions(subscription, downloadSummaryForActions(downloads), true, len(enabledFeeds) > 0)
	case "organizer":
		downloads, ok := h.loadDownloads(c, subscription.ID)
		if !ok {
			return
		}
		files, check := h.buildOrganizerDiagnostics(subscription, downloads)
		result.Check = check
		result.Files = organizerDiagnosticsPatch(files)
		result.Actions = h.buildActions(subscription, downloadSummaryForActions(downloads), true, len(enabledFeeds) > 0)
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "未知的诊断检查项",
		})
		return
	}
	result.Check.Checked = true

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data":    result,
	})
}

func (h *SubscriptionDiagnosticsHandler) RetryFailed(c *gin.Context) {
	subscription, ok := h.loadSubscription(c)
	if !ok {
		return
	}

	downloads, err := h.downloadRepo.ListBySubscriptionID(subscription.ID)
	if err != nil {
		logger.Error("Failed to list downloads for retry",
			"subscription_id", subscription.ID,
			"error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取订阅下载记录失败",
		})
		return
	}

	response := SubscriptionRetryFailedResponse{
		SubscriptionID: subscription.ID,
		Results:        make([]SubscriptionRetryResult, 0),
	}

	for _, download := range downloads {
		if download.Status != model.DownloadStatusFailed && download.Status != model.DownloadStatusStalled {
			continue
		}

		result := SubscriptionRetryResult{
			ID:     download.ID,
			Title:  download.Title,
			Status: download.Status,
		}

		if strings.TrimSpace(download.TorrentURL) == "" {
			response.Skipped++
			result.Message = "缺少种子链接，无法重试"
			response.Results = append(response.Results, result)
			continue
		}

		if err := h.retryDownload(subscription, &download); err != nil {
			response.Failed++
			result.Message = err.Error()
			response.Results = append(response.Results, result)
			continue
		}

		response.Retried++
		result.Success = true
		result.Status = model.DownloadStatusRetryCleanup
		result.Message = "已排队清理旧下载，清理完成后将自动重试"
		response.Results = append(response.Results, result)
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data":    response,
	})
}

func (h *SubscriptionDiagnosticsHandler) loadSubscription(c *gin.Context) (*model.Subscription, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid subscription ID",
		})
		return nil, false
	}

	subscription, err := h.subscriptionRepo.GetByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "Subscription not found",
		})
		return nil, false
	}

	return subscription, true
}

func (h *SubscriptionDiagnosticsHandler) loadDownloads(c *gin.Context, subscriptionID uint) ([]model.Download, bool) {
	if h.downloadRepo == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "下载仓储未初始化",
		})
		return nil, false
	}

	downloads, err := h.downloadRepo.ListBySubscriptionID(subscriptionID)
	if err != nil {
		logger.Error("Failed to list downloads for subscription diagnostics",
			"subscription_id", subscriptionID,
			"error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取订阅下载记录失败",
		})
		return nil, false
	}
	return downloads, true
}

func (h *SubscriptionDiagnosticsHandler) loadEnabledFeeds(subscription *model.Subscription) ([]model.SubscriptionFeed, error) {
	if h.feedRepo == nil {
		if strings.TrimSpace(subscription.RssURL) == "" {
			return []model.SubscriptionFeed{}, nil
		}
		return []model.SubscriptionFeed{{
			SubscriptionID: subscription.ID,
			Name:           subscription.Name,
			Fansub:         subscription.Fansub,
			RSSURL:         subscription.RssURL,
			EpisodeOffset:  subscription.EpisodeOffset,
			Enabled:        true,
		}}, nil
	}

	feeds, err := h.feedRepo.ListBySubscription(subscription.ID)
	if err != nil {
		return nil, err
	}
	enabled := make([]model.SubscriptionFeed, 0, len(feeds))
	for i := range feeds {
		if feeds[i].Enabled {
			enabled = append(enabled, feeds[i])
		}
	}
	return enabled, nil
}

func feedHealthSnapshots(feeds []model.SubscriptionFeed) []rss.FeedHealthCheckResult {
	results := make([]rss.FeedHealthCheckResult, 0, len(feeds))
	for i := range feeds {
		results = append(results, rss.FeedHealthCheckResult{
			SubscriptionFeedID: feeds[i].ID,
			Name:               feeds[i].Name,
			Fansub:             feeds[i].Fansub,
			RSSURL:             feeds[i].RSSURL,
			Status:             rss.HealthStatusUnknown,
			LastPostDate:       feeds[i].LastRSSPubTime,
			LastSuccessAt:      feeds[i].LastSuccessAt,
			LastError:          feeds[i].LastError,
		})
	}
	return results
}

func (h *SubscriptionDiagnosticsHandler) buildEnabledCheck(subscription *model.Subscription) SubscriptionDiagnosticCheck {
	if !subscription.Enabled || subscription.Status == "paused" {
		return SubscriptionDiagnosticCheck{
			Key:     "subscription_enabled",
			Label:   "订阅状态",
			Status:  SubscriptionDiagnosticWarning,
			Summary: "订阅已暂停",
			Detail:  "后台不会为暂停的订阅自动获取 RSS 或创建下载任务。",
		}
	}

	return SubscriptionDiagnosticCheck{
		Key:     "subscription_enabled",
		Label:   "订阅状态",
		Status:  SubscriptionDiagnosticHealthy,
		Summary: "订阅已启用",
		Detail:  "后台调度会按配置继续检查这个订阅。",
	}
}

func (h *SubscriptionDiagnosticsHandler) runRSSReachabilityCheck(
	ctx context.Context,
	subscription *model.Subscription,
	enabledFeeds []model.SubscriptionFeed,
) (*rss.HealthCheckResult, SubscriptionDiagnosticCheck) {
	proxyURL := ""
	if h.configRepo != nil {
		if config, err := h.configRepo.Get("system_proxy"); err == nil && config != nil {
			proxyURL = config.Value
		}
	}
	if err := h.rssHealthChecker.SetProxy(proxyURL); err != nil {
		return nil, SubscriptionDiagnosticCheck{
			Key:     "rss_reachability",
			Label:   "RSS 可达性",
			Status:  SubscriptionDiagnosticError,
			Summary: "RSS 代理配置无效",
			Detail:  err.Error(),
		}
	}

	checkCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	healthResult := h.rssHealthChecker.CheckSubscriptionFeeds(checkCtx, subscription, enabledFeeds)
	return healthResult, h.buildRSSReachabilityCheck(healthResult)
}

func (h *SubscriptionDiagnosticsHandler) buildRSSReachabilityCheck(result *rss.HealthCheckResult) SubscriptionDiagnosticCheck {
	if len(result.Feeds) == 0 {
		return SubscriptionDiagnosticCheck{
			Key:     "rss_reachability",
			Label:   "RSS 可达性",
			Status:  SubscriptionDiagnosticUnknown,
			Summary: "未配置启用的订阅源",
			Detail:  "这个订阅没有启用的 RSS feed，无法执行增量检查。",
		}
	}

	check := SubscriptionDiagnosticCheck{
		Key:   "rss_reachability",
		Label: "RSS 可达性",
	}
	healthyCount := 0
	failedDetails := make([]string, 0)
	for _, feed := range result.Feeds {
		if feed.Status == rss.HealthStatusHealthy {
			healthyCount++
			continue
		}
		detail := feed.ErrorMessage
		if strings.TrimSpace(detail) == "" {
			detail = string(feed.Status)
		}
		failedDetails = append(failedDetails, fmt.Sprintf("%s: %s", feed.Name, detail))
	}

	switch {
	case healthyCount == len(result.Feeds):
		check.Status = SubscriptionDiagnosticHealthy
		check.Summary = fmt.Sprintf("%d 个订阅源均可访问", healthyCount)
		check.Detail = "所有启用的 RSS feed 均可访问并成功解析。"
	case healthyCount > 0:
		check.Status = SubscriptionDiagnosticWarning
		check.Summary = fmt.Sprintf("%d/%d 个订阅源可用", healthyCount, len(result.Feeds))
		check.Detail = strings.Join(failedDetails, "；")
	case result.Status == rss.HealthStatusUnknown:
		check.Status = SubscriptionDiagnosticUnknown
		check.Summary = "订阅源状态未知"
		check.Detail = strings.Join(failedDetails, "；")
	default:
		check.Status = SubscriptionDiagnosticError
		check.Summary = "全部订阅源检查失败"
		check.Detail = strings.Join(failedDetails, "；")
	}

	if strings.TrimSpace(check.Detail) == "" {
		check.Detail = "未返回额外原因。"
	}
	return check
}

func (h *SubscriptionDiagnosticsHandler) buildRSSFreshnessCheck(feeds []model.SubscriptionFeed) SubscriptionDiagnosticCheck {
	if len(feeds) == 0 {
		return SubscriptionDiagnosticCheck{
			Key:     "rss_freshness",
			Label:   "最近检查",
			Status:  SubscriptionDiagnosticUnknown,
			Summary: "没有启用的订阅源",
			Detail:  "启用 feed 后会记录成功检查时间和发布时间水位线。",
		}
	}
	var latestSuccess, latestPub *time.Time
	for i := range feeds {
		if feeds[i].LastSuccessAt != nil && (latestSuccess == nil || feeds[i].LastSuccessAt.After(*latestSuccess)) {
			value := *feeds[i].LastSuccessAt
			latestSuccess = &value
		}
		if feeds[i].LastRSSPubTime != nil && (latestPub == nil || feeds[i].LastRSSPubTime.After(*latestPub)) {
			value := *feeds[i].LastRSSPubTime
			latestPub = &value
		}
	}
	if latestSuccess == nil {
		return SubscriptionDiagnosticCheck{
			Key:     "rss_freshness",
			Label:   "最近检查",
			Status:  SubscriptionDiagnosticWarning,
			Summary: "尚未记录检查时间",
			Detail:  "可以从诊断面板触发一次 RSS 采集，确认是否能拿到最新条目。",
		}
	}

	age := time.Since(*latestSuccess)
	check := SubscriptionDiagnosticCheck{
		Key:   "rss_freshness",
		Label: "最近检查",
	}
	if age > 72*time.Hour {
		check.Status = SubscriptionDiagnosticWarning
		check.Summary = fmt.Sprintf("%s 未检查", formatDiagnosticDuration(age))
	} else if age > 24*time.Hour {
		check.Status = SubscriptionDiagnosticWarning
		check.Summary = fmt.Sprintf("%s 未检查", formatDiagnosticDuration(age))
	} else {
		check.Status = SubscriptionDiagnosticHealthy
		check.Summary = fmt.Sprintf("%s 前检查", formatDiagnosticDuration(age))
	}

	if latestPub != nil {
		check.Detail = fmt.Sprintf("最新 feed 水位线：%s", latestPub.Format("2006-01-02 15:04"))
	} else {
		check.Detail = "尚未记录 RSS 条目的发布时间水位线。"
	}
	return check
}

func (h *SubscriptionDiagnosticsHandler) buildEpisodeProgressCheck(missingEpisodes []int) SubscriptionDiagnosticCheck {
	if len(missingEpisodes) == 0 {
		return SubscriptionDiagnosticCheck{
			Key:     "episode_progress",
			Label:   "待收集集数",
			Status:  SubscriptionDiagnosticHealthy,
			Summary: "没有待收集集数",
			Detail:  "订阅当前集数不落后于已发现的最新集数；这不是磁盘文件完整性检查。",
		}
	}

	return SubscriptionDiagnosticCheck{
		Key:     "episode_progress",
		Label:   "待收集集数",
		Status:  SubscriptionDiagnosticWarning,
		Summary: fmt.Sprintf("待收集 %d 集", len(missingEpisodes)),
		Detail:  fmt.Sprintf("订阅进度差：%s；这不是磁盘文件完整性检查。", formatEpisodeList(missingEpisodes, 20)),
	}
}

func (h *SubscriptionDiagnosticsHandler) buildDownloadDiagnostics(downloads []model.Download) (SubscriptionDownloadDiagnostics, SubscriptionDiagnosticCheck) {
	summary := SubscriptionDownloadDiagnostics{
		Total:       len(downloads),
		FailedItems: make([]SubscriptionDownloadDiagnosticItem, 0),
	}

	for _, download := range downloads {
		switch download.Status {
		case model.DownloadStatusPending:
			summary.Pending++
		case model.DownloadStatusDownloading:
			summary.Downloading++
		case model.DownloadStatusStalled:
			summary.Stalled++
		case model.DownloadStatusFailed:
			summary.Failed++
		case model.DownloadStatusCompleted:
			summary.Completed++
		case model.DownloadStatusOrganizing:
			summary.Organizing++
		}

		diagnostics := buildDownloadDiagnostics(&download)
		if diagnostics.CanRetry && (download.Status == model.DownloadStatusFailed || download.Status == model.DownloadStatusStalled) {
			summary.Retryable++
		}
		if download.Status == model.DownloadStatusFailed || download.Status == model.DownloadStatusStalled {
			summary.FailedItems = append(summary.FailedItems, SubscriptionDownloadDiagnosticItem{
				ID:           download.ID,
				Title:        download.Title,
				Episode:      download.Episode,
				Status:       download.Status,
				Severity:     diagnostics.Severity,
				Category:     diagnostics.Category,
				Reason:       diagnostics.Detail,
				CanRetry:     diagnostics.CanRetry,
				RetryBlocked: diagnostics.RetryBlocked,
			})
		}
	}

	check := SubscriptionDiagnosticCheck{
		Key:   "downloads",
		Label: "下载任务",
	}
	switch {
	case summary.Failed > 0:
		check.Status = SubscriptionDiagnosticError
		check.Summary = fmt.Sprintf("%d 个任务失败", summary.Failed)
		check.Detail = "失败任务可以从诊断面板批量重试。"
	case summary.Stalled > 0:
		check.Status = SubscriptionDiagnosticWarning
		check.Summary = fmt.Sprintf("%d 个任务停滞", summary.Stalled)
		check.Detail = "停滞通常和种子活跃度、网络或保存路径有关。"
	case summary.Total == 0:
		check.Status = SubscriptionDiagnosticUnknown
		check.Summary = "暂无下载记录"
		check.Detail = "这个订阅还没有创建过下载任务。"
	default:
		check.Status = SubscriptionDiagnosticHealthy
		check.Summary = fmt.Sprintf("共 %d 个任务，%d 个已完成", summary.Total, summary.Completed)
		check.Detail = fmt.Sprintf("下载中 %d，待处理 %d。", summary.Downloading, summary.Pending)
	}
	return summary, check
}

func (h *SubscriptionDiagnosticsHandler) buildQBittorrentCheck(downloads []model.Download, summary *SubscriptionDownloadDiagnostics) SubscriptionDiagnosticCheck {
	check := SubscriptionDiagnosticCheck{
		Key:   "qbittorrent",
		Label: "qBittorrent",
	}
	if h.qbClient == nil {
		check.Status = SubscriptionDiagnosticUnknown
		check.Summary = "下载器客户端未初始化"
		check.Detail = "当前进程没有可用的 qBittorrent 客户端，无法确认下载器状态。"
		return check
	}

	torrents, err := h.qbClient.GetTorrentsByCategory("")
	if err != nil {
		check.Status = SubscriptionDiagnosticError
		check.Summary = "下载器不可用"
		check.Detail = err.Error()
		return check
	}

	torrentHashes := make(map[string]struct{}, len(torrents))
	for _, torrent := range torrents {
		if torrent == nil {
			continue
		}
		torrentHashes[strings.ToLower(strings.TrimSpace(torrent.Hash))] = struct{}{}
	}

	for _, download := range downloads {
		if download.TorrentHash == "" || !isActiveDownloadStatus(download.Status) {
			continue
		}
		if _, exists := torrentHashes[strings.ToLower(strings.TrimSpace(download.TorrentHash))]; !exists {
			summary.MissingTorrentTasks++
		}
	}

	if summary.MissingTorrentTasks > 0 {
		check.Status = SubscriptionDiagnosticWarning
		check.Summary = fmt.Sprintf("%d 个活跃任务未在 qBittorrent 中找到", summary.MissingTorrentTasks)
		check.Detail = "数据库记录存在，但下载器中缺少对应 hash，可以重试失败任务或重新采集。"
		return check
	}

	check.Status = SubscriptionDiagnosticHealthy
	check.Summary = "下载器连接正常"
	check.Detail = fmt.Sprintf("qBittorrent 当前共有 %d 个任务。", len(torrents))
	return check
}

func (h *SubscriptionDiagnosticsHandler) buildFileDiagnostics(subscription *model.Subscription, downloads []model.Download) (SubscriptionFileDiagnostics, SubscriptionDiagnosticCheck) {
	files := SubscriptionFileDiagnostics{
		RenameEnabled: subscription.RenameEnabled,
	}
	accumulateCompletedFileDiagnostics(&files, subscription, downloads)

	fileCheck := SubscriptionDiagnosticCheck{
		Key:   "files",
		Label: "已记录路径",
	}
	switch {
	case files.CompletedMissingFile > 0:
		fileCheck.Status = SubscriptionDiagnosticWarning
		fileCheck.Summary = fmt.Sprintf("%d 个已完成任务未记录路径", files.CompletedMissingFile)
		fileCheck.Detail = "数据库记录显示已完成，但没有 file_path 或 renamed_path。"
	case files.CompletedWithFile > 0:
		fileCheck.Status = SubscriptionDiagnosticHealthy
		fileCheck.Summary = fmt.Sprintf("%d 个已完成任务已记录路径", files.CompletedWithFile)
		fileCheck.Detail = "这里只核对数据库记录，不访问本地文件系统。"
	default:
		fileCheck.Status = SubscriptionDiagnosticUnknown
		fileCheck.Summary = "暂无可核对路径记录"
		fileCheck.Detail = "还没有已完成下载可用于核对路径记录。"
	}
	return files, fileCheck
}

func (h *SubscriptionDiagnosticsHandler) buildOrganizerDiagnostics(subscription *model.Subscription, downloads []model.Download) (SubscriptionFileDiagnostics, SubscriptionDiagnosticCheck) {
	files := SubscriptionFileDiagnostics{RenameEnabled: subscription.RenameEnabled}
	accumulateCompletedFileDiagnostics(&files, subscription, downloads)
	organizerCheck := SubscriptionDiagnosticCheck{
		Key:   "organizer",
		Label: "整理/重命名",
	}
	switch {
	case !subscription.RenameEnabled:
		organizerCheck.Status = SubscriptionDiagnosticUnknown
		organizerCheck.Summary = "重命名未启用"
		organizerCheck.Detail = "订阅配置关闭了重命名，诊断不会将未重命名视为异常。"
	case files.MissingRenamed > 0:
		organizerCheck.Status = SubscriptionDiagnosticWarning
		organizerCheck.Summary = fmt.Sprintf("%d 个已完成任务尚未记录重命名路径", files.MissingRenamed)
		organizerCheck.Detail = "可以触发重新整理或重命名来补齐目标路径。"
	case files.CompletedWithFile > 0:
		organizerCheck.Status = SubscriptionDiagnosticHealthy
		organizerCheck.Summary = "整理路径已记录"
		organizerCheck.Detail = "已完成任务具有数据库路径记录。"
	default:
		organizerCheck.Status = SubscriptionDiagnosticUnknown
		organizerCheck.Summary = "暂无整理记录"
		organizerCheck.Detail = "还没有已完成下载可用于判断整理状态。"
	}
	return files, organizerCheck
}

func accumulateCompletedFileDiagnostics(files *SubscriptionFileDiagnostics, subscription *model.Subscription, downloads []model.Download) {
	for _, download := range downloads {
		if download.Status != model.DownloadStatusCompleted {
			continue
		}
		if strings.TrimSpace(download.RenamedPath) == "" && subscription.RenameEnabled {
			files.MissingRenamed++
		}
		if downloadHasRecordedFilePath(download) {
			files.CompletedWithFile++
		} else {
			files.CompletedMissingFile++
		}
	}
}

func episodeProgressFilePatch(pending []int) *SubscriptionFileDiagnosticsPatch {
	return &SubscriptionFileDiagnosticsPatch{MissingEpisodes: &pending}
}

func fileDiagnosticsPatch(files SubscriptionFileDiagnostics) *SubscriptionFileDiagnosticsPatch {
	return &SubscriptionFileDiagnosticsPatch{
		RenameEnabled:        &files.RenameEnabled,
		CompletedWithFile:    &files.CompletedWithFile,
		CompletedMissingFile: &files.CompletedMissingFile,
	}
}

func organizerDiagnosticsPatch(files SubscriptionFileDiagnostics) *SubscriptionFileDiagnosticsPatch {
	return &SubscriptionFileDiagnosticsPatch{
		RenameEnabled:        &files.RenameEnabled,
		CompletedWithFile:    &files.CompletedWithFile,
		CompletedMissingFile: &files.CompletedMissingFile,
		MissingRenamed:       &files.MissingRenamed,
	}
}

func downloadDiagnosticsPatch(summary SubscriptionDownloadDiagnostics) *SubscriptionDownloadDiagnosticsPatch {
	return &SubscriptionDownloadDiagnosticsPatch{
		Total:       &summary.Total,
		Pending:     &summary.Pending,
		Downloading: &summary.Downloading,
		Stalled:     &summary.Stalled,
		Failed:      &summary.Failed,
		Completed:   &summary.Completed,
		Organizing:  &summary.Organizing,
		Retryable:   &summary.Retryable,
		FailedItems: &summary.FailedItems,
	}
}

func qbittorrentDiagnosticsPatch(missingTorrentTasks int) *SubscriptionDownloadDiagnosticsPatch {
	return &SubscriptionDownloadDiagnosticsPatch{MissingTorrentTasks: &missingTorrentTasks}
}

func (h *SubscriptionDiagnosticsHandler) buildActions(
	subscription *model.Subscription,
	downloads SubscriptionDownloadDiagnostics,
	downloadsChecked bool,
	hasEnabledFeeds bool,
) []SubscriptionDiagnosticAction {
	base := fmt.Sprintf("/api/v1/subscriptions/%d", subscription.ID)
	retryEnabled := downloadsChecked && downloads.Retryable > 0
	retryReason := "请先检查下载任务"
	if downloadsChecked {
		retryReason = disabledReason(retryEnabled, "没有可重试的失败或停滞任务")
	}
	reorganizeEnabled := downloadsChecked && downloads.Completed > 0
	reorganizeReason := "请先检查下载任务"
	if downloadsChecked {
		reorganizeReason = disabledReason(reorganizeEnabled, "没有已完成下载可整理")
	}
	renameEnabled := downloadsChecked && subscription.RenameEnabled && downloads.Completed > 0
	renameReason := "请先检查下载任务"
	if downloadsChecked {
		renameReason = disabledReason(renameEnabled, "重命名未启用或没有已完成下载")
	}
	return []SubscriptionDiagnosticAction{
		{
			Key:      "refresh_rss",
			Label:    "刷新 RSS",
			Method:   http.MethodPost,
			Endpoint: base + "/collect-episodes",
			Enabled:  hasEnabledFeeds,
			Reason:   disabledReason(hasEnabledFeeds, "未配置启用的订阅源"),
		},
		{
			Key:      "retry_failed",
			Label:    "重试失败任务",
			Method:   http.MethodPost,
			Endpoint: base + "/diagnostics/retry-failed",
			Enabled:  retryEnabled,
			Reason:   retryReason,
		},
		{
			Key:      "reorganize_files",
			Label:    "重新整理",
			Method:   http.MethodPost,
			Endpoint: base + "/reorganize-files",
			Enabled:  reorganizeEnabled,
			Reason:   reorganizeReason,
		},
		{
			Key:      "rename_files",
			Label:    "重新命名",
			Method:   http.MethodPost,
			Endpoint: base + "/rename-files",
			Enabled:  renameEnabled,
			Reason:   renameReason,
		},
		{
			Key:      "toggle_subscription",
			Label:    map[bool]string{true: "暂停订阅", false: "启用订阅"}[subscription.Enabled],
			Method:   http.MethodPost,
			Endpoint: base + "/toggle",
			Enabled:  true,
		},
	}
}

func (h *SubscriptionDiagnosticsHandler) retryDownload(subscription *model.Subscription, download *model.Download) error {
	var err error
	if h.requeueSvc != nil {
		err = h.requeueSvc.RequeueDownload(download, subscription)
	} else if download.Episode > 0 {
		err = errEpisodeRetryLifecycleUnavailable
	} else {
		resetDownloadForManualRetry(download)
		err = h.downloadRepo.Update(download)
	}
	if err != nil {
		return fmt.Errorf("重置下载任务失败: %w", err)
	}
	return nil
}

func initialSubscriptionDiagnosticChecks() []SubscriptionDiagnosticCheck {
	definitions := []struct {
		key   string
		label string
	}{
		{key: "subscription_enabled", label: "订阅状态"},
		{key: "rss_reachability", label: "RSS 可达性"},
		{key: "rss_freshness", label: "最近检查"},
		{key: "episode_progress", label: "待收集集数"},
		{key: "downloads", label: "下载任务"},
		{key: "qbittorrent", label: "qBittorrent"},
		{key: "files", label: "已记录路径"},
		{key: "organizer", label: "整理/重命名"},
	}

	checks := make([]SubscriptionDiagnosticCheck, 0, len(definitions))
	for _, definition := range definitions {
		checks = append(checks, SubscriptionDiagnosticCheck{
			Key:     definition.key,
			Label:   definition.label,
			Status:  SubscriptionDiagnosticUnknown,
			Summary: "未检查",
			Detail:  "",
		})
	}
	return checks
}

func summarizeSubscriptionChecks(checks []SubscriptionDiagnosticCheck) SubscriptionDiagnosticSummary {
	summary := SubscriptionDiagnosticSummary{
		Overall: string(SubscriptionDiagnosticUnknown),
		Total:   len(checks),
	}
	for _, check := range checks {
		if check.Checked {
			summary.Checked++
		} else {
			summary.Unknown++
			continue
		}
		switch check.Status {
		case SubscriptionDiagnosticHealthy:
			summary.Healthy++
		case SubscriptionDiagnosticWarning:
			summary.Warning++
		case SubscriptionDiagnosticError:
			summary.Error++
		}
	}
	if summary.Error > 0 {
		summary.Overall = string(SubscriptionDiagnosticError)
	} else if summary.Warning > 0 {
		summary.Overall = string(SubscriptionDiagnosticWarning)
	} else if summary.Healthy > 0 {
		summary.Overall = string(SubscriptionDiagnosticHealthy)
	}
	return summary
}

func computeMissingEpisodes(subscription *model.Subscription) []int {
	latest := subscription.RelativeLatestEpisode()
	if latest <= 0 {
		return []int{}
	}
	current := subscription.RelativeCurrentEpisode()
	if current >= latest {
		return []int{}
	}
	missing := make([]int, 0, latest-current)
	for episode := current + 1; episode <= latest; episode++ {
		missing = append(missing, episode)
	}
	return missing
}

func downloadSummaryForActions(downloads []model.Download) SubscriptionDownloadDiagnostics {
	summary := SubscriptionDownloadDiagnostics{}
	for _, download := range downloads {
		if download.Status == model.DownloadStatusCompleted {
			summary.Completed++
		}
		diagnostics := buildDownloadDiagnostics(&download)
		if diagnostics.CanRetry && (download.Status == model.DownloadStatusFailed || download.Status == model.DownloadStatusStalled) {
			summary.Retryable++
		}
	}
	return summary
}

func downloadHasRecordedFilePath(download model.Download) bool {
	return strings.TrimSpace(download.RenamedPath) != "" || strings.TrimSpace(download.FilePath) != ""
}

func isActiveDownloadStatus(status string) bool {
	switch status {
	case model.DownloadStatusPending, model.DownloadStatusDownloading, model.DownloadStatusStalled, model.DownloadStatusOrganizing:
		return true
	default:
		return false
	}
}

func disabledReason(enabled bool, reason string) string {
	if enabled {
		return ""
	}
	return reason
}

func formatDiagnosticDuration(duration time.Duration) string {
	if duration < time.Minute {
		return "刚刚"
	}
	if duration < time.Hour {
		return fmt.Sprintf("%d 分钟", int(duration.Minutes()))
	}
	if duration < 24*time.Hour {
		return fmt.Sprintf("%d 小时", int(duration.Hours()))
	}
	return fmt.Sprintf("%d 天", int(duration.Hours()/24))
}

func formatEpisodeList(episodes []int, limit int) string {
	if len(episodes) == 0 {
		return ""
	}
	if limit <= 0 || limit > len(episodes) {
		limit = len(episodes)
	}
	parts := make([]string, 0, limit+1)
	for _, episode := range episodes[:limit] {
		parts = append(parts, strconv.Itoa(episode))
	}
	if len(episodes) > limit {
		parts = append(parts, fmt.Sprintf("等 %d 集", len(episodes)))
	}
	return strings.Join(parts, ", ")
}
