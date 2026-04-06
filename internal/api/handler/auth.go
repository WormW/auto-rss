package handler

import (
	"net/http"

	"github.com/WormW/auto-rss/internal/config"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/service/auth"
	"github.com/gin-gonic/gin"
)

// AuthHandler 认证处理器
type AuthHandler struct {
	cfg        *config.Config
	jwtService auth.JWTService
}

// NewAuthHandler 创建认证处理器
func NewAuthHandler(cfg *config.Config, jwtService auth.JWTService) *AuthHandler {
	return &AuthHandler{
		cfg:        cfg,
		jwtService: jwtService,
	}
}

// LoginRequest 登录请求
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Login 用户登录
// POST /api/v1/auth/login
func (h *AuthHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid request: username and password required",
		})
		return
	}

	logger.Info("Login attempt", "username", req.Username, "client_ip", c.ClientIP())

	// 验证用户名密码（单用户模式，从配置读取）
	if req.Username != h.cfg.JWTUsername || req.Password != h.cfg.JWTPassword {
		logger.Warn("Login failed: invalid credentials", "username", req.Username, "client_ip", c.ClientIP())
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "Invalid username or password",
		})
		return
	}

	// 生成token对
	tokenPair, err := h.jwtService.GenerateTokenPair(req.Username)
	if err != nil {
		logger.Error("Failed to generate token pair", "error", err, "username", req.Username)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to generate tokens",
		})
		return
	}

	logger.Info("Login successful", "username", req.Username, "client_ip", c.ClientIP())

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Login successful",
		"data":    tokenPair,
	})
}

// RefreshRequest 刷新token请求
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

// Refresh 刷新access token
// POST /api/v1/auth/refresh
func (h *AuthHandler) Refresh(c *gin.Context) {
	var req RefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid request: refresh_token required",
		})
		return
	}

	// 使用refresh token获取新token对
	tokenPair, err := h.jwtService.RefreshToken(req.RefreshToken)
	if err != nil {
		logger.Warn("Token refresh failed", "error", err, "client_ip", c.ClientIP())

		// 根据错误类型返回不同状态码
		statusCode := http.StatusUnauthorized
		message := "Invalid or expired refresh token"

		if err.Error() == "token reuse detected" {
			message = "Token reuse detected. Please login again."
			logger.Error("Security event: token reuse detected", "client_ip", c.ClientIP())
		}

		c.JSON(statusCode, gin.H{
			"code":    statusCode,
			"message": message,
		})
		return
	}

	logger.Info("Token refreshed successfully", "client_ip", c.ClientIP())

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Token refreshed successfully",
		"data":    tokenPair,
	})
}

// LogoutRequest 登出请求
type LogoutRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

// Logout 用户登出（使refresh token失效）
// POST /api/v1/auth/logout
func (h *AuthHandler) Logout(c *gin.Context) {
	var req LogoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid request: refresh_token required",
		})
		return
	}

	// 从refresh token获取userID并删除该用户的所有token
	// 这里简化处理：直接使用配置中的用户名
	if err := h.jwtService.Logout(h.cfg.JWTUsername); err != nil {
		logger.Error("Logout failed", "error", err, "client_ip", c.ClientIP())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Logout failed",
		})
		return
	}

	logger.Info("Logout successful", "client_ip", c.ClientIP())

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Logout successful",
	})
}
