package retry

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"
)

// Config 重试配置
type Config struct {
	MaxAttempts    int           // 最大重试次数
	Interval       time.Duration // 初始间隔
	MaxInterval    time.Duration // 最大间隔
	BackoffFactor  float64       // 退避因子
	Timeout        time.Duration // 单次操作超时
	Jitter         bool          // 是否添加随机抖动
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		MaxAttempts:   5,
		Interval:      1 * time.Second,
		MaxInterval:   30 * time.Second,
		BackoffFactor: 2.0,
		Timeout:       10 * time.Second,
		Jitter:        true,
	}
}

// PollConfig 轮询配置
type PollConfig struct {
	MaxAttempts   int
	Interval      time.Duration
	MaxInterval   time.Duration
	BackoffFactor float64
	Timeout       time.Duration
	Predicate     func() (bool, error) // 条件判断函数
}

// DefaultPollConfig 返回默认轮询配置
func DefaultPollConfig() *PollConfig {
	return &PollConfig{
		MaxAttempts:   10,
		Interval:      2 * time.Second,
		MaxInterval:   30 * time.Second,
		BackoffFactor: 1.5,
		Timeout:       5 * time.Minute,
		Predicate:     nil,
	}
}

// Poll 轮询等待条件满足
// 用于等待异步操作完成（如下载任务状态变化）
func Poll(ctx context.Context, config *PollConfig) error {
	if config == nil {
		config = DefaultPollConfig()
	}

	if config.Predicate == nil {
		return fmt.Errorf("predicate function is required")
	}

	// 创建超时上下文
	if config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, config.Timeout)
		defer cancel()
	}

	interval := config.Interval

	for attempt := 0; attempt < config.MaxAttempts; attempt++ {
		// 检查上下文是否已取消
		select {
		case <-ctx.Done():
			return fmt.Errorf("poll context cancelled: %w", ctx.Err())
		default:
		}

		// 执行条件判断
		done, err := config.Predicate()
		if err != nil {
			return fmt.Errorf("predicate error on attempt %d: %w", attempt+1, err)
		}
		if done {
			return nil // 条件满足，轮询成功
		}

		// 计算下一次等待时间（指数退避）
		if attempt < config.MaxAttempts-1 {
			sleepDuration := calculateBackoff(interval, config.MaxInterval, config.BackoffFactor, attempt, true)
			
			select {
			case <-ctx.Done():
				return fmt.Errorf("poll context cancelled during wait: %w", ctx.Err())
			case <-time.After(sleepDuration):
				// 继续下一次尝试
			}
		}
	}

	return fmt.Errorf("poll timeout after %d attempts", config.MaxAttempts)
}

// PollWithResult 轮询并返回结果
func PollWithResult[T any](ctx context.Context, config *PollConfig, fetcher func() (T, bool, error)) (T, error) {
	var result T
	
	if config == nil {
		config = DefaultPollConfig()
	}

	if config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, config.Timeout)
		defer cancel()
	}

	interval := config.Interval

	for attempt := 0; attempt < config.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return result, fmt.Errorf("poll context cancelled: %w", ctx.Err())
		default:
		}

		value, done, err := fetcher()
		if err != nil {
			return result, fmt.Errorf("fetcher error on attempt %d: %w", attempt+1, err)
		}
		if done {
			return value, nil
		}

		if attempt < config.MaxAttempts-1 {
			sleepDuration := calculateBackoff(interval, config.MaxInterval, config.BackoffFactor, attempt, true)
			
			select {
			case <-ctx.Done():
				return result, fmt.Errorf("poll context cancelled during wait: %w", ctx.Err())
			case <-time.After(sleepDuration):
			}
		}
	}

	return result, fmt.Errorf("poll timeout after %d attempts", config.MaxAttempts)
}

// Do 执行带重试的操作
func Do(ctx context.Context, config *Config, operation func() error) error {
	if config == nil {
		config = DefaultConfig()
	}

	var lastErr error
	interval := config.Interval

	for attempt := 0; attempt < config.MaxAttempts; attempt++ {
		// 检查上下文
		select {
		case <-ctx.Done():
			return fmt.Errorf("retry context cancelled: %w", ctx.Err())
		default:
		}

		// 执行操作
		err := operation()
		if err == nil {
			return nil // 成功
		}
		lastErr = err

		// 计算退避时间
		if attempt < config.MaxAttempts-1 {
			sleepDuration := calculateBackoff(interval, config.MaxInterval, config.BackoffFactor, attempt, config.Jitter)
			
			select {
			case <-ctx.Done():
				return fmt.Errorf("retry context cancelled during wait: %w", ctx.Err())
			case <-time.After(sleepDuration):
				// 继续下一次重试
			}
		}
	}

	return fmt.Errorf("operation failed after %d attempts: %w", config.MaxAttempts, lastErr)
}

// calculateBackoff 计算退避时间
func calculateBackoff(initial, max time.Duration, factor float64, attempt int, jitter bool) time.Duration {
	// 指数退避
	backoff := float64(initial) * math.Pow(factor, float64(attempt))
	
	// 添加随机抖动 (±20%)
	if jitter {
		jitterFactor := 0.8 + rand.Float64()*0.4
		backoff = backoff * jitterFactor
	}
	
	// 限制最大间隔
	if backoff > float64(max) {
		backoff = float64(max)
	}
	
	return time.Duration(backoff)
}

// LinearBackoff 线性退避（用于固定间隔场景）
func LinearBackoff(interval time.Duration, maxAttempts int) *Config {
	return &Config{
		MaxAttempts:   maxAttempts,
		Interval:      interval,
		MaxInterval:   interval,
		BackoffFactor: 1.0,
		Jitter:        false,
	}
}
