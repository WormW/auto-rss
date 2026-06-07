package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/spf13/viper"
	"gorm.io/gorm"
)

// RateLimitConfig 限流配置
type RateLimitConfig struct {
	RPS        float64       // 每秒请求数
	Burst      int           // 突发请求数
	AuthRPM    int           // 认证端点每分钟请求数
	MaxEntries int           // 最大缓存条目数
	TTL        time.Duration // 不活跃客户端清理时间
}

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
	// 调度器启动失败是否阻断 API 启动
	BlockAPIBootOnSchedulerFailure bool

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

	// JWT配置
	AuthEnabled           bool
	JWTSecret             string
	JWTAccessTokenExpiry  time.Duration
	JWTRefreshTokenExpiry time.Duration
	JWTUsername           string
	JWTPassword           string

	// 限流配置
	RateLimit RateLimitConfig
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
		DBPath:                         getEnv("DB_PATH", "./data/auto-rss.db"),
		QBHost:                         getEnv("QB_HOST", "http://localhost:8080"),
		QBUsername:                     getEnv("QB_USERNAME", "admin"),
		QBPassword:                     getEnv("QB_PASSWORD", ""),
		RSSInterval:                    getEnv("RSS_INTERVAL", "30m"),
		BlockAPIBootOnSchedulerFailure: getEnv("BLOCK_API_BOOT_ON_SCHEDULER_FAILURE", "true") == "true",
		BangumiUpdateInterval:          getEnvAsInt("BANGUMI_UPDATE_INTERVAL", 6), // 默认6小时
		LogLevel:                       getEnv("LOG_LEVEL", "info"),
		ServerPort:                     getEnvAsInt("SERVER_PORT", 7892),
		DownloadPath:                   getEnv("DOWNLOAD_PATH", "/downloads"),

		// 文件整理配置
		FileOrganizerEnabled: getEnv("FILE_ORGANIZER_ENABLED", "false") == "true",
		FileOrganizerDir:     getEnv("FILE_ORGANIZER_DIR", ""),

		// JWT配置
		AuthEnabled:           getEnv("AUTH_ENABLED", "false") == "true",
		JWTSecret:             getEnv("JWT_SECRET", "your-secret-key-change-in-production"),
		JWTAccessTokenExpiry:  getEnvAsDuration("JWT_ACCESS_TOKEN_EXPIRY", 30*time.Minute),
		JWTRefreshTokenExpiry: getEnvAsDuration("JWT_REFRESH_TOKEN_EXPIRY", 7*24*time.Hour),
		JWTUsername:           getEnv("JWT_USERNAME", "admin"),
		JWTPassword:           getEnv("JWT_PASSWORD", "admin"),

		// 限流配置
		RateLimit: RateLimitConfig{
			RPS:        getEnvAsFloat64("RATE_LIMIT_RPS", 10.0),
			Burst:      getEnvAsInt("RATE_LIMIT_BURST", 20),
			AuthRPM:    getEnvAsInt("RATE_LIMIT_AUTH_RPM", 5),
			MaxEntries: getEnvAsInt("RATE_LIMIT_MAX_ENTRIES", 10000),
			TTL:        getEnvAsDuration("RATE_LIMIT_TTL", 1*time.Hour),
		},
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
	if c.AuthEnabled {
		if strings.TrimSpace(c.JWTSecret) == "" {
			return fmt.Errorf("JWT_SECRET is required when AUTH_ENABLED=true")
		}
		if c.JWTSecret == "your-secret-key-change-in-production" {
			return fmt.Errorf("JWT_SECRET must be changed from the default when AUTH_ENABLED=true")
		}
		if len(c.JWTSecret) < 32 {
			return fmt.Errorf("JWT_SECRET must be at least 32 characters when AUTH_ENABLED=true")
		}
		if strings.TrimSpace(c.JWTUsername) == "" {
			return fmt.Errorf("JWT_USERNAME is required when AUTH_ENABLED=true")
		}
		if strings.TrimSpace(c.JWTPassword) == "" {
			return fmt.Errorf("JWT_PASSWORD is required when AUTH_ENABLED=true")
		}
		if c.JWTPassword == "admin" {
			return fmt.Errorf("JWT_PASSWORD must be changed from the default when AUTH_ENABLED=true")
		}
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

// getEnvAsFloat64 获取环境变量并转换为浮点数
func getEnvAsFloat64(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		if floatValue, err := strconv.ParseFloat(value, 64); err == nil {
			return floatValue
		}
	}
	return defaultValue
}

// getEnvAsDuration 获取环境变量并转换为时间间隔
func getEnvAsDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
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
