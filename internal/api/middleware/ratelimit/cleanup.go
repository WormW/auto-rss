package ratelimit

import (
	"context"
	"sync"
	"time"

	"github.com/WormW/auto-rss/internal/pkg/logger"
)

// CleanupManager 后台清理管理器
type CleanupManager struct {
	store    *Store
	interval time.Duration
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	mu       sync.RWMutex
	running  bool
}

// NewCleanupManager 创建新的清理管理器
// store: 要清理的存储
// interval: 清理间隔（建议5分钟）
func NewCleanupManager(store *Store, interval time.Duration) *CleanupManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &CleanupManager{
		store:    store,
		interval: interval,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Start 启动后台清理goroutine
func (cm *CleanupManager) Start() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.running {
		return
	}

	cm.running = true
	cm.wg.Add(1)

	go func() {
		defer cm.wg.Done()
		ticker := time.NewTicker(cm.interval)
		defer ticker.Stop()

		logger.Info("Rate limit cleanup manager started",
			"interval", cm.interval,
		)

		for {
			select {
			case <-cm.ctx.Done():
				logger.Info("Rate limit cleanup manager stopped")
				return
			case <-ticker.C:
				removed := cm.store.Cleanup()
				if removed > 0 {
					entries, max, ttl := cm.store.Stats()
					logger.Debug("Rate limit cleanup completed",
						"removed", removed,
						"remaining", entries,
						"max", max,
						"ttl", ttl,
					)
				}
			}
		}
	}()
}

// Stop 停止后台清理goroutine
func (cm *CleanupManager) Stop() {
	cm.mu.Lock()
	if !cm.running {
		cm.mu.Unlock()
		return
	}
	cm.running = false
	cm.mu.Unlock()

	cm.cancel()
	cm.wg.Wait()
}

// IsRunning 返回清理管理器是否正在运行
func (cm *CleanupManager) IsRunning() bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.running
}
