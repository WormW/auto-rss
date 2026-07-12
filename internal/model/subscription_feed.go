package model

import "time"

type SubscriptionFeed struct {
	ID               uint       `json:"id" gorm:"primaryKey"`
	SubscriptionID   uint       `json:"subscription_id" gorm:"uniqueIndex:idx_subscription_feed_url,priority:1;index;not null"`
	Name             string     `json:"name" gorm:"size:100;not null"`
	Fansub           string     `json:"fansub" gorm:"size:100"`
	RSSURL           string     `json:"rss_url" gorm:"type:text;not null"`
	RSSURLNormalized string     `json:"-" gorm:"column:rss_url_normalized;size:2048;uniqueIndex:idx_subscription_feed_url,priority:2;not null"`
	EpisodeOffset    int        `json:"episode_offset" gorm:"not null;default:0"`
	Enabled          bool       `json:"enabled" gorm:"not null;default:true;index"`
	LastRSSPubTime   *time.Time `json:"last_rss_pub_time"`
	BaselinePending  bool       `json:"baseline_pending" gorm:"not null;default:true;index"`
	LastCheckTime    *time.Time `json:"last_check_time"`
	LastSuccessAt    *time.Time `json:"last_success_at"`
	LastError        string     `json:"last_error" gorm:"type:text"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

func (SubscriptionFeed) TableName() string {
	return "subscription_feeds"
}

func (f SubscriptionFeed) RelativeEpisode(original int) int {
	if original <= f.EpisodeOffset {
		return 0
	}
	return original - f.EpisodeOffset
}

type SubscriptionFeedSeenItem struct {
	ID                 uint      `json:"id" gorm:"primaryKey"`
	SubscriptionFeedID uint      `json:"subscription_feed_id" gorm:"uniqueIndex:idx_feed_seen_resource,priority:1;index;not null"`
	ResourceKey        string    `json:"resource_key" gorm:"uniqueIndex:idx_feed_seen_resource,priority:2;size:512;not null"`
	OriginalEpisode    int       `json:"original_episode"`
	FirstSeenAt        time.Time `json:"first_seen_at" gorm:"not null"`
}

func (SubscriptionFeedSeenItem) TableName() string {
	return "subscription_feed_seen_items"
}
