package model

import "time"

// Config 配置模型
type Config struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	Key         string    `json:"key" gorm:"unique;not null;index"`
	Value       string    `json:"value" gorm:"not null;type:text"`
	Description string    `json:"description"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TableName 指定表名
func (Config) TableName() string {
	return "configs"
}
