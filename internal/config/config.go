package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/spf13/viper"
	"gorm.io/gorm"
)

// Config 应用配置
type Config struct {
	// 数据库配置
	DBPath string

	// qBittorrent 配置
	QBHost     string
	QBUsername string
	QBPassword string

	// RSS 配置
	RSSInterval string

	// Bangumi 更新配置
	BangumiUpdateInterval int // 小时为单位，0表示禁用自动更新

	// 日志配置
	LogLevel string

	// 服务器配置
	ServerPort int

	// 下载配置
	DownloadPath string

	// 文件整理配置
	FileOrganizerEnabled bool   // 是否启用文件自动整理
	FileOrganizerDir     string // 整理目录（监控和目标是同一目录）

	// JWT 认证配置
	JWTUsername             string
	JWTPassword             string
	JWTSecret               string
	JWTAccessTokenExpiry    time.Duration
	JWTRefreshTokenExpiry   time.Duration
}

// Load 加载配置
func Load() (*Config, error) {
	// 设置配置文件
	viper.SetConfigName(".env")
	viper.SetConfigType("env")
	viper.AddConfigPath(".")

	// 环境变量优先
	viper.AutomaticEnv()

	// 尝试读取配置文件 (可选)
	_ = viper.ReadInConfig()

	cfg := &Config{
		DBPath:                getEnv("DB_PATH", "./data/auto-rss.db"),
		QBHost:                getEnv("QB_HOST", "http://localhost:8080"),
		QBUsername:            getEnv("QB_USERNAME", "admin"),
		QBPassword:            getEnv("QB_PASSWORD", ""),
		RSSInterval:           getEnv("RSS_INTERVAL", "30m"),
		BangumiUpdateInterval: getEnvAsInt("BANGUMI_UPDATE_INTERVAL", 6), // 默认6小时
		LogLevel:              getEnv("LOG_LEVEL", "info"),
		ServerPort:            getEnvAsInt("SERVER_PORT", 7892),
		DownloadPath:          getEnv("DOWNLOAD_PATH", "/downloads"),

		// 文件整理配置
		FileOrganizerEnabled: getEnv("FILE_ORGANIZER_ENABLED", "false") == "true",
		FileOrganizerDir:     getEnv("FILE_ORGANIZER_DIR", ""),

		// JWT 认证配置
		JWTUsername:             getEnv("JWT_USERNAME", "admin"),
		JWTPassword:             getEnv("JWT_PASSWORD", ""),
		JWTSecret:               getEnv("JWT_SECRET", ""),
		JWTAccessTokenExpiry:    30 * time.Minute,
		JWTRefreshTokenExpiry:   7 * 24 * time.Hour,
	}

	// 验证配置
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate 验证配置
func (c *Config) Validate() error {
	if c.DBPath == "" {
		return fmt.Errorf("DB_PATH is required")
	}
	if c.QBHost == "" {
		return fmt.Errorf("QB_HOST is required")
	}
	if c.ServerPort <= 0 || c.ServerPort > 65535 {
		return fmt.Errorf("invalid SERVER_PORT: %d", c.ServerPort)
	}
	if c.JWTSecret == "" {
		return fmt.Errorf("JWT_SECRET is required")
	}
	if c.JWTPassword == "" {
		return fmt.Errorf("JWT_PASSWORD is required")
	}
	return nil
}

// getEnv 获取环境变量，如果不存在则返回默认值
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvAsInt 获取环境变量并转换为整数
func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// LoadFromDB 从数据库加载配置并覆盖现有配置
func (c *Config) LoadFromDB(db *gorm.DB) error {
	var configs []model.Config
	if err := db.Find(&configs).Error; err != nil {
		return err
	}

	for _, cfg := range configs {
		switch cfg.Key {
		case "qb_host", "qbittorrent_host":
			if cfg.Value != "" {
				c.QBHost = cfg.Value
			}
		case "qb_username", "qbittorrent_username":
			if cfg.Value != "" {
				c.QBUsername = cfg.Value
			}
		case "qb_password", "qbittorrent_password":
			if cfg.Value != "" {
				c.QBPassword = cfg.Value
			}
		case "download_path":
			if cfg.Value != "" {
				c.DownloadPath = cfg.Value
			}
		case "bangumi_update_interval":
			if cfg.Value != "" {
				if intValue, err := strconv.Atoi(cfg.Value); err == nil {
					c.BangumiUpdateInterval = intValue
				}
			}
		case "file_organizer_enabled":
			if cfg.Value != "" {
				c.FileOrganizerEnabled = (cfg.Value == "true" || cfg.Value == "1")
			}
		case "file_organizer_dir":
			if cfg.Value != "" {
				c.FileOrganizerDir = cfg.Value
			}
		}
	}

	return nil
}
