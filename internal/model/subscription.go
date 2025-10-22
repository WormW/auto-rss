package model

import "time"

// Subscription 订阅模型
type Subscription struct {
	ID              uint       `json:"id" gorm:"primaryKey"`
	Name            string     `json:"name" gorm:"not null;index"`
	RssURL          string     `json:"rss_url" gorm:"not null"`
	Season          int        `json:"season" gorm:"default:1"`
	Status          string     `json:"status" gorm:"default:active;index"` // active, paused
	FilterKeywords  string     `json:"filter_keywords" gorm:"type:text"`   // JSON array
	ExcludeKeywords string     `json:"exclude_keywords" gorm:"type:text"`  // JSON array
	SubgroupID      *int       `json:"subgroup_id" gorm:"index"`           // 字幕组 ID (可选)
	DownloadPath    string     `json:"download_path"`
	RenameEnabled   bool       `json:"rename_enabled" gorm:"default:true"`
	LastCheckTime   *time.Time `json:"last_check_time"`

	// 新增字段 - 参考ani-rss
	Fansub          string     `json:"fansub" gorm:"type:varchar(100)"`         // 字幕组名称
	Language        string     `json:"language" gorm:"type:varchar(10)"`        // 字幕语言 (CHS, CHT, etc.)
	UpdateDay       string     `json:"update_day" gorm:"type:varchar(10)"`      // 更新星期 (0-6)
	TotalEpisodes   int        `json:"total_episodes" gorm:"default:0"`         // 总集数 (0表示未知)
	CurrentEpisode  int        `json:"current_episode" gorm:"default:0"`        // 当前集数（已收集的集数）
	LatestEpisode   int        `json:"latest_episode" gorm:"default:0"`         // 最新更新的集数（从RSS/番剧源获取）
	EpisodeOffset   int        `json:"episode_offset" gorm:"default:0"`         // 集数偏移
	FilterRules     string     `json:"filter_rules" gorm:"type:text"`           // 过滤规则
	Enabled         bool       `json:"enabled" gorm:"default:true;index"`       // 是否启用
	LastDownloadAt  *time.Time `json:"last_download_at"`                        // 最后下载时间

	// Bangumi相关字段
	BangumiID         int     `json:"bangumi_id" gorm:"index"`                      // Bangumi条目ID
	BangumiScore      float64 `json:"bangumi_score"`                                // Bangumi评分
	BangumiSummary    string  `json:"bangumi_summary" gorm:"type:text"`             // 番剧简介
	BangumiCover      string  `json:"bangumi_cover" gorm:"type:varchar(500)"`       // 封面图URL(原始)
	BangumiCoverLocal string  `json:"bangumi_cover_local" gorm:"type:varchar(500)"` // 本地封面路径
	BangumiRank       int     `json:"bangumi_rank"`                                 // 排名
	BangumiSeason     int     `json:"bangumi_season" gorm:"default:0"`              // 季度(从Bangumi提取)
	AirDate           string  `json:"air_date" gorm:"type:varchar(20)"`             // 开播日期 (YYYY-MM-DD)
	AirYear           int     `json:"air_year"`                                     // 开播年份

	// RSS源相关
	RSSSourceID *uint  `json:"rss_source_id" gorm:"index"`            // RSS源ID（如果从RSS源创建）
	SourceType  string `json:"source_type" gorm:"default:manual"`     // manual: 手动填写, rss_source: 从RSS源选择

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	Downloads []Download `json:"downloads,omitempty" gorm:"foreignKey:SubscriptionID"`
	RSSSource *RSSSource `json:"rss_source,omitempty" gorm:"foreignKey:RSSSourceID"`
}

// TableName 指定表名
func (Subscription) TableName() string {
	return "subscriptions"
}
