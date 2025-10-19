package main

import (
	"fmt"
	"log"

	"github.com/WormW/auto-rss/internal/api/router"
	"github.com/WormW/auto-rss/internal/config"
	"github.com/WormW/auto-rss/internal/pkg/database"
	"github.com/WormW/auto-rss/internal/pkg/logger"
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

	// 设置 Gin 模式
	if cfg.LogLevel == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	// 初始化路由
	r := router.Setup(db, cfg)

	// 启动服务器
	addr := fmt.Sprintf(":%d", cfg.ServerPort)
	logger.Info("Server starting", "address", addr)

	if err := r.Run(addr); err != nil {
		logger.Fatal("Failed to start server", "error", err)
	}
}
