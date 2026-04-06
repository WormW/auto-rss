package ratelimit

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Bucket 令牌桶限流器
type Bucket struct {
	limiter    *rate.Limiter
	lastAccess time.Time
	mu         sync.RWMutex
	limit      rate.Limit
	burst      int
}

// NewBucket 创建新的令牌桶
// rps: 每秒请求数 (requests per second)
// burst: 突发请求容量
func NewBucket(rps float64, burst int) *Bucket {
	return &Bucket{
		limiter:    rate.NewLimiter(rate.Limit(rps), burst),
		lastAccess: time.Now(),
		limit:      rate.Limit(rps),
		burst:      burst,
	}
}

// Allow 尝试获取一个令牌
// 返回 true 表示允许请求，false 表示被限流
func (b *Bucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.lastAccess = time.Now()
	return b.limiter.Allow()
}

// Tokens 返回当前可用的令牌数
func (b *Bucket) Tokens() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return int(b.limiter.Tokens())
}

// ResetTime 计算下一次令牌可用的时间
func (b *Bucket) ResetTime() time.Time {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// 如果没有令牌了，计算下次可用时间
	if b.limiter.Tokens() < 1 {
		// 等待一个令牌的时间 = 1 / limit
		waitTime := time.Duration(float64(time.Second) / float64(b.limit))
		return time.Now().Add(waitTime)
	}
	return time.Now()
}

// LastAccess 返回上次访问时间
func (b *Bucket) LastAccess() time.Time {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.lastAccess
}

// IsExpired 检查是否超过 TTL 未访问
func (b *Bucket) IsExpired(ttl time.Duration) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return time.Since(b.lastAccess) > ttl
}
