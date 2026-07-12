package handler

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/constants"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/pkg/utils"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/disk"
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
	downloadPath     string
	rssHealthChecker *rss.RSSHealthChecker
	requeueSvc       DownloadRequeueService
}

type SubscriptionDiagnosticCheck struct {
	Key     string                       `json:"key"`
	Label   string                       `json:"label"`
	Status  SubscriptionDiagnosticStatus `json:"status"`
	Summary string                       `json:"summary"`
	Detail  string                       `json:"detail"`
}

type SubscriptionDiagnosticSummary struct {
	Overall string `json:"overall"`
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
	ExpectedPath         string `json:"expected_path"`
	FolderExists         bool   `json:"folder_exists"`
	RenameEnabled        bool   `json:"rename_enabled"`
	CompletedWithFile    int    `json:"completed_with_file"`
	CompletedMissingFile int    `json:"completed_missing_file"`
	MissingRenamed       int    `json:"missing_renamed"`
	MissingEpisodes      []int  `json:"missing_episodes"`
}

type SubscriptionDiskDiagnostics struct {
	Path                string  `json:"path"`
	Exists              bool    `json:"exists"`
	Status              string  `json:"status"`
	TotalBytes          int64   `json:"total_bytes"`
	FreeBytes           int64   `json:"free_bytes"`
	UsedBytes           int64   `json:"used_bytes"`
	UsagePercent        float64 `json:"usage_percent"`
	WarningThresholdGB  int64   `json:"warning_threshold_gb"`
	CriticalThresholdGB int64   `json:"critical_threshold_gb"`
	Error               string  `json:"error,omitempty"`
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
	Disk           SubscriptionDiskDiagnostics     `json:"disk"`
	Actions        []SubscriptionDiagnosticAction  `json:"actions"`
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
	downloadPath string,
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
		downloadPath:     downloadPath,
		rssHealthChecker: rss.NewHealthChecker(subscriptionRepo, feedRepo),
		requeueSvc:       requeue,
	}
}

func (h *SubscriptionDiagnosticsHandler) Get(c *gin.Context) {
	subscription, ok := h.loadSubscription(c)
	if !ok {
		return
	}

	downloads, err := h.downloadRepo.ListBySubscriptionID(subscription.ID)
	if err != nil {
		logger.Error("Failed to list downloads for subscription diagnostics",
			"subscription_id", subscription.ID,
			"error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取订阅下载记录失败",
		})
		return
	}
	feeds := make([]model.SubscriptionFeed, 0)
	if h.feedRepo != nil {
		feeds, err = h.feedRepo.ListBySubscription(subscription.ID)
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
	}
	enabledFeeds := make([]model.SubscriptionFeed, 0, len(feeds))
	for _, feed := range feeds {
		if feed.Enabled {
			enabledFeeds = append(enabledFeeds, feed)
		}
	}

	basePath := h.resolveBaseDownloadPath()
	expectedPath := utils.GenerateDownloadPath(basePath, subscription.Name)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 12*time.Second)
	defer cancel()

	downloadsSummary, downloadCheck := h.buildDownloadDiagnostics(downloads)
	filesSummary, fileChecks := h.buildFileDiagnostics(subscription, downloads, expectedPath)
	diskSummary, diskCheck := h.buildDiskDiagnostics(basePath)
	healthResult := h.rssHealthChecker.CheckSubscriptionFeeds(ctx, subscription, enabledFeeds)

	checks := []SubscriptionDiagnosticCheck{
		h.buildEnabledCheck(subscription),
		h.buildRSSReachabilityCheck(healthResult),
		h.buildRSSFreshnessCheck(enabledFeeds),
		h.buildMissingEpisodesCheck(filesSummary.MissingEpisodes),
		downloadCheck,
		h.buildQBittorrentCheck(downloads, &downloadsSummary),
	}
	checks = append(checks, fileChecks...)
	checks = append(checks, diskCheck)

	summary := summarizeSubscriptionChecks(checks)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data": SubscriptionDiagnosticsResponse{
			SubscriptionID: subscription.ID,
			Name:           subscription.Name,
			Enabled:        subscription.Enabled,
			CheckedAt:      time.Now(),
			Feeds:          healthResult.Feeds,
			Summary:        summary,
			Checks:         checks,
			Downloads:      downloadsSummary,
			Files:          filesSummary,
			Disk:           diskSummary,
			Actions:        h.buildActions(subscription, downloadsSummary, len(enabledFeeds) > 0),
		},
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

func (h *SubscriptionDiagnosticsHandler) buildMissingEpisodesCheck(missingEpisodes []int) SubscriptionDiagnosticCheck {
	if len(missingEpisodes) == 0 {
		return SubscriptionDiagnosticCheck{
			Key:     "missing_episodes",
			Label:   "缺失集数",
			Status:  SubscriptionDiagnosticHealthy,
			Summary: "未发现缺失集",
			Detail:  "当前已收集集数不落后于最新集数。",
		}
	}

	return SubscriptionDiagnosticCheck{
		Key:     "missing_episodes",
		Label:   "缺失集数",
		Status:  SubscriptionDiagnosticWarning,
		Summary: fmt.Sprintf("缺失 %d 集", len(missingEpisodes)),
		Detail:  fmt.Sprintf("缺失集数：%s", formatEpisodeList(missingEpisodes, 20)),
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

	version, err := h.qbClient.GetVersion()
	if err != nil {
		check.Status = SubscriptionDiagnosticError
		check.Summary = "下载器不可用"
		check.Detail = err.Error()
		return check
	}

	for _, download := range downloads {
		if download.TorrentHash == "" || !isActiveDownloadStatus(download.Status) {
			continue
		}
		if _, err := h.qbClient.GetTorrentInfo(download.TorrentHash); err != nil {
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
	check.Detail = fmt.Sprintf("qBittorrent 版本：%s", strings.TrimSpace(version))
	return check
}

func (h *SubscriptionDiagnosticsHandler) buildFileDiagnostics(subscription *model.Subscription, downloads []model.Download, expectedPath string) (SubscriptionFileDiagnostics, []SubscriptionDiagnosticCheck) {
	files := SubscriptionFileDiagnostics{
		ExpectedPath:    expectedPath,
		FolderExists:    pathIsDir(expectedPath),
		RenameEnabled:   subscription.RenameEnabled,
		MissingEpisodes: computeMissingEpisodes(subscription),
	}

	for _, download := range downloads {
		if download.Status != model.DownloadStatusCompleted {
			continue
		}
		if download.RenamedPath == "" && subscription.RenameEnabled {
			files.MissingRenamed++
		}
		if downloadHasRecordedFilePath(download) {
			files.CompletedWithFile++
		} else {
			files.CompletedMissingFile++
		}
	}

	fileCheck := SubscriptionDiagnosticCheck{
		Key:   "files",
		Label: "本地文件",
	}
	switch {
	case files.CompletedMissingFile > 0:
		fileCheck.Status = SubscriptionDiagnosticWarning
		fileCheck.Summary = fmt.Sprintf("%d 个已完成任务未记录文件路径", files.CompletedMissingFile)
		fileCheck.Detail = "数据库记录显示已完成，但没有 file_path 或 renamed_path；如需核验磁盘文件，请使用扫描本地文件。"
	case !files.FolderExists && len(downloads) > 0:
		fileCheck.Status = SubscriptionDiagnosticWarning
		fileCheck.Summary = "订阅目录不存在"
		fileCheck.Detail = fmt.Sprintf("预期目录：%s", expectedPath)
	case files.CompletedWithFile > 0:
		fileCheck.Status = SubscriptionDiagnosticHealthy
		fileCheck.Summary = fmt.Sprintf("%d 个已完成任务已记录文件路径", files.CompletedWithFile)
		fileCheck.Detail = fmt.Sprintf("预期目录：%s；如需核验磁盘文件，请使用扫描本地文件。", expectedPath)
	default:
		fileCheck.Status = SubscriptionDiagnosticUnknown
		fileCheck.Summary = "暂无可核对文件"
		fileCheck.Detail = fmt.Sprintf("预期目录：%s", expectedPath)
	}

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
		organizerCheck.Detail = "已完成任务具有数据库路径记录；磁盘存在性由扫描本地文件核验。"
	default:
		organizerCheck.Status = SubscriptionDiagnosticUnknown
		organizerCheck.Summary = "暂无整理记录"
		organizerCheck.Detail = "还没有已完成下载可用于判断整理状态。"
	}

	return files, []SubscriptionDiagnosticCheck{fileCheck, organizerCheck}
}

func (h *SubscriptionDiagnosticsHandler) buildDiskDiagnostics(path string) (SubscriptionDiskDiagnostics, SubscriptionDiagnosticCheck) {
	warningGB := h.getConfigInt64("disk.warning_threshold_gb", disk.DefaultWarningThresholdGB)
	criticalGB := h.getConfigInt64("disk.critical_threshold_gb", disk.DefaultCriticalThresholdGB)
	info := SubscriptionDiskDiagnostics{
		Path:                path,
		WarningThresholdGB:  warningGB,
		CriticalThresholdGB: criticalGB,
		Status:              disk.StatusHealthy,
	}
	check := SubscriptionDiagnosticCheck{
		Key:   "disk",
		Label: "磁盘空间",
	}

	if _, err := os.Stat(path); err != nil {
		info.Status = "missing"
		info.Error = err.Error()
		check.Status = SubscriptionDiagnosticWarning
		check.Summary = "下载根目录不存在"
		check.Detail = fmt.Sprintf("%s：%s", path, err.Error())
		return info, check
	}
	info.Exists = true

	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		info.Status = "error"
		info.Error = err.Error()
		check.Status = SubscriptionDiagnosticError
		check.Summary = "无法读取磁盘空间"
		check.Detail = err.Error()
		return info, check
	}

	blockSize := uint64(stat.Bsize)
	totalBytes := int64(blockSize * stat.Blocks)
	freeBytes := int64(blockSize * stat.Bavail)
	usedBytes := totalBytes - freeBytes
	freeGB := float64(freeBytes) / (1024 * 1024 * 1024)

	info.TotalBytes = totalBytes
	info.FreeBytes = freeBytes
	info.UsedBytes = usedBytes
	if totalBytes > 0 {
		info.UsagePercent = float64(usedBytes) / float64(totalBytes) * 100
	}

	switch {
	case freeGB < float64(criticalGB):
		info.Status = disk.StatusCritical
		check.Status = SubscriptionDiagnosticError
		check.Summary = fmt.Sprintf("剩余 %.1f GB，低于危险阈值", freeGB)
		check.Detail = fmt.Sprintf("危险阈值：%d GB，下载路径：%s", criticalGB, path)
	case freeGB < float64(warningGB):
		info.Status = disk.StatusWarning
		check.Status = SubscriptionDiagnosticWarning
		check.Summary = fmt.Sprintf("剩余 %.1f GB，低于警告阈值", freeGB)
		check.Detail = fmt.Sprintf("警告阈值：%d GB，下载路径：%s", warningGB, path)
	default:
		info.Status = disk.StatusHealthy
		check.Status = SubscriptionDiagnosticHealthy
		check.Summary = fmt.Sprintf("剩余 %.1f GB", freeGB)
		check.Detail = fmt.Sprintf("使用率 %.1f%%，下载路径：%s", info.UsagePercent, path)
	}

	return info, check
}

func (h *SubscriptionDiagnosticsHandler) buildActions(subscription *model.Subscription, downloads SubscriptionDownloadDiagnostics, hasEnabledFeeds bool) []SubscriptionDiagnosticAction {
	base := fmt.Sprintf("/api/v1/subscriptions/%d", subscription.ID)
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
			Enabled:  downloads.Retryable > 0,
			Reason:   disabledReason(downloads.Retryable > 0, "没有可重试的失败或停滞任务"),
		},
		{
			Key:      "scan_files",
			Label:    "扫描本地文件",
			Method:   http.MethodPost,
			Endpoint: base + "/scan-folder",
			Enabled:  true,
		},
		{
			Key:      "reorganize_files",
			Label:    "重新整理",
			Method:   http.MethodPost,
			Endpoint: base + "/reorganize-files",
			Enabled:  downloads.Completed > 0,
			Reason:   disabledReason(downloads.Completed > 0, "没有已完成下载可整理"),
		},
		{
			Key:      "rename_files",
			Label:    "重新命名",
			Method:   http.MethodPost,
			Endpoint: base + "/rename-files",
			Enabled:  subscription.RenameEnabled && downloads.Completed > 0,
			Reason:   disabledReason(subscription.RenameEnabled && downloads.Completed > 0, "重命名未启用或没有已完成下载"),
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

func (h *SubscriptionDiagnosticsHandler) resolveBaseDownloadPath() string {
	if h.configRepo != nil {
		if config, err := h.configRepo.Get("download_path"); err == nil && config != nil && strings.TrimSpace(config.Value) != "" {
			return config.Value
		}
	}
	if strings.TrimSpace(h.downloadPath) != "" {
		return h.downloadPath
	}
	return constants.DefaultDownloadPath
}

func (h *SubscriptionDiagnosticsHandler) getConfigInt64(key string, defaultValue int64) int64 {
	if h.configRepo == nil {
		return defaultValue
	}
	config, err := h.configRepo.Get(key)
	if err != nil || config == nil {
		return defaultValue
	}
	value, err := strconv.ParseInt(config.Value, 10, 64)
	if err != nil {
		return defaultValue
	}
	return value
}

func summarizeSubscriptionChecks(checks []SubscriptionDiagnosticCheck) SubscriptionDiagnosticSummary {
	summary := SubscriptionDiagnosticSummary{Overall: string(SubscriptionDiagnosticHealthy)}
	for _, check := range checks {
		switch check.Status {
		case SubscriptionDiagnosticHealthy:
			summary.Healthy++
		case SubscriptionDiagnosticWarning:
			summary.Warning++
		case SubscriptionDiagnosticError:
			summary.Error++
		case SubscriptionDiagnosticUnknown:
			summary.Unknown++
		}
	}
	if summary.Error > 0 {
		summary.Overall = string(SubscriptionDiagnosticError)
	} else if summary.Warning > 0 {
		summary.Overall = string(SubscriptionDiagnosticWarning)
	} else if summary.Healthy == 0 && summary.Unknown > 0 {
		summary.Overall = string(SubscriptionDiagnosticUnknown)
	}
	return summary
}

func computeMissingEpisodes(subscription *model.Subscription) []int {
	if subscription.LatestEpisode <= 0 {
		return []int{}
	}
	current := subscription.CurrentEpisode
	if current >= subscription.LatestEpisode {
		return []int{}
	}
	missing := make([]int, 0, subscription.LatestEpisode-current)
	for episode := current + 1; episode <= subscription.LatestEpisode; episode++ {
		missing = append(missing, episode)
	}
	return missing
}

func downloadHasRecordedFilePath(download model.Download) bool {
	return strings.TrimSpace(download.RenamedPath) != "" || strings.TrimSpace(download.FilePath) != ""
}

func pathIsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
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
