package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/WormW/auto-rss/internal/config"
	"github.com/WormW/auto-rss/internal/pkg/database"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/recovery"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run cmd/recovery-scan/main.go <dry-run|apply>")
		os.Exit(1)
	}

	mode := os.Args[1]
	dryRun := mode != "apply"

	// 加载配置
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 打开数据库
	db, err := database.Init(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	// 运行迁移（确保表存在）
	if err := database.Migrate(db); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	subRepo := repository.NewSubscriptionRepository(db)
	downloadRepo := repository.NewDownloadRepository(db)
	configRepo := repository.NewConfigRepository(db)

	scanner := recovery.NewScanner(db, subRepo, downloadRepo, configRepo, nil)
	result, err := scanner.Scan(&recovery.ScanRequest{DryRun: dryRun})
	if err != nil {
		log.Fatalf("Scan failed: %v", err)
	}

	pretty, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(pretty))
}
