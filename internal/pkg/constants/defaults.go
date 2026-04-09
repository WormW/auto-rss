package constants

import "time"

// 时间相关常量
const (
	// DefaultRSSCheckInterval 默认 RSS 检查间隔
	DefaultRSSCheckInterval = 30 * time.Minute

	// MinTimeout 最小超时时间
	MinTimeout = 1 * time.Second

	// DefaultHTTPTimeout 默认 HTTP 请求超时
	DefaultHTTPTimeout = 30 * time.Second

	// DefaultQBittorrentWait 默认 qBittorrent 等待时间
	DefaultQBittorrentWait = 2 * time.Second

	// DefaultDBTimeout 默认数据库操作超时
	DefaultDBTimeout = 10 * time.Second

	// DefaultCacheExpiration 默认缓存过期时间
	DefaultCacheExpiration = 5 * time.Minute

	// DefaultContextTimeout 默认上下文超时
	DefaultContextTimeout = 30 * time.Second
)

// 数值相关常量
const (
	// DefaultPageSize 默认分页大小
	DefaultPageSize = 20

	// MaxPageSize 最大分页大小
	MaxPageSize = 100

	// DefaultRetryAttempts 默认重试次数
	DefaultRetryAttempts = 3

	// MaxRetryAttempts 最大重试次数
	MaxRetryAttempts = 10
)

// 文件路径相关常量
const (
	// DefaultDownloadPath 默认下载路径
	DefaultDownloadPath = "./downloads"

	// DefaultCoverPath 默认封面存储路径
	DefaultCoverPath = "./data/covers"

	// DefaultLogPath 默认日志路径
	DefaultLogPath = "./logs"

	// DefaultConfigPath 默认配置文件路径
	DefaultConfigPath = "./config.yaml"
)

// 限流相关常量
const (
	// DefaultRateLimitRPS 默认每秒请求数限制
	DefaultRateLimitRPS = 10.0

	// DefaultRateLimitBurst 默认突发请求数
	DefaultRateLimitBurst = 20

	// DefaultRateLimitMaxEntries 默认限流器最大条目数
	DefaultRateLimitMaxEntries = 10000

	// DefaultRateLimitTTL 默认限流记录过期时间
	DefaultRateLimitTTL = 1 * time.Hour

	// DefaultAuthRPM 默认认证接口每分钟请求限制
	DefaultAuthRPM = 5
)
