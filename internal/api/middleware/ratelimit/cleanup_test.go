package ratelimit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewCleanupManager(t *testing.T) {
	s := NewStore(100, time.Hour, 10.0, 20)
	cm := NewCleanupManager(s, 5*time.Minute)

	assert.NotNil(t, cm)
	assert.False(t, cm.IsRunning())
}

func TestCleanupManagerStartStop(t *testing.T) {
	s := NewStore(100, time.Hour, 10.0, 20)
	cm := NewCleanupManager(s, 100*time.Millisecond)

	// 启动
	cm.Start()
	assert.True(t, cm.IsRunning())

	// 等待一段时间
	time.Sleep(50 * time.Millisecond)

	// 停止
	cm.Stop()
	assert.False(t, cm.IsRunning())
}

func TestCleanupManagerCleanup(t *testing.T) {
	ttl := 50 * time.Millisecond
	s := NewStore(100, ttl, 10.0, 20)

	// 创建一些桶
	s.GetBucket("ip1")
	s.GetBucket("ip2")
	assert.Equal(t, 2, s.Len())

	// 创建短间隔的清理管理器
	cm := NewCleanupManager(s, 100*time.Millisecond)
	cm.Start()
	defer cm.Stop()

	// 等待条目过期和清理运行
	time.Sleep(ttl + 150*time.Millisecond)

	// 应该被清理
	assert.Equal(t, 0, s.Len())
}

func TestCleanupManagerMultipleStarts(t *testing.T) {
	s := NewStore(100, time.Hour, 10.0, 20)
	cm := NewCleanupManager(s, 100*time.Millisecond)

	// 多次启动应该安全
	cm.Start()
	cm.Start()
	cm.Start()

	assert.True(t, cm.IsRunning())

	cm.Stop()
	assert.False(t, cm.IsRunning())
}

func TestCleanupManagerStopWithoutStart(t *testing.T) {
	s := NewStore(100, time.Hour, 10.0, 20)
	cm := NewCleanupManager(s, 5*time.Minute)

	// 停止未启动的管理器应该安全
	cm.Stop()
	assert.False(t, cm.IsRunning())
}
