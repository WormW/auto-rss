package ratelimit

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Bucket 令牌桶限流器
type Bucket struct {
	limiter    *rate.Limiter
	burst      int
	lastAccess time.Time
	mu         sync.RWMutex
}

// NewBucket 创建新的令牌桶
// rps: 每秒请求数 (requests per second)
// burst: 突发请求数
func NewBucket(rps float64, burst int) *Bucket {
	return &Bucket{
		limiter:    rate.NewLimiter(rate.Limit(rps), burst),
		burst:      burst,
		lastAccess: time.Now(),
	}
}

// Allow 检查是否允许一个请求通过
// 更新最后访问时间，返回是否允许
func (b *Bucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastAccess = time.Now()
	return b.limiter.Allow()
}

// Tokens 返回当前桶中剩余的令牌数
func (b *Bucket) Tokens() float64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.limiter.Tokens()
}

// ResetTime 返回令牌桶重置时间（用于X-RateLimit-Reset头）
// 计算：当前时间 + 剩余令牌数 / 速率
func (b *Bucket) ResetTime() time.Time {
	b.mu.RLock()
	defer b.mu.RUnlock()
	tokens := b.limiter.Tokens()
	rateLimit := b.limiter.Limit()
	if rateLimit <= 0 {
		return time.Now().Add(time.Hour)
	}
	// 计算填满桶需要的时间
	seconds := float64(b.burst-int(tokens)) / float64(rateLimit)
	return time.Now().Add(time.Duration(seconds * float64(time.Second)))
}

// LastAccess 返回最后访问时间（用于TTL清理）
func (b *Bucket) LastAccess() time.Time {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.lastAccess
}
