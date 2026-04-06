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
	r.POST("/api/v1/auth/login", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "login ok"})
	})
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy"})
	})
	return r
}

func TestRateLimitHeadersPresent(t *testing.T) {
	store := ratelimit.NewStore(1000, time.Hour, 100.0, 100)
	router := setupTestRouter(store)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.NotEmpty(t, w.Header().Get("X-RateLimit-Limit"))
	assert.NotEmpty(t, w.Header().Get("X-RateLimit-Remaining"))
	assert.NotEmpty(t, w.Header().Get("X-RateLimit-Reset"))

	// Verify header values are valid
	limit, err := strconv.Atoi(w.Header().Get("X-RateLimit-Limit"))
	assert.NoError(t, err)
	assert.Greater(t, limit, 0)

	remaining, err := strconv.Atoi(w.Header().Get("X-RateLimit-Remaining"))
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, remaining, 0)

	resetTime, err := strconv.ParseInt(w.Header().Get("X-RateLimit-Reset"), 10, 64)
	assert.NoError(t, err)
	assert.Greater(t, resetTime, int64(0))
}

func TestRateLimitExcludedPath(t *testing.T) {
	store := ratelimit.NewStore(1000, time.Hour, 100.0, 100)
	router := setupTestRouter(store)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	// Excluded paths should NOT have rate limit headers
	assert.Empty(t, w.Header().Get("X-RateLimit-Limit"))
	assert.Empty(t, w.Header().Get("X-RateLimit-Remaining"))
	assert.Empty(t, w.Header().Get("X-RateLimit-Reset"))
}

func TestRateLimitExceeded(t *testing.T) {
	// Create store with very low limits to trigger rate limiting quickly
	store := ratelimit.NewStore(1000, time.Hour, 1.0, 1)
	router := setupTestRouter(store)

	// First request should succeed
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest("GET", "/api/v1/test", nil)
	router.ServeHTTP(w1, req1)
	assert.Equal(t, 200, w1.Code)

	// Second request should be rate limited (429)
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/api/v1/test", nil)
	router.ServeHTTP(w2, req2)

	assert.Equal(t, 429, w2.Code)
	assert.NotEmpty(t, w2.Header().Get("Retry-After"))
	assert.NotEmpty(t, w2.Header().Get("X-RateLimit-Limit"))
	assert.NotEmpty(t, w2.Header().Get("X-RateLimit-Remaining"))
	assert.NotEmpty(t, w2.Header().Get("X-RateLimit-Reset"))

	// Verify Retry-After is a positive integer
	retryAfter, err := strconv.Atoi(w2.Header().Get("Retry-After"))
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, retryAfter, 1)

	// Verify error response body
	assert.Contains(t, w2.Body.String(), "429")
	assert.Contains(t, w2.Body.String(), "请求过于频繁")
}

func TestRateLimitAuthPath(t *testing.T) {
	// Create store with different settings for auth paths
	store := ratelimit.NewStore(1000, time.Hour, 100.0, 100)

	// Use custom config with low auth limits
	config := RateLimitConfig{
		Store:         store,
		GeneralRPS:    100.0,
		GeneralBurst:  100,
		AuthRPM:       1, // Very low for testing
		AuthBurst:     1,
		AuthPaths:     []string{"/api/v1/auth/login", "/api/v1/auth/refresh"},
		ExcludedPaths: []string{"/health"},
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RateLimitWithConfig(config))
	r.POST("/api/v1/auth/login", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "login ok"})
	})
	r.GET("/api/v1/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// First auth request should succeed
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest("POST", "/api/v1/auth/login", nil)
	r.ServeHTTP(w1, req1)
	assert.Equal(t, 200, w1.Code)

	// Second auth request should be rate limited (auth has stricter limits)
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("POST", "/api/v1/auth/login", nil)
	r.ServeHTTP(w2, req2)
	assert.Equal(t, 429, w2.Code)

	// General endpoint should still work (different bucket)
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest("GET", "/api/v1/test", nil)
	r.ServeHTTP(w3, req3)
	assert.Equal(t, 200, w3.Code)
}

func TestRateLimitDifferentIPs(t *testing.T) {
	store := ratelimit.NewStore(1000, time.Hour, 1.0, 1)
	router := setupTestRouter(store)

	// First IP makes request
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest("GET", "/api/v1/test", nil)
	req1.RemoteAddr = "192.168.1.1:12345"
	router.ServeHTTP(w1, req1)
	assert.Equal(t, 200, w1.Code)

	// Same IP makes second request (should be rate limited)
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/api/v1/test", nil)
	req2.RemoteAddr = "192.168.1.1:12346"
	router.ServeHTTP(w2, req2)
	assert.Equal(t, 429, w2.Code)

	// Different IP makes request (should succeed - independent quota)
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest("GET", "/api/v1/test", nil)
	req3.RemoteAddr = "192.168.1.2:12345"
	router.ServeHTTP(w3, req3)
	assert.Equal(t, 200, w3.Code)
}

func TestRateLimitHeadersOnEveryResponse(t *testing.T) {
	store := ratelimit.NewStore(1000, time.Hour, 100.0, 100)
	router := setupTestRouter(store)

	// Make multiple requests and verify headers are present each time
	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v1/test", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, 200, w.Code)
		assert.NotEmpty(t, w.Header().Get("X-RateLimit-Limit"), "Request %d: X-RateLimit-Limit missing", i)
		assert.NotEmpty(t, w.Header().Get("X-RateLimit-Remaining"), "Request %d: X-RateLimit-Remaining missing", i)
		assert.NotEmpty(t, w.Header().Get("X-RateLimit-Reset"), "Request %d: X-RateLimit-Reset missing", i)
	}
}

func TestRateLimitResetTime(t *testing.T) {
	store := ratelimit.NewStore(1000, time.Hour, 10.0, 20)
	router := setupTestRouter(store)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/test", nil)
	router.ServeHTTP(w, req)

	resetTime, err := strconv.ParseInt(w.Header().Get("X-RateLimit-Reset"), 10, 64)
	assert.NoError(t, err)

	// Reset time should be in the future (or very close to now)
	now := time.Now().Unix()
	assert.GreaterOrEqual(t, resetTime, now)

	// Reset time should not be too far in the future (within a few seconds for burst=20, rps=10)
	assert.Less(t, resetTime, now+10)
}
