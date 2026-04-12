package ratelimit

import (
	"context"
	"sync"
	"time"
)

// TokenBucket 令牌桶限流器
type TokenBucket struct {
	rate       float64   // 每秒生成的令牌数
	capacity   int       // 桶的容量
	tokens     float64   // 当前令牌数
	lastUpdate time.Time // 上次更新时间
	mu         sync.Mutex
}

// NewTokenBucket 创建新的令牌桶
// rate: 每秒生成的令牌数
// capacity: 桶的容量
func NewTokenBucket(rate float64, capacity int) *TokenBucket {
	return &TokenBucket{
		rate:       rate,
		capacity:   capacity,
		tokens:     float64(capacity),
		lastUpdate: time.Now(),
	}
}

// Allow 尝试获取一个令牌
func (tb *TokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.updateTokens()

	if tb.tokens >= 1 {
		tb.tokens--
		return true
	}
	return false
}

// Wait 等待直到获取到令牌或上下文取消
func (tb *TokenBucket) Wait(ctx context.Context) error {
	for {
		if tb.Allow() {
			return nil
		}

		// 计算需要等待的时间
		tb.mu.Lock()
		waitTime := time.Duration((1 - tb.tokens) / tb.rate * float64(time.Second))
		tb.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
			continue
		}
	}
}

// updateTokens 更新当前令牌数
func (tb *TokenBucket) updateTokens() {
	now := time.Now()
	elapsed := now.Sub(tb.lastUpdate).Seconds()
	tb.tokens = min(tb.tokens+elapsed*tb.rate, float64(tb.capacity))
	tb.lastUpdate = now
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// RateLimiter 请求限流器
type RateLimiter struct {
	buckets map[string]*TokenBucket
	mu      sync.RWMutex
	// 默认配置
	defaultRate     float64
	defaultCapacity int
}

// NewRateLimiter 创建新的请求限流器
func NewRateLimiter(defaultRate float64, defaultCapacity int) *RateLimiter {
	return &RateLimiter{
		buckets:         make(map[string]*TokenBucket),
		defaultRate:     defaultRate,
		defaultCapacity: defaultCapacity,
	}
}

// GetBucket 获取指定 key 的令牌桶（如果不存在则创建）
func (rl *RateLimiter) GetBucket(key string) *TokenBucket {
	rl.mu.RLock()
	bucket, exists := rl.buckets[key]
	rl.mu.RUnlock()

	if exists {
		return bucket
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	// 双重检查
	if bucket, exists := rl.buckets[key]; exists {
		return bucket
	}

	bucket = NewTokenBucket(rl.defaultRate, rl.defaultCapacity)
	rl.buckets[key] = bucket
	return bucket
}

// Allow 尝试获取指定 key 的令牌
func (rl *RateLimiter) Allow(key string) bool {
	return rl.GetBucket(key).Allow()
}

// Wait 等待获取指定 key 的令牌
func (rl *RateLimiter) Wait(ctx context.Context, key string) error {
	return rl.GetBucket(key).Wait(ctx)
}

// Cleanup 清理长时间未使用的桶（可选的内存优化）
func (rl *RateLimiter) Cleanup(maxIdle time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// 由于我们使用了动态更新的令牌桶，没有记录最后使用时间
	// 如果需要清理功能，可以在 TokenBucket 中添加 lastAccess 字段
	// 这里作为示例，仅提供接口
}
