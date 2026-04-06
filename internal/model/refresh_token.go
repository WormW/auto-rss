package model

import "time"

// RefreshToken 刷新令牌模型
type RefreshToken struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	TokenHash string    `json:"-" gorm:"uniqueIndex;not null;size:64"` // SHA-256 hash
	UserID    string    `json:"user_id" gorm:"not null;index"`         // 单用户模式下固定为 "admin"
	Used      bool      `json:"used" gorm:"default:false;index"`       // 是否已使用
	UsedAt    *time.Time `json:"used_at"`                                // 使用时间
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at" gorm:"index"`               // 过期时间
}

// TableName 指定表名
func (RefreshToken) TableName() string {
	return "refresh_tokens"
}

// IsExpired 检查token是否过期
func (rt *RefreshToken) IsExpired() bool {
	return time.Now().After(rt.ExpiresAt)
}

// IsValid 检查token是否有效（未使用且未过期）
func (rt *RefreshToken) IsValid() bool {
	return !rt.Used && !rt.IsExpired()
}
