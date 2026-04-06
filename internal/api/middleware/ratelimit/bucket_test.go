package ratelimit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewBucket(t *testing.T) {
	b := NewBucket(10.0, 20)
	assert.NotNil(t, b)
	assert.Equal(t, 20.0, b.Tokens()) // 初始满桶
}

func TestBucketAllow(t *testing.T) {
	b := NewBucket(100.0, 1) // 高速率，burst=1

	// 第一个请求应该允许
	assert.True(t, b.Allow())

	// 第二个请求应该拒绝（burst=1）
	assert.False(t, b.Allow())
}

func TestBucketRefill(t *testing.T) {
	b := NewBucket(10.0, 1) // 10 req/s, burst=1

	// 消耗令牌
	assert.True(t, b.Allow())
	assert.False(t, b.Allow()) // 应该被拒绝

	// 等待100ms（应该获得1个令牌）
	time.Sleep(110 * time.Millisecond)
	assert.True(t, b.Allow()) // 现在应该允许
}

func TestBucketTokens(t *testing.T) {
	b := NewBucket(10.0, 10)

	// 初始应该是满的
	tokens := b.Tokens()
	assert.GreaterOrEqual(t, tokens, 9.0)
	assert.LessOrEqual(t, tokens, 10.0)

	// 消耗一些令牌
	b.Allow()
	b.Allow()
	b.Allow()

	// 应该减少
	newTokens := b.Tokens()
	assert.Less(t, newTokens, tokens)
}

func TestBucketResetTime(t *testing.T) {
	b := NewBucket(10.0, 10)

	// 消耗所有令牌
	for i := 0; i < 10; i++ {
		b.Allow()
	}

	resetTime := b.ResetTime()
	now := time.Now()

	// 重置时间应该在现在之后
	assert.True(t, resetTime.After(now))
	// 重置时间应该在1秒内（10个令牌，10 req/s = 1秒）
	assert.True(t, resetTime.Before(now.Add(2*time.Second)))
}

func TestBucketLastAccess(t *testing.T) {
	before := time.Now()
	b := NewBucket(10.0, 10)

	// 触发Allow更新lastAccess
	b.Allow()

	after := time.Now()
	lastAccess := b.LastAccess()

	assert.True(t, lastAccess.After(before) || lastAccess.Equal(before))
	assert.True(t, lastAccess.Before(after) || lastAccess.Equal(after))
}

func TestBucketConcurrency(t *testing.T) {
	b := NewBucket(1000.0, 100)

	// 并发请求
	allowed := make(chan bool, 100)
	for i := 0; i < 100; i++ {
		go func() {
			allowed <- b.Allow()
		}()
	}

	// 收集结果
	allowCount := 0
	for i := 0; i < 100; i++ {
		if <-allowed {
			allowCount++
		}
	}

	// 最多允许burst个
	assert.LessOrEqual(t, allowCount, 100)
	assert.Greater(t, allowCount, 0)
}
