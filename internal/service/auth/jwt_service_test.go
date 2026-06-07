package auth

import (
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/config"
	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newAuthTestService(t *testing.T) (JWTService, repository.RefreshTokenRepository, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	if err := db.AutoMigrate(&model.RefreshToken{}); err != nil {
		t.Fatalf("failed to migrate refresh token table: %v", err)
	}

	cfg := &config.Config{
		JWTSecret:             "0123456789abcdef0123456789abcdef",
		JWTAccessTokenExpiry:  time.Minute,
		JWTRefreshTokenExpiry: time.Hour,
	}
	repo := repository.NewRefreshTokenRepository(db)
	return NewJWTService(cfg, repo), repo, db
}

func TestLogoutRefreshTokenRejectsInvalidTokenWithoutDeletingValidTokens(t *testing.T) {
	svc, _, db := newAuthTestService(t)

	tokenPair, err := svc.GenerateTokenPair("admin")
	if err != nil {
		t.Fatalf("failed to generate token pair: %v", err)
	}

	if err := svc.LogoutRefreshToken("not-a-real-refresh-token"); err == nil {
		t.Fatal("expected invalid refresh token logout to fail")
	}

	var count int64
	if err := db.Model(&model.RefreshToken{}).Count(&count).Error; err != nil {
		t.Fatalf("failed to count refresh tokens: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected valid refresh token to remain, got count %d", count)
	}

	if _, err := svc.RefreshToken(tokenPair.RefreshToken); err != nil {
		t.Fatalf("expected remaining refresh token to still be usable, got %v", err)
	}
}

func TestLogoutRefreshTokenRevokesOnlyProvidedToken(t *testing.T) {
	svc, _, db := newAuthTestService(t)

	first, err := svc.GenerateTokenPair("admin")
	if err != nil {
		t.Fatalf("failed to generate first token pair: %v", err)
	}
	second, err := svc.GenerateTokenPair("admin")
	if err != nil {
		t.Fatalf("failed to generate second token pair: %v", err)
	}

	if err := svc.LogoutRefreshToken(first.RefreshToken); err != nil {
		t.Fatalf("failed to logout first refresh token: %v", err)
	}

	var count int64
	if err := db.Model(&model.RefreshToken{}).Count(&count).Error; err != nil {
		t.Fatalf("failed to count refresh tokens: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one refresh token after single-token logout, got %d", count)
	}

	if _, err := svc.RefreshToken(second.RefreshToken); err != nil {
		t.Fatalf("expected second refresh token to remain usable, got %v", err)
	}
}
