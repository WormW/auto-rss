package subscription

import (
	"fmt"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/bangumi"
	"github.com/WormW/auto-rss/internal/service/mikan"
)

// ImportItem 批量导入项
type ImportItem struct {
	Title      string
	Fansub     string
	RssURL     string
	Season     int
	SourceID   uint
	SourceName string
}

// ImportResult 单个导入项的结果
type ImportResult struct {
	Title        string              `json:"title"`
	Success      bool                `json:"success"`
	Message      string              `json:"message"`
	Skipped      bool                `json:"skipped"`
	Subscription *model.Subscription `json:"subscription,omitempty"`
}

// BatchImporter 批量导入订阅服务
type BatchImporter interface {
	Import(items []ImportItem) ([]ImportResult, error)
}

type batchImporter struct {
	mikanService     mikan.Service
	bangumiEnricher  bangumi.Enricher
	subscriptionRepo subscriptionRepository
	configRepo       repository.ConfigRepository
}

type subscriptionRepository interface {
	List(offset, limit int) ([]model.Subscription, int64, error)
	GetByRSSURLAndSeason(rssURL string, season int) (*model.Subscription, error)
	Create(subscription *model.Subscription) error
	Update(subscription *model.Subscription) error
}

func NewBatchImporter(
	mikanService mikan.Service,
	bangumiEnricher bangumi.Enricher,
	subscriptionRepo subscriptionRepository,
	configRepo repository.ConfigRepository,
) BatchImporter {
	return &batchImporter{
		mikanService:     mikanService,
		bangumiEnricher:  bangumiEnricher,
		subscriptionRepo: subscriptionRepo,
		configRepo:       configRepo,
	}
}

func (b *batchImporter) Import(items []ImportItem) ([]ImportResult, error) {
	b.setProxy()
	results := make([]ImportResult, 0, len(items))

	existingSubs, _, err := b.subscriptionRepo.List(1, 9999)
	if err != nil {
		logger.Error("Failed to get existing subscriptions", "error", err)
	}
	existingKeys := make(map[string]bool)
	for _, sub := range existingSubs {
		season := sub.Season
		if season <= 0 {
			season = 1
		}
		key := fmt.Sprintf("%s|%s|%d", sub.Name, sub.RssURL, season)
		existingKeys[key] = true
	}

	// 对导入列表自身去重
	seenKeys := make(map[string]bool)

	for _, item := range items {
		result := ImportResult{Title: item.Title, Success: false}
		season := item.Season
		if season <= 0 {
			season = 1
		}
		key := fmt.Sprintf("%s|%s|%d", item.Title, item.RssURL, season)

		if seenKeys[key] {
			result.Skipped = true
			result.Success = true
			result.Message = "导入列表内重复,跳过"
			results = append(results, result)
			continue
		}
		seenKeys[key] = true

		if existingKeys[key] {
			result.Skipped = true
			result.Success = true
			result.Message = "已存在,跳过"
			results = append(results, result)
			continue
		}

		searchResult, err := b.mikanService.Search(item.Title)
		if err != nil {
			result.Message = "搜索失败: " + err.Error()
			results = append(results, result)
			logger.Error("Mikan search failed", "title", item.Title, "error", err)
			continue
		}

		if searchResult == nil || len(searchResult.Groups) == 0 {
			result.Message = "Mikan搜索无结果"
			results = append(results, result)
			logger.Warn("Mikan search returned no results", "title", item.Title)
			continue
		}

		var foundAnime *mikan.AnimeItem
		for _, group := range searchResult.Groups {
			for _, anime := range group.Items {
				if anime.Title == item.Title {
					foundAnime = anime
					break
				}
			}
			if foundAnime != nil {
				break
			}
		}

		if foundAnime == nil {
			if len(searchResult.Groups) > 0 && len(searchResult.Groups[0].Items) > 0 {
				foundAnime = searchResult.Groups[0].Items[0]
				logger.Info("Using first search result as fallback", "original_title", item.Title, "matched_title", foundAnime.Title)
			} else {
				result.Message = "未找到匹配的番剧"
				results = append(results, result)
				logger.Warn("No matching anime found in Mikan", "title", item.Title)
				continue
			}
		}

		fansubGroups, err := b.mikanService.GetFansubGroups(foundAnime.URL)
		if err != nil {
			result.Message = "获取字幕组失败: " + err.Error()
			results = append(results, result)
			logger.Error("Failed to get fansub groups", "title", item.Title, "error", err)
			continue
		}

		var selectedGroup *mikan.FansubGroup
		if item.Fansub != "" {
			for _, group := range fansubGroups {
				if group.Name == item.Fansub {
					selectedGroup = group
					break
				}
			}
		}
		if selectedGroup == nil && len(fansubGroups) > 0 {
			selectedGroup = fansubGroups[0]
		}
		if selectedGroup == nil {
			result.Message = "未找到可用的字幕组"
			results = append(results, result)
			logger.Warn("No fansub group available", "title", item.Title)
			continue
		}

		subscription := &model.Subscription{
			Name: item.Title, RssURL: selectedGroup.RSS, Season: season,
			Status: "active", Enabled: true, Fansub: selectedGroup.Name, RenameEnabled: true,
		}

		logger.Info("Fetching Bangumi data before creating subscription", "title", item.Title)
		if err := b.bangumiEnricher.Enrich(subscription, false); err != nil {
			logger.Warn("Failed to enrich subscription with Bangumi data", "title", item.Title, "error", err)
		}

		// 创建前数据库层面最终确认（防并发）
		if subscription.RssURL != "" {
			if _, dupErr := b.subscriptionRepo.GetByRSSURLAndSeason(subscription.RssURL, subscription.Season); dupErr == nil {
				result.Skipped = true
				result.Success = true
				result.Message = "创建前检测到已存在,跳过"
				results = append(results, result)
				existingKeys[key] = true
				continue
			}
		}

		if err := b.subscriptionRepo.Create(subscription); err != nil {
			result.Message = "创建订阅失败: " + err.Error()
			results = append(results, result)
			logger.Error("Failed to create subscription", "title", item.Title, "error", err)
			continue
		}

		if subscription.BangumiID == 0 {
			if err := b.bangumiEnricher.Enrich(subscription, false); err != nil {
				logger.Warn("Failed to enrich subscription after creation", "title", item.Title, "error", err)
			}
			if err := b.subscriptionRepo.Update(subscription); err != nil {
				logger.Error("Failed to update subscription with Bangumi data", "subscription_id", subscription.ID, "error", err)
			}
		}

		result.Success = true
		result.Message = "导入成功"
		result.Subscription = subscription
		results = append(results, result)
		existingKeys[key] = true

		logger.Info("Subscription imported successfully", "title", item.Title, "fansub", selectedGroup.Name, "subscription_id", subscription.ID)
	}

	return results, nil
}

func (b *batchImporter) setProxy() {
	if b.configRepo != nil {
		proxyConfig, err := b.configRepo.Get("system_proxy")
		if err == nil && proxyConfig != nil && proxyConfig.Value != "" {
			logger.Debug("Setting proxy for Mikan service", "proxy", proxyConfig.Value)
			b.mikanService.SetProxy(proxyConfig.Value)
		}
	}
}
