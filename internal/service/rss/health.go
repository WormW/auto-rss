package rss

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/repository"
)

// HealthStatus RSS源健康状态
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"   // 健康
	HealthStatusUnhealthy HealthStatus = "unhealthy" // 不健康（可访问但内容异常）
	HealthStatusDead      HealthStatus = "dead"      // 失效（无法访问）
	HealthStatusUnknown   HealthStatus = "unknown"   // 未知（未检查）
)

// RSSHealthChecker RSS健康检查器
type RSSHealthChecker struct {
	subscriptionRepo repository.SubscriptionRepository
	httpClient       *http.Client
}

// HealthCheckResult 健康检查结果
type HealthCheckResult struct {
	SubscriptionID uint         `json:"subscription_id"`
	Name           string       `json:"name"`
	RssURL         string       `json:"rss_url"`
	Status         HealthStatus `json:"status"`
	LastCheckTime  time.Time    `json:"last_check_time"`
	ResponseTime   int64        `json:"response_time_ms"` // 响应时间（毫秒）
	ErrorMessage   string       `json:"error_message,omitempty"`
	LastPostDate   *time.Time   `json:"last_post_date,omitempty"` // 最新文章发布时间
}

// NewHealthChecker 创建健康检查器
func NewHealthChecker(subRepo repository.SubscriptionRepository) *RSSHealthChecker {
	return &RSSHealthChecker{
		subscriptionRepo: subRepo,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// CheckSubscription 检查单个订阅的健康状态
func (c *RSSHealthChecker) CheckSubscription(ctx context.Context, sub *model.Subscription) *HealthCheckResult {
	result := &HealthCheckResult{
		SubscriptionID: sub.ID,
		Name:           sub.Name,
		RssURL:         sub.RssURL,
		Status:         HealthStatusUnknown,
		LastCheckTime:  time.Now(),
	}

	if sub.RssURL == "" {
		result.Status = HealthStatusUnknown
		result.ErrorMessage = "RSS URL is empty"
		return result
	}

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, "GET", sub.RssURL, nil)
	if err != nil {
		result.Status = HealthStatusDead
		result.ErrorMessage = fmt.Sprintf("Failed to create request: %v", err)
		return result
	}

	req.Header.Set("User-Agent", "Auto-RSS/1.0")
	resp, err := c.httpClient.Do(req)
	result.ResponseTime = time.Since(start).Milliseconds()

	if err != nil {
		result.Status = HealthStatusDead
		result.ErrorMessage = fmt.Sprintf("Request failed: %v", err)
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		result.Status = HealthStatusDead
		result.ErrorMessage = fmt.Sprintf("HTTP %d", resp.StatusCode)
		return result
	}

	// 尝试解析 RSS
	parser := NewParser()
	items, err := parser.Parse(resp.Body)
	if err != nil {
		result.Status = HealthStatusUnhealthy
		result.ErrorMessage = fmt.Sprintf("Parse failed: %v", err)
		return result
	}

	result.Status = HealthStatusHealthy

	// 获取最新文章时间
	if len(items) > 0 && !items[0].PubTime.IsZero() {
		result.LastPostDate = &items[0].PubTime
	}

	return result
}

// CheckAllSubscriptions 检查所有订阅的健康状态
func (c *RSSHealthChecker) CheckAllSubscriptions(ctx context.Context) ([]*HealthCheckResult, error) {
	subs, err := c.subscriptionRepo.GetActiveSubscriptions()
	if err != nil {
		return nil, err
	}

	var results []*HealthCheckResult
	for _, sub := range subs {
		result := c.CheckSubscription(ctx, &sub)
		results = append(results, result)

		// 避免请求过快
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}

	return results, nil
}

// GetDeadSubscriptions 获取失效的订阅
func (c *RSSHealthChecker) GetDeadSubscriptions(ctx context.Context) ([]*HealthCheckResult, error) {
	results, err := c.CheckAllSubscriptions(ctx)
	if err != nil {
		return nil, err
	}

	var dead []*HealthCheckResult
	for _, r := range results {
		if r.Status == HealthStatusDead {
			dead = append(dead, r)
		}
	}

	return dead, nil
}

// StartPeriodicCheck 启动定期检查
func (c *RSSHealthChecker) StartPeriodicCheck(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			results, err := c.CheckAllSubscriptions(ctx)
			cancel()

			if err != nil {
				logger.Error("RSS health check failed", "error", err)
				continue
			}

			deadCount := 0
			for _, r := range results {
				if r.Status == HealthStatusDead {
					deadCount++
					logger.Warn("Dead RSS subscription detected",
						"subscription_id", r.SubscriptionID,
						"name", r.Name,
						"rss_url", r.RssURL,
						"error", r.ErrorMessage)
				}
			}

			logger.Info("RSS health check completed",
				"total", len(results),
				"dead", deadCount)
		}
	}()
}
