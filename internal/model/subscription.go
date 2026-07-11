package model

import "time"

const MaxSubscriptionEpisodes = 10000

// Subscription 订阅模型
type Subscription struct {
	ID              uint       `json:"id" gorm:"primaryKey"`
	Name            string     `json:"name" gorm:"not null;index"`
	RssURL          string     `json:"rss_url"`
	Season          int        `json:"season" gorm:"default:1"`
	Status          string     `json:"status" gorm:"default:active;index"` // active, paused
	FilterKeywords  string     `json:"filter_keywords" gorm:"type:text"`   // JSON array
	ExcludeKeywords string     `json:"exclude_keywords" gorm:"type:text"`  // JSON array
	SubgroupID      *int       `json:"subgroup_id" gorm:"index"`           // 字幕组 ID (可选)
	RenameEnabled   bool       `json:"rename_enabled" gorm:"default:true"`
	LastCheckTime   *time.Time `json:"last_check_time"`

	// 新增字段 - 参考ani-rss
	Fansub             string     `json:"fansub" gorm:"type:varchar(100)"`    // 字幕组名称
	Language           string     `json:"language" gorm:"type:varchar(10)"`   // 字幕语言 (CHS, CHT, etc.)
	UpdateDay          string     `json:"update_day" gorm:"type:varchar(10)"` // 更新星期 (0-6)
	TotalEpisodes      int        `json:"total_episodes" gorm:"default:0"`    // 总集数 (0表示未知)
	CurrentEpisode     int        `json:"current_episode" gorm:"default:0"`   // 当前集数（已收集的集数）
	LatestEpisode      int        `json:"latest_episode" gorm:"default:0"`    // 最新更新的集数（从RSS/番剧源获取）
	EpisodeOffset      int        `json:"episode_offset" gorm:"default:0"`    // 集数偏移
	FilterRules        string     `json:"filter_rules" gorm:"type:text"`      // 过滤规则
	Enabled            bool       `json:"enabled" gorm:"default:true;index"`  // 是否启用
	LastDownloadAt     *time.Time `json:"last_download_at"`                   // 最后下载时间
	LastRSSPubTime     *time.Time `json:"last_rss_pub_time"`                  // RSS增量水位线（仅接受更大的发布时间）
	RSSBaselinePending bool       `json:"rss_baseline_pending" gorm:"default:false;index"`

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
	RSSSourceID *uint  `json:"rss_source_id" gorm:"index"`        // RSS源ID（如果从RSS源创建）
	SourceType  string `json:"source_type" gorm:"default:manual"` // manual: 手动填写, rss_source: 从RSS源选择

	// 合集种子
	CollectionTorrent string `json:"collection_torrent" gorm:"type:text"` // 合集种子地址(磁力链接或.torrent URL)

	// 语言偏好设置
	LanguagePreference string `json:"language_preference" gorm:"type:varchar(10);default:'auto'"` // auto/chs/cht/both

	// 追番日历相关
	AirDay          string `json:"air_day" gorm:"type:varchar(10)"`                    // 更新星期: 0-6 (0=周日)
	AirTime         string `json:"air_time" gorm:"type:varchar(10)"`                   // 更新时间: "23:00"
	AirTimezone     string `json:"air_timezone" gorm:"type:varchar(10);default:'JST'"` // 时区
	NotifyEnabled   bool   `json:"notify_enabled" gorm:"default:true"`                 // 是否开启更新提醒
	NotifyBeforeMin int    `json:"notify_before_min" gorm:"default:10"`                // 提前提醒分钟数

	// 完结时间追踪 - 用于判断完结后多久停止检查
	CompletedAt *time.Time `json:"completed_at"` // 完结时间（首次检测到相对当前集数达到总集数时设置）

	// 智能拉取配置
	SmartFetchEnabled  *bool  `json:"smart_fetch_enabled" gorm:"default:null"`      // 单订阅智能拉取开关；nil 表示跟随全局
	SmartFetchOverride string `json:"smart_fetch_override" gorm:"type:varchar(20)"` // 覆盖策略：follow/always/never

	// 分组相关字段
	GroupID *uint              `json:"group_id" gorm:"index"`                     // 所属分组ID
	Group   *SubscriptionGroup `json:"group,omitempty" gorm:"foreignKey:GroupID"` // 所属分组
	Tags    string             `json:"tags" gorm:"type:text"`                     // 标签（JSON数组格式）

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	Downloads []Download `json:"downloads,omitempty" gorm:"foreignKey:SubscriptionID"`
	RSSSource *RSSSource `json:"rss_source,omitempty" gorm:"foreignKey:RSSSourceID"`
}

// IsCalendarOnly reports whether the subscription is only used for calendar
// reminders and should not participate in RSS/download collection.
func (s Subscription) IsCalendarOnly() bool {
	return s.SourceType == "calendar" && s.RssURL == "" && s.CollectionTorrent == ""
}

// RelativeEpisode 将 RSS 原始集号转换为当前季度内的相对集数。
func (s Subscription) RelativeEpisode(originalEpisode int) int {
	offset := s.EpisodeOffset
	if offset < 0 {
		offset = 0
	}
	relativeEpisode := originalEpisode - offset
	if relativeEpisode < 0 {
		return 0
	}
	return relativeEpisode
}

func (s Subscription) RelativeCurrentEpisode() int {
	return s.RelativeEpisode(s.CurrentEpisode)
}

func (s Subscription) RelativeLatestEpisode() int {
	return s.RelativeEpisode(s.LatestEpisode)
}

func (s Subscription) IsCompleted() bool {
	return s.TotalEpisodes > 0 && s.RelativeCurrentEpisode() >= s.TotalEpisodes
}

// SubscriptionGroup 订阅分组模型
type SubscriptionGroup struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	Name        string    `json:"name" gorm:"not null;size:100;index"`    // 分组名称
	Description string    `json:"description" gorm:"size:500"`            // 分组描述
	Color       string    `json:"color" gorm:"size:20;default:'#18a058'"` // 分组颜色
	SortOrder   int       `json:"sort_order" gorm:"default:0"`            // 排序顺序
	IsDefault   bool      `json:"is_default" gorm:"default:false"`        // 是否为默认分组
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TableName 指定表名
func (SubscriptionGroup) TableName() string {
	return "subscription_groups"
}

// TableName 指定表名
func (Subscription) TableName() string {
	return "subscriptions"
}

// SubscriptionTag 订阅标签模型
type SubscriptionTag struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	Name        string    `json:"name" gorm:"not null;size:50;index;uniqueIndex"` // 标签名称（唯一）
	Color       string    `json:"color" gorm:"size:20;default:'#18a058'"`         // 标签颜色
	Description string    `json:"description" gorm:"size:200"`                    // 标签描述
	SortOrder   int       `json:"sort_order" gorm:"default:0"`                    // 排序
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TableName 指定表名
func (SubscriptionTag) TableName() string {
	return "subscription_tags"
}

// SubscriptionTagRelation 订阅-标签关联表
type SubscriptionTagRelation struct {
	SubscriptionID uint `json:"subscription_id" gorm:"primaryKey;index"`
	TagID          uint `json:"tag_id" gorm:"primaryKey;index"`
	CreatedAt      time.Time
}

// TableName 指定表名
func (SubscriptionTagRelation) TableName() string {
	return "subscription_tag_relations"
}

// SubscriptionWithTags 带标签的订阅
type SubscriptionWithTags struct {
	Subscription
	Tags []SubscriptionTag `json:"tags"`
}
