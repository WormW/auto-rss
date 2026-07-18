package mcpserver

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/WormW/auto-rss/internal/config"
	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/constants"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/pkg/utils"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/bangumi"
	"github.com/WormW/auto-rss/internal/service/calendar"
	"github.com/WormW/auto-rss/internal/service/disk"
	"github.com/WormW/auto-rss/internal/service/downloader"
	"github.com/WormW/auto-rss/internal/service/mikan"
	"github.com/WormW/auto-rss/internal/service/recovery"
	"github.com/WormW/auto-rss/internal/service/scheduler"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"
)

const recoveryPreviewSampleLimit = 10

type Dependencies struct {
	DB               *gorm.DB
	Config           *config.Config
	SubscriptionRepo repository.SubscriptionRepository
	DownloadRepo     repository.DownloadRepository
	ConfigRepo       repository.ConfigRepository
	RSSSourceRepo    repository.RSSSourceRepository
	LogRepo          repository.LogRepository
	Scheduler        scheduler.Scheduler
	QBClient         downloader.QBittorrentClient
}

type Server struct {
	cfg              *config.Config
	db               *gorm.DB
	subscriptionRepo repository.SubscriptionRepository
	downloadRepo     repository.DownloadRepository
	configRepo       repository.ConfigRepository
	rssSourceRepo    repository.RSSSourceRepository
	logRepo          repository.LogRepository
	scheduler        scheduler.Scheduler
	qbClient         downloader.QBittorrentClient
	mikanService     *mikan.MikanService
	bangumiService   *bangumi.BangumiService
	calendarService  *calendar.Calendar
	diskMonitor      *disk.Monitor
	mcpServer        *mcp.Server
	registeredTools  []registeredMCPTool
}

func New(deps Dependencies) *Server {
	s := &Server{
		cfg:              deps.Config,
		db:               deps.DB,
		subscriptionRepo: deps.SubscriptionRepo,
		downloadRepo:     deps.DownloadRepo,
		configRepo:       deps.ConfigRepo,
		rssSourceRepo:    deps.RSSSourceRepo,
		logRepo:          deps.LogRepo,
		scheduler:        deps.Scheduler,
		qbClient:         deps.QBClient,
		mikanService:     mikan.NewMikanService(""),
		bangumiService:   bangumi.NewBangumiService(),
		calendarService:  calendar.NewCalendar(deps.SubscriptionRepo, deps.DownloadRepo),
		diskMonitor:      disk.NewMonitor(deps.DB, deps.DownloadRepo, deps.SubscriptionRepo, deps.ConfigRepo),
	}

	s.mcpServer = mcp.NewServer(&mcp.Implementation{
		Name:    "auto-rss",
		Version: "v0.1.0",
	}, nil)
	s.registerTools()
	return s
}

func (s *Server) Handler() http.Handler {
	handler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		return s.mcpServer
	}, &mcp.StreamableHTTPOptions{
		JSONResponse:   true,
		Stateless:      true,
		SessionTimeout: 30 * time.Minute,
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="auto-rss-mcp"`)
			http.Error(w, "missing or invalid MCP bearer token", http.StatusUnauthorized)
			return
		}
		if !s.allowedOrigin(r) {
			http.Error(w, "origin is not allowed for MCP", http.StatusForbidden)
			return
		}
		s.writeCORSHeaders(w, r)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		handler.ServeHTTP(w, r)
	})
}

func (s *Server) registerTools() {
	addTool(s, "get_system_overview", "Get a compact operational overview of Auto-RSS: subscription counts, download status counts, RSS source counts, and disk state. Use this first when the user asks what needs attention or whether the system is healthy. This is read-only and avoids exposing secret configuration values.", true, s.getSystemOverview)
	addTool(s, "list_subscriptions", "List Auto-RSS subscriptions with cursor pagination and optional filters. Use this to find subscription IDs before toggling or inspecting a subscription. Do not use it for raw download history; call list_downloads for that.", true, s.listSubscriptions)
	addTool(s, "get_subscription", "Get one subscription plus its recent downloads. Use this when you need detailed status, episode progress, calendar fields, Bangumi metadata, or recent failures for a known subscription ID. This is read-only.", true, s.getSubscription)
	addTool(s, "create_subscription", "Create a new anime RSS subscription. Use this after you have a concrete RSS feed URL, usually from get_mikan_fansubs or a user-provided feed. Use search_bangumi first when you need a Bangumi subject ID or episode metadata; do not call this just to preview search results.", false, s.createSubscription)
	addTool(s, "toggle_subscription", "Enable, disable, or toggle a subscription. Use this when the user explicitly wants Auto-RSS to start or stop tracking a subscription. This changes database state but does not delete downloads.", false, s.toggleSubscription)
	addTool(s, "list_downloads", "List download records with cursor pagination and optional status or subscription filters. Use this to diagnose failed, stalled, pending, or completed downloads. It returns decision-ready fields and does not include torrent URLs.", true, s.listDownloads)
	addTool(s, "get_download", "Get one download record by ID. Use this before retrying a download or when a user asks about one specific failed or stalled item. This is read-only.", true, s.getDownload)
	addTool(s, "retry_download", "Reset a failed or stalled download and ask qBittorrent to add the torrent again when configured. Use only when the user explicitly asks to retry or repair a download. The tool returns an actionable error if qBittorrent cannot accept the torrent.", false, s.retryDownload)
	addTool(s, "refresh_rss", "Trigger an asynchronous RSS check now. Use this when the user asks Auto-RSS to scan feeds immediately. It returns once the check has been queued; inspect logs or downloads afterwards for results.", false, s.refreshRSS)
	addTool(s, "preview_recovery_scan", "Preview recovery candidates by running the recovery scanner in dry-run mode. This is read-only, accepts an optional subscription_id, never applies fixes, and returns bounded counts plus concise samples instead of full file dumps.", true, s.previewRecoveryScan)
	addTool(s, "search_mikan", "Search Mikan for anime by title. Use this to discover candidate anime pages before fetching fansub RSS feeds. Do not create subscriptions from search results until you have selected a fansub RSS URL.", true, s.searchMikan)
	addTool(s, "get_mikan_season", "List Mikan anime for a specific year and season. Use this for seasonal discovery, then call get_mikan_fansubs on an anime URL to get concrete RSS feeds.", true, s.getMikanSeason)
	addTool(s, "get_mikan_fansubs", "Get fansub groups and RSS feed URLs for one Mikan anime page. Use this after search_mikan or get_mikan_season when choosing which RSS feed to subscribe to.", true, s.getMikanFansubs)
	addTool(s, "search_bangumi", "Search Bangumi anime metadata by title, or return the best match only. Use this to identify subject IDs, total episodes, air dates, scores, and names before creating or enriching subscriptions. This calls an external API.", true, s.searchBangumi)
	addTool(s, "get_bangumi_subject", "Get detailed Bangumi metadata for a known subject ID. Use this when you need a subject's canonical names, summary, score, rank, air date, or episode count.", true, s.getBangumiSubject)
	addTool(s, "get_calendar", "Get Auto-RSS airing calendar data. Use today_only for today's expected next episodes, or week_offset for a week view. This is read-only and based on subscription calendar fields.", true, s.getCalendar)
	addTool(s, "get_disk_status", "Get disk usage for the configured download path and threshold status. Use this before advising cleanup or diagnosing paused downloads due to low space. This is read-only.", true, s.getDiskStatus)
	addTool(s, "list_logs", "List recent Auto-RSS logs with cursor pagination and optional level/module filters. Use this to explain failures after refresh_rss, retry_download, or qBittorrent connectivity issues. This is read-only.", true, s.listLogs)
}

type registeredMCPTool struct {
	Tool         mcp.Tool
	InputFields  []string
	OutputFields []string
}

func (s *Server) registeredMCPTools() []registeredMCPTool {
	tools := make([]registeredMCPTool, len(s.registeredTools))
	copy(tools, s.registeredTools)
	return tools
}

func addTool[In, Out any](s *Server, name, description string, readOnly bool, handler mcp.ToolHandlerFor[In, Out]) {
	definition := registeredToolDefinition[In, Out](name, description, readOnly)
	tool := definition.Tool
	mcp.AddTool(s.mcpServer, &tool, handler)
	s.registeredTools = append(s.registeredTools, definition)
}

func registeredToolDefinition[In, Out any](name, description string, readOnly bool) registeredMCPTool {
	openWorld := true
	destructive := !readOnly
	return registeredMCPTool{
		Tool: mcp.Tool{
			Name:        name,
			Title:       strings.ReplaceAll(name, "_", " "),
			Description: description,
			Annotations: &mcp.ToolAnnotations{
				ReadOnlyHint:    readOnly,
				DestructiveHint: &destructive,
				OpenWorldHint:   &openWorld,
			},
		},
		InputFields:  jsonFieldNames[In](),
		OutputFields: jsonFieldNames[Out](),
	}
}

func jsonFieldNames[T any]() []string {
	typ := reflect.TypeFor[T]()
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return nil
	}

	fields := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		fields = append(fields, name)
	}
	return fields
}

func (s *Server) authorized(r *http.Request) bool {
	if s.cfg == nil || s.cfg.MCPToken == "" {
		return false
	}
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return false
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.MCPToken)) == 1
}

func (s *Server) allowedOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	for _, allowed := range s.cfg.MCPAllowedOrigins {
		if allowed == origin {
			return true
		}
	}
	return false
}

func (s *Server) writeCORSHeaders(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, MCP-Protocol-Version, Mcp-Session-Id")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Vary", "Origin")
}

func (s *Server) setProxy() {
	if s.configRepo == nil {
		return
	}
	proxyConfig, err := s.configRepo.Get("system_proxy")
	if err != nil || proxyConfig == nil || proxyConfig.Value == "" {
		return
	}
	_ = s.mikanService.SetProxy(proxyConfig.Value)
	_ = s.bangumiService.SetProxy(proxyConfig.Value)
}

func resultWithText[Out any](out Out) (*mcp.CallToolResult, Out, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: toJSONText(out)}},
	}, out, nil
}

func (s *Server) getSystemOverview(ctx context.Context, req *mcp.CallToolRequest, input struct{}) (*mcp.CallToolResult, SystemOverview, error) {
	var overview SystemOverview

	subscriptions, total, err := s.subscriptionRepo.List(0, repository.MaxPageSize)
	if err != nil {
		return nil, overview, fmt.Errorf("failed to list subscriptions: %w", err)
	}
	overview.Subscriptions.Total = total
	for _, sub := range subscriptions {
		if sub.Status == "active" {
			overview.Subscriptions.Active++
		}
		if sub.Status == "paused" {
			overview.Subscriptions.Paused++
		}
		if !sub.Enabled {
			overview.Subscriptions.Disabled++
		}
	}

	downloads, downloadTotal, err := s.downloadRepo.List(0, repository.MaxPageSize, "")
	if err != nil {
		return nil, overview, fmt.Errorf("failed to list downloads: %w", err)
	}
	overview.Downloads.Total = downloadTotal
	overview.Downloads.ByStatus = make(map[string]int)
	for _, download := range downloads {
		overview.Downloads.ByStatus[download.Status]++
		if download.Status == model.DownloadStatusFailed && download.RetryCount < download.MaxRetries {
			overview.Downloads.FailedReady++
		}
	}

	sources, sourceTotal, err := s.rssSourceRepo.List(1, repository.MaxPageSize, nil)
	if err != nil {
		return nil, overview, fmt.Errorf("failed to list RSS sources: %w", err)
	}
	overview.RSSSources.Total = sourceTotal
	for _, source := range sources {
		if source.Enabled {
			overview.RSSSources.Enabled++
		}
	}

	diskStatus, err := s.currentDiskStatus()
	if err == nil {
		overview.Disk = diskStatus
	} else {
		logger.Warn("MCP overview could not read disk status", "error", err.Error())
	}

	return resultWithText(overview)
}

func (s *Server) listSubscriptions(ctx context.Context, req *mcp.CallToolRequest, input ListSubscriptionsInput) (*mcp.CallToolResult, ListSubscriptionsOutput, error) {
	offset, err := decodeCursor(input.Cursor)
	if err != nil {
		return nil, ListSubscriptionsOutput{}, err
	}
	limit := normalizeLimit(input.Limit)

	var items []model.Subscription
	var total int64
	query := strings.ToLower(strings.TrimSpace(input.Query))

	dbQuery := s.db.Model(&model.Subscription{})
	if input.Status != "" {
		dbQuery = dbQuery.Where("status = ?", input.Status)
	}
	if input.Enabled != nil {
		dbQuery = dbQuery.Where("enabled = ?", *input.Enabled)
	}
	if query != "" {
		like := "%" + query + "%"
		dbQuery = dbQuery.Where("LOWER(name) LIKE ? OR LOWER(fansub) LIKE ? OR LOWER(rss_url) LIKE ?", like, like, like)
	}

	if err := dbQuery.Count(&total).Error; err != nil {
		return nil, ListSubscriptionsOutput{}, fmt.Errorf("failed to count subscriptions: %w", err)
	}
	if err := dbQuery.Order("updated_at DESC").Offset(offset).Limit(limit).Find(&items).Error; err != nil {
		return nil, ListSubscriptionsOutput{}, fmt.Errorf("failed to list subscriptions: %w", err)
	}

	summaries := make([]SubscriptionSummary, 0, len(items))
	for _, item := range items {
		summaries = append(summaries, summarizeSubscription(item))
	}

	out := ListSubscriptionsOutput{
		Items: summaries,
		PageInfo: PageInfo{
			Total:      total,
			Limit:      limit,
			NextCursor: nextCursor(offset, len(items), total),
		},
	}
	return resultWithText(out)
}

func (s *Server) getSubscription(ctx context.Context, req *mcp.CallToolRequest, input GetSubscriptionInput) (*mcp.CallToolResult, GetSubscriptionOutput, error) {
	sub, err := s.subscriptionRepo.GetByID(input.ID)
	if err != nil {
		return nil, GetSubscriptionOutput{}, fmt.Errorf("subscription %d was not found", input.ID)
	}

	recent, err := s.downloadRepo.GetRecentBySubscription(input.ID, 10)
	if err != nil {
		return nil, GetSubscriptionOutput{}, fmt.Errorf("failed to get recent downloads for subscription %d: %w", input.ID, err)
	}

	out := GetSubscriptionOutput{
		Subscription:    summarizeSubscription(*sub),
		RecentDownloads: make([]DownloadSummary, 0, len(recent)),
	}
	for _, download := range recent {
		out.RecentDownloads = append(out.RecentDownloads, summarizeDownload(download))
	}
	return resultWithText(out)
}

func (s *Server) createSubscription(ctx context.Context, req *mcp.CallToolRequest, input CreateSubscriptionInput) (*mcp.CallToolResult, CreateSubscriptionOutput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.RSSURL = strings.TrimSpace(input.RSSURL)
	if input.Name == "" {
		return nil, CreateSubscriptionOutput{}, fmt.Errorf("name is required")
	}
	if input.RSSURL == "" {
		return nil, CreateSubscriptionOutput{}, fmt.Errorf("rss_url is required")
	}

	if existing, err := s.subscriptionRepo.GetByRSSURL(input.RSSURL); err == nil && existing != nil {
		out := CreateSubscriptionOutput{Subscription: summarizeSubscription(*existing)}
		return resultWithText(out)
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, CreateSubscriptionOutput{}, fmt.Errorf("failed to check existing subscription: %w", err)
	}

	season := input.Season
	if season <= 0 {
		season = 1
	}
	renameEnabled := true
	if input.RenameEnabled != nil {
		renameEnabled = *input.RenameEnabled
	}
	languagePreference := strings.TrimSpace(input.LanguagePreference)
	if languagePreference == "" {
		languagePreference = "auto"
	}

	sub := &model.Subscription{
		Name:               input.Name,
		RssURL:             input.RSSURL,
		Season:             season,
		Status:             "active",
		Enabled:            true,
		Fansub:             strings.TrimSpace(input.Fansub),
		LanguagePreference: languagePreference,
		TotalEpisodes:      input.TotalEpisodes,
		BangumiID:          input.BangumiID,
		RenameEnabled:      renameEnabled,
	}
	if err := s.subscriptionRepo.Create(sub); err != nil {
		return nil, CreateSubscriptionOutput{}, fmt.Errorf("failed to create subscription: %w", err)
	}

	out := CreateSubscriptionOutput{Subscription: summarizeSubscription(*sub)}
	return resultWithText(out)
}

func (s *Server) toggleSubscription(ctx context.Context, req *mcp.CallToolRequest, input ToggleSubscriptionInput) (*mcp.CallToolResult, ToggleSubscriptionOutput, error) {
	sub, err := s.subscriptionRepo.GetByID(input.ID)
	if err != nil {
		return nil, ToggleSubscriptionOutput{}, fmt.Errorf("subscription %d was not found", input.ID)
	}

	if input.Enabled == nil {
		sub.Enabled = !sub.Enabled
	} else {
		sub.Enabled = *input.Enabled
	}
	if sub.Enabled && sub.Status == "paused" {
		sub.Status = "active"
	}
	if !sub.Enabled && sub.Status == "active" {
		sub.Status = "paused"
	}

	if err := s.subscriptionRepo.Update(sub); err != nil {
		return nil, ToggleSubscriptionOutput{}, fmt.Errorf("failed to update subscription %d: %w", input.ID, err)
	}

	out := ToggleSubscriptionOutput{Subscription: summarizeSubscription(*sub)}
	return resultWithText(out)
}

func (s *Server) listDownloads(ctx context.Context, req *mcp.CallToolRequest, input ListDownloadsInput) (*mcp.CallToolResult, ListDownloadsOutput, error) {
	offset, err := decodeCursor(input.Cursor)
	if err != nil {
		return nil, ListDownloadsOutput{}, err
	}
	limit := normalizeLimit(input.Limit)

	if input.SubscriptionID != 0 {
		var downloads []model.Download
		var total int64
		dbQuery := s.db.Model(&model.Download{}).Where("subscription_id = ?", input.SubscriptionID)
		if input.Status != "" {
			dbQuery = dbQuery.Where("status = ?", input.Status)
		}
		if err := dbQuery.Count(&total).Error; err != nil {
			return nil, ListDownloadsOutput{}, fmt.Errorf("failed to count downloads for subscription %d: %w", input.SubscriptionID, err)
		}
		if err := dbQuery.Preload("Subscription").Order("created_at DESC").Offset(offset).Limit(limit).Find(&downloads).Error; err != nil {
			return nil, ListDownloadsOutput{}, fmt.Errorf("failed to list downloads for subscription %d: %w", input.SubscriptionID, err)
		}
		page := make([]DownloadSummary, 0, len(downloads))
		for _, download := range downloads {
			page = append(page, summarizeDownload(download))
		}
		out := ListDownloadsOutput{
			Items: page,
			PageInfo: PageInfo{
				Total:      total,
				Limit:      limit,
				NextCursor: nextCursor(offset, len(page), total),
			},
		}
		return resultWithText(out)
	}

	downloads, total, err := s.downloadRepo.List(offset, limit, input.Status)
	if err != nil {
		return nil, ListDownloadsOutput{}, fmt.Errorf("failed to list downloads: %w", err)
	}
	out := ListDownloadsOutput{
		Items: make([]DownloadSummary, 0, len(downloads)),
		PageInfo: PageInfo{
			Total:      total,
			Limit:      limit,
			NextCursor: nextCursor(offset, len(downloads), total),
		},
	}
	for _, download := range downloads {
		out.Items = append(out.Items, summarizeDownload(download))
	}
	return resultWithText(out)
}

func (s *Server) getDownload(ctx context.Context, req *mcp.CallToolRequest, input GetDownloadInput) (*mcp.CallToolResult, GetDownloadOutput, error) {
	download, err := s.downloadRepo.GetByID(input.ID)
	if err != nil {
		return nil, GetDownloadOutput{}, fmt.Errorf("download %d was not found", input.ID)
	}
	out := GetDownloadOutput{Download: summarizeDownload(*download)}
	return resultWithText(out)
}

func (s *Server) retryDownload(ctx context.Context, req *mcp.CallToolRequest, input RetryDownloadInput) (*mcp.CallToolResult, RetryDownloadOutput, error) {
	download, err := s.downloadRepo.GetByID(input.ID)
	if err != nil {
		return nil, RetryDownloadOutput{}, fmt.Errorf("download %d was not found", input.ID)
	}

	if err := validateRetryDownloadPreconditions(download); err != nil {
		return nil, RetryDownloadOutput{}, err
	}

	if download.TorrentHash != "" && s.qbClient != nil {
		if err := s.qbClient.RemoveTorrentTask(download.TorrentHash); err != nil {
			logger.Warn("MCP retry could not remove old qBittorrent task", "download_id", input.ID, "hash", download.TorrentHash, "error", err.Error())
		}
	}

	download.RetryCount = 0
	download.RetryReason = "mcp_retry"
	download.NextRetryAt = nil
	download.LastError = ""
	download.ErrorMessage = ""
	download.Status = model.DownloadStatusPending
	download.TorrentHash = ""

	if s.qbClient != nil {
		basePath := constants.DefaultDownloadPath
		if s.configRepo != nil {
			if cfg, err := s.configRepo.Get("download_path"); err == nil && cfg != nil && cfg.Value != "" {
				basePath = cfg.Value
			}
		}
		downloadPath := basePath
		if download.Subscription.Name != "" {
			downloadPath = utils.GenerateDownloadPath(basePath, download.Subscription.Name)
		}
		torrentHash, err := s.qbClient.AddTorrent(download.TorrentURL, downloadPath, "")
		if err != nil {
			download.Status = model.DownloadStatusFailed
			download.LastError = err.Error()
			_ = s.downloadRepo.Update(download)
			return nil, RetryDownloadOutput{}, fmt.Errorf("qBittorrent could not add the torrent for download %d: %w", input.ID, err)
		}
		download.Status = model.DownloadStatusDownloading
		download.TorrentHash = torrentHash
	}

	if err := s.downloadRepo.Update(download); err != nil {
		return nil, RetryDownloadOutput{}, fmt.Errorf("failed to persist retry state for download %d: %w", input.ID, err)
	}

	out := RetryDownloadOutput{
		Download: summarizeDownload(*download),
		Message:  "download retry was queued",
	}
	return resultWithText(out)
}

func validateRetryDownloadPreconditions(download *model.Download) error {
	if download.Status != model.DownloadStatusFailed && download.Status != model.DownloadStatusStalled {
		return fmt.Errorf("download %d cannot be retried while status is %q; only failed or stalled downloads can be retried", download.ID, download.Status)
	}
	if strings.TrimSpace(download.TorrentURL) == "" {
		return fmt.Errorf("download %d cannot be retried because it has no torrent URL", download.ID)
	}
	return nil
}

func (s *Server) refreshRSS(ctx context.Context, req *mcp.CallToolRequest, input RefreshRSSInput) (*mcp.CallToolResult, RefreshRSSOutput, error) {
	if s.scheduler == nil {
		return nil, RefreshRSSOutput{}, fmt.Errorf("RSS scheduler is not available")
	}
	if err := s.scheduler.RunRSSCheckNow(); err != nil {
		return nil, RefreshRSSOutput{}, fmt.Errorf("failed to trigger RSS refresh: %w", err)
	}
	return resultWithText(RefreshRSSOutput{Message: "RSS refresh queued"})
}

func (s *Server) previewRecoveryScan(ctx context.Context, req *mcp.CallToolRequest, input PreviewRecoveryInput) (*mcp.CallToolResult, RecoveryPreviewOutput, error) {
	var subscriptionID *uint
	if input.SubscriptionID != 0 {
		subscriptionID = &input.SubscriptionID
	}

	scanner := recovery.NewScanner(s.db, s.subscriptionRepo, s.downloadRepo, s.configRepo, s.bangumiService, configuredDownloadPath(s.cfg))
	result, err := scanner.Scan(&recovery.ScanRequest{
		DryRun:         true,
		SubscriptionID: subscriptionID,
	})
	if err != nil {
		return nil, RecoveryPreviewOutput{}, fmt.Errorf("failed to preview recovery scan: %w", err)
	}

	return resultWithText(summarizeRecoveryPreview(result))
}

func configuredDownloadPath(cfg *config.Config) string {
	if cfg != nil && strings.TrimSpace(cfg.DownloadPath) != "" {
		return cfg.DownloadPath
	}
	return constants.DefaultDownloadPath
}

func summarizeRecoveryPreview(result *recovery.ScanResult) RecoveryPreviewOutput {
	out := RecoveryPreviewOutput{
		DryRun:                 true,
		PreviewOnly:            true,
		Applied:                result.Applied,
		ScannedFiles:           result.ScannedFiles,
		MatchedFiles:           result.MatchedFiles,
		OrphanFileCount:        len(result.OrphanFiles),
		OrphanFileSamples:      limitStrings(result.OrphanFiles, recoveryPreviewSampleLimit),
		OrphanFileOmittedCount: omittedCount(len(result.OrphanFiles), recoveryPreviewSampleLimit),
		SubscriptionCount:      len(result.Subscriptions),
		Subscriptions:          make([]RecoverySubscriptionPreview, 0, len(result.Subscriptions)),
	}

	for _, sub := range result.Subscriptions {
		preview := RecoverySubscriptionPreview{
			SubscriptionID:         sub.SubscriptionID,
			Name:                   sub.Name,
			CurrentEpisodeOld:      sub.CurrentEpisodeOld,
			CurrentEpisodeNew:      sub.CurrentEpisodeNew,
			LatestEpisodeOld:       sub.LatestEpisodeOld,
			LatestEpisodeNew:       sub.LatestEpisodeNew,
			EpisodesOnDiskCount:    len(sub.EpisodesOnDisk),
			EpisodeSamples:         limitInts(sub.EpisodesOnDisk, recoveryPreviewSampleLimit),
			EpisodeOmittedCount:    omittedCount(len(sub.EpisodesOnDisk), recoveryPreviewSampleLimit),
			MatchedFileCount:       len(sub.MatchedEpisodes),
			DownloadsToUpdateCount: len(sub.DownloadsToUpdate),
			DownloadsToUpdateIDs:   limitUints(sub.DownloadsToUpdate, recoveryPreviewSampleLimit),
			DownloadsToCreateCount: len(sub.DownloadsToCreate),
			DownloadsToCreate:      limitInts(sub.DownloadsToCreate, recoveryPreviewSampleLimit),
			DownloadsMissingCount:  len(sub.DownloadsMissing),
			DownloadsMissingIDs:    limitUints(sub.DownloadsMissing, recoveryPreviewSampleLimit),
		}

		out.DownloadsToUpdateCount += preview.DownloadsToUpdateCount
		out.DownloadsToCreateCount += preview.DownloadsToCreateCount
		out.DownloadsMissingCount += preview.DownloadsMissingCount
		out.Subscriptions = append(out.Subscriptions, preview)
	}

	return out
}

func limitStrings(items []string, limit int) []string {
	if len(items) == 0 {
		return nil
	}
	if len(items) > limit {
		return append([]string(nil), items[:limit]...)
	}
	return append([]string(nil), items...)
}

func limitInts(items []int, limit int) []int {
	if len(items) == 0 {
		return nil
	}
	if len(items) > limit {
		return append([]int(nil), items[:limit]...)
	}
	return append([]int(nil), items...)
}

func limitUints(items []uint, limit int) []uint {
	if len(items) == 0 {
		return nil
	}
	if len(items) > limit {
		return append([]uint(nil), items[:limit]...)
	}
	return append([]uint(nil), items...)
}

func omittedCount(length, limit int) int {
	if length <= limit {
		return 0
	}
	return length - limit
}
