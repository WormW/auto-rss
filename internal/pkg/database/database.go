package database

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Config 数据库配置
type Config struct {
	MaxOpenConns    int           // 最大打开连接数
	MaxIdleConns    int           // 最大空闲连接数
	ConnMaxLifetime time.Duration // 连接最大生命周期
	ConnMaxIdleTime time.Duration // 空闲连接最大存活时间
	BusyTimeout     time.Duration // 忙等待超时时间
}

// DefaultConfig 返回默认数据库配置
func DefaultConfig() *Config {
	return &Config{
		MaxOpenConns:    10,               // SQLite 并发性能有限，不宜设置过高
		MaxIdleConns:    5,                // 保持一定数量的空闲连接
		ConnMaxLifetime: 30 * time.Minute, // 连接最多存活30分钟
		ConnMaxIdleTime: 10 * time.Minute, // 空闲连接最多存活10分钟
		BusyTimeout:     5 * time.Second,  // 忙等待超时5秒
	}
}

// Init 初始化数据库连接
func Init(dbPath string) (*gorm.DB, error) {
	return InitWithConfig(dbPath, DefaultConfig())
}

// InitWithConfig 使用自定义配置初始化数据库连接
func InitWithConfig(dbPath string, cfg *Config) (*gorm.DB, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	// 确保数据目录存在
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %w", err)
	}

	// 构建 DSN，添加 SQLite 优化参数
	dsn := fmt.Sprintf("%s?_busy_timeout=%d&_journal_mode=WAL&_synchronous=NORMAL",
		dbPath,
		cfg.BusyTimeout.Milliseconds(),
	)

	// 打开数据库连接
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// 配置连接池
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}

	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

	return db, nil
}

// CheckConnection 检查数据库连接是否健康
func CheckConnection(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get sql.DB: %w", err)
	}

	if err := sqlDB.Ping(); err != nil {
		return fmt.Errorf("database ping failed: %w", err)
	}

	return nil
}

// GetStats 获取数据库连接池统计信息
func GetStats(db *gorm.DB) (*DBStats, error) {
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}

	stats := sqlDB.Stats()
	return &DBStats{
		OpenConnections:   stats.OpenConnections,
		InUse:             stats.InUse,
		Idle:              stats.Idle,
		WaitCount:         stats.WaitCount,
		WaitDuration:      stats.WaitDuration,
		MaxIdleClosed:     stats.MaxIdleClosed,
		MaxLifetimeClosed: stats.MaxLifetimeClosed,
	}, nil
}

// DBStats 数据库连接池统计信息
type DBStats struct {
	OpenConnections   int           // 打开的连接数
	InUse             int           // 正在使用的连接数
	Idle              int           // 空闲连接数
	WaitCount         int64         // 等待连接的总次数
	WaitDuration      time.Duration // 等待连接的总时间
	MaxIdleClosed     int64         // 因超过最大空闲数而关闭的连接数
	MaxLifetimeClosed int64         // 因超过最大生命周期而关闭的连接数
}

// Migrate 执行数据库迁移
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&model.RSSSource{},
		&model.Subscription{},
		&model.SubscriptionFeed{},
		&model.SubscriptionFeedSeenItem{},
		&model.Download{},
		&model.SubscriptionEpisode{},
		&model.EpisodeResourceCandidate{},
		&model.Config{},
		&model.Log{},
		&model.Notification{},
		&model.NotificationSetting{},
	)
}
