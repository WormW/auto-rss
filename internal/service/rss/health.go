package rss

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
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
	feedRepo         SubscriptionFeedReader
	mu               sync.RWMutex
	httpClient       *http.Client
	proxyURL         string
}

type SubscriptionFeedReader interface {
	ListBySubscription(subscriptionID uint) ([]model.SubscriptionFeed, error)
	ListEnabledBySubscriptionIDs(subscriptionIDs []uint) ([]model.SubscriptionFeed, error)
}

type FeedHealthCheckResult struct {
	SubscriptionFeedID uint         `json:"subscription_feed_id"`
	Name               string       `json:"name"`
	Fansub             string       `json:"fansub"`
	RSSURL             string       `json:"rss_url"`
	Status             HealthStatus `json:"status"`
	ResponseTime       int64        `json:"response_time_ms"`
	ErrorMessage       string       `json:"error_message,omitempty"`
	LastPostDate       *time.Time   `json:"last_post_date,omitempty"`
	LastSuccessAt      *time.Time   `json:"last_success_at,omitempty"`
	LastError          string       `json:"last_error,omitempty"`
}

// HealthCheckResult 健康检查结果
type HealthCheckResult struct {
	SubscriptionID uint                    `json:"subscription_id"`
	Name           string                  `json:"name"`
	Status         HealthStatus            `json:"status"`
	LastCheckTime  time.Time               `json:"last_check_time"`
	Feeds          []FeedHealthCheckResult `json:"feeds"`
}

// NewHealthChecker 创建健康检查器
func NewHealthChecker(
	subRepo repository.SubscriptionRepository,
	feedRepo SubscriptionFeedReader,
) *RSSHealthChecker {
	return &RSSHealthChecker{
		subscriptionRepo: subRepo,
		feedRepo:         feedRepo,
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: newHealthTransport(nil),
		},
	}
}

// SetProxy 设置健康检查请求代理，空值会清除已有代理。
func (c *RSSHealthChecker) SetProxy(proxyURL string) error {
	proxyURL = strings.TrimSpace(proxyURL)
	var proxy func(*http.Request) (*url.URL, error)
	if proxyURL != "" {
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			return fmt.Errorf("invalid proxy URL: %w", err)
		}
		proxy = http.ProxyURL(parsed)
	}

	c.mu.Lock()
	if c.proxyURL == proxyURL {
		c.mu.Unlock()
		return nil
	}
	oldTransport, _ := c.httpClient.Transport.(*http.Transport)
	c.httpClient = &http.Client{
		Timeout:   30 * time.Second,
		Transport: newHealthTransport(proxy),
	}
	c.proxyURL = proxyURL
	c.mu.Unlock()

	if oldTransport != nil {
		oldTransport.CloseIdleConnections()
	}
	return nil
}

func newHealthTransport(proxy func(*http.Request) (*url.URL, error)) *http.Transport {
	return &http.Transport{
		Proxy: proxy,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}
}

// CheckSubscription 检查单个订阅的健康状态
func (c *RSSHealthChecker) CheckSubscription(ctx context.Context, sub *model.Subscription) *HealthCheckResult {
	if c.feedRepo == nil {
		feeds := []model.SubscriptionFeed{}
		if strings.TrimSpace(sub.RssURL) != "" {
			feeds = append(feeds, model.SubscriptionFeed{
				SubscriptionID: sub.ID,
				Name:           sub.Name,
				Fansub:         sub.Fansub,
				RSSURL:         sub.RssURL,
				EpisodeOffset:  sub.EpisodeOffset,
				Enabled:        true,
			})
		}
		return c.CheckSubscriptionFeeds(ctx, sub, feeds)
	}
	feeds, err := c.feedRepo.ListBySubscription(sub.ID)
	if err != nil {
		return &HealthCheckResult{
			SubscriptionID: sub.ID,
			Name:           sub.Name,
			Status:         HealthStatusUnknown,
			LastCheckTime:  time.Now(),
			Feeds:          []FeedHealthCheckResult{},
		}
	}
	return c.CheckSubscriptionFeeds(ctx, sub, feeds)
}

func (c *RSSHealthChecker) CheckSubscriptionFeeds(
	ctx context.Context,
	sub *model.Subscription,
	feeds []model.SubscriptionFeed,
) *HealthCheckResult {
	result := &HealthCheckResult{
		SubscriptionID: sub.ID,
		Name:           sub.Name,
		Status:         HealthStatusUnknown,
		LastCheckTime:  time.Now(),
		Feeds:          make([]FeedHealthCheckResult, 0, len(feeds)),
	}

	var healthy, unhealthy, dead, enabled int
	for i := range feeds {
		if !feeds[i].Enabled {
			continue
		}
		enabled++
		feedResult := c.checkFeed(ctx, &feeds[i])
		result.Feeds = append(result.Feeds, feedResult)
		switch feedResult.Status {
		case HealthStatusHealthy:
			healthy++
		case HealthStatusUnhealthy:
			unhealthy++
		case HealthStatusDead:
			dead++
		}
	}

	switch {
	case healthy > 0:
		result.Status = HealthStatusHealthy
	case unhealthy > 0:
		result.Status = HealthStatusUnhealthy
	case enabled > 0 && dead == enabled:
		result.Status = HealthStatusDead
	default:
		result.Status = HealthStatusUnknown
	}
	return result
}

func (c *RSSHealthChecker) checkFeed(ctx context.Context, feed *model.SubscriptionFeed) FeedHealthCheckResult {
	result := FeedHealthCheckResult{
		SubscriptionFeedID: feed.ID,
		Name:               feed.Name,
		Fansub:             feed.Fansub,
		RSSURL:             feed.RSSURL,
		Status:             HealthStatusUnknown,
		LastSuccessAt:      feed.LastSuccessAt,
		LastError:          feed.LastError,
	}
	if feed.RSSURL == "" {
		result.ErrorMessage = "RSS URL is empty"
		return result
	}
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feed.RSSURL, nil)
	if err != nil {
		result.Status = HealthStatusDead
		result.ErrorMessage = fmt.Sprintf("Failed to create request: %v", err)
		return result
	}

	req.Header.Set("User-Agent", "Auto-RSS/1.0")
	c.mu.RLock()
	client := c.httpClient
	c.mu.RUnlock()
	resp, err := client.Do(req)
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

	subscriptionIDs := make([]uint, 0, len(subs))
	for i := range subs {
		subscriptionIDs = append(subscriptionIDs, subs[i].ID)
	}
	feeds := make([]model.SubscriptionFeed, 0)
	if c.feedRepo != nil {
		feeds, err = c.feedRepo.ListEnabledBySubscriptionIDs(subscriptionIDs)
		if err != nil {
			return nil, err
		}
	}
	feedsBySubscription := make(map[uint][]model.SubscriptionFeed)
	for _, feed := range feeds {
		feedsBySubscription[feed.SubscriptionID] = append(feedsBySubscription[feed.SubscriptionID], feed)
	}

	var results []*HealthCheckResult
	for i, sub := range subs {
		result := c.CheckSubscriptionFeeds(ctx, &sub, feedsBySubscription[sub.ID])
		results = append(results, result)

		if i == len(subs)-1 {
			continue
		}
		// 避免不同订阅之间请求过快。
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
						"feeds", len(r.Feeds))
				}
			}

			logger.Info("RSS health check completed",
				"total", len(results),
				"dead", deadCount)
		}
	}()
}
