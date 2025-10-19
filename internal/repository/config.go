package repository

import (
	"github.com/WormW/auto-rss/internal/model"
	"gorm.io/gorm"
)

// ConfigRepository 配置仓储接口
type ConfigRepository interface {
	Get(key string) (*model.Config, error)
	Set(key, value string) error
	Delete(key string) error
	GetAll() ([]model.Config, error)
}

type configRepository struct {
	db *gorm.DB
}

// NewConfigRepository 创建配置仓储实例
func NewConfigRepository(db *gorm.DB) ConfigRepository {
	return &configRepository{db: db}
}

// Get 获取配置值
func (r *configRepository) Get(key string) (*model.Config, error) {
	var config model.Config
	err := r.db.Where("key = ?", key).First(&config).Error
	if err != nil {
		return nil, err
	}
	return &config, nil
}

// Set 设置配置值
func (r *configRepository) Set(key, value string) error {
	config := &model.Config{
		Key:   key,
		Value: value,
	}
	return r.db.Where("key = ?", key).Assign(config).FirstOrCreate(config).Error
}

// Delete 删除配置
func (r *configRepository) Delete(key string) error {
	return r.db.Where("key = ?", key).Delete(&model.Config{}).Error
}

// GetAll 获取所有配置
func (r *configRepository) GetAll() ([]model.Config, error) {
	var configs []model.Config
	err := r.db.Find(&configs).Error
	return configs, err
}
