package subscription

import (
	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/bangumi"
	"github.com/WormW/auto-rss/internal/service/mikan"
)

// RSSAnimeImportItem RSS番剧导入项
type RSSAnimeImportItem struct {
	Title      string `json:"title" binding:"required"` // 番剧名称
	Fansub     string `json:"fansub"`                   // 字幕组
	RssURL     string `json:"rss_url"`                  // RSS URL
	SourceID   uint   `json:"source_id"`                // RSS源ID
	SourceName string `json:"source_name"`              // RSS源名称
}

// BatchImportFromRSSRequest 从RSS批量导入请求
type BatchImportFromRSSRequest struct {
	Items []RSSAnimeImportItem `json:"items" binding:"required,dive"`
}

// ImportResult 单个导入项的结果
type ImportResult struct {
	Title        string              `json:"title"`
	Success      bool                `json:"success"`
	Message      string              `json:"message"`
	Skipped      bool                `json:"skipped"` // 是否跳过(已存在)
	Subscription *model.Subscription `json:"subscription,omitempty"`
}

// BatchImporter 批量导入订阅服务
type BatchImporter interface {
	// Import 从 RSS 项目批量导入订阅
	// 返回导入结果列表，每个项目对应一个结果
	Import(items []RSSAnimeImportItem) ([]ImportResult, error)
}

// batchImporter 实现 BatchImporter 接口
type batchImporter struct {
	mikanService   *mikan.MikanService
	bangumiEnricher bangumi.Enricher
	repo           repository.SubscriptionRepository
}

// NewBatchImporter 创建批量导入服务
func NewBatchImporter(
	mikanService *mikan.MikanService,
	bangumiEnricher bangumi.Enricher,
	repo repository.SubscriptionRepository,
) BatchImporter {
	return &batchImporter{
		mikanService:    mikanService,
		bangumiEnricher: bangumiEnricher,
		repo:            repo,
	}
}

// Import 从 RSS 项目批量导入订阅
func (b *batchImporter) Import(items []RSSAnimeImportItem) ([]ImportResult, error) {
	results := make([]ImportResult, 0, len(items))

	// 获取已有订阅用于去重
	existingSubs, _, err := b.repo.List(1, 9999)
	if err != nil {
		logger.Error("Failed to get existing subscriptions", "error", err)
	}
	existingNames := make(map[string]bool)
	for _, sub := range existingSubs {
		existingNames[sub.Name] = true
	}

	for _, item := range items {
		result := b.importItem(item, existingNames)
		results = append(results, result)

		if result.Success && !result.Skipped {
			existingNames[item.Title] = true // 防止重复导入
		}
	}

	return results, nil
}

// importItem 导入单个项目
func (b *batchImporter) importItem(item RSSAnimeImportItem, existingNames map[string]bool) ImportResult {
	result := ImportResult{
		Title:   item.Title,
		Success: false,
	}

	// 检查是否已存在
	if existingNames[item.Title] {
		result.Skipped = true
		result.Success = true
		result.Message = "已存在,跳过"
		return result
	}

	// 通过Mikan搜索找到对应的番剧
	searchResult, err := b.mikanService.Search(item.Title)
	if err != nil {
		result.Message = "搜索失败: " + err.Error()
		logger.Error("Mikan search failed", "title", item.Title, "error", err)
		return result
	}

	// 查找匹配的番剧
	var foundAnime *mikan.AnimeItem

	// 如果搜索结果为空或没有分组,尝试使用第一个结果
	if searchResult == nil || len(searchResult.Groups) == 0 {
		result.Message = "Mikan搜索无结果"
		logger.Warn("Mikan search returned no results", "title", item.Title)
		return result
	}

	// 先尝试精确匹配
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

	// 如果精确匹配失败,使用第一个搜索结果
	if foundAnime == nil {
		if len(searchResult.Groups) > 0 && len(searchResult.Groups[0].Items) > 0 {
			foundAnime = searchResult.Groups[0].Items[0]
			logger.Info("Using first search result as fallback",
				"original_title", item.Title,
				"matched_title", foundAnime.Title)
		} else {
			result.Message = "未找到匹配的番剧"
			logger.Warn("No matching anime found in Mikan", "title", item.Title)
			return result
		}
	}

	// 获取字幕组列表
	fansubGroups, err := b.mikanService.GetFansubGroups(foundAnime.URL)
	if err != nil {
		result.Message = "获取字幕组失败: " + err.Error()
		logger.Error("Failed to get fansub groups", "title", item.Title, "error", err)
		return result
	}

	// 查找匹配的字幕组
	var selectedGroup *mikan.FansubGroup
	if item.Fansub != "" {
		for i := range fansubGroups {
			if fansubGroups[i].Name == item.Fansub {
				selectedGroup = fansubGroups[i]
				break
			}
		}
	}

	// 如果没有匹配的字幕组,使用第一个
	if selectedGroup == nil && len(fansubGroups) > 0 {
		selectedGroup = fansubGroups[0]
	}

	if selectedGroup == nil {
		result.Message = "未找到可用的字幕组"
		logger.Warn("No fansub group available", "title", item.Title)
		return result
	}

	// 创建订阅
	subscription := &model.Subscription{
		Name:          item.Title,
		RssURL:        selectedGroup.RSS,
		Season:        1,
		Status:        "active",
		Enabled:       true,
		Fansub:        selectedGroup.Name,
		RenameEnabled: true,
		// DownloadPath 不设置，统一使用系统配置
	}

	// 先从 Bangumi 获取数据
	logger.Info("Fetching Bangumi data before creating subscription",
		"title", item.Title)
	if err := b.bangumiEnricher.Enrich(subscription, false); err != nil {
		logger.Warn("Failed to enrich with Bangumi data", "title", item.Title, "error", err)
		// 继续创建订阅，即使 Bangumi 数据获取失败
	}

	// 保存订阅
	if err := b.repo.Create(subscription); err != nil {
		result.Message = "创建订阅失败: " + err.Error()
		logger.Error("Failed to create subscription", "title", item.Title, "error", err)
		return result
	}

	// 如果 enrich 没有在创建前完成，再次尝试更新
	if subscription.BangumiID == 0 {
		if err := b.bangumiEnricher.Enrich(subscription, false); err != nil {
			logger.Warn("Failed to enrich after create", "title", item.Title, "error", err)
		} else {
			// 保存Bangumi数据
			if err := b.repo.Update(subscription); err != nil {
				logger.Error("Failed to update subscription with Bangumi data", "subscription_id", subscription.ID, "error", err)
			}
		}
	}

	result.Success = true
	result.Message = "导入成功"
	result.Subscription = subscription

	logger.Info("Subscription imported successfully",
		"title", item.Title,
		"fansub", selectedGroup.Name,
		"subscription_id", subscription.ID)

	return result
}
