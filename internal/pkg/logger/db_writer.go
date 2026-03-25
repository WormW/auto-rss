package logger

import (
	"encoding/json"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"go.uber.org/zap/zapcore"
	"gorm.io/gorm"
)

// 日志清理配置
const (
	maxLogCount   = 10000  // 最大日志条数
	cleanupBatch  = 2000   // 每次清理的条数
	retentionDays = 30     // 保留天数
)

// DBWriter 数据库日志写入器
type DBWriter struct {
	db *gorm.DB
}

// NewDBWriter 创建数据库写入器
func NewDBWriter(db *gorm.DB) *DBWriter {
	return &DBWriter{db: db}
}

// Write 实现 io.Writer 接口
func (w *DBWriter) Write(p []byte) (n int, err error) {
	var logEntry map[string]interface{}
	if err := json.Unmarshal(p, &logEntry); err != nil {
		return len(p), nil
	}

	level, _ := logEntry["level"].(string)
	msg, _ := logEntry["msg"].(string)
	caller, _ := logEntry["caller"].(string)

	// 从 caller 路径推断模块
	module := inferModule(caller)

	// 移除已处理的字段
	delete(logEntry, "level")
	delete(logEntry, "msg")
	delete(logEntry, "ts")
	delete(logEntry, "caller")

	contextBytes, _ := json.Marshal(logEntry)

	log := &model.Log{
		Level:     level,
		Module:    module,
		Message:   msg,
		Context:   string(contextBytes),
		CreatedAt: time.Now(),
	}

	go func() {
		_ = w.db.Create(log).Error
		// 异步执行日志清理
		w.cleanupOldLogs()
	}()

	return len(p), nil
}

// inferModule 从 caller 路径推断模块名
func inferModule(caller string) string {
	switch {
	case contains(caller, "scheduler"):
		return "rss"
	case contains(caller, "downloader"), contains(caller, "download"):
		return "download"
	case contains(caller, "organizer"):
		return "organizer"
	case contains(caller, "bangumi"):
		return "bangumi"
	case contains(caller, "subscription"):
		return "subscription"
	case contains(caller, "config"):
		return "config"
	default:
		return "system"
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsImpl(s, substr))
}

func containsImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// cleanupOldLogs 清理旧日志
// 策略：
// 1. 当日志总数超过 maxLogCount 时，删除最旧的 cleanupBatch 条
// 2. 删除超过 retentionDays 天的日志
func (w *DBWriter) cleanupOldLogs() {
	// 检查总条数
	var count int64
	if err := w.db.Model(&model.Log{}).Count(&count).Error; err != nil {
		return
	}

	// 如果超过最大条数，删除最旧的一批
	if count > maxLogCount {
		// 查找最旧的 cleanupBatch 条记录的 ID
		var ids []uint
		w.db.Model(&model.Log{}).
			Order("created_at ASC").
			Limit(cleanupBatch).
			Pluck("id", &ids)

		if len(ids) > 0 {
			w.db.Delete(&model.Log{}, ids)
		}
	}

	// 删除超过保留期限的日志
	cutoffTime := time.Now().AddDate(0, 0, -retentionDays)
	w.db.Where("created_at < ?", cutoffTime).Delete(&model.Log{})
}

// Sync 实现 zapcore.WriteSyncer 接口
func (w *DBWriter) Sync() error {
	return nil
}

var _ zapcore.WriteSyncer = (*DBWriter)(nil)
