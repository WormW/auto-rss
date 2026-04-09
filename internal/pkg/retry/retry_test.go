package retry

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, 5, cfg.MaxAttempts)
	assert.Equal(t, 1*time.Second, cfg.Interval)
	assert.Equal(t, 30*time.Second, cfg.MaxInterval)
	assert.Equal(t, 2.0, cfg.BackoffFactor)
	assert.Equal(t, 10*time.Second, cfg.Timeout)
	assert.True(t, cfg.Jitter)
}

func TestDefaultPollConfig(t *testing.T) {
	cfg := DefaultPollConfig()

	assert.Equal(t, 10, cfg.MaxAttempts)
	assert.Equal(t, 2*time.Second, cfg.Interval)
	assert.Equal(t, 30*time.Second, cfg.MaxInterval)
	assert.Equal(t, 1.5, cfg.BackoffFactor)
	assert.Equal(t, 5*time.Minute, cfg.Timeout)
	assert.Nil(t, cfg.Predicate)
}

func TestDo_SuccessOnFirstAttempt(t *testing.T) {
	var callCount int32
	operation := func() error {
		atomic.AddInt32(&callCount, 1)
		return nil
	}

	ctx := context.Background()
	cfg := &Config{
		MaxAttempts:   3,
		Interval:      100 * time.Millisecond,
		MaxInterval:   500 * time.Millisecond,
		BackoffFactor: 2.0,
		Jitter:        false,
	}

	err := Do(ctx, cfg, operation)

	assert.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&callCount))
}

func TestDo_SuccessAfterRetries(t *testing.T) {
	var callCount int32
	operation := func() error {
		count := atomic.AddInt32(&callCount, 1)
		if count < 3 {
			return errors.New("temporary error")
		}
		return nil
	}

	ctx := context.Background()
	cfg := &Config{
		MaxAttempts:   5,
		Interval:      50 * time.Millisecond,
		MaxInterval:   200 * time.Millisecond,
		BackoffFactor: 2.0,
		Jitter:        false,
	}

	err := Do(ctx, cfg, operation)

	assert.NoError(t, err)
	assert.Equal(t, int32(3), atomic.LoadInt32(&callCount))
}

func TestDo_FailureAfterMaxAttempts(t *testing.T) {
	var callCount int32
	expectedErr := errors.New("persistent error")
	operation := func() error {
		atomic.AddInt32(&callCount, 1)
		return expectedErr
	}

	ctx := context.Background()
	cfg := &Config{
		MaxAttempts:   3,
		Interval:      50 * time.Millisecond,
		MaxInterval:   200 * time.Millisecond,
		BackoffFactor: 2.0,
		Jitter:        false,
	}

	err := Do(ctx, cfg, operation)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "operation failed after 3 attempts")
	assert.Contains(t, err.Error(), "persistent error")
	assert.Equal(t, int32(3), atomic.LoadInt32(&callCount))
}

func TestDo_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var callCount int32

	operation := func() error {
		count := atomic.AddInt32(&callCount, 1)
		if count == 2 {
			cancel() // 在第二次尝试时取消上下文
		}
		return errors.New("error")
	}

	cfg := &Config{
		MaxAttempts:   5,
		Interval:      100 * time.Millisecond,
		MaxInterval:   500 * time.Millisecond,
		BackoffFactor: 2.0,
		Jitter:        false,
	}

	err := Do(ctx, cfg, operation)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context cancelled")
}

func TestDo_NilConfig(t *testing.T) {
	var callCount int32
	operation := func() error {
		atomic.AddInt32(&callCount, 1)
		return nil
	}

	ctx := context.Background()
	err := Do(ctx, nil, operation)

	assert.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&callCount))
}

func TestPoll_Success(t *testing.T) {
	var callCount int32
	predicate := func() (bool, error) {
		count := atomic.AddInt32(&callCount, 1)
		return count >= 3, nil
	}

	ctx := context.Background()
	cfg := &PollConfig{
		MaxAttempts:   10,
		Interval:      50 * time.Millisecond,
		MaxInterval:   200 * time.Millisecond,
		BackoffFactor: 1.5,
		Predicate:     predicate,
	}

	err := Poll(ctx, cfg)

	assert.NoError(t, err)
	assert.Equal(t, int32(3), atomic.LoadInt32(&callCount))
}

func TestPoll_PredicateError(t *testing.T) {
	expectedErr := errors.New("predicate error")
	predicate := func() (bool, error) {
		return false, expectedErr
	}

	ctx := context.Background()
	cfg := &PollConfig{
		MaxAttempts:   3,
		Interval:      50 * time.Millisecond,
		MaxInterval:   200 * time.Millisecond,
		BackoffFactor: 1.5,
		Predicate:     predicate,
	}

	err := Poll(ctx, cfg)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "predicate error")
}

func TestPoll_Timeout(t *testing.T) {
	predicate := func() (bool, error) {
		return false, nil // 永远返回 false
	}

	ctx := context.Background()
	cfg := &PollConfig{
		MaxAttempts:   3,
		Interval:      50 * time.Millisecond,
		MaxInterval:   100 * time.Millisecond,
		BackoffFactor: 1.5,
		Predicate:     predicate,
	}

	err := Poll(ctx, cfg)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "poll timeout after 3 attempts")
}

func TestPoll_NilPredicate(t *testing.T) {
	ctx := context.Background()
	cfg := &PollConfig{
		MaxAttempts:   3,
		Interval:      50 * time.Millisecond,
		MaxInterval:   100 * time.Millisecond,
		BackoffFactor: 1.5,
		Predicate:     nil,
	}

	err := Poll(ctx, cfg)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "predicate function is required")
}

func TestPoll_ContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	predicate := func() (bool, error) {
		return false, nil
	}

	cfg := &PollConfig{
		MaxAttempts:   100, // 很大，不会被用到
		Interval:      100 * time.Millisecond,
		MaxInterval:   100 * time.Millisecond,
		BackoffFactor: 1.0,
		Predicate:     predicate,
	}

	start := time.Now()
	err := Poll(ctx, cfg)
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context cancelled")
	// 应该因为超时上下文而提前退出
	assert.True(t, elapsed < 300*time.Millisecond,
		"Expected timeout to trigger around 150ms, got %v", elapsed)
}

func TestPoll_NilConfig(t *testing.T) {
	ctx := context.Background()
	err := Poll(ctx, nil)

	// 应该使用默认配置，但因为 predicate 为 nil 会返回错误
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "predicate function is required")
}

func TestPollWithResult_Success(t *testing.T) {
	var callCount int32
	fetcher := func() (string, bool, error) {
		count := atomic.AddInt32(&callCount, 1)
		if count >= 3 {
			return "success", true, nil
		}
		return "", false, nil
	}

	ctx := context.Background()
	cfg := &PollConfig{
		MaxAttempts:   10,
		Interval:      50 * time.Millisecond,
		MaxInterval:   200 * time.Millisecond,
		BackoffFactor: 1.5,
	}

	result, err := PollWithResult(ctx, cfg, fetcher)

	assert.NoError(t, err)
	assert.Equal(t, "success", result)
	assert.Equal(t, int32(3), atomic.LoadInt32(&callCount))
}

func TestPollWithResult_FetcherError(t *testing.T) {
	expectedErr := errors.New("fetcher error")
	fetcher := func() (string, bool, error) {
		return "", false, expectedErr
	}

	ctx := context.Background()
	cfg := &PollConfig{
		MaxAttempts:   3,
		Interval:      50 * time.Millisecond,
		MaxInterval:   200 * time.Millisecond,
		BackoffFactor: 1.5,
	}

	result, err := PollWithResult(ctx, cfg, fetcher)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "fetcher error")
	assert.Empty(t, result)
}

func TestPollWithResult_Timeout(t *testing.T) {
	fetcher := func() (string, bool, error) {
		return "", false, nil // 永远返回未完成
	}

	ctx := context.Background()
	cfg := &PollConfig{
		MaxAttempts:   3,
		Interval:      50 * time.Millisecond,
		MaxInterval:   100 * time.Millisecond,
		BackoffFactor: 1.5,
	}

	result, err := PollWithResult(ctx, cfg, fetcher)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "poll timeout")
	assert.Empty(t, result)
}

func TestCalculateBackoff_Exponential(t *testing.T) {
	initial := 1 * time.Second
	max := 10 * time.Second
	factor := 2.0

	// 测试指数增长
	d1 := calculateBackoff(initial, max, factor, 0, false)
	assert.Equal(t, 1*time.Second, d1)

	d2 := calculateBackoff(initial, max, factor, 1, false)
	assert.Equal(t, 2*time.Second, d2)

	d3 := calculateBackoff(initial, max, factor, 2, false)
	assert.Equal(t, 4*time.Second, d3)

	d4 := calculateBackoff(initial, max, factor, 3, false)
	assert.Equal(t, 8*time.Second, d4)

	// 超过最大值应该被限制
	d5 := calculateBackoff(initial, max, factor, 4, false)
	assert.Equal(t, 10*time.Second, d5)
}

func TestCalculateBackoff_WithJitter(t *testing.T) {
	initial := 1 * time.Second
	max := 10 * time.Second
	factor := 2.0

	// 多次测试以确保抖动范围正确
	for i := 0; i < 10; i++ {
		d := calculateBackoff(initial, max, factor, 1, true)
		// 应该是 2s 的 80% 到 120%
		assert.True(t, d >= 1600*time.Millisecond && d <= 2400*time.Millisecond,
			"Expected duration between 1.6s and 2.4s, got %v", d)
	}
}

func TestCalculateBackoff_NoJitter(t *testing.T) {
	initial := 1 * time.Second
	max := 10 * time.Second
	factor := 2.0

	d := calculateBackoff(initial, max, factor, 2, false)
	assert.Equal(t, 4*time.Second, d)
}

func TestLinearBackoff(t *testing.T) {
	interval := 5 * time.Second
	maxAttempts := 10

	cfg := LinearBackoff(interval, maxAttempts)

	assert.Equal(t, maxAttempts, cfg.MaxAttempts)
	assert.Equal(t, interval, cfg.Interval)
	assert.Equal(t, interval, cfg.MaxInterval)
	assert.Equal(t, 1.0, cfg.BackoffFactor)
	assert.False(t, cfg.Jitter)
}

func TestLinearBackoff_Usage(t *testing.T) {
	var callCount int32
	operation := func() error {
		count := atomic.AddInt32(&callCount, 1)
		if count < 3 {
			return errors.New("error")
		}
		return nil
	}

	ctx := context.Background()
	cfg := LinearBackoff(50*time.Millisecond, 5)

	start := time.Now()
	err := Do(ctx, cfg, operation)
	elapsed := time.Since(start)

	assert.NoError(t, err)
	assert.Equal(t, int32(3), atomic.LoadInt32(&callCount))
	// 线性退避，每次等待固定 50ms，应该有 2 次等待
	// 允许一些误差
	assert.True(t, elapsed >= 90*time.Millisecond && elapsed < 200*time.Millisecond,
		"Expected elapsed time around 100ms, got %v", elapsed)
}

func TestPollWithTimeout(t *testing.T) {
	predicate := func() (bool, error) {
		return false, nil // 永远返回 false
	}

	ctx := context.Background()
	cfg := &PollConfig{
		MaxAttempts:   100, // 很大，不会被用到
		Interval:      50 * time.Millisecond,
		MaxInterval:   100 * time.Millisecond,
		BackoffFactor: 1.5,
		Timeout:       150 * time.Millisecond,
		Predicate:     predicate,
	}

	start := time.Now()
	err := Poll(ctx, cfg)
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context cancelled")
	// 应该因为超时上下文而提前退出
	assert.True(t, elapsed < 300*time.Millisecond,
		"Expected timeout to trigger around 150ms, got %v", elapsed)
}

func TestIntegration_RetryAndPoll(t *testing.T) {
	// 测试组合使用 retry 和 poll
	var resourceReady int32

	// 模拟一个异步初始化的资源
	go func() {
		time.Sleep(200 * time.Millisecond)
		atomic.StoreInt32(&resourceReady, 1)
	}()

	// 使用 poll 等待资源就绪
	ctx := context.Background()
	pollCfg := &PollConfig{
		MaxAttempts:   20,
		Interval:      50 * time.Millisecond,
		MaxInterval:   100 * time.Millisecond,
		BackoffFactor: 1.2,
		Predicate: func() (bool, error) {
			return atomic.LoadInt32(&resourceReady) == 1, nil
		},
	}

	err := Poll(ctx, pollCfg)
	assert.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&resourceReady))

	// 然后使用 retry 操作该资源
	var operationCount int32
	operation := func() error {
		count := atomic.AddInt32(&operationCount, 1)
		if count == 1 {
			return errors.New("temporary failure")
		}
		return nil
	}

	retryCfg := &Config{
		MaxAttempts:   3,
		Interval:      50 * time.Millisecond,
		MaxInterval:   100 * time.Millisecond,
		BackoffFactor: 2.0,
		Jitter:        false,
	}

	err = Do(ctx, retryCfg, operation)
	assert.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(&operationCount))
}
