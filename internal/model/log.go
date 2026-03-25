package model

import "time"

// Log 日志模型
type Log struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Level     string    `json:"level" gorm:"not null;index:idx_level_time,priority:1"` // DEBUG, INFO, WARN, ERROR
	Module    string    `json:"module" gorm:"index"`         // 模块: rss, download, organizer, system
	Message   string    `json:"message" gorm:"not null"`
	Context   string    `json:"context" gorm:"type:text"` // JSON object
	CreatedAt time.Time `json:"created_at" gorm:"index:idx_level_time,priority:2"`
}

// TableName 指定表名
func (Log) TableName() string {
	return "logs"
}
