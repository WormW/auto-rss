package repository

import (
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// setupTestDB 创建测试数据库
func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}

	// 自动迁移
	err = db.AutoMigrate(&model.Log{})
	if err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	return db
}

// seedTestLogs 添加测试日志数据
func seedTestLogs(t *testing.T, db *gorm.DB, count int, daysAgo []int) {
	for i := 0; i < count; i++ {
		days := 1
		if i < len(daysAgo) {
			days = daysAgo[i]
		}

		log := model.Log{
			Level:     "INFO",
			Module:    "TEST",
			Message:   "Test log message",
			CreatedAt: time.Now().AddDate(0, 0, -days),
		}
		err := db.Create(&log).Error
		assert.NoError(t, err)
	}
}

func TestLogRepository_DeleteBefore_Parameterized(t *testing.T) {
	db := setupTestDB(t)
	repo := NewLogRepository(db)

	tests := []struct {
		name          string
		days          int
		wantErr       bool
		errContains   string
		expectedCount int64
	}{
		{
			name:          "正常删除7天前的日志",
			days:          7,
			wantErr:       false,
			expectedCount: 3,
		},
		{
			name:        "负数天数应该返回错误",
			days:        -1,
			wantErr:     true,
			errContains: "days must be positive",
		},
		{
			name:        "零天数应该返回错误",
			days:        0,
			wantErr:     true,
			errContains: "days must be positive",
		},
		{
			name:          "正数参数使用参数化删除",
			days:          2,
			wantErr:       false,
			expectedCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 清理并准备数据
			db.Exec("DELETE FROM logs")
			seedTestLogs(t, db, 5, []int{1, 3, 5, 10, 20})

			err := repo.DeleteBefore(tt.days)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			assert.NoError(t, err)

			// 验证删除结果
			var count int64
			db.Model(&model.Log{}).Count(&count)
			assert.Equal(t, tt.expectedCount, count)
		})
	}
}

func TestLogRepository_DeleteBefore_SQLInjectionPrevention(t *testing.T) {
	db := setupTestDB(t)
	repo := NewLogRepository(db)

	// 准备测试数据 - 创建一些日志
	seedTestLogs(t, db, 3, []int{1, 5, 10})

	// 验证初始状态
	var initialCount int64
	db.Model(&model.Log{}).Count(&initialCount)
	assert.Equal(t, int64(3), initialCount, "初始应该有3条日志")

	// 正常删除5天前的日志
	err := repo.DeleteBefore(5)
	assert.NoError(t, err)

	// 验证只删除了符合条件的日志
	var afterCount int64
	db.Model(&model.Log{}).Count(&afterCount)
	assert.Equal(t, int64(1), afterCount, "应该只剩下1条最近1天的日志")
}

func TestLogRepository_DeleteBefore_EdgeCases(t *testing.T) {
	db := setupTestDB(t)
	repo := NewLogRepository(db)

	tests := []struct {
		name        string
		days        int
		wantErr     bool
		errContains string
	}{
		{
			name:    "大数值天数",
			days:    3650, // 10年
			wantErr: false,
		},
		{
			name:    "极大数值",
			days:    100000,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db.Exec("DELETE FROM logs")
			seedTestLogs(t, db, 2, []int{1, 100})

			err := repo.DeleteBefore(tt.days)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestLogRepository_List_WithFilters(t *testing.T) {
	db := setupTestDB(t)
	repo := NewLogRepository(db)

	// 准备测试数据
	logs := []model.Log{
		{Level: "INFO", Module: "RSS", Message: "RSS fetched"},
		{Level: "ERROR", Module: "DOWNLOAD", Message: "Download failed"},
		{Level: "WARN", Module: "RSS", Message: "RSS warning"},
		{Level: "INFO", Module: "SCHEDULER", Message: "Task scheduled"},
	}

	for _, log := range logs {
		err := db.Create(&log).Error
		assert.NoError(t, err)
	}

	tests := []struct {
		name          string
		page          int
		pageSize      int
		level         string
		module        string
		expectedLen   int
		expectedTotal int64
	}{
		{
			name:          "无过滤",
			page:          1,
			pageSize:      10,
			level:         "",
			module:        "",
			expectedLen:   4,
			expectedTotal: 4,
		},
		{
			name:          "按Level过滤",
			page:          1,
			pageSize:      10,
			level:         "INFO",
			module:        "",
			expectedLen:   2,
			expectedTotal: 2,
		},
		{
			name:          "按Module过滤",
			page:          1,
			pageSize:      10,
			level:         "",
			module:        "RSS",
			expectedLen:   2,
			expectedTotal: 2,
		},
		{
			name:          "组合过滤",
			page:          1,
			pageSize:      10,
			level:         "INFO",
			module:        "RSS",
			expectedLen:   1,
			expectedTotal: 1,
		},
		{
			name:          "分页",
			page:          1,
			pageSize:      2,
			level:         "",
			module:        "",
			expectedLen:   2,
			expectedTotal: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, total, err := repo.List(tt.page, tt.pageSize, tt.level, tt.module)
			assert.NoError(t, err)
			assert.Len(t, results, tt.expectedLen)
			assert.Equal(t, tt.expectedTotal, total)
		})
	}
}

func TestLogRepository_Create(t *testing.T) {
	db := setupTestDB(t)
	repo := NewLogRepository(db)

	log := &model.Log{
		Level:   "INFO",
		Module:  "TEST",
		Message: "Test message",
	}

	err := repo.Create(log)
	assert.NoError(t, err)
	assert.NotZero(t, log.ID)
	assert.WithinDuration(t, time.Now(), log.CreatedAt, time.Second)
}
