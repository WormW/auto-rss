package middleware

import (
	"net/http"
	"strings"

	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/service/auth"
	"github.com/gin-gonic/gin"
)

// AuthMiddleware 创建JWT认证中间件
func AuthMiddleware(jwtService auth.JWTService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从Authorization header提取token
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			logger.Warn("Missing Authorization header", "path", c.Request.URL.Path, "client_ip", c.ClientIP())
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "Authorization header required",
			})
			c.Abort()
			return
		}

		// 检查Bearer前缀
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			logger.Warn("Invalid Authorization header format", "path", c.Request.URL.Path, "client_ip", c.ClientIP())
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "Invalid Authorization header format. Expected: Bearer <token>",
			})
			c.Abort()
			return
		}

		tokenString := parts[1]

		// 验证token
		claims, err := jwtService.ValidateAccessToken(tokenString)
		if err != nil {
			logger.Warn("Invalid or expired token",
				"error", err,
				"path", c.Request.URL.Path,
				"client_ip", c.ClientIP())
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "Invalid or expired token",
			})
			c.Abort()
			return
		}

		// 将用户信息存储到context
		c.Set("userID", claims.UserID)
		c.Set("claims", claims)

		logger.Debug("Request authenticated",
			"userID", claims.UserID,
			"path", c.Request.URL.Path,
			"client_ip", c.ClientIP())

		c.Next()
	}
}
