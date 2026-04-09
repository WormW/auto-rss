package database

import (
	"fmt"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// RunMigrations 运行数据库迁移
func RunMigrations(db *gorm.DB) error {
	m := gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{
		{
			ID: "202504090001", // 初始迁移 - 创建所有表
			Migrate: func(tx *gorm.DB) error {
				return tx.AutoMigrate(
					&model.Subscription{},
					&model.Download{},
					&model.Config{},
					&model.RSSSource{},
					&model.Log{},
					&model.RefreshToken{},
				)
			},
			Rollback: func(tx *gorm.DB) error {
				// 回滚时删除所有表
				return tx.Migrator().DropTable(
					"subscriptions",
					"downloads",
					"configs",
					"rss_sources",
					"logs",
					"refresh_tokens",
				)
			},
		},
		{
			ID: "202504090002", // 添加索引优化
			Migrate: func(tx *gorm.DB) error {
				// 为常用查询字段添加索引
				if err := tx.Exec("CREATE INDEX IF NOT EXISTS idx_subscriptions_status ON subscriptions(status)").Error; err != nil {
					return err
				}
				if err := tx.Exec("CREATE INDEX IF NOT EXISTS idx_downloads_status ON downloads(status)").Error; err != nil {
					return err
				}
				if err := tx.Exec("CREATE INDEX IF NOT EXISTS idx_logs_created_at ON logs(created_at)").Error; err != nil {
					return err
				}
				return nil
			},
			Rollback: func(tx *gorm.DB) error {
				tx.Exec("DROP INDEX IF EXISTS idx_subscriptions_status")
				tx.Exec("DROP INDEX IF EXISTS idx_downloads_status")
				tx.Exec("DROP INDEX IF EXISTS idx_logs_created_at")
				return nil
			},
		},
	})

	// 设置迁移超时
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("获取底层数据库连接失败: %w", err)
	}
	sqlDB.SetConnMaxLifetime(time.Hour)

	return m.Migrate()
}

// GetCurrentVersion 获取当前迁移版本
func GetCurrentVersion(db *gorm.DB) (string, error) {
	var migration gormigrate.MigrationRecord
	err := db.Order("id DESC").First(&migration).Error
	if err == gorm.ErrRecordNotFound {
		return "none", nil
	}
	if err != nil {
		return "", err
	}
	return migration.ID, nil
}

// ResetMigrations 重置所有迁移（危险操作，仅用于开发）
func ResetMigrations(db *gorm.DB) error {
	m := gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{
		{
			ID: "202504090001",
			Migrate: func(tx *gorm.DB) error {
				return tx.AutoMigrate(
					&model.Subscription{},
					&model.Download{},
					&model.Config{},
					&model.RSSSource{},
					&model.Log{},
					&model.RefreshToken{},
				)
			},
			Rollback: func(tx *gorm.DB) error {
				return tx.Migrator().DropTable(
					"subscriptions",
					"downloads",
					"configs",
					"rss_sources",
					"logs",
					"refresh_tokens",
				)
			},
		},
	})
	return m.RollbackTo("202504090001")
}
