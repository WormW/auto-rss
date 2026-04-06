package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/WormW/auto-rss/internal/config"
	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/golang-jwt/jwt/v5"
)

// TokenPair 包含access token和refresh token
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"` // 秒
}

// Claims JWT claims结构
type Claims struct {
	UserID string `json:"user_id"`
	Type   string `json:"type"` // "access" 或 "refresh"
	jwt.RegisteredClaims
}

// JWTService JWT服务接口
type JWTService interface {
	// GenerateTokenPair 生成token对
	GenerateTokenPair(userID string) (*TokenPair, error)
	// ValidateAccessToken 验证access token
	ValidateAccessToken(tokenString string) (*Claims, error)
	// RefreshToken 使用refresh token获取新token对
	RefreshToken(refreshToken string) (*TokenPair, error)
	// Logout 登出（使所有token失效）
	Logout(userID string) error
}

// jwtService JWT服务实现
type jwtService struct {
	cfg              *config.Config
	refreshTokenRepo repository.RefreshTokenRepository
}

// NewJWTService 创建JWT服务
func NewJWTService(cfg *config.Config, refreshTokenRepo repository.RefreshTokenRepository) JWTService {
	return &jwtService{
		cfg:              cfg,
		refreshTokenRepo: refreshTokenRepo,
	}
}

// GenerateTokenPair 生成新的token对
func (s *jwtService) GenerateTokenPair(userID string) (*TokenPair, error) {
	now := time.Now()

	// 生成access token
	accessClaims := Claims{
		UserID: userID,
		Type:   "access",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(s.cfg.JWTAccessTokenExpiry)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Subject:   userID,
		},
	}

	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessTokenString, err := accessToken.SignedString([]byte(s.cfg.JWTSecret))
	if err != nil {
		return nil, fmt.Errorf("failed to sign access token: %w", err)
	}

	// 生成refresh token（随机字符串）
	refreshTokenBytes := make([]byte, 32)
	if _, err := rand.Read(refreshTokenBytes); err != nil {
		return nil, fmt.Errorf("failed to generate refresh token: %w", err)
	}
	refreshTokenString := base64.URLEncoding.EncodeToString(refreshTokenBytes)

	// 存储refresh token hash
	refreshTokenHash := repository.HashToken(refreshTokenString)
	refreshTokenModel := &model.RefreshToken{
		TokenHash: refreshTokenHash,
		UserID:    userID,
		Used:      false,
		CreatedAt: now,
		ExpiresAt: now.Add(s.cfg.JWTRefreshTokenExpiry),
	}

	if err := s.refreshTokenRepo.Create(refreshTokenModel); err != nil {
		return nil, fmt.Errorf("failed to store refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessTokenString,
		RefreshToken: refreshTokenString,
		TokenType:    "Bearer",
		ExpiresIn:    int64(s.cfg.JWTAccessTokenExpiry.Seconds()),
	}, nil
}

// ValidateAccessToken 验证access token
func (s *jwtService) ValidateAccessToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.cfg.JWTSecret), nil
	})

	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		if claims.Type != "access" {
			return nil, errors.New("invalid token type")
		}
		return claims, nil
	}

	return nil, errors.New("invalid token claims")
}

// RefreshToken 使用refresh token获取新token对
func (s *jwtService) RefreshToken(refreshToken string) (*TokenPair, error) {
	// 计算hash查找
	tokenHash := repository.HashToken(refreshToken)

	tokenModel, err := s.refreshTokenRepo.FindByTokenHash(tokenHash)
	if err != nil {
		return nil, errors.New("invalid refresh token")
	}

	// 检查是否已使用（重用检测）
	if tokenModel.Used {
		// 安全事件：token重用，删除该用户的所有token
		_ = s.refreshTokenRepo.DeleteByUserID(tokenModel.UserID)
		return nil, errors.New("token reuse detected")
	}

	// 检查是否过期
	if tokenModel.IsExpired() {
		return nil, errors.New("refresh token expired")
	}

	// 标记为已使用
	if err := s.refreshTokenRepo.MarkAsUsed(tokenModel.ID); err != nil {
		return nil, fmt.Errorf("failed to mark token as used: %w", err)
	}

	// 生成新token对
	return s.GenerateTokenPair(tokenModel.UserID)
}

// Logout 登出，删除用户的所有refresh token
func (s *jwtService) Logout(userID string) error {
	return s.refreshTokenRepo.DeleteByUserID(userID)
}
