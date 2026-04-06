package middleware

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/gin-gonic/gin"
)

// TokenClaims represents the JWT token claims
type TokenClaims struct {
	UserID    uint      `json:"user_id"`
	Username  string    `json:"username"`
	TokenType string    `json:"token_type"`
	ExpiresAt time.Time `json:"exp"`
}

// JWTService defines the interface for JWT operations
type JWTService interface {
	ValidateAccessToken(tokenString string) (*TokenClaims, error)
}

// Common errors for JWT validation
var (
	ErrInvalidToken = errors.New("invalid token")
	ErrExpiredToken = errors.New("token expired")
	ErrTokenMissing = errors.New("token missing")
)

// AuthMiddleware 创建JWT认证中间件
func AuthMiddleware(jwtService JWTService) gin.HandlerFunc {
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
