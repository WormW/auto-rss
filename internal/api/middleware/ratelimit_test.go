package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/api/middleware/ratelimit"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func setupTestRouter(store *ratelimit.Store) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RateLimit(store))
	r.GET("/api/v1/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy"})
	})
	r.GET("/api/v1/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy"})
	})
	r.GET("/ready", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ready"})
	})
	r.GET("/live", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "alive"})
	})
	r.POST("/api/v1/auth/login", func(c *gin.Context) {
		c.JSON(200, gin.H{"token": "test"})
	})
	return r
}

// TestRateLimitHeadersPresent 测试限流头存在
func TestRateLimitHeadersPresent(t *testing.T) {
	store := ratelimit.NewStore(100, time.Hour, 100.0, 200) // 高限制，避免触发
	r := setupTestRouter(store)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/test", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.NotEmpty(t, w.Header().Get("X-RateLimit-Limit"))
	assert.NotEmpty(t, w.Header().Get("X-RateLimit-Remaining"))
	assert.NotEmpty(t, w.Header().Get("X-RateLimit-Reset"))
}

// TestRateLimitExcludedPath 测试排除路径
func TestRateLimitExcludedPath(t *testing.T) {
	store := ratelimit.NewStore(100, time.Hour, 100.0, 200)
	r := setupTestRouter(store)

	for _, path := range DefaultRateLimitExcludedPaths() {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", path, nil)
			r.ServeHTTP(w, req)

			assert.Equal(t, 200, w.Code)
			// 排除路径不应该有限流头
			assert.Empty(t, w.Header().Get("X-RateLimit-Limit"))
			assert.Empty(t, w.Header().Get("X-RateLimit-Remaining"))
			assert.Empty(t, w.Header().Get("X-RateLimit-Reset"))
		})
	}
}

// TestRateLimitExceeded 测试限流触发
func TestRateLimitExceeded(t *testing.T) {
	// 创建低限制的存储，容易触发限流
	store := ratelimit.NewStore(100, time.Hour, 1.0, 1)
	r := setupTestRouter(store)

	// 第一个请求应该通过
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest("GET", "/api/v1/test", nil)
	r.ServeHTTP(w1, req1)
	assert.Equal(t, 200, w1.Code)

	// 第二个请求应该被限流
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/api/v1/test", nil)
	r.ServeHTTP(w2, req2)

	assert.Equal(t, 429, w2.Code)
	assert.NotEmpty(t, w2.Header().Get("Retry-After"))
	assert.NotEmpty(t, w2.Header().Get("X-RateLimit-Limit"))
}

// TestRateLimitAuthPath 测试认证端点限流
func TestRateLimitAuthPath(t *testing.T) {
	store := ratelimit.NewStore(100, time.Hour, 100.0, 200)
	r := setupTestRouter(store)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/auth/login", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	// 认证端点应该有限流头
	assert.NotEmpty(t, w.Header().Get("X-RateLimit-Limit"))
}

// TestRateLimitDifferentIPs 测试不同IP独立配额
func TestRateLimitDifferentIPs(t *testing.T) {
	store := ratelimit.NewStore(100, time.Hour, 1.0, 1)
	r := setupTestRouter(store)

	// IP1 的第一个请求
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest("GET", "/api/v1/test", nil)
	req1.RemoteAddr = "192.168.1.1:12345"
	r.ServeHTTP(w1, req1)
	assert.Equal(t, 200, w1.Code)

	// IP2 的第一个请求（应该通过，因为是不同IP）
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/api/v1/test", nil)
	req2.RemoteAddr = "192.168.1.2:12345"
	r.ServeHTTP(w2, req2)
	assert.Equal(t, 200, w2.Code)
}

// TestRateLimitRetryAfter 测试 Retry-After 头
func TestRateLimitRetryAfter(t *testing.T) {
	store := ratelimit.NewStore(100, time.Hour, 1.0, 1)
	r := setupTestRouter(store)

	// 消耗令牌
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest("GET", "/api/v1/test", nil)
	r.ServeHTTP(w1, req1)
	assert.Equal(t, 200, w1.Code)

	// 触发限流
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/api/v1/test", nil)
	r.ServeHTTP(w2, req2)
	assert.Equal(t, 429, w2.Code)

	retryAfter := w2.Header().Get("Retry-After")
	assert.NotEmpty(t, retryAfter)

	// 验证是有效的数字
	retrySeconds, err := strconv.Atoi(retryAfter)
	assert.NoError(t, err)
	assert.Greater(t, retrySeconds, 0)
}
