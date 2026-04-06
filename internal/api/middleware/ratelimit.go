package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/WormW/auto-rss/internal/api/middleware/ratelimit"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/gin-gonic/gin"
)

// RateLimitConfig 限流中间件配置
type RateLimitConfig struct {
	Store         *ratelimit.Store
	GeneralRPS    float64  // 10.0
	GeneralBurst  int      // 20
	AuthRPM       int      // 5 (requests per minute)
	AuthBurst     int      // 5
	AuthPaths     []string // ["/api/v1/auth/login", "/api/v1/auth/refresh"]
	ExcludedPaths []string // ["/health", "/api/v1/health"]
}

// RateLimitWithConfig 使用自定义配置创建限流中间件
func RateLimitWithConfig(config RateLimitConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		clientIP := c.ClientIP()

		// 检查排除路径
		if isExcludedPath(path, config.ExcludedPaths) {
			c.Next()
			return
		}

		// 根据路径确定限流策略
		var bucket *ratelimit.Bucket
		var limit, remaining int
		var resetTime time.Time

		isAuthPath := isAuthPath(path, config.AuthPaths)

		if isAuthPath {
			// 认证端点：5 req/min = 0.083 req/s
			rps := float64(config.AuthRPM) / 60.0
			bucket = config.Store.GetBucketWithRate(clientIP+":"+path, rps, config.AuthBurst)
			limit = config.AuthBurst
		} else {
			// 普通端点
			bucket = config.Store.GetBucket(clientIP)
			limit = config.GeneralBurst
		}

		// 检查是否允许请求
		allowed := bucket.Allow()
		remaining = int(bucket.Tokens())
		resetTime = bucket.ResetTime()

		// 设置限流响应头（所有响应都包含，D-07）
		c.Header("X-RateLimit-Limit", strconv.Itoa(limit))
		c.Header("X-RateLimit-Remaining", strconv.Itoa(remaining))
		c.Header("X-RateLimit-Reset", strconv.FormatInt(resetTime.Unix(), 10))

		if !allowed {
			// 计算 Retry-After
			retryAfter := int(time.Until(resetTime).Seconds())
			if retryAfter < 1 {
				retryAfter = 1
			}

			c.Header("Retry-After", strconv.Itoa(retryAfter))

			logger.Warn("Rate limit exceeded",
				"client_ip", clientIP,
				"path", path,
				"limit", limit,
			)

			c.JSON(http.StatusTooManyRequests, gin.H{
				"code":    429,
				"message": "请求过于频繁，请稍后再试",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// RateLimit 使用默认配置创建限流中间件
func RateLimit(store *ratelimit.Store) gin.HandlerFunc {
	return RateLimitWithConfig(RateLimitConfig{
		Store:         store,
		GeneralRPS:    10.0,
		GeneralBurst:  20,
		AuthRPM:       5,
		AuthBurst:     5,
		AuthPaths:     []string{"/api/v1/auth/login", "/api/v1/auth/refresh"},
		ExcludedPaths: []string{"/health", "/api/v1/health"},
	})
}

// isAuthPath 检查路径是否为认证路径
func isAuthPath(path string, authPaths []string) bool {
	for _, authPath := range authPaths {
		if path == authPath {
			return true
		}
	}
	return false
}

// isExcludedPath 检查路径是否为排除路径
func isExcludedPath(path string, excludedPaths []string) bool {
	for _, excluded := range excludedPaths {
		if path == excluded {
			return true
		}
	}
	return false
}
