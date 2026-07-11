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
func (f *LanguageFilter) CheckLanguageAllow(
	sub *model.Subscription,
	itemLang rss.LanguageType,
) (allowed bool, reason string) {
	// 如果语言未知，允许下载
	if itemLang == rss.LangUnknown {
		return true, "language_unknown"
	}

	// 获取用户的语言偏好
	preference := rss.NormalizeLanguagePreference(sub.LanguagePreference)

	// 获取历史统计（用于 auto 模式）
	historyStats := f.getLanguageStats(sub.ID)

	return rss.ShouldDownload(preference, itemLang, nil, historyStats)
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
