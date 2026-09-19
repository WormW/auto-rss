package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/WormW/auto-rss/internal/api/router"
	"github.com/WormW/auto-rss/internal/app"
	"github.com/WormW/auto-rss/internal/config"
	"github.com/WormW/auto-rss/internal/pkg/database"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/bangumi"
	"github.com/WormW/auto-rss/internal/service/downloader"
	"github.com/WormW/auto-rss/internal/service/medialibrary"
	"github.com/gin-gonic/gin"
)

func main() {
	if err := run(); err != nil {
		logger.Error("Server exited with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// 加载配置
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// 初始化日志
	if err := logger.Init(cfg.LogLevel); err != nil {
		return fmt.Errorf("failed to init logger: %w", err)
	}
	defer logger.Sync()

	logger.Info("Starting Auto-RSS server...")

	// 初始化数据库
	db, err := database.Init(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("failed to init database: %w", err)
	}
	logger.Info("Database initialized", "path", cfg.DBPath)

	// 运行数据库迁移
	if err := database.Migrate(db); err != nil {
		return fmt.Errorf("failed to migrate database: %w", err)
	}
	logger.Info("Database migration completed")

	// 运行额外迁移（索引创建等）
	if err := database.RunMigrations(db); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}
	logger.Info("Database index migration completed")

	// 从数据库加载配置并覆盖默认配置
	if err := cfg.LoadFromDB(db); err != nil {
		logger.Warn("Failed to load config from database", "error", err)
	} else {
		logger.Info("Configuration loaded from database",
			"download_path", cfg.DownloadPath,
			"qb_host", cfg.QBHost)
	}

	// 默认仅输出结构化日志；数据库日志为兼容查询接口按需启用。
	if cfg.LogDBEnabled {
		if err := logger.InitWithDB(cfg.LogLevel, db); err != nil {
			return fmt.Errorf("failed to reinit logger with DB: %w", err)
		}
		logger.Info("Logger initialized with database writer")
	}

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
	appCtx := app.NewContext()
	appCtx.SetMediaLibraryService(medialibrary.NewService(configRepo, downloadRepo))

	// 初始化路由（传递应用上下文）
	r, err := router.Setup(db, cfg, qbClient, appCtx, renameTemplate)
	if err != nil {
		return fmt.Errorf("failed to setup router: %w", err)
	}

	// 优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// 启动服务器（非阻塞）
	addr := fmt.Sprintf(":%d", cfg.ServerPort)
	logger.Info("Server starting", "address", addr)

	go func() {
		if err := r.Run(addr); err != nil {
			logger.Error("Failed to start server", "error", err)
			quit <- syscall.SIGTERM
		}
	}()

	// 等待退出信号
	<-quit
	logger.Info("Shutting down server...")

	// 关闭后台任务
	appCtx.Shutdown()
	logger.Info("App context shutdown complete")

	// 停止 Bangumi 更新服务
	bangumiUpdater.Stop()
	logger.Info("Bangumi updater stopped")

	logger.Info("Server exited")
	return nil
}
