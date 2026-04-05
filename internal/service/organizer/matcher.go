package organizer

import (
	"strings"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/bangumi"
)

// SubscriptionMatcher 订阅匹配服务
type SubscriptionMatcher interface {
	// Match 根据文件名信息查找最匹配的订阅
	// 返回匹配的订阅和匹配分数（0-1）
	Match(info *FileNameInfo) (*model.Subscription, float64)

	// SetMinMatchScore 设置最小匹配分数阈值
	SetMinMatchScore(score float64)
}

// subscriptionMatcher 订阅匹配服务实现
type subscriptionMatcher struct {
	parser           *FileNameParser
	subscriptionRepo repository.SubscriptionRepository
	bangumiService   *bangumi.BangumiService // optional, can be nil
	minMatchScore    float64
}

// NewSubscriptionMatcher 创建订阅匹配服务
func NewSubscriptionMatcher(
	parser *FileNameParser,
	subscriptionRepo repository.SubscriptionRepository,
	bangumiService *bangumi.BangumiService,
) SubscriptionMatcher {
	return &subscriptionMatcher{
		parser:           parser,
		subscriptionRepo: subscriptionRepo,
		bangumiService:   bangumiService,
		minMatchScore:    0.7, // default 70%
	}
}

// Match 根据文件名信息查找最匹配的订阅
func (m *subscriptionMatcher) Match(info *FileNameInfo) (*model.Subscription, float64) {
	// 获取所有订阅
	subscriptions, _, err := m.subscriptionRepo.List(0, 10000)
	if err != nil {
		logger.Error("Failed to list subscriptions", "error", err)
		return nil, 0
	}

	var bestMatch *model.Subscription
	var bestScore float64

	// 首先尝试本地匹配
	for i := range subscriptions {
		sub := &subscriptions[i]
		// 计算标题相似度
		score := m.parser.MatchTitle(info.Title, sub.Name)

		// 如果有字幕组信息，且订阅也指定了字幕组，则额外加分
		if info.Fansub != "" && sub.Fansub != "" {
			if strings.EqualFold(info.Fansub, sub.Fansub) {
				score += 0.1 // 字幕组匹配加10分
			}
		}

		// 更新最佳匹配
		if score > bestScore {
			bestScore = score
			bestMatch = sub
		}
	}

	// 如果本地匹配成功，直接返回
	if bestScore >= m.minMatchScore {
		logger.Debug("Local match successful",
			"file_title", info.Title,
			"subscription_name", bestMatch.Name,
			"score", bestScore)
		return bestMatch, bestScore
	}

	// 本地匹配失败，尝试通过 Bangumi API 查询
	logger.Info("Local match failed, trying Bangumi API",
		"file_title", info.Title,
		"best_score", bestScore)

	if m.bangumiService != nil {
		subject, err := m.bangumiService.SearchByName(info.Title)
		if err != nil {
			logger.Warn("Failed to search Bangumi",
				"title", info.Title,
				"error", err)
		} else if subject != nil {
			logger.Info("Found Bangumi match",
				"file_title", info.Title,
				"bangumi_id", subject.ID,
				"bangumi_name", subject.Name,
				"bangumi_name_cn", subject.NameCN)

			// 使用 Bangumi ID 或中文名再次匹配订阅
			for i := range subscriptions {
				sub := &subscriptions[i]
				// 优先通过 Bangumi ID 匹配
				if sub.BangumiID == subject.ID {
					logger.Info("Matched by Bangumi ID",
						"subscription_name", sub.Name,
						"bangumi_id", subject.ID)
					return sub, 1.0
				}

				// 如果 ID 不匹配，尝试匹配中文名或日文名
				cnScore := m.parser.MatchTitle(subject.NameCN, sub.Name)
				jpScore := m.parser.MatchTitle(subject.Name, sub.Name)
				maxScore := cnScore
				if jpScore > maxScore {
					maxScore = jpScore
				}

				if maxScore > bestScore {
					bestScore = maxScore
					bestMatch = sub
				}
			}

			if bestScore >= m.minMatchScore {
				logger.Info("Matched by Bangumi name",
					"subscription_name", bestMatch.Name,
					"score", bestScore)
				return bestMatch, bestScore
			}
		}
	}

	// 仍然未匹配成功
	logger.Warn("No matching subscription found",
		"file_title", info.Title,
		"best_score", bestScore)
	return nil, bestScore
}

// SetMinMatchScore 设置最小匹配分数阈值
func (m *subscriptionMatcher) SetMinMatchScore(score float64) {
	m.minMatchScore = score
}
