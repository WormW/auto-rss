package model

import "time"

// Notification 通知记录模型
type Notification struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Type      string    `json:"type" gorm:"index"` // telegram, email, webhook, websocket
	Title     string    `json:"title"`
	Message   string    `json:"message" gorm:"type:text"`
	Status    string    `json:"status" gorm:"index"` // pending, sent, failed
	Error     string    `json:"error" gorm:"type:text"`
	EventID   string    `json:"event_id" gorm:"index"` // 用于去重
	CreatedAt time.Time `json:"created_at" gorm:"index"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定表名
func (Notification) TableName() string {
	return "notifications"
}

// NotificationSetting 通知渠道配置模型
type NotificationSetting struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Channel   string    `json:"channel" gorm:"uniqueIndex"` // telegram, email, webhook
	Enabled   bool      `json:"enabled"`
	Config    string    `json:"config" gorm:"type:text"` // JSON config
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定表名
func (NotificationSetting) TableName() string {
	return "notification_settings"
}

// NotificationEvent 通知事件类型
type NotificationEvent string

const (
	EventDownloadComplete NotificationEvent = "download.complete"
	EventDownloadFailed   NotificationEvent = "download.failed"
	EventRSSUpdate        NotificationEvent = "rss.update"
	EventSystemError      NotificationEvent = "system.error"
	EventAiringSoon       NotificationEvent = "calendar.airing_soon"
	EventNewEpisode       NotificationEvent = "calendar.new_episode"
)

// NotificationPayload 通知内容载荷
type NotificationPayload struct {
	Event     NotificationEvent `json:"event"`
	Title     string            `json:"title"`
	Message   string            `json:"message"`
	Data      map[string]any    `json:"data,omitempty"`
	EventID   string            `json:"event_id"`
	Timestamp time.Time         `json:"timestamp"`
}
