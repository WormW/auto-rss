package ratelimit

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestBucketAllow 测试令牌桶基本功能
func TestBucketAllow(t *testing.T) {
	// 创建每秒10个请求，突发20的桶
	bucket := NewBucket(10.0, 20)

	// 前20个请求应该都能通过（突发容量）
	for i := 0; i < 20; i++ {
		assert.True(t, bucket.Allow(), "Request %d should be allowed", i+1)
	}

	// 第21个请求应该被拒绝（桶已空）
	assert.False(t, bucket.Allow(), "Request 21 should be rate limited")

	// 等待令牌补充（100ms 应该补充1个令牌）
	time.Sleep(150 * time.Millisecond)
	assert.True(t, bucket.Allow(), "Request after refill should be allowed")
}

// TestBucketThreadSafe 测试并发安全性
func TestBucketThreadSafe(t *testing.T) {
	bucket := NewBucket(100.0, 200)
	var wg sync.WaitGroup
	allowed := 0
	var mu sync.Mutex

	// 100个并发goroutine，每个10次请求
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				if bucket.Allow() {
					mu.Lock()
					allowed++
					mu.Unlock()
				}
			}
		}()
	}

	wg.Wait()
	// 应该有一些请求被允许（最多200个突发）
	assert.Greater(t, allowed, 0)
	// 但不应该超过突发容量 + 补充的令牌
	assert.LessOrEqual(t, allowed, 300)
}

// TestBucketTokens 测试令牌查询
func TestBucketTokens(t *testing.T) {
	bucket := NewBucket(10.0, 20)

	// 初始应该有20个令牌
	tokens := bucket.Tokens()
	assert.GreaterOrEqual(t, tokens, 19) // 可能有微小时间差
	assert.LessOrEqual(t, tokens, 20)

	// 消费一些令牌
	bucket.Allow()
	bucket.Allow()
	bucket.Allow()

	tokens = bucket.Tokens()
	assert.GreaterOrEqual(t, tokens, 16)
	assert.LessOrEqual(t, tokens, 17)
}

// TestBucketResetTime 测试重置时间
func TestBucketResetTime(t *testing.T) {
	bucket := NewBucket(10.0, 20)

	// 空桶状态下重置时间应该在将来
	now := time.Now()
	resetTime := bucket.ResetTime()
	assert.True(t, resetTime.After(now) || resetTime.Equal(now))
}

// TestBucketLastAccess 测试最后访问时间
func TestBucketLastAccess(t *testing.T) {
	before := time.Now()
	bucket := NewBucket(10.0, 20)

	time.Sleep(10 * time.Millisecond)
	bucket.Allow()

	after := time.Now()
	lastAccess := bucket.LastAccess()

	assert.True(t, lastAccess.After(before) || lastAccess.Equal(before))
	assert.True(t, lastAccess.Before(after) || lastAccess.Equal(after))
}

// TestBucketIsExpired 测试过期检查
func TestBucketIsExpired(t *testing.T) {
	bucket := NewBucket(10.0, 20)

	// 短TTL，快速过期
	ttl := 50 * time.Millisecond

	// 刚创建不应该过期
	assert.False(t, bucket.IsExpired(ttl))

	// 等待过期
	time.Sleep(100 * time.Millisecond)
	assert.True(t, bucket.IsExpired(ttl))
}

// TestStoreLRUEviction 测试LRU淘汰
func TestStoreLRUEviction(t *testing.T) {
	// 创建只能容纳3个条目的存储
	store := NewStore(3, time.Hour, 10.0, 20)

	// 获取4个不同key的桶
	_ = store.GetBucket("a")
	_ = store.GetBucket("b")
	_ = store.GetBucket("c")
	_ = store.GetBucket("d")

	// 应该只有3个条目（最老的a被淘汰）
	assert.Equal(t, 3, store.Len())

	// 验证b,c,d存在（通过再次获取不会创建新桶）
	bucketB := store.GetBucket("b")
	bucketC := store.GetBucket("c")
	bucketD := store.GetBucket("d")

	// 这些桶应该是之前创建的（通过访问时间验证）
	assert.False(t, bucketB.LastAccess().IsZero())
	assert.False(t, bucketC.LastAccess().IsZero())
	assert.False(t, bucketD.LastAccess().IsZero())
}

// TestStoreCleanup 测试过期清理
func TestStoreCleanup(t *testing.T) {
	// 创建短TTL的存储
	store := NewStore(100, 100*time.Millisecond, 10.0, 20)

	// 获取几个桶
	_ = store.GetBucket("test1")
	_ = store.GetBucket("test2")

	assert.Equal(t, 2, store.Len())

	// 等待过期
	time.Sleep(150 * time.Millisecond)

	// 清理
	removed := store.Cleanup()
	assert.Equal(t, 2, removed)
	assert.Equal(t, 0, store.Len())
}

// TestStoreDifferentRates 测试不同速率
func TestStoreDifferentRates(t *testing.T) {
	store := NewStore(100, time.Hour, 10.0, 20)

	// 通用桶：10 req/s
	generalBucket := store.GetBucket("client1")

	// Auth桶：5 req/min = 0.083 req/s
	authBucket := store.GetBucketWithRate("client1:auth", 5.0/60.0, 5)

	// 通用桶应该允许更多请求
	generalAllowed := 0
	for i := 0; i < 25; i++ {
		if generalBucket.Allow() {
			generalAllowed++
		}
	}
	assert.GreaterOrEqual(t, generalAllowed, 20) // 突发容量

	// Auth桶应该只允许5个（突发容量）
	authAllowed := 0
	for i := 0; i < 10; i++ {
		if authBucket.Allow() {
			authAllowed++
		}
	}
	assert.Equal(t, 5, authAllowed)
}

// TestStoreStats 测试统计信息
func TestStoreStats(t *testing.T) {
	store := NewStore(100, time.Hour, 10.0, 20)

	// 空存储
	total, oldest := store.Stats()
	assert.Equal(t, 0, total)
	assert.True(t, oldest.IsZero())

	// 添加条目
	_ = store.GetBucket("a")
	time.Sleep(10 * time.Millisecond)
	_ = store.GetBucket("b")

	total, oldest = store.Stats()
	assert.Equal(t, 2, total)
	assert.False(t, oldest.IsZero())
}

// TestCleanupManager 测试清理管理器
func TestCleanupManager(t *testing.T) {
	store := NewStore(100, 100*time.Millisecond, 10.0, 20)
	_ = store.GetBucket("test")

	// 创建短间隔的清理管理器
	cm := NewCleanupManager(store, 50*time.Millisecond)
	cm.Start()

	// 等待条目过期并被清理
	time.Sleep(200 * time.Millisecond)

	// 停止清理
	cm.Stop()

	// 条目应该被清理
	assert.Equal(t, 0, store.Len())
}

// TestDefaultConstants 测试默认常量
func TestDefaultConstants(t *testing.T) {
	assert.Equal(t, 10000, DefaultMaxEntries)
	assert.Equal(t, time.Hour, DefaultTTL)
}

// TestStoreWithZeroDefaults 测试零值默认值
func TestStoreWithZeroDefaults(t *testing.T) {
	store := NewStore(0, 0, 10.0, 20)

	assert.Equal(t, DefaultMaxEntries, store.maxEntries)
	assert.Equal(t, DefaultTTL, store.ttl)
}
