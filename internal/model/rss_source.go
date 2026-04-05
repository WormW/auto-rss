package model

import "time"

// RSSSource RSS源模型
type RSSSource struct {
	ID          uint          `json:"id" gorm:"primaryKey"`
	Name        string        `json:"name" gorm:"not null;index"`         // RSS源名称，如"Mikanani"
	BaseURL     string        `json:"base_url" gorm:"not null"`           // RSS源基础URL
	Description string        `json:"description" gorm:"type:text"`       // 描述
	Enabled     bool          `json:"enabled" gorm:"default:true;index"`  // 是否启用
	Timeout     time.Duration `json:"timeout" gorm:"default:30000000000"` // 超时时间（纳秒），默认30秒
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

// TableName 指定表名
func (RSSSource) TableName() string {
	return "rss_sources"
}

// DefaultRSSTimeout returns the default RSS timeout (30 seconds)
func DefaultRSSTimeout() time.Duration {
	return 30 * time.Second
}

// RSSAnime 从RSS源获取的番剧信息
type RSSAnime struct {
	Title      string   `json:"title"`       // 番剧标题
	RssURL     string   `json:"rss_url"`     // RSS订阅地址
	Fansub     string   `json:"fansub"`      // 字幕组
	UpdateDay  string   `json:"update_day"`  // 更新日期
	Episodes   []string `json:"episodes"`    // 已发布的集数
	SourceID   uint     `json:"source_id"`   // 来源RSS源ID
	SourceName string   `json:"source_name"` // 来源RSS源名称
}
