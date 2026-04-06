package ratelimit

import (
	"container/list"
	"sync"
	"time"
)

const (
	DefaultMaxEntries = 10000        // 默认最大条目数
	DefaultTTL        = 1 * time.Hour // 默认TTL
	DefaultRPS        = 10.0         // 默认每秒请求数
	DefaultBurst      = 20           // 默认突发请求数
)

// entry 存储桶的条目，用于LRU链表
type entry struct {
	key    string
	bucket *Bucket
	elem   *list.Element // 在LRU链表中的位置
}

// Store 限流器存储，支持LRU淘汰和TTL清理
type Store struct {
	mu           sync.RWMutex
	buckets      map[string]*entry // key -> entry
	lru          *list.List        // LRU链表 (front = most recent)
	maxEntries   int               // 最大条目数
	ttl          time.Duration     // 不活跃清理时间
	defaultRPS   float64           // 默认每秒请求数
	defaultBurst int               // 默认突发请求数
}

// NewStore 创建新的限流器存储
func NewStore(maxEntries int, ttl time.Duration, rps float64, burst int) *Store {
	return &Store{
		buckets:      make(map[string]*entry),
		lru:          list.New(),
		maxEntries:   maxEntries,
		ttl:          ttl,
		defaultRPS:   rps,
		defaultBurst: burst,
	}
}

// GetBucket 获取或创建指定key的令牌桶（使用默认速率）
func (s *Store) GetBucket(key string) *Bucket {
	return s.GetBucketWithRate(key, s.defaultRPS, s.defaultBurst)
}

// GetBucketWithRate 获取或创建指定key和速率的令牌桶
func (s *Store) GetBucketWithRate(key string, rps float64, burst int) *Bucket {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 检查是否已存在
	if e, exists := s.buckets[key]; exists {
		// 移动到LRU链表头部（最近使用）
		s.lru.MoveToFront(e.elem)
		return e.bucket
	}

	// 检查是否需要LRU淘汰
	if s.maxEntries > 0 && len(s.buckets) >= s.maxEntries {
		s.evictLRU()
	}

	// 创建新桶
	bucket := NewBucket(rps, burst)
	elem := s.lru.PushFront(key)
	s.buckets[key] = &entry{
		key:    key,
		bucket: bucket,
		elem:   elem,
	}

	return bucket
}

// evictLRU 淘汰最久未使用的条目
func (s *Store) evictLRU() {
	if s.lru.Len() == 0 {
		return
	}
	// 从链表尾部获取最久未使用的key
	elem := s.lru.Back()
	if elem != nil {
		key := elem.Value.(string)
		delete(s.buckets, key)
		s.lru.Remove(elem)
	}
}

// Cleanup 清理过期的条目（超过TTL未访问）
// 返回清理的条目数
func (s *Store) Cleanup() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-s.ttl)
	var toRemove []string

	for key, e := range s.buckets {
		if e.bucket.LastAccess().Before(cutoff) {
			toRemove = append(toRemove, key)
		}
	}

	for _, key := range toRemove {
		if e, exists := s.buckets[key]; exists {
			s.lru.Remove(e.elem)
			delete(s.buckets, key)
		}
	}

	return len(toRemove)
}

// Len 返回当前存储的条目数
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.buckets)
}

// Stats 返回存储统计信息
func (s *Store) Stats() (entries int, max int, ttl time.Duration) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.buckets), s.maxEntries, s.ttl
}
