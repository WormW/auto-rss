package bangumi

import (
	"strconv"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/repository"
)

// Enricher 为订阅填充 Bangumi 元数据
type Enricher interface {
	// Enrich 为订阅填充 Bangumi 数据
	// force: 是否强制刷新（即使已有 Bangumi ID）
	Enrich(subscription *model.Subscription, force bool) error
}

// enricher 实现 Enricher 接口
type enricher struct {
	bangumiService *BangumiService
	imageService   *ImageService
	configRepo     repository.ConfigRepository
}

// NewEnricher 创建 Bangumi 富化服务
// Dependencies: bangumi.BangumiService, bangumi.ImageService, configRepo
func NewEnricher(
	bangumiService *BangumiService,
	imageService *ImageService,
	configRepo repository.ConfigRepository,
) Enricher {
	return &enricher{
		bangumiService: bangumiService,
		imageService:   imageService,
		configRepo:     configRepo,
	}
}

// Enrich 为订阅填充 Bangumi 数据
func (e *enricher) Enrich(subscription *model.Subscription, force bool) error {
	// 如果已经有Bangumi ID且不是强制刷新，跳过
	if subscription.BangumiID > 0 && !force {
		logger.Debug("Subscription already has Bangumi data, skipping enrichment",
			"subscription_id", subscription.ID,
			"subscription_name", subscription.Name,
			"bangumi_id", subscription.BangumiID)
		return nil
	}

	logger.Info("Starting Bangumi data enrichment",
		"subscription_id", subscription.ID,
		"subscription_name", subscription.Name)

	// 设置代理
	e.setProxy()

	var subject *Subject
	var err error

	// 如果已有 Bangumi ID，直接通过 ID 获取详细信息
	if subscription.BangumiID > 0 {
		logger.Info("Using existing Bangumi ID to fetch data",
			"subscription_id", subscription.ID,
			"subscription_name", subscription.Name,
			"bangumi_id", subscription.BangumiID)

		subject, err = e.bangumiService.GetSubject(subscription.BangumiID)
		if err != nil {
			logger.Warn("Failed to fetch Bangumi data by ID",
				"subscription_name", subscription.Name,
				"bangumi_id", subscription.BangumiID,
				"error", err.Error())
			return err
		}
	} else {
		// 没有 Bangumi ID，通过番剧名称搜索
		logger.Info("Searching Bangumi by name",
			"subscription_id", subscription.ID,
			"subscription_name", subscription.Name)

		subject, err = e.bangumiService.SearchByName(subscription.Name)
		if err != nil {
			logger.Warn("Failed to fetch Bangumi data by name",
				"subscription_name", subscription.Name,
				"error", err.Error())
			return err
		}
	}

	logger.Info("Bangumi data fetched successfully",
		"subscription_name", subscription.Name,
		"bangumi_id", subject.ID,
		"bangumi_score", subject.Score)

	// 填充Bangumi数据
	e.populateSubscription(subscription, subject)

	return nil
}

// setProxy 设置代理
func (e *enricher) setProxy() {
	if e.configRepo != nil {
		proxyConfig, err := e.configRepo.Get("system_proxy")
		if err == nil && proxyConfig != nil && proxyConfig.Value != "" {
			logger.Debug("Setting proxy for services", "proxy", proxyConfig.Value)
			e.bangumiService.SetProxy(proxyConfig.Value)
			e.imageService.SetProxy(proxyConfig.Value)
		} else {
			logger.Debug("No proxy configured", "err", err)
		}
	}
}

// populateSubscription 将 Bangumi 数据填充到订阅
func (e *enricher) populateSubscription(subscription *model.Subscription, subject *Subject) {
	// 填充Bangumi数据
	subscription.BangumiID = subject.ID
	subscription.BangumiScore = subject.Score
	subscription.BangumiSummary = subject.Summary

	// 使用 name_cn 作为番剧名称（如果有的话）
	if subject.NameCN != "" {
		logger.Info("Using Bangumi name_cn as subscription name",
			"original_name", subscription.Name,
			"name_cn", subject.NameCN)
		subscription.Name = subject.NameCN
	}
	if subject.Images != nil {
		subscription.BangumiCover = subject.Images.Large

		// 下载封面到本地
		if subject.Images.Large != "" {
			logger.Debug("Downloading cover image",
				"subscription_name", subscription.Name,
				"cover_url", subject.Images.Large)

			localPath, err := e.imageService.DownloadCover(subject.Images.Large, subject.ID)
			if err != nil {
				logger.Error("Failed to download cover",
					"subscription_name", subscription.Name,
					"cover_url", subject.Images.Large,
					"error", err.Error())
			} else {
				subscription.BangumiCoverLocal = localPath
				logger.Info("Cover downloaded successfully",
					"subscription_name", subscription.Name,
					"local_path", localPath)
			}
		}
	}
	if subject.Rating != nil {
		subscription.BangumiRank = subject.Rating.Rank
	}

	// 自动填充季度信息
	if subject.Season > 0 {
		subscription.Season = subject.Season
	}

	// 如果总集数为0，尝试从Bangumi获取
	if subscription.TotalEpisodes == 0 && subject.TotalEps > 0 {
		subscription.TotalEpisodes = subject.TotalEps
	}

	// 获取最新已播出的集数
	if subject.ID > 0 && subject.TotalEps > 0 {
		// 先尝试通过API获取精确的已播出集数
		latestEp, err := e.bangumiService.GetLatestEpisode(subject.ID)
		if err != nil || latestEp == 0 {
			// API不可用时，使用总集数作为参考
			// 对于已完结番剧，这就是最终集数
			// 对于正在播出的，RSS会提供更准确的信息
			subscription.LatestEpisode = subject.TotalEps
			logger.Info("Using total episodes as latest episode reference",
				"subscription_name", subscription.Name,
				"total_episodes", subject.TotalEps,
				"reason", "Bangumi episodes API unavailable")
		} else {
			subscription.LatestEpisode = latestEp
			logger.Info("Got latest episode from Bangumi API",
				"subscription_name", subscription.Name,
				"latest_episode", latestEp)
		}
	}

	// 如果更新日期为空，尝试从Bangumi获取
	if subscription.UpdateDay == "" && subject.AirWeekday >= 0 {
		subscription.UpdateDay = strconv.Itoa(subject.AirWeekday)
	}

	// 提取开播日期和年份
	if subject.AirDate != "" {
		subscription.AirDate = subject.AirDate
		// 从日期字符串提取年份 (格式: YYYY-MM-DD)
		if len(subject.AirDate) >= 4 {
			if year, err := strconv.Atoi(subject.AirDate[:4]); err == nil {
				subscription.AirYear = year
			}
		}
	}

	logger.Info("Subscription enriched with Bangumi data",
		"subscription_name", subscription.Name,
		"bangumi_id", subscription.BangumiID,
		"bangumi_score", subscription.BangumiScore,
		"season", subscription.Season,
		"total_episodes", subscription.TotalEpisodes,
		"air_date", subscription.AirDate,
		"air_year", subscription.AirYear,
		"has_cover", subscription.BangumiCoverLocal != "")
}
