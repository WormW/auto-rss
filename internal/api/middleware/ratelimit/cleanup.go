package ratelimit

import (
	"context"
	"time"

	"github.com/WormW/auto-rss/internal/pkg/logger"
)

// CleanupManager 管理后台清理任务
type CleanupManager struct {
	store    *Store
	interval time.Duration
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewCleanupManager 创建新的清理管理器
// store: 需要清理的存储
// interval: 清理间隔，如果为 0 则使用默认 5 分钟
func NewCleanupManager(store *Store, interval time.Duration) *CleanupManager {
	if interval <= 0 {
		interval = 5 * time.Minute
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &CleanupManager{
		store:    store,
		interval: interval,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Start 启动后台清理 goroutine
func (cm *CleanupManager) Start() {
	go cm.run()
	logger.Info("Rate limit cleanup started", "interval", cm.interval)
}

// Stop 停止后台清理 goroutine
func (cm *CleanupManager) Stop() {
	cm.cancel()
	logger.Info("Rate limit cleanup stopped")
}

// run 清理循环
func (cm *CleanupManager) run() {
	ticker := time.NewTicker(cm.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			removed := cm.store.Cleanup()
			if removed > 0 {
				logger.Debug("Rate limit cleanup completed", "removed", removed)
			}
		case <-cm.ctx.Done():
			return
		}
	}
}
