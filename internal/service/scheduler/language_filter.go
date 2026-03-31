package scheduler

import (
	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/rss"
)

// LanguageFilter 语言过滤器
type LanguageFilter struct {
	downloadRepo repository.DownloadRepository
}

// NewLanguageFilter 创建语言过滤器
func NewLanguageFilter(downloadRepo repository.DownloadRepository) *LanguageFilter {
	return &LanguageFilter{
		downloadRepo: downloadRepo,
	}
}

// CheckLanguageAllow 检查是否应该下载该语言的条目
// 返回值：
//   - allowed: 是否允许下载
//   - reason: 决策原因
//   - existingIDs: 需要替换的已存在下载ID（如更高版本）
func (f *LanguageFilter) CheckLanguageAllow(
	sub *model.Subscription,
	episode int,
	itemLang rss.LanguageType,
	itemTitle string,
) (allowed bool, reason string, replaceDownloadID uint) {
	// 如果语言未知，允许下载
	if itemLang == rss.LangUnknown {
		return true, "language_unknown", 0
	}

	// 获取该订阅该集数的所有下载记录
	existingDownloads, err := f.downloadRepo.GetBySubscriptionAndEpisodeWithLang(sub.ID, episode)
	if err != nil {
		// 查询失败时，保守起见允许下载
		return true, "query_failed_allow", 0
	}

	// 如果没有现有下载，允许下载
	if len(existingDownloads) == 0 {
		return true, "no_existing_download", 0
	}

	// 获取用户的语言偏好
	preference := rss.NormalizeLanguagePreference(sub.LanguagePreference)

	// 获取历史统计（用于 auto 模式）
	historyStats := f.getLanguageStats(sub.ID)

	// 构建已存在的语言列表
	var existingLangs []rss.LanguageType
	for _, d := range existingDownloads {
		existingLangs = append(existingLangs, rss.LanguageType(d.Language))
	}

	// 检查是否应该下载
	allowed, reason = rss.ShouldDownload(preference, itemLang, existingLangs, historyStats)

	// 如果不允许下载，直接返回
	if !allowed {
		return false, reason, 0
	}

	// 检查是否需要替换现有版本（相同语言但更高版本号）
	replaceDownloadID = f.findReplaceTarget(existingDownloads, itemLang, itemTitle)
	if replaceDownloadID > 0 {
		reason = reason + "_replace_existing"
	}

	return true, reason, replaceDownloadID
}

// getLanguageStats 获取订阅的历史语言统计
func (f *LanguageFilter) getLanguageStats(subID uint) map[rss.LanguageType]int {
	stats := make(map[rss.LanguageType]int)

	// 获取最近 20 条下载记录
	downloads, err := f.downloadRepo.GetRecentBySubscription(subID, 20)
	if err != nil {
		return stats
	}

	for _, d := range downloads {
		lang := rss.LanguageType(d.Language)
		if lang == "" {
			lang = rss.LangUnknown
		}
		stats[lang]++
	}

	return stats
}

// findReplaceTarget 查找需要替换的现有下载
// 当同一语言的新版本(v2等)出现时，替换旧版本
func (f *LanguageFilter) findReplaceTarget(
	existingDownloads []model.Download,
	newLang rss.LanguageType,
	newTitle string,
) uint {
	newVersion := parseTitleVersion(newTitle)

	for _, d := range existingDownloads {
		// 只考虑相同语言的
		if rss.LanguageType(d.Language) != newLang {
			continue
		}

		// 检查版本号
		oldVersion := parseTitleVersion(d.Title)
		if newVersion > oldVersion {
			return d.ID
		}
	}

	return 0
}

// UpdateSubscriptionPreference 更新订阅的语言偏好（基于历史自动学习）
func (f *LanguageFilter) UpdateSubscriptionPreference(sub *model.Subscription) (rss.LanguagePreference, error) {
	// 只在 auto 模式下自动更新
	if rss.NormalizeLanguagePreference(sub.LanguagePreference) != rss.LangPrefAuto {
		return rss.NormalizeLanguagePreference(sub.LanguagePreference), nil
	}

	// 获取统计
	stats := f.getLanguageStats(sub.ID)

	// 推断偏好
	inferred := rss.LangPrefCHS // 默认简体
	total := stats[rss.LangCHS] + stats[rss.LangCHT]

	if total >= 3 {
		if stats[rss.LangCHT] > stats[rss.LangCHS] {
			chRatio := float64(stats[rss.LangCHT]) / float64(total)
			if chRatio > 0.6 {
				inferred = rss.LangPrefCHT
			}
		} else {
			chsRatio := float64(stats[rss.LangCHS]) / float64(total)
			if chsRatio > 0.6 {
				inferred = rss.LangPrefCHS
			} else {
				inferred = rss.LangPrefBoth
			}
		}
	}

	return inferred, nil
}
