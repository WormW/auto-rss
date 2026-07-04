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
	dryRun, err := parseMode(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		printUsage(os.Stderr)
		os.Exit(1)
	}

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

func parseMode(args []string) (bool, error) {
	if len(args) != 1 {
		return false, fmt.Errorf("expected exactly one mode")
	}

	switch args[0] {
	case "dry-run":
		return true, nil
	case "apply":
		return false, nil
	default:
		return false, fmt.Errorf("unsupported mode %q", args[0])
	}
}

func printUsage(out *os.File) {
	fmt.Fprintln(out, "Usage: go run cmd/recovery-scan/main.go <dry-run|apply>")
}
