package model

import "time"

// Download status constants
const (
	DownloadStatusPending                = "pending"
	DownloadStatusRetryCleanup           = "retry_cleanup"
	DownloadStatusRetryCleanupProcessing = "retry_cleanup_processing"
	DownloadStatusDownloading            = "downloading"
	DownloadStatusStalled                = "stalled"
	DownloadStatusCompleted              = "completed"
	DownloadStatusFailed                 = "failed"
	DownloadStatusOrganizing             = "organizing"
)

// Download 下载任务模型
type Download struct {
	ID                        uint       `json:"id" gorm:"primaryKey"`
	SubscriptionID            uint       `json:"subscription_id" gorm:"index:idx_sub_status,priority:1"`
	Title                     string     `json:"title" gorm:"not null"`
	Episode                   int        `json:"episode"`
	Fansub                    string     `json:"fansub" gorm:"index"`                                      // 字幕组名称
	Language                  string     `json:"language" gorm:"type:varchar(10);default:'unknown';index"` // 语言: chs/cht/jp/en/unknown
	TorrentURL                string     `json:"torrent_url" gorm:"not null"`
	TorrentHash               string     `json:"torrent_hash" gorm:"unique;index"`
	FilePath                  string     `json:"file_path"`
	RenamedPath               string     `json:"renamed_path"`
	MediaLibraryPath          string     `json:"media_library_path"`
	MediaLibraryRefreshStatus string     `json:"media_library_refresh_status" gorm:"type:varchar(20);default:'pending'"`
	MediaLibraryRefreshError  string     `json:"media_library_refresh_error" gorm:"type:text"`
	MediaLibraryRefreshedAt   *time.Time `json:"media_library_refreshed_at"`
	Status                    string     `json:"status" gorm:"default:pending;index:idx_sub_status,priority:2"` // pending, downloading, stalled, completed, failed
	Purpose                   string     `json:"purpose" gorm:"size:20;default:normal;index"`
	ReplacementCandidateID    *uint      `json:"replacement_candidate_id" gorm:"index"`
	ReplacementTorrentOwned   bool       `json:"replacement_torrent_owned" gorm:"default:false;index"`
	QbTaskID                  string     `json:"qb_task_id"`
	ErrorMessage              string     `json:"error_message" gorm:"type:text"`
	DownloadedAt              *time.Time `json:"downloaded_at"`
	CreatedAt                 time.Time  `json:"created_at" gorm:"index"`
	UpdatedAt                 time.Time  `json:"updated_at"`

	// 重试相关字段
	RetryCount  int        `json:"retry_count" gorm:"default:0"`         // 已重试次数
	MaxRetries  int        `json:"max_retries" gorm:"default:5"`         // 最大重试次数
	NextRetryAt *time.Time `json:"next_retry_at"`                        // 下次重试时间
	LastError   string     `json:"last_error" gorm:"type:text"`          // 最后错误信息
	RetryReason string     `json:"retry_reason" gorm:"type:varchar(50)"` // 重试原因

	Subscription Subscription `json:"subscription,omitempty" gorm:"foreignKey:SubscriptionID"`
}

// TableName 指定表名
func (Download) TableName() string {
	return "downloads"
}
