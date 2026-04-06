package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"gorm.io/gorm"
)

// RefreshTokenRepository 刷新令牌仓储接口
type RefreshTokenRepository interface {
	Create(token *model.RefreshToken) error
	FindByTokenHash(hash string) (*model.RefreshToken, error)
	MarkAsUsed(id uint) error
	DeleteExpired() error
	DeleteByUserID(userID string) error
}

// refreshTokenRepository 刷新令牌仓储实现
type refreshTokenRepository struct {
	db *gorm.DB
}

// NewRefreshTokenRepository 创建刷新令牌仓储
func NewRefreshTokenRepository(db *gorm.DB) RefreshTokenRepository {
	return &refreshTokenRepository{db: db}
}

// Create 创建刷新令牌
func (r *refreshTokenRepository) Create(token *model.RefreshToken) error {
	return r.db.Create(token).Error
}

// FindByTokenHash 根据token hash查找
func (r *refreshTokenRepository) FindByTokenHash(hash string) (*model.RefreshToken, error) {
	var token model.RefreshToken
	err := r.db.Where("token_hash = ?", hash).First(&token).Error
	if err != nil {
		return nil, err
	}
	return &token, nil
}

// MarkAsUsed 标记为已使用
func (r *refreshTokenRepository) MarkAsUsed(id uint) error {
	now := time.Now()
	return r.db.Model(&model.RefreshToken{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"used":     true,
			"used_at":  &now,
		}).Error
}

// DeleteExpired 删除过期的token
func (r *refreshTokenRepository) DeleteExpired() error {
	return r.db.Where("expires_at < ?", time.Now()).Delete(&model.RefreshToken{}).Error
}

// DeleteByUserID 删除指定用户的所有token
func (r *refreshTokenRepository) DeleteByUserID(userID string) error {
	return r.db.Where("user_id = ?", userID).Delete(&model.RefreshToken{}).Error
}

// HashToken 计算token的SHA-256 hash（工具函数）
func HashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
