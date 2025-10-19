package repository

import (
	"github.com/WormW/auto-rss/internal/model"
	"gorm.io/gorm"
)

// RSSSourceRepository RSS源数据访问接口
type RSSSourceRepository interface {
	Create(source *model.RSSSource) error
	Update(source *model.RSSSource) error
	Delete(id uint) error
	FindByID(id uint) (*model.RSSSource, error)
	List(page, pageSize int, enabled *bool) ([]model.RSSSource, int64, error)
	FindByName(name string) (*model.RSSSource, error)
}

type rssSourceRepository struct {
	db *gorm.DB
}

// NewRSSSourceRepository 创建RSS源仓库实例
func NewRSSSourceRepository(db *gorm.DB) RSSSourceRepository {
	return &rssSourceRepository{db: db}
}

func (r *rssSourceRepository) Create(source *model.RSSSource) error {
	return r.db.Create(source).Error
}

func (r *rssSourceRepository) Update(source *model.RSSSource) error {
	return r.db.Save(source).Error
}

func (r *rssSourceRepository) Delete(id uint) error {
	return r.db.Delete(&model.RSSSource{}, id).Error
}

func (r *rssSourceRepository) FindByID(id uint) (*model.RSSSource, error) {
	var source model.RSSSource
	err := r.db.First(&source, id).Error
	if err != nil {
		return nil, err
	}
	return &source, nil
}

func (r *rssSourceRepository) List(page, pageSize int, enabled *bool) ([]model.RSSSource, int64, error) {
	var sources []model.RSSSource
	var total int64

	query := r.db.Model(&model.RSSSource{})

	// 按启用状态过滤
	if enabled != nil {
		query = query.Where("enabled = ?", *enabled)
	}

	// 获取总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询
	offset := (page - 1) * pageSize
	err := query.Order("created_at DESC").
		Offset(offset).
		Limit(pageSize).
		Find(&sources).Error

	return sources, total, err
}

func (r *rssSourceRepository) FindByName(name string) (*model.RSSSource, error) {
	var source model.RSSSource
	err := r.db.Where("name = ?", name).First(&source).Error
	if err != nil {
		return nil, err
	}
	return &source, nil
}
