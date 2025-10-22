package repository

import (
	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
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
	// 先尝试查找已存在的记录
	var config model.Config
	result := r.db.Where("key = ?", key).First(&config)

	if result.Error == gorm.ErrRecordNotFound {
		// 记录不存在，创建新记录
		logger.Debug("Creating new config record", "key", key, "value", value)
		config = model.Config{
			Key:   key,
			Value: value,
		}
		if err := r.db.Create(&config).Error; err != nil {
			logger.Error("Failed to create config", "key", key, "error", err)
			return err
		}
		logger.Info("Config created successfully", "key", key, "id", config.ID)
		return nil
	} else if result.Error != nil {
		// 其他错误
		logger.Error("Failed to query config", "key", key, "error", result.Error)
		return result.Error
	}

	// 记录已存在，更新 value
	logger.Debug("Updating existing config record", "key", key, "old_value", config.Value, "new_value", value, "id", config.ID)
	if err := r.db.Model(&config).Update("value", value).Error; err != nil {
		logger.Error("Failed to update config", "key", key, "error", err)
		return err
	}
	logger.Info("Config updated successfully", "key", key, "id", config.ID)
	return nil
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
