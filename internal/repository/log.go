package repository

import (
	"github.com/WormW/auto-rss/internal/model"
	"gorm.io/gorm"
)

// LogRepository 日志仓储接口
type LogRepository interface {
	Create(log *model.Log) error
	List(page, pageSize int, level, module string) ([]*model.Log, int64, error)
	DeleteBefore(before int) error
}

type logRepository struct {
	db *gorm.DB
}

func NewLogRepository(db *gorm.DB) LogRepository {
	return &logRepository{db: db}
}

func (r *logRepository) Create(log *model.Log) error {
	return r.db.Create(log).Error
}

func (r *logRepository) List(page, pageSize int, level, module string) ([]*model.Log, int64, error) {
	var logs []*model.Log
	var total int64

	query := r.db.Model(&model.Log{})

	if level != "" {
		query = query.Where("level = ?", level)
	}
	if module != "" {
		query = query.Where("module = ?", module)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	if err := query.Order("created_at DESC").
		Limit(pageSize).
		Offset(offset).
		Find(&logs).Error; err != nil {
		return nil, 0, err
	}

	return logs, total, nil
}

func (r *logRepository) DeleteBefore(days int) error {
	return r.db.Where("created_at < datetime('now', '-' || ? || ' days')", days).
		Delete(&model.Log{}).Error
}
