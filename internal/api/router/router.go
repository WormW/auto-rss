package router

import (
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/WormW/auto-rss/internal/api/handler"
	"github.com/WormW/auto-rss/internal/api/middleware"
	"github.com/WormW/auto-rss/internal/api/middleware/ratelimit"
	"github.com/WormW/auto-rss/internal/app"
	"github.com/WormW/auto-rss/internal/config"
	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/pkg/utils"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/auth"
	"github.com/WormW/auto-rss/internal/service/backup"
	"github.com/WormW/auto-rss/internal/service/bangumi"
	"github.com/WormW/auto-rss/internal/service/calendar"
	"github.com/WormW/auto-rss/internal/service/disk"
	"github.com/WormW/auto-rss/internal/service/downloader"
	"github.com/WormW/auto-rss/internal/service/notification"
	"github.com/WormW/auto-rss/internal/service/rss"
	"github.com/WormW/auto-rss/internal/service/scheduler"
	"github.com/WormW/auto-rss/internal/webui"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var newScheduler = func(db *gorm.DB, subscriptionRepo repository.SubscriptionRepository, downloadRepo repository.DownloadRepository, configRepo repository.ConfigRepository, rssInterval string, rssParser rss.Parser, qbClient downloader.QBittorrentClient) scheduler.Scheduler {
	return scheduler.NewScheduler(db, subscriptionRepo, downloadRepo, configRepo, rssInterval, rssParser, qbClient)
}

// Setup 设置路由
func Setup(db *gorm.DB, cfg *config.Config, qbClient downloader.QBittorrentClient, appCtx *app.Context, renameTemplate string) (*gin.Engine, error) {
	r := gin.New()

	// 应用中间件
	// 初始化指标收集器
	handler.InitMetrics()
	metricsCollector := handler.GetDefaultCollector()

	// 应用指标中间件
	r.Use(handler.MetricsMiddleware(metricsCollector))

	// 错误处理和恢复中间件（必须在最前面捕获所有错误）
	r.Use(middleware.RecoveryWithResponse())
	r.Use(middleware.ErrorHandler())
	r.Use(middleware.Logger())
	r.Use(middleware.CORS())

	// 初始化限流器存储
	rateLimitStore := ratelimit.NewStore(
		cfg.RateLimit.MaxEntries, // Default: 10000
		cfg.RateLimit.TTL,        // Default: 1 hour
		cfg.RateLimit.RPS,        // Default: 10.0
		cfg.RateLimit.Burst,      // Default: 20
	)

	// 启动后台清理
	cleanupManager := ratelimit.NewCleanupManager(rateLimitStore, 5*time.Minute)
	cleanupManager.Start()
	appCtx.RegisterShutdownHook(cleanupManager.Stop)

	// 应用限流中间件（使用配置值）
	r.Use(middleware.RateLimitWithConfig(middleware.RateLimitConfig{
		Store:         rateLimitStore,
		GeneralRPS:    cfg.RateLimit.RPS,
		GeneralBurst:  cfg.RateLimit.Burst,
		AuthRPM:       cfg.RateLimit.AuthRPM,
		AuthBurst:     5, // 固定突发值
		AuthPaths:     []string{"/api/v1/auth/login", "/api/v1/auth/refresh"},
		ExcludedPaths: []string{"/health", "/api/v1/health"},
	}))

	// 静态文件服务 - 封面图片（带 fallback）
	coverPath := utils.GetCoverPath()
	r.GET("/covers/*filepath", coverHandler(coverPath, db))

	// 初始化服务
	rssParser := rss.NewParser()

	// 初始化仓储
	subscriptionRepo := repository.NewSubscriptionRepository(db)
	downloadRepo := repository.NewDownloadRepository(db)
	configRepo := repository.NewConfigRepository(db)
	rssSourceRepo := repository.NewRSSSourceRepository(db)
	logRepo := repository.NewLogRepository(db)

	// 初始化调度器
	rssScheduler := newScheduler(db, subscriptionRepo, downloadRepo, configRepo, cfg.RSSInterval, rssParser, qbClient)

	// 初始化通知服务
	notificationSvc := notification.NewService(db)
	wsHub := notificationSvc.GetWebSocketHub()

	// 初始化日历服务
	calendarSvc := calendar.NewCalendar(subscriptionRepo, downloadRepo)
	calendarSvc.SetNotificationService(notificationSvc)

	// 初始化磁盘监控服务
	diskMonitor := disk.NewMonitor(db, downloadRepo, subscriptionRepo, configRepo)
	_ = diskMonitor.LoadConfig()
	diskMonitor.SetNotificationService(notificationSvc)
	diskMonitor.Start()
	appCtx.RegisterShutdownHook(diskMonitor.Stop)

	// 初始化下载监控服务（在 handler 之前，因为某些 handler 可能需要它）
	downloadMonitor := downloader.NewDownloadMonitor(db, qbClient, downloadRepo, subscriptionRepo, configRepo, renameTemplate)
	downloadMonitor.SetNotificationService(notificationSvc)
	downloadMonitor.Start(30 * time.Second)
	appCtx.RegisterShutdownHook(downloadMonitor.Stop)

	// 初始化 JWT 服务
	refreshTokenRepo := repository.NewRefreshTokenRepository(db)
	jwtService := auth.NewJWTService(cfg, refreshTokenRepo)

	// 初始化处理器
	subscriptionHandler := handler.NewSubscriptionHandler(subscriptionRepo, downloadRepo, configRepo, qbClient, cfg.DownloadPath)
	subscriptionDiagnosticsHandler := handler.NewSubscriptionDiagnosticsHandler(subscriptionRepo, downloadRepo, configRepo, qbClient, cfg.DownloadPath)
	downloadHandler := handler.NewDownloadHandler(downloadRepo, qbClient, configRepo)
	downloadHistoryHandler := handler.NewDownloadHistoryHandler(downloadRepo)
	rssHandler := handler.NewRSSHandler(rssScheduler)
	configHandler := handler.NewConfigHandler(configRepo)
	rssSourceHandler := handler.NewRSSSourceHandler(rssSourceRepo, configRepo, rssParser)
	mikanHandler := handler.NewMikanHandler(configRepo, subscriptionRepo)
	bangumiHandler := handler.NewBangumiHandler(configRepo)
	logHandler := handler.NewLogHandler(logRepo)
	fileOrganizerHandler := handler.NewFileOrganizerHandler(appCtx)
	recoveryHandler := handler.NewRecoveryHandler(db, subscriptionRepo, downloadRepo, configRepo, nil)
	calendarHandler := handler.NewCalendarHandler(subscriptionRepo, downloadRepo)
	diskHandler := handler.NewDiskHandler(db, downloadRepo, subscriptionRepo, configRepo)
	tagHandler := handler.NewTagHandler(subscriptionRepo)
	scannerHandler := handler.NewScannerHandler(db, subscriptionRepo, downloadRepo, configRepo)
	authHandler := handler.NewAuthHandler(cfg, jwtService)
	notificationHandler := handler.NewNotificationHandler(db, notificationSvc, wsHub, jwtService, cfg.AuthEnabled)
	backupHandler := handler.NewBackupHandler(backup.NewService(db))

	// API v1 路由组
	v1 := r.Group("/api/v1")
	{
		authGroup := v1.Group("/auth")
		{
			authGroup.GET("/status", authHandler.Status)
			authGroup.POST("/login", authHandler.Login)
			authGroup.POST("/refresh", authHandler.Refresh)
			authGroup.POST("/logout", authHandler.Logout)
		}

		protected := v1.Group("")
		if cfg.AuthEnabled {
			protected.Use(middleware.AuthMiddleware(jwtService))
		}

		// Mikan 搜索
		mikan := protected.Group("/mikan")
		{
			mikan.GET("/search", mikanHandler.Search)
			mikan.GET("/season", mikanHandler.GetBySeason)
			mikan.GET("/fansub-groups", mikanHandler.GetFansubGroups)
		}

		// Bangumi 搜索
		bangumi := protected.Group("/bangumi")
		{
			bangumi.GET("/search", bangumiHandler.Search)
			bangumi.GET("/search-by-name", bangumiHandler.SearchByName)
			bangumi.GET("/subjects/:id", bangumiHandler.GetSubject)
		}

		// RSS 源管理
		rssSources := protected.Group("/rss-sources")
		{
			rssSources.POST("", rssSourceHandler.Create)
			rssSources.GET("", rssSourceHandler.List)
			rssSources.GET("/:id", rssSourceHandler.Get)
			rssSources.PUT("/:id", rssSourceHandler.Update)
			rssSources.DELETE("/:id", rssSourceHandler.Delete)
			rssSources.GET("/:id/animes", rssSourceHandler.FetchAnimes)
		}

		// 订阅管理
		subscriptions := protected.Group("/subscriptions")
		{
			subscriptions.POST("", subscriptionHandler.Create)
			subscriptions.GET("", subscriptionHandler.List)
			subscriptions.POST("/preview", subscriptionHandler.Preview)
			subscriptions.GET("/:id", subscriptionHandler.GetByID)
			subscriptions.PUT("/:id", subscriptionHandler.Update)
			subscriptions.DELETE("/:id", subscriptionHandler.Delete)
			subscriptions.GET("/:id/diagnostics", subscriptionDiagnosticsHandler.Get)
			subscriptions.POST("/:id/diagnostics/retry-failed", subscriptionDiagnosticsHandler.RetryFailed)
			subscriptions.POST("/:id/toggle", subscriptionHandler.Toggle)
			subscriptions.POST("/:id/enrich-bangumi", subscriptionHandler.EnrichBangumi)
			subscriptions.POST("/:id/download-collection", subscriptionHandler.DownloadCollection)
			subscriptions.POST("/:id/collect-episodes", subscriptionHandler.CollectEpisodes)
			subscriptions.POST("/:id/reorganize-files", subscriptionHandler.ReorganizeFiles)
			subscriptions.POST("/:id/rename-files", subscriptionHandler.RenameFiles)
			subscriptions.POST("/:id/scan-folder", scannerHandler.ScanFolder)
			subscriptions.POST("/batch-import-from-rss", subscriptionHandler.BatchImportFromRSS)
			// 批量操作
			subscriptions.POST("/batch/enable", subscriptionHandler.BatchUpdateEnabled)
			subscriptions.POST("/batch/delete", subscriptionHandler.BatchDelete)
			subscriptions.POST("/batch/group", subscriptionHandler.BatchUpdateGroup)
			// 导入/导出
			subscriptions.GET("/export", subscriptionHandler.ExportSubscriptions)
			subscriptions.POST("/import", subscriptionHandler.ImportSubscriptions)
			// 统计
			subscriptions.GET("/statistics", subscriptionHandler.GetStatistics)
			// 分组管理
			subscriptions.GET("/groups", subscriptionHandler.ListGroups)
			subscriptions.POST("/groups", subscriptionHandler.CreateGroup)
			subscriptions.GET("/groups/:id", subscriptionHandler.GetGroup)
			subscriptions.PUT("/groups/:id", subscriptionHandler.UpdateGroup)
			subscriptions.DELETE("/groups/:id", subscriptionHandler.DeleteGroup)
		}

		// 下载管理
		downloads := protected.Group("/downloads")
		{
			downloads.GET("", downloadHandler.List)
			downloads.GET("/:id/diagnostics", downloadHandler.Diagnostics)
			downloads.GET("/:id", downloadHandler.GetByID)
			downloads.DELETE("/:id", downloadHandler.Delete)
			downloads.POST("/:id/retry", downloadHandler.Retry)
			downloads.POST("/batch-delete", downloadHandler.BatchDelete)
			downloads.DELETE("/clear", downloadHandler.Clear)
			// 下载历史和统计
			downloads.GET("/history", downloadHistoryHandler.GetHistory)
			downloads.GET("/statistics", downloadHistoryHandler.GetStatistics)
		}

		// RSS 管理
		rssGroup := protected.Group("/rss")
		{
			rssGroup.POST("/refresh", rssHandler.Refresh)

			// RSS 健康检查
			rssHealthChecker := rss.NewHealthChecker(subscriptionRepo)
			rssHealthHandler := handler.NewRSSHealthHandler(rssHealthChecker, subscriptionRepo)

			rssGroup.GET("/health", rssHealthHandler.CheckAll)
			rssGroup.GET("/health/:subscription_id", rssHealthHandler.CheckOne)
			rssGroup.GET("/dead", rssHealthHandler.GetDead)
			rssGroup.POST("/health-check", rssHealthHandler.TriggerCheck)
		}

		// 配置管理
		configs := protected.Group("/config")
		{
			configs.GET("", configHandler.GetAll)
			configs.PUT("", configHandler.Update)
			configs.POST("/qbittorrent/test", configHandler.TestQBittorrent)
			configs.POST("/qbittorrent/save", configHandler.SaveQBittorrentConfig)

			// 重命名模板配置
			configs.GET("/rename/presets", configHandler.GetRenamePresets)
			configs.GET("/rename/template", configHandler.GetRenameTemplate)
			configs.POST("/rename/template", configHandler.SaveRenameTemplate)
			configs.POST("/rename/preview", configHandler.PreviewRenameTemplate)
		}

		// 日志管理
		logs := protected.Group("/logs")
		{
			logs.GET("", logHandler.List)
			logs.POST("/clear", logHandler.Clear)
		}

		// 文件整理
		fileOrganizer := protected.Group("/file-organizer")
		{
			fileOrganizer.POST("/trigger", fileOrganizerHandler.TriggerScan)
			fileOrganizer.POST("/reload", fileOrganizerHandler.ReloadConfig)
		}

		// 扫描恢复
		recovery := protected.Group("/recovery")
		{
			recovery.POST("/scan", recoveryHandler.Scan)
		}

		// 任务管理
		taskHandler := handler.NewTaskHandler()
		tasks := protected.Group("/tasks")
		{
			tasks.GET("/current", taskHandler.GetCurrent)
			tasks.GET("/history", taskHandler.GetHistory)
			tasks.POST("/cancel", taskHandler.Cancel)
		}

		// 通知管理
		notifications := protected.Group("/notifications")
		{
			notifications.GET("", notificationHandler.ListNotifications)
			notifications.GET("/settings", notificationHandler.GetSettings)
			notifications.GET("/settings/:channel", notificationHandler.GetSetting)
			notifications.PUT("/settings", notificationHandler.UpdateSetting)
			notifications.DELETE("/settings/:channel", notificationHandler.DeleteSetting)
			notifications.POST("/test", notificationHandler.TestChannel)
			notifications.GET("/websocket/status", notificationHandler.GetWebSocketStatus)
			notifications.GET("/webhook/templates", notificationHandler.GetWebhookTemplates)
		}

		// 日历管理
		calendars := protected.Group("/calendar")
		{
			calendars.GET("", calendarHandler.GetWeekSchedule)
			calendars.GET("/today", calendarHandler.GetTodaySchedule)
		}

		// 磁盘监控
		disks := protected.Group("/disk")
		{
			disks.GET("/status", diskHandler.GetStatus)
			disks.GET("/info", diskHandler.GetInfo)
			disks.GET("/settings", diskHandler.GetSettings)
			disks.PUT("/settings", diskHandler.UpdateSettings)
			disks.POST("/cleanup", diskHandler.TriggerCleanup)
			disks.GET("/history", diskHandler.GetHistory)
		}

		// 标签管理
		tags := protected.Group("/tags")
		{
			tags.GET("", tagHandler.List)
			tags.POST("", tagHandler.Create)
			tags.PUT("/:id", tagHandler.Update)
			tags.DELETE("/:id", tagHandler.Delete)
		}

		// 配置备份与迁移
		backups := protected.Group("/backup")
		{
			backups.GET("/export", backupHandler.Export)
			backups.POST("/preview", backupHandler.Preview)
			backups.POST("/import", backupHandler.Import)
		}

		// 订阅标签关联 (在订阅路由组内)
		subscriptions.GET("/:id/tags", tagHandler.GetSubscriptionTags)
		subscriptions.POST("/:id/tags", tagHandler.AddTagsToSubscription)
		subscriptions.DELETE("/:id/tags/:tag_id", tagHandler.RemoveTagFromSubscription)
	}

	// WebSocket 端点
	r.GET("/ws/notifications", notificationHandler.WebSocketHandler)

	// 启动后台调度器
	if err := rssScheduler.Start(); err != nil {
		if cfg.BlockAPIBootOnSchedulerFailure {
			return nil, fmt.Errorf("failed to start RSS scheduler: %w", err)
		}
		logger.Error("Failed to start RSS scheduler, API continues without scheduler", "error", err)
	}
	appCtx.RegisterShutdownHook(rssScheduler.Stop)

	// 初始化健康检查器
	healthChecker := handler.NewHealthChecker(db, qbClient)

	// 健康检查端点
	r.GET("/health", healthChecker.HealthHandler)
	r.GET("/ready", healthChecker.ReadyHandler)
	r.GET("/live", healthChecker.LiveHandler)
	r.GET("/api/v1/health", healthChecker.HealthHandler)

	// Prometheus 指标端点
	r.GET("/metrics", handler.MetricsHandler())

	// 静态文件服务 (前端)
	if distFS, err := webui.DistFS(); err == nil {
		if assetsFS, err := fs.Sub(distFS, "assets"); err == nil {
			r.StaticFS("/assets", http.FS(assetsFS))
		}

		serveIndex := func(c *gin.Context) {
			indexHTML, err := fs.ReadFile(distFS, "index.html")
			if err != nil {
				c.Status(http.StatusNotFound)
				return
			}
			c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
		}

		r.GET("/", serveIndex)
		r.NoRoute(serveIndex)
	}

	return r, nil
}

// coverHandler 封面图片处理：本地存在则直接返回，否则从 Bangumi 原 URL fallback 并异步重新下载
func coverHandler(coverPath string, db *gorm.DB) gin.HandlerFunc {
	imgService := bangumi.NewImageService(coverPath)
	return func(c *gin.Context) {
		filename := filepath.Base(c.Param("filepath"))
		if filename == "" || filename == "." || filename == "/" {
			c.Status(http.StatusNotFound)
			return
		}

		localPath := filepath.Join(coverPath, filename)
		if _, err := os.Stat(localPath); err == nil {
			c.File(localPath)
			return
		}

		// 本地缺失，尝试从文件名提取 bangumi_id 做 fallback
		bangumiID := extractBangumiIDFromCoverFilename(filename)
		if bangumiID <= 0 {
			c.Status(http.StatusNotFound)
			return
		}

		var sub model.Subscription
		if err := db.Where("bangumi_id = ?", bangumiID).First(&sub).Error; err != nil {
			c.Status(http.StatusNotFound)
			return
		}

		if sub.BangumiCover == "" {
			c.Status(http.StatusNotFound)
			return
		}

		// 设置代理（如果配置了）
		configRepo := repository.NewConfigRepository(db)
		if proxyConfig, err := configRepo.Get("system_proxy"); err == nil && proxyConfig != nil && proxyConfig.Value != "" {
			imgService.SetProxy(proxyConfig.Value)
		}

		// 代理返回原始图片
		proxyCoverImage(c, sub.BangumiCover)

		// 异步触发重新下载
		go func() {
			if _, err := imgService.DownloadCover(sub.BangumiCover, bangumiID); err != nil {
				logger.Error("Failed to re-download cover", "filename", filename, "error", err)
			} else {
				logger.Info("Cover re-downloaded successfully", "filename", filename)
			}
		}()
	}
}

// extractBangumiIDFromCoverFilename 从封面文件名提取 bangumi_id
func extractBangumiIDFromCoverFilename(filename string) int {
	re := regexp.MustCompile(`^bangumi_(\d+)_[a-f0-9]+\.[a-z]+$`)
	if matches := re.FindStringSubmatch(filename); len(matches) > 1 {
		if id, err := strconv.Atoi(matches[1]); err == nil {
			return id
		}
	}
	return 0
}

// proxyCoverImage 代理返回远程封面图片
func proxyCoverImage(c *gin.Context, imageURL string) {
	req, err := http.NewRequest("GET", imageURL, nil)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Referer", "https://bgm.tv/")
	req.Header.Set("Accept", "image/webp,image/apng,image/*,*/*;q=0.8")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.Status(http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.Status(resp.StatusCode)
		return
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		c.Status(http.StatusBadGateway)
		return
	}

	c.Data(http.StatusOK, contentType, data)
}
