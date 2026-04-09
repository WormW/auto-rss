package database

import (
	"fmt"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// LegacyMigrationRecord 旧版迁移记录表结构
// 用于向后兼容，检测是否存在旧版迁移记录
type LegacyMigrationRecord struct {
	ID        uint   `gorm:"primaryKey"`
	Version   string `gorm:"uniqueIndex;not null"`
	AppliedAt int64  `gorm:"not null"`
}

// TableName 指定表名
func (LegacyMigrationRecord) TableName() string {
	return "migration_records"
}

// MigrationRecord gormigrate 迁移记录表结构
// 与 gormigrate 内部使用的结构对应
type MigrationRecord struct {
	ID        string `gorm:"primaryKey;size:255"`
	AppliedAt time.Time
}

// TableName 指定表名
func (MigrationRecord) TableName() string {
	return "migrations"
}

// RunMigrations 运行数据库迁移
// 支持从旧版迁移系统平滑过渡到 gormigrate
func RunMigrations(db *gorm.DB) error {
	// 首先检查是否需要从旧版迁移系统过渡
	if err := migrateFromLegacy(db); err != nil {
		return fmt.Errorf("failed to migrate from legacy system: %w", err)
	}

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

// migrateFromLegacy 从旧版迁移系统过渡到 gormigrate
// 如果检测到旧版 migration_records 表存在，将其记录迁移到新表
func migrateFromLegacy(db *gorm.DB) error {
	// 检查旧版迁移表是否存在
	if !db.Migrator().HasTable("migration_records") {
		// 旧表不存在，无需迁移
		return nil
	}

	// 检查是否已经迁移到 gormigrate（migrations 表存在且有记录）
	if db.Migrator().HasTable("migrations") {
		var count int64
		if err := db.Table("migrations").Count(&count).Error; err == nil && count > 0 {
			// gormigrate 表已存在且有记录，说明已经迁移过了
			return nil
		}
	}

	// 读取旧版迁移记录
	var legacyRecords []LegacyMigrationRecord
	if err := db.Find(&legacyRecords).Error; err != nil {
		// 可能表结构不同，忽略错误继续
		return nil
	}

	// 如果有旧版迁移记录，在 gormigrate 表中标记为已执行
	if len(legacyRecords) > 0 {
		// 确保 gormigrate 表存在
		if err := db.AutoMigrate(&MigrationRecord{}); err != nil {
			return fmt.Errorf("failed to create gormigrate table: %w", err)
		}

		// 将旧版迁移记录转换为 gormigrate 格式
		// 假设旧版 "202403260001" 对应新版的 "202504090002"（索引迁移）
		for _, record := range legacyRecords {
			newID := mapLegacyVersionToNew(record.Version)
			if newID != "" {
				newRecord := MigrationRecord{
					ID:        newID,
					AppliedAt: time.Unix(record.AppliedAt, 0),
				}
				// 使用忽略冲突的方式插入
				if err := db.Where("id = ?", newID).FirstOrCreate(&newRecord).Error; err != nil {
					return fmt.Errorf("failed to insert migration record %s: %w", newID, err)
				}
			}
		}
	}

	return nil
}

// mapLegacyVersionToNew 将旧版迁移版本号映射到新版
func mapLegacyVersionToNew(legacyVersion string) string {
	// 版本映射表
	versionMap := map[string]string{
		"202403260001": "202504090002", // 索引迁移
		// 可以在这里添加更多映射
	}

	if newVersion, ok := versionMap[legacyVersion]; ok {
		return newVersion
	}

	// 如果没有对应关系，返回空字符串表示忽略
	return ""
}

// GetCurrentVersion 获取当前迁移版本
func GetCurrentVersion(db *gorm.DB) (string, error) {
	var migration MigrationRecord
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

// MigrateRSSTimeout 设置现有 RSS 源的默认超时（向后兼容函数）
// Deprecated: 此函数保留用于向后兼容，新的迁移系统不再需要
func MigrateRSSTimeout(db *gorm.DB) error {
	// 此函数现在是一个空操作，因为新的迁移系统已经处理了这个问题
	// 保留此函数是为了不破坏可能调用它的现有测试
	return nil
}
