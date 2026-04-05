package bangumi

import "github.com/WormW/auto-rss/internal/model"

// BangumiEnricher 自动获取并填充 Bangumi 元数据
type BangumiEnricher interface {
	// Enrich 为订阅填充 Bangumi 数据
	// force: 是否强制刷新（即使已有 Bangumi ID）
	Enrich(subscription *model.Subscription, force bool) error
}
