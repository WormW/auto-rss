package model

import "time"

// DiskSample stores a point-in-time disk usage reading for trend charts.
type DiskSample struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	Path         string    `json:"path" gorm:"not null;index"`
	DownloadPath string    `json:"download_path" gorm:"not null;index"`
	TotalBytes   int64     `json:"total_bytes"`
	UsedBytes    int64     `json:"used_bytes"`
	FreeBytes    int64     `json:"free_bytes"`
	UsagePercent float64   `json:"usage_percent"`
	Status       string    `json:"status" gorm:"index"`
	CreatedAt    time.Time `json:"created_at" gorm:"index"`
}

func (DiskSample) TableName() string {
	return "disk_samples"
}

// DiskCleanupRecord stores summary information for manual and automatic cleanup runs.
type DiskCleanupRecord struct {
	ID                 uint      `json:"id" gorm:"primaryKey"`
	Trigger            string    `json:"trigger" gorm:"index"`
	Strategy           string    `json:"strategy" gorm:"index"`
	DownloadPath       string    `json:"download_path" gorm:"not null;index"`
	DeletedCount       int       `json:"deleted_count"`
	SkippedCount       int       `json:"skipped_count"`
	FailedCount        int       `json:"failed_count"`
	FailedPaths        string    `json:"failed_paths" gorm:"type:text"`
	FreedBytes         int64     `json:"freed_bytes"`
	BeforeFreeBytes    int64     `json:"before_free_bytes"`
	AfterFreeBytes     int64     `json:"after_free_bytes"`
	MediaLibraryStatus string    `json:"media_library_status"`
	Message            string    `json:"message" gorm:"type:text"`
	CreatedAt          time.Time `json:"created_at" gorm:"index"`
}

func (DiskCleanupRecord) TableName() string {
	return "disk_cleanup_records"
}
