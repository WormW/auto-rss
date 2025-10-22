package router

import (
	"github.com/WormW/auto-rss/internal/api/handler"
	"github.com/WormW/auto-rss/internal/api/middleware"
	"github.com/WormW/auto-rss/internal/app"
	"github.com/WormW/auto-rss/internal/config"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/downloader"
	"github.com/WormW/auto-rss/internal/service/rss"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Setup 设置路由
func Setup(db *gorm.DB, cfg *config.Config, qbClient downloader.QBittorrentClient, appCtx *app.Context) *gin.Engine {
	r := gin.New()

	// 应用中间件
	r.Use(middleware.Logger())
	r.Use(middleware.Recovery())
	r.Use(middleware.CORS())

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

	// 初始化处理器
	subscriptionHandler := handler.NewSubscriptionHandler(subscriptionRepo, downloadRepo, configRepo, qbClient, cfg.DownloadPath)
	downloadHandler := handler.NewDownloadHandler(downloadRepo)
	rssHandler := handler.NewRSSHandler()
	configHandler := handler.NewConfigHandler(configRepo)
	rssSourceHandler := handler.NewRSSSourceHandler(rssSourceRepo, configRepo, rssParser)
	mikanHandler := handler.NewMikanHandler(configRepo, subscriptionRepo)
	bangumiHandler := handler.NewBangumiHandler(configRepo)
	logHandler := handler.NewLogHandler(logRepo)
	fileOrganizerHandler := handler.NewFileOrganizerHandler(appCtx)

	// API v1 路由组
	v1 := r.Group("/api/v1")
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
			subscriptions.POST("/:id/collect-episodes", subscriptionHandler.CollectEpisodes)
			subscriptions.POST("/batch-import-from-rss", subscriptionHandler.BatchImportFromRSS)
		}

		// 下载管理
		downloads := v1.Group("/downloads")
		{
			downloads.GET("", downloadHandler.List)
			downloads.GET("/:id", downloadHandler.GetByID)
			downloads.DELETE("/:id", downloadHandler.Delete)
			downloads.POST("/:id/retry", downloadHandler.Retry)
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
	}

	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status": "ok",
		})
	})

	// 静态文件服务 (前端)
	r.Static("/assets", "./web/dist/assets")
	r.StaticFile("/", "./web/dist/index.html")
	r.NoRoute(func(c *gin.Context) {
		c.File("./web/dist/index.html")
	})

	return r
}
