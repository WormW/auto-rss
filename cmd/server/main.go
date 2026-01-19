package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/WormW/auto-rss/internal/api/router"
	"github.com/WormW/auto-rss/internal/app"
	"github.com/WormW/auto-rss/internal/config"
	"github.com/WormW/auto-rss/internal/pkg/database"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/bangumi"
	"github.com/WormW/auto-rss/internal/service/downloader"
	"github.com/gin-gonic/gin"
)

func main() {
	// 加载配置
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 初始化日志
	if err := logger.Init(cfg.LogLevel); err != nil {
		log.Fatalf("Failed to init logger: %v", err)
	}
	defer logger.Sync()

	logger.Info("Starting Auto-RSS server...")

	// 初始化数据库
	db, err := database.Init(cfg.DBPath)
	if err != nil {
		logger.Fatal("Failed to init database", "error", err)
	}
	logger.Info("Database initialized", "path", cfg.DBPath)

	// 运行数据库迁移
	if err := database.Migrate(db); err != nil {
		logger.Fatal("Failed to migrate database", "error", err)
	}
	logger.Info("Database migration completed")

	// 从数据库加载配置并覆盖默认配置
	if err := cfg.LoadFromDB(db); err != nil {
		logger.Warn("Failed to load config from database", "error", err)
	} else {
		logger.Info("Configuration loaded from database",
			"download_path", cfg.DownloadPath,
			"qb_host", cfg.QBHost)
	}

	// 重新初始化日志以包含数据库写入
	if err := logger.InitWithDB(cfg.LogLevel, db); err != nil {
		log.Fatalf("Failed to reinit logger with DB: %v", err)
	}
	logger.Info("Logger initialized with database writer")

	// 设置 Gin 模式
	if cfg.LogLevel == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	// 初始化 repository
	downloadRepo := repository.NewDownloadRepository(db)
	subscriptionRepo := repository.NewSubscriptionRepository(db)
	configRepo := repository.NewConfigRepository(db)

	// 初始化 qBittorrent 客户端
	qbClient := downloader.NewQBittorrentClient()
	logger.Info("Attempting to connect to qBittorrent",
		"host", cfg.QBHost,
		"username", cfg.QBUsername,
		"password_length", len(cfg.QBPassword))
	if err := qbClient.Login(cfg.QBHost, cfg.QBUsername, cfg.QBPassword); err != nil {
		logger.Warn("Failed to login to qBittorrent, download monitor will retry", "error", err)
	} else {
		logger.Info("Connected to qBittorrent", "host", cfg.QBHost)
	}

	// 获取重命名模板配置
	renameTemplate := "${title}/Season ${season}/${title} S${seasonFormat}E${episodeFormat}"
	if templateConfig, err := configRepo.Get("rename_template"); err == nil && templateConfig != nil {
		renameTemplate = templateConfig.Value
		logger.Info("Loaded rename template from config", "template", renameTemplate)
	}

	// 初始化下载监控服务
	downloadMonitor := downloader.NewDownloadMonitor(
		qbClient,
		downloadRepo,
		subscriptionRepo,
		configRepo,
		renameTemplate,
	)

	// 启动下载监控（30秒检查一次）
	downloadMonitor.Start(30 * time.Second)
	logger.Info("Download monitor started", "interval", "30s", "category", "AutoRss")

	// 初始化 Bangumi 更新服务
	bangumiService := bangumi.NewBangumiService()
	bangumiUpdater := bangumi.NewBangumiUpdater(
		bangumiService,
		subscriptionRepo,
		cfg.BangumiUpdateInterval,
	)

	// 启动 Bangumi 更新服务
	bangumiUpdater.Start()

	// 创建应用上下文
	appCtx := app.NewContext(db, cfg, subscriptionRepo, bangumiService)
	appCtx.SetRenameTemplate(renameTemplate)

	// 初始化文件整理服务
	if err := appCtx.InitializeFileOrganizer(); err != nil {
		logger.Error("Failed to initialize file organizer", "error", err)
	}

	// 初始化路由（传递应用上下文）
	r := router.Setup(db, cfg, qbClient, appCtx)

	// 优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// 启动服务器（非阻塞）
	addr := fmt.Sprintf(":%d", cfg.ServerPort)
	logger.Info("Server starting", "address", addr)

	go func() {
		if err := r.Run(addr); err != nil {
			logger.Fatal("Failed to start server", "error", err)
		}
	}()

	// 等待退出信号
	<-quit
	logger.Info("Shutting down server...")

	// 关闭应用上下文（包括文件整理服务）
	appCtx.Shutdown()
	logger.Info("App context shutdown complete")

	// 停止 Bangumi 更新服务
	bangumiUpdater.Stop()
	logger.Info("Bangumi updater stopped")

	// 停止下载监控
	downloadMonitor.Stop()
	logger.Info("Download monitor stopped")

	logger.Info("Server exited")
}
