package config

import (
	"sync"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"gorm.io/gorm"
)

// ConfigStore 配置存储接口
type ConfigStore interface {
	Get(key string) (string, error)
	Set(key, value string) error
	Refresh() error
}

// CachedConfigStore 带缓存的配置存储
type CachedConfigStore struct {
	db          *gorm.DB
	cache       map[string]string
	mu          sync.RWMutex
	lastRefresh time.Time
	ttl         time.Duration
}

// NewCachedConfigStore 创建带缓存的配置存储实例
func NewCachedConfigStore(db *gorm.DB, ttl time.Duration) *CachedConfigStore {
	if ttl <= 0 {
		ttl = 1 * time.Minute // 默认 TTL 1 分钟
	}
	return &CachedConfigStore{
		db:    db,
		cache: make(map[string]string),
		ttl:   ttl,
	}
}

// Get 从缓存获取配置值，如果缓存未命中或过期则从数据库加载
func (s *CachedConfigStore) Get(key string) (string, error) {
	// 先尝试从缓存读取
	s.mu.RLock()
	if value, ok := s.cache[key]; ok {
		// 检查是否过期
		if time.Since(s.lastRefresh) < s.ttl {
			s.mu.RUnlock()
			logger.Debug("Config cache hit", "key", key)
			return value, nil
		}
	}
	s.mu.RUnlock()

	// 缓存未命中或已过期，从数据库加载
	logger.Debug("Config cache miss, loading from DB", "key", key)
	return s.loadFromDB(key)
}

// Set 设置配置值并更新缓存
func (s *CachedConfigStore) Set(key, value string) error {
	// 先更新数据库
	var config model.Config
	result := s.db.Where("key = ?", key).First(&config)

	if result.Error == gorm.ErrRecordNotFound {
		// 记录不存在，创建新记录
		config = model.Config{
			Key:   key,
			Value: value,
		}
		if err := s.db.Create(&config).Error; err != nil {
			logger.Error("Failed to create config", "key", key, "error", err)
			return err
		}
		logger.Debug("Config created in DB", "key", key)
	} else if result.Error != nil {
		logger.Error("Failed to query config", "key", key, "error", result.Error)
		return result.Error
	} else {
		// 记录已存在，更新 value
		if err := s.db.Model(&config).Update("value", value).Error; err != nil {
			logger.Error("Failed to update config", "key", key, "error", err)
			return err
		}
		logger.Debug("Config updated in DB", "key", key)
	}

	// 更新缓存
	s.mu.Lock()
	s.cache[key] = value
	s.mu.Unlock()

	logger.Debug("Config cache updated after Set", "key", key)
	return nil
}

// Refresh 强制刷新缓存
func (s *CachedConfigStore) Refresh() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 清空缓存
	s.cache = make(map[string]string)
	s.lastRefresh = time.Time{}

	// 重新加载所有配置
	var configs []model.Config
	if err := s.db.Find(&configs).Error; err != nil {
		logger.Error("Failed to refresh config cache", "error", err)
		return err
	}

	for _, cfg := range configs {
		s.cache[cfg.Key] = cfg.Value
	}
	s.lastRefresh = time.Now()

	logger.Info("Config cache refreshed", "count", len(configs))
	return nil
}

// loadFromDB 从数据库加载单个配置
func (s *CachedConfigStore) loadFromDB(key string) (string, error) {
	var config model.Config
	err := s.db.Where("key = ?", key).First(&config).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", nil
		}
		logger.Error("Failed to load config from DB", "key", key, "error", err)
		return "", err
	}

	// 检查是否需要全量刷新缓存
	s.mu.Lock()
	if time.Since(s.lastRefresh) >= s.ttl {
		// 异步刷新整个缓存
		go s.refreshCache()
	}
	s.cache[key] = config.Value
	s.mu.Unlock()

	return config.Value, nil
}

// refreshCache 刷新整个缓存（内部方法，需要在外部已经加锁的情况下调用）
func (s *CachedConfigStore) refreshCache() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 再次检查是否仍然需要刷新
	if time.Since(s.lastRefresh) < s.ttl {
		return
	}

	var configs []model.Config
	if err := s.db.Find(&configs).Error; err != nil {
		logger.Error("Failed to refresh config cache", "error", err)
		return
	}

	newCache := make(map[string]string, len(configs))
	for _, cfg := range configs {
		newCache[cfg.Key] = cfg.Value
	}
	s.cache = newCache
	s.lastRefresh = time.Now()

	logger.Debug("Config cache background refreshed", "count", len(configs))
}

// Invalidate 使缓存中的特定 key 失效
func (s *CachedConfigStore) Invalidate(key string) {
	s.mu.Lock()
	delete(s.cache, key)
	s.mu.Unlock()
	logger.Debug("Config cache invalidated", "key", key)
}

// InvalidateAll 使整个缓存失效
func (s *CachedConfigStore) InvalidateAll() {
	s.mu.Lock()
	s.cache = make(map[string]string)
	s.lastRefresh = time.Time{}
	s.mu.Unlock()
	logger.Debug("Config cache fully invalidated")
}
