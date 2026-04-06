package ratelimit

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewStore(t *testing.T) {
	s := NewStore(100, time.Hour, 10.0, 20)
	assert.NotNil(t, s)
	assert.Equal(t, 0, s.Len())

	entries, max, ttl := s.Stats()
	assert.Equal(t, 0, entries)
	assert.Equal(t, 100, max)
	assert.Equal(t, time.Hour, ttl)
}

func TestStoreGetBucket(t *testing.T) {
	s := NewStore(100, time.Hour, 10.0, 20)

	// 获取新桶
	b1 := s.GetBucket("ip1")
	assert.NotNil(t, b1)

	// 再次获取应该返回同一个桶
	b2 := s.GetBucket("ip1")
	assert.Equal(t, b1, b2)

	// 不同key应该返回不同桶
	b3 := s.GetBucket("ip2")
	assert.NotEqual(t, b1, b3)
}

func TestStoreGetBucketWithRate(t *testing.T) {
	s := NewStore(100, time.Hour, 10.0, 20)

	// 使用自定义速率
	b := s.GetBucketWithRate("ip1", 5.0, 10)
	assert.NotNil(t, b)

	// 应该能存储10个（burst）
	assert.Equal(t, 10.0, b.Tokens())
}

func TestStoreLRUEviction(t *testing.T) {
	s := NewStore(3, time.Hour, 10.0, 20) // 最多3个条目

	// 创建3个桶
	s.GetBucket("ip1")
	s.GetBucket("ip2")
	s.GetBucket("ip3")
	assert.Equal(t, 3, s.Len())

	// 访问ip1使其变为最近使用
	s.GetBucket("ip1")

	// 创建第4个，应该淘汰最久未使用的(ip2)
	s.GetBucket("ip4")
	assert.Equal(t, 3, s.Len())

	// ip1应该还在
	b1 := s.GetBucket("ip1")
	assert.NotNil(t, b1)
}

func TestStoreCleanup(t *testing.T) {
	ttl := 100 * time.Millisecond
	s := NewStore(100, ttl, 10.0, 20)

	// 创建桶
	s.GetBucket("ip1")
	s.GetBucket("ip2")
	assert.Equal(t, 2, s.Len())

	// 立即清理，应该没有过期
	removed := s.Cleanup()
	assert.Equal(t, 0, removed)
	assert.Equal(t, 2, s.Len())

	// 等待过期
	time.Sleep(ttl + 10*time.Millisecond)

	// 清理应该移除过期条目
	removed = s.Cleanup()
	assert.Equal(t, 2, removed)
	assert.Equal(t, 0, s.Len())
}

func TestStoreCleanupPartial(t *testing.T) {
	ttl := 200 * time.Millisecond
	s := NewStore(100, ttl, 10.0, 20)

	// 创建桶
	s.GetBucket("ip1")

	// 等待一段时间
	time.Sleep(100 * time.Millisecond)

	// 创建另一个桶（更新lastAccess）
	s.GetBucket("ip2")

	// 再等待，使ip1过期但ip2不过期
	time.Sleep(150 * time.Millisecond)

	// 清理应该只移除ip1
	removed := s.Cleanup()
	assert.Equal(t, 1, removed)
	assert.Equal(t, 1, s.Len())
}

func TestStoreConcurrency(t *testing.T) {
	s := NewStore(1000, time.Hour, 10.0, 20)

	// 并发获取桶
	done := make(chan bool, 100)
	for i := 0; i < 100; i++ {
		go func(i int) {
			key := fmt.Sprintf("ip%d", i%10) // 10个不同的key
			b := s.GetBucket(key)
			assert.NotNil(t, b)
			done <- true
		}(i)
	}

	// 等待所有完成
	for i := 0; i < 100; i++ {
		<-done
	}

	// 应该只有10个不同的桶
	assert.Equal(t, 10, s.Len())
}
