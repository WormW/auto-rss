package model

import "time"

// Download 下载任务模型
type Download struct {
	ID             uint       `json:"id" gorm:"primaryKey"`
	SubscriptionID uint       `json:"subscription_id" gorm:"index"`
	Title          string     `json:"title" gorm:"not null"`
	Episode        int        `json:"episode"`
	Fansub         string     `json:"fansub" gorm:"index"` // 字幕组名称
	TorrentURL     string     `json:"torrent_url" gorm:"not null"`
	TorrentHash    string     `json:"torrent_hash" gorm:"unique;index"`
	FilePath       string     `json:"file_path"`
	RenamedPath    string     `json:"renamed_path"`
	Status         string     `json:"status" gorm:"default:pending;index"` // pending, downloading, completed, failed
	QbTaskID       string     `json:"qb_task_id"`
	ErrorMessage   string     `json:"error_message" gorm:"type:text"`
	DownloadedAt   *time.Time `json:"downloaded_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`

	Subscription Subscription `json:"subscription,omitempty" gorm:"foreignKey:SubscriptionID"`
}

// TableName 指定表名
func (Download) TableName() string {
	return "downloads"
}
