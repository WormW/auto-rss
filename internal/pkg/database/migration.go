package database

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// MigrationRecord 迁移记录表
// 用于跟踪已执行的迁移，确保幂等性
type MigrationRecord struct {
	ID        uint   `gorm:"primaryKey"`
	Version   string `gorm:"uniqueIndex;not null"`
	AppliedAt int64  `gorm:"not null"`
}

// TableName 指定表名
func (MigrationRecord) TableName() string {
	return "migration_records"
}

// Migration 迁移函数类型
type Migration func(*gorm.DB) error

// migrations 注册所有迁移
var migrations = []struct {
	version string
	name    string
	fn      Migration
}{
	{"202403260001", "create_indexes", createIndexes},
}

// RunMigrations 执行所有未执行的迁移
func RunMigrations(db *gorm.DB) error {
	// 创建迁移记录表
	if err := db.AutoMigrate(&MigrationRecord{}); err != nil {
		return fmt.Errorf("failed to create migration_records table: %w", err)
	}

	// 执行每个迁移
	for _, m := range migrations {
		var record MigrationRecord
		result := db.Where("version = ?", m.version).First(&record)

		if result.Error == nil {
			// 已执行过，跳过
			continue
		}

		if result.Error != nil && result.Error != gorm.ErrRecordNotFound {
			return fmt.Errorf("failed to check migration %s: %w", m.version, result.Error)
		}

		// 执行迁移
		if err := m.fn(db); err != nil {
			return fmt.Errorf("failed to run migration %s (%s): %w", m.version, m.name, err)
		}

		// 记录迁移
		record = MigrationRecord{
			Version:   m.version,
			AppliedAt: timeNow(),
		}
		if err := db.Create(&record).Error; err != nil {
			return fmt.Errorf("failed to record migration %s: %w", m.version, err)
		}
	}

	return nil
}

// createIndexes 创建数据库索引
// 注意：SQLite 和 GORM 支持通过 AutoMigrate 自动创建索引
// 这里主要用于显式创建一些复杂的索引或处理兼容性问题
func createIndexes(db *gorm.DB) error {
	// GORM 的 AutoMigrate 会自动创建模型中定义的索引
	// 这里可以添加一些自定义的 SQL 索引创建（如果需要）
	
	// 示例：如果某些索引需要特殊处理，可以在这里添加
	// 例如：CREATE INDEX IF NOT EXISTS ...
	
	return nil
}

// timeNow 返回当前时间戳（秒）
func timeNow() int64 {
	return time.Now().Unix()
}
