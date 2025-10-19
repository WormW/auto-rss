package logger

import (
	"encoding/json"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"go.uber.org/zap/zapcore"
	"gorm.io/gorm"
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
	// 解析日志JSON
	var logEntry map[string]interface{}
	if err := json.Unmarshal(p, &logEntry); err != nil {
		return len(p), nil // 解析失败不影响日志输出
	}

	// 提取字段
	level, _ := logEntry["level"].(string)
	msg, _ := logEntry["msg"].(string)

	// 移除已处理的字段,剩下的作为context
	delete(logEntry, "level")
	delete(logEntry, "msg")
	delete(logEntry, "ts")
	delete(logEntry, "caller")

	// 序列化context
	contextBytes, _ := json.Marshal(logEntry)

	// 创建日志记录
	log := &model.Log{
		Level:     level,
		Message:   msg,
		Context:   string(contextBytes),
		CreatedAt: time.Now(),
	}

	// 写入数据库 (异步,不阻塞)
	go func() {
		if err := w.db.Create(log).Error; err != nil {
			// 写入失败只打印到标准输出,不影响程序运行
		}
	}()

	return len(p), nil
}

// Sync 实现 zapcore.WriteSyncer 接口
func (w *DBWriter) Sync() error {
	return nil
}

var _ zapcore.WriteSyncer = (*DBWriter)(nil)
