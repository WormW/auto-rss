package model

import "time"

const (
	EpisodeStatusMissing          = "missing"
	EpisodeStatusDownloading      = "downloading"
	EpisodeStatusDownloaded       = "downloaded"
	EpisodeStatusMarkedDownloaded = "marked_downloaded"
	EpisodeStatusIgnored          = "ignored"

	CandidateStatusPending               = "pending"
	CandidateStatusKeptExisting          = "kept_existing"
	CandidateStatusReplacing             = "replacing"
	CandidateStatusAccepted              = "accepted"
	CandidateStatusAcceptedCleanupFailed = "accepted_cleanup_failed"
	CandidateStatusFailed                = "failed"

	DownloadPurposeNormal      = "normal"
	DownloadPurposeReplacement = "replacement"

	EpisodeStatusSourceAutomatic = "automatic"
	EpisodeStatusSourceUser      = "user"
	EpisodeStatusSourceMigration = "migration"
)

type SubscriptionEpisode struct {
	ID                uint       `json:"id" gorm:"primaryKey"`
	SubscriptionID    uint       `json:"subscription_id" gorm:"uniqueIndex:idx_subscription_episode,priority:1;index"`
	Episode           int        `json:"episode" gorm:"uniqueIndex:idx_subscription_episode,priority:2"`
	Status            string     `json:"status" gorm:"size:32;not null;index"`
	ActiveDownloadID  *uint      `json:"active_download_id" gorm:"index"`
	ActiveTorrentHash string     `json:"active_torrent_hash" gorm:"size:128"`
	ActiveTorrentURL  string     `json:"active_torrent_url" gorm:"type:text"`
	ActiveTitle       string     `json:"active_title" gorm:"type:text"`
	StatusSource      string     `json:"status_source" gorm:"size:32;not null"`
	DownloadedAt      *time.Time `json:"downloaded_at"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type EpisodeResource struct {
	Hash  string
	URL   string
	Title string
}

type EpisodeResourceCandidate struct {
	ID                    uint       `json:"id" gorm:"primaryKey"`
	SubscriptionEpisodeID uint       `json:"subscription_episode_id" gorm:"uniqueIndex:idx_episode_candidate_resource,priority:1;index;not null"`
	ResourceKey           string     `json:"resource_key" gorm:"uniqueIndex:idx_episode_candidate_resource,priority:2;size:512"`
	TorrentHash           string     `json:"torrent_hash" gorm:"size:128"`
	TorrentURL            string     `json:"torrent_url" gorm:"type:text"`
	Title                 string     `json:"title" gorm:"type:text"`
	Fansub                string     `json:"fansub" gorm:"size:100"`
	Language              string     `json:"language" gorm:"size:16"`
	PubTime               *time.Time `json:"pub_time"`
	SourceRSSURL          string     `json:"source_rss_url" gorm:"type:text"`
	Status                string     `json:"status" gorm:"size:40;not null;index"`
	FailureReason         string     `json:"failure_reason" gorm:"type:text"`
	StagedPath            string     `json:"staged_path" gorm:"type:text"`
	OldResourcePath       string     `json:"old_resource_path" gorm:"type:text"`
	RollbackPath          string     `json:"rollback_path" gorm:"type:text"`
	FinalPath             string     `json:"final_path" gorm:"type:text"`
	ReplacementStage      string     `json:"replacement_stage" gorm:"size:40;index"`
	ReplacementDownloadID *uint      `json:"replacement_download_id" gorm:"index"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

func (SubscriptionEpisode) TableName() string {
	return "subscription_episodes"
}

func (EpisodeResourceCandidate) TableName() string {
	return "episode_resource_candidates"
}
