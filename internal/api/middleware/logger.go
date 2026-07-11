package middleware

import (
	"time"

	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/gin-gonic/gin"
)

// Logger 日志中间件
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		if shouldSkipRequestLog(path) {
			return
		}

		latency := time.Since(start)
		statusCode := c.Writer.Status()
		clientIP := c.ClientIP()
		method := c.Request.Method

		logger.Info("HTTP Request",
			"status", statusCode,
			"latency", latency,
			"client_ip", clientIP,
			"method", method,
			"path", path,
			"query", query,
		)
	}
}

func shouldSkipRequestLog(path string) bool {
	switch path {
	case "/health", "/api/v1/health", "/ready", "/live", "/api/v1/logs", "/api/v1/logs/clear":
		return true
	default:
		return false
	}
}
