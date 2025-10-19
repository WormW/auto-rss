package repository

import (
	"github.com/WormW/auto-rss/internal/model"
	"gorm.io/gorm"
)

// LogRepository 日志仓储接口
type LogRepository interface {
	Create(log *model.Log) error
	List(page, pageSize int, level string) ([]*model.Log, int64, error)
	DeleteBefore(before int) error // 删除N天前的日志
}

// logRepository 日志仓储实现
type logRepository struct {
	db *gorm.DB
}

// NewLogRepository 创建日志仓储实例
func NewLogRepository(db *gorm.DB) LogRepository {
	return &logRepository{db: db}
}

// Create 创建日志
func (r *logRepository) Create(log *model.Log) error {
	return r.db.Create(log).Error
}

// List 查询日志列表
func (r *logRepository) List(page, pageSize int, level string) ([]*model.Log, int64, error) {
	var logs []*model.Log
	var total int64

	query := r.db.Model(&model.Log{})

	// 按级别过滤
	if level != "" {
		query = query.Where("level = ?", level)
	}

	// 计算总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询
	offset := (page - 1) * pageSize
	if err := query.Order("created_at DESC").
		Limit(pageSize).
		Offset(offset).
		Find(&logs).Error; err != nil {
		return nil, 0, err
	}

	return logs, total, nil
}

// DeleteBefore 删除N天前的日志
func (r *logRepository) DeleteBefore(days int) error {
	// 删除创建时间在days天之前的日志
	return r.db.Where("created_at < datetime('now', '-' || ? || ' days')", days).
		Delete(&model.Log{}).Error
}
