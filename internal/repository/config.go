package repository

import (
	"sync"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"gorm.io/gorm"
)

// ConfigRepository 配置仓储接口
type ConfigRepository interface {
	Get(key string) (*model.Config, error)
	GetCached(key string) (string, error)
	Set(key, value string) error
	Delete(key string) error
	GetAll() ([]model.Config, error)
}

// CachedConfigRepository 带缓存的配置仓储接口
type CachedConfigRepository interface {
	ConfigRepository
	RefreshCache() error
}

type configRepository struct {
	db    *gorm.DB
	cache map[string]string
	mu    sync.RWMutex
	ttl   time.Duration
}

// NewConfigRepository 创建配置仓储实例
func NewConfigRepository(db *gorm.DB) ConfigRepository {
	return &configRepository{
		db:    db,
		cache: make(map[string]string),
		ttl:   1 * time.Minute, // 默认 TTL 1 分钟
	}
}

// NewConfigRepositoryWithCache 创建带缓存的配置仓储实例
func NewConfigRepositoryWithCache(db *gorm.DB, ttl time.Duration) CachedConfigRepository {
	if ttl <= 0 {
		ttl = 1 * time.Minute
	}
	return &configRepository{
		db:    db,
		cache: make(map[string]string),
		ttl:   ttl,
	}
}

// Get 获取配置值（直接从数据库）
func (r *configRepository) Get(key string) (*model.Config, error) {
	var config model.Config
	err := r.db.Where("key = ?", key).First(&config).Error
	if err != nil {
		return nil, err
	}
	return &config, nil
}

// GetCached 从缓存获取配置值
func (r *configRepository) GetCached(key string) (string, error) {
	// 先尝试从缓存读取
	r.mu.RLock()
	if value, ok := r.cache[key]; ok {
		r.mu.RUnlock()
		logger.Debug("Config cache hit", "key", key)
		return value, nil
	}
	r.mu.RUnlock()

	// 缓存未命中，从数据库加载
	logger.Debug("Config cache miss", "key", key)
	config, err := r.Get(key)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", nil
		}
		return "", err
	}

	// 更新缓存
	r.mu.Lock()
	r.cache[key] = config.Value
	r.mu.Unlock()

	return config.Value, nil
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
	} else if result.Error != nil {
		// 其他错误
		logger.Error("Failed to query config", "key", key, "error", result.Error)
		return result.Error
	} else {
		// 记录已存在，更新 value
		logger.Debug("Updating existing config record", "key", key, "old_value", config.Value, "new_value", value, "id", config.ID)
		if err := r.db.Model(&config).Update("value", value).Error; err != nil {
			logger.Error("Failed to update config", "key", key, "error", err)
			return err
		}
		logger.Info("Config updated successfully", "key", key, "id", config.ID)
	}

	// 更新缓存
	r.mu.Lock()
	r.cache[key] = value
	r.mu.Unlock()
	logger.Debug("Config cache updated", "key", key)

	return nil
}

// Delete 删除配置
func (r *configRepository) Delete(key string) error {
	// 删除数据库记录
	err := r.db.Where("key = ?", key).Delete(&model.Config{}).Error
	if err != nil {
		return err
	}

	// 从缓存中删除
	r.mu.Lock()
	delete(r.cache, key)
	r.mu.Unlock()
	logger.Debug("Config cache deleted", "key", key)

	return nil
}

// GetAll 获取所有配置
func (r *configRepository) GetAll() ([]model.Config, error) {
	var configs []model.Config
	err := r.db.Find(&configs).Error
	return configs, err
}

// RefreshCache 强制刷新缓存
func (r *configRepository) RefreshCache() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 重新加载所有配置
	var configs []model.Config
	if err := r.db.Find(&configs).Error; err != nil {
		logger.Error("Failed to refresh config cache", "error", err)
		return err
	}

	// 清空并重建缓存
	r.cache = make(map[string]string, len(configs))
	for _, cfg := range configs {
		r.cache[cfg.Key] = cfg.Value
	}

	logger.Info("Config cache refreshed", "count", len(configs))
	return nil
}
