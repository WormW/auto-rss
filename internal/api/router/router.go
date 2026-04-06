package router

import (
	"io/fs"
	"net/http"
	"time"

	"github.com/WormW/auto-rss/internal/api/handler"
	"github.com/WormW/auto-rss/internal/api/middleware"
	"github.com/WormW/auto-rss/internal/api/middleware/ratelimit"
	"github.com/WormW/auto-rss/internal/app"
	"github.com/WormW/auto-rss/internal/config"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/auth"
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

// Setup 设置路由
func Setup(db *gorm.DB, cfg *config.Config, qbClient downloader.QBittorrentClient, appCtx *app.Context, jwtService auth.JWTService) *gin.Engine {
	r := gin.New()

	// 应用中间件
	r.Use(middleware.Logger())
	r.Use(middleware.Recovery())
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

	// 应用限流中间件（在认证之前，防止认证计算资源被耗尽）
	r.Use(middleware.RateLimitWithConfig(middleware.RateLimitConfig{
		Store:         rateLimitStore,
		GeneralRPS:    cfg.RateLimit.RPS,
		GeneralBurst:  cfg.RateLimit.Burst,
		AuthRPM:       cfg.RateLimit.AuthRPM,
		AuthBurst:     5, // Fixed burst for auth
		AuthPaths:     []string{"/api/v1/auth/login", "/api/v1/auth/refresh"},
		ExcludedPaths: []string{"/health", "/api/v1/health"},
	}))

	// 静态文件服务 - 封面图片
	r.Static("/covers", "./data/covers")

	// 初始化服务
	rssParser := rss.NewParser()

	// 初始化仓储
	subscriptionRepo := repository.NewSubscriptionRepository(db)
	downloadRepo := repository.NewDownloadRepository(db)
	configRepo := repository.NewConfigRepository(db)
	rssSourceRepo := repository.NewRSSSourceRepository(db)
	logRepo := repository.NewLogRepository(db)

	// 初始化调度器
	rssScheduler := scheduler.NewScheduler(db, subscriptionRepo, downloadRepo, configRepo, cfg.RSSInterval, rssParser, qbClient)

	// 初始化通知服务
	notificationSvc := notification.NewService(db)
	wsHub := notificationSvc.GetWebSocketHub()

	// 初始化日历服务
	calendarSvc := calendar.NewCalendar(subscriptionRepo, downloadRepo)
	calendarSvc.SetNotificationService(notificationSvc)

	// 初始化磁盘监控服务
	diskMonitor := disk.NewMonitor(downloadRepo, subscriptionRepo, configRepo)
	_ = diskMonitor.LoadConfig()
	diskMonitor.SetNotificationService(notificationSvc)
	diskMonitor.Start()

	// 初始化下载监控服务（在 handler 之前，因为某些 handler 可能需要它）
	downloadMonitor := downloader.NewDownloadMonitor(db, qbClient, downloadRepo, subscriptionRepo, configRepo, "")
	downloadMonitor.SetNotificationService(notificationSvc)
	downloadMonitor.Start(30 * time.Second)

	// 初始化处理器
	subscriptionHandler := handler.NewSubscriptionHandler(subscriptionRepo, downloadRepo, configRepo, qbClient, cfg.DownloadPath)
	downloadHandler := handler.NewDownloadHandler(downloadRepo, qbClient, configRepo)
	rssHandler := handler.NewRSSHandler(rssScheduler)
	configHandler := handler.NewConfigHandler(configRepo)
	rssSourceHandler := handler.NewRSSSourceHandler(rssSourceRepo, configRepo, rssParser)
	mikanHandler := handler.NewMikanHandler(configRepo, subscriptionRepo)
	bangumiHandler := handler.NewBangumiHandler(configRepo)
	logHandler := handler.NewLogHandler(logRepo)
	fileOrganizerHandler := handler.NewFileOrganizerHandler(appCtx)
	notificationHandler := handler.NewNotificationHandler(db, notificationSvc, wsHub)
	calendarHandler := handler.NewCalendarHandler(subscriptionRepo, downloadRepo)
	diskHandler := handler.NewDiskHandler(db, downloadRepo, subscriptionRepo, configRepo)
	authHandler := handler.NewAuthHandler(cfg, jwtService)

	// 认证中间件（用于保护路由）
	authMiddleware := middleware.AuthMiddleware(jwtService)

	// API v1 公开路由（无需认证）
	v1Public := r.Group("/api/v1")
	{
		// 健康检查
		v1Public.GET("/health", func(c *gin.Context) {
			c.JSON(200, gin.H{"status": "ok"})
		})
	}

	// 认证路由（公开）
	v1Auth := r.Group("/api/v1/auth")
	{
		v1Auth.POST("/login", authHandler.Login)
		v1Auth.POST("/refresh", authHandler.Refresh)
		v1Auth.POST("/logout", authHandler.Logout)
	}

	// API v1 受保护路由（需要认证）
	v1 := r.Group("/api/v1")
	v1.Use(authMiddleware)
	{
		// Mikan 搜索
		mikan := v1.Group("/mikan")
		{
			mikan.GET("/search", mikanHandler.Search)
			mikan.GET("/season", mikanHandler.GetBySeason)
			mikan.GET("/fansub-groups", mikanHandler.GetFansubGroups)
		}

		// Bangumi 搜索
		bangumi := v1.Group("/bangumi")
		{
			bangumi.GET("/search", bangumiHandler.Search)
			bangumi.GET("/search-by-name", bangumiHandler.SearchByName)
			bangumi.GET("/subjects/:id", bangumiHandler.GetSubject)
		}

		// RSS 源管理
		rssSources := v1.Group("/rss-sources")
		{
			rssSources.POST("", rssSourceHandler.Create)
			rssSources.GET("", rssSourceHandler.List)
			rssSources.GET("/:id", rssSourceHandler.Get)
			rssSources.PUT("/:id", rssSourceHandler.Update)
			rssSources.DELETE("/:id", rssSourceHandler.Delete)
			rssSources.GET("/:id/animes", rssSourceHandler.FetchAnimes)
		}

		// 订阅管理
		subscriptions := v1.Group("/subscriptions")
		{
			subscriptions.POST("", subscriptionHandler.Create)
			subscriptions.GET("", subscriptionHandler.List)
			subscriptions.GET("/:id", subscriptionHandler.GetByID)
			subscriptions.PUT("/:id", subscriptionHandler.Update)
			subscriptions.DELETE("/:id", subscriptionHandler.Delete)
			subscriptions.POST("/:id/toggle", subscriptionHandler.Toggle)
			subscriptions.POST("/:id/enrich-bangumi", subscriptionHandler.EnrichBangumi)
			subscriptions.POST("/:id/download-collection", subscriptionHandler.DownloadCollection)
			subscriptions.POST("/:id/collect-episodes", subscriptionHandler.CollectEpisodes)
			subscriptions.POST("/:id/reorganize-files", subscriptionHandler.ReorganizeFiles)
			subscriptions.POST("/:id/rename-files", subscriptionHandler.RenameFiles)
			subscriptions.POST("/batch-import-from-rss", subscriptionHandler.BatchImportFromRSS)
		}

		// 下载管理
		downloads := v1.Group("/downloads")
		{
			downloads.GET("", downloadHandler.List)
			downloads.GET("/:id", downloadHandler.GetByID)
			downloads.DELETE("/:id", downloadHandler.Delete)
			downloads.POST("/:id/retry", downloadHandler.Retry)
			downloads.POST("/batch-delete", downloadHandler.BatchDelete)
			downloads.DELETE("/clear", downloadHandler.Clear)
		}

		// RSS 管理
		rss := v1.Group("/rss")
		{
			rss.POST("/refresh", rssHandler.Refresh)
		}

		// 配置管理
		configs := v1.Group("/config")
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
		logs := v1.Group("/logs")
		{
			logs.GET("", logHandler.List)
			logs.POST("/clear", logHandler.Clear)
		}

		// 文件整理
		fileOrganizer := v1.Group("/file-organizer")
		{
			fileOrganizer.POST("/trigger", fileOrganizerHandler.TriggerScan)
			fileOrganizer.POST("/reload", fileOrganizerHandler.ReloadConfig)
		}

		// 任务管理
		taskHandler := handler.NewTaskHandler()
		tasks := v1.Group("/tasks")
		{
			tasks.GET("/current", taskHandler.GetCurrent)
			tasks.GET("/history", taskHandler.GetHistory)
			tasks.POST("/cancel", taskHandler.Cancel)
		}

		// 通知管理
		notifications := v1.Group("/notifications")
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
		calendars := v1.Group("/calendar")
		{
			calendars.GET("", calendarHandler.GetWeekSchedule)
			calendars.GET("/today", calendarHandler.GetTodaySchedule)
		}

		// 磁盘监控
		disks := v1.Group("/disk")
		{
			disks.GET("/status", diskHandler.GetStatus)
			disks.GET("/info", diskHandler.GetInfo)
			disks.GET("/settings", diskHandler.GetSettings)
			disks.PUT("/settings", diskHandler.UpdateSettings)
			disks.POST("/cleanup", diskHandler.TriggerCleanup)
			disks.GET("/history", diskHandler.GetHistory)
		}
	}

	// WebSocket 端点
	r.GET("/ws/notifications", notificationHandler.WebSocketHandler)

	// 启动后台调度器
	if err := rssScheduler.Start(); err != nil {
		panic(err)
	}

	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status": "ok",
		})
	})

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

	return r
}
