package middleware

import (
	"errors"
	"time"
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
