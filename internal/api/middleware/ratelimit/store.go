package ratelimit

import (
	"container/list"
	"sync"
	"time"
)

// 默认配置常量
const (
	DefaultMaxEntries = 10000
	DefaultTTL        = 1 * time.Hour
)

// entry 是存储的内部条目类型
type entry struct {
	key    string
	bucket *Bucket
	elem   *list.Element // LRU 链表元素
}

// Store 管理多个客户端的令牌桶存储
// 实现 LRU 淘汰和 TTL 清理
type Store struct {
	buckets      map[string]*entry
	lru          *list.List // 队首 = 最近访问
	mu           sync.RWMutex
	maxEntries   int
	ttl          time.Duration
	defaultRPS   float64
	defaultBurst int
}

// NewStore 创建新的限流存储
// maxEntries: 最大条目数，超出时 LRU 淘汰
// ttl: 不活跃客户端数据保留时间
// rps: 默认每秒请求数
// burst: 默认突发容量
func NewStore(maxEntries int, ttl time.Duration, rps float64, burst int) *Store {
	if maxEntries <= 0 {
		maxEntries = DefaultMaxEntries
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}

	return &Store{
		buckets:      make(map[string]*entry),
		lru:          list.New(),
		maxEntries:   maxEntries,
		ttl:          ttl,
		defaultRPS:   rps,
		defaultBurst: burst,
	}
}

// GetBucket 获取或创建指定 key 的令牌桶（使用默认速率）
func (s *Store) GetBucket(key string) *Bucket {
	return s.GetBucketWithRate(key, s.defaultRPS, s.defaultBurst)
}

// GetBucketWithRate 获取或创建指定 key 的令牌桶（使用指定速率）
func (s *Store) GetBucketWithRate(key string, rps float64, burst int) *Bucket {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 检查是否已存在
	if e, exists := s.buckets[key]; exists {
		// 移动到 LRU 队首（最近使用）
		s.lru.MoveToFront(e.elem)
		return e.bucket
	}

	// 检查是否需要 LRU 淘汰
	if len(s.buckets) >= s.maxEntries {
		s.evictOldest()
	}

	// 创建新桶
	bucket := NewBucket(rps, burst)
	e := &entry{
		key:    key,
		bucket: bucket,
		elem:   s.lru.PushFront(key),
	}
	s.buckets[key] = e

	return bucket
}

// evictOldest 淘汰最久未使用的条目
func (s *Store) evictOldest() {
	if back := s.lru.Back(); back != nil {
		key := back.Value.(string)
		delete(s.buckets, key)
		s.lru.Remove(back)
	}
}

// Cleanup 清理过期的条目
// 返回清理的条目数量
func (s *Store) Cleanup() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	var removed int
	for key, e := range s.buckets {
		if e.bucket.IsExpired(s.ttl) {
			s.lru.Remove(e.elem)
			delete(s.buckets, key)
			removed++
		}
	}

	return removed
}

// Len 返回当前存储的条目数
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.buckets)
}

// Stats 返回统计信息
// 返回: 总条目数, 最久未访问时间
func (s *Store) Stats() (int, time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.buckets) == 0 {
		return 0, time.Time{}
	}

	// 最久未访问的在 LRU 队尾
	if back := s.lru.Back(); back != nil {
		key := back.Value.(string)
		if e, exists := s.buckets[key]; exists {
			return len(s.buckets), e.bucket.LastAccess()
		}
	}

	return len(s.buckets), time.Time{}
}
