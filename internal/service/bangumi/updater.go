package bangumi

import (
	"strconv"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/repository"
)

// BangumiUpdater Bangumi信息定期更新服务
type BangumiUpdater struct {
	bangumiService   *BangumiService
	subscriptionRepo repository.SubscriptionRepository
	ticker           *time.Ticker
	stopChan         chan struct{}
	intervalHours    int
}

// NewBangumiUpdater 创建Bangumi更新服务
func NewBangumiUpdater(
	bangumiService *BangumiService,
	subscriptionRepo repository.SubscriptionRepository,
	intervalHours int,
) *BangumiUpdater {
	return &BangumiUpdater{
		bangumiService:   bangumiService,
		subscriptionRepo: subscriptionRepo,
		stopChan:         make(chan struct{}),
		intervalHours:    intervalHours,
	}
}

// Start 启动更新服务
func (u *BangumiUpdater) Start() {
	if u.intervalHours <= 0 {
		logger.Info("Bangumi updater disabled", "interval_hours", u.intervalHours)
		return
	}

	interval := time.Duration(u.intervalHours) * time.Hour
	u.ticker = time.NewTicker(interval)

	logger.Info("Bangumi updater started",
		"interval", interval.String(),
		"interval_hours", u.intervalHours)

	go func() {
		// 延迟5秒后执行第一次更新，确保服务已完全初始化
		time.Sleep(5 * time.Second)
		u.updateAllSubscriptions()

		for {
			select {
			case <-u.ticker.C:
				u.updateAllSubscriptions()
			case <-u.stopChan:
				logger.Info("Bangumi updater stopped")
				return
			}
		}
	}()
}

// Stop 停止更新服务
func (u *BangumiUpdater) Stop() {
	if u.ticker != nil {
		u.ticker.Stop()
	}
	close(u.stopChan)
}

// updateAllSubscriptions 更新所有订阅的Bangumi信息
// 只在番剧更新日当天触发，且每周只更新一次
func (u *BangumiUpdater) updateAllSubscriptions() {
	logger.Info("Starting Bangumi update for all subscriptions")

	// 获取当前星期几 (0=星期日, 1=星期一, ..., 6=星期六)
	today := time.Now().Weekday()
	todayInt := int(today)

	logger.Info("Current weekday", "weekday", today.String(), "day_int", todayInt)

	// 获取所有订阅（使用足够大的 limit 来获取所有订阅）
	subscriptions, _, err := u.subscriptionRepo.List(0, 10000)
	if err != nil {
		logger.Error("Failed to list subscriptions for Bangumi update", "error", err)
		return
	}

	// 统计信息
	totalCount := 0
	successCount := 0
	skippedCount := 0
	failedCount := 0

	for _, sub := range subscriptions {
		// 跳过没有 Bangumi ID 的订阅
		if sub.BangumiID == 0 {
			continue
		}

		totalCount++

		// 检查是否是更新日
		if sub.UpdateDay != "" {
			updateDay, err := strconv.Atoi(sub.UpdateDay)
			if err != nil {
				logger.Warn("Invalid update_day format",
					"id", sub.ID,
					"name", sub.Name,
					"update_day", sub.UpdateDay,
					"error", err)
				failedCount++
				continue
			}

			// 不是更新日，跳过
			if updateDay != todayInt {
				logger.Debug("Not update day, skipping",
					"id", sub.ID,
					"name", sub.Name,
					"update_day", updateDay,
					"today", todayInt)
				skippedCount++
				continue
			}

			// 检查本周是否已经更新过
			if sub.UpdatedAt.After(time.Time{}) {
				// 计算上次更新是在本周还是之前
				now := time.Now()
				lastUpdate := sub.UpdatedAt

				// 获取本周的开始时间（周日00:00:00）
				weekStart := now.AddDate(0, 0, -int(now.Weekday()))
				weekStart = time.Date(weekStart.Year(), weekStart.Month(), weekStart.Day(), 0, 0, 0, 0, weekStart.Location())

				// 如果上次更新在本周，跳过
				if lastUpdate.After(weekStart) {
					logger.Debug("Already updated this week, skipping",
						"id", sub.ID,
						"name", sub.Name,
						"last_update", lastUpdate.Format("2006-01-02 15:04:05"),
						"week_start", weekStart.Format("2006-01-02"))
					skippedCount++
					continue
				}
			}
		}

		// 更新番剧信息
		logger.Debug("Updating subscription",
			"id", sub.ID,
			"name", sub.Name,
			"bangumi_id", sub.BangumiID)

		// 获取最新集数
		latestEp, err := u.bangumiService.GetLatestEpisode(sub.BangumiID)
		if err != nil {
			logger.Warn("Failed to get latest episode for subscription",
				"id", sub.ID,
				"name", sub.Name,
				"bangumi_id", sub.BangumiID,
				"error", err)
			failedCount++
			continue
		}

		// 如果集数有更新，则更新数据库
		threshold := sub.TotalEpisodes + sub.EpisodeOffset
		if latestEp > threshold && sub.TotalEpisodes > 0 {
			logger.Warn("Bangumi episode sort appears cumulative, skipping update",
				"id", sub.ID,
				"name", sub.Name,
				"latest_episode", latestEp,
				"total_episodes", sub.TotalEpisodes,
				"episode_offset", sub.EpisodeOffset,
				"threshold", threshold)
			skippedCount++
		} else if latestEp > sub.LatestEpisode || shouldCorrectFalseCompletion(sub, latestEp) {
			previous := sub.LatestEpisode
			sub.LatestEpisode = latestEp
			if err := u.subscriptionRepo.Update(&sub); err != nil {
				logger.Error("Failed to update subscription latest episode",
					"id", sub.ID,
					"name", sub.Name,
					"latest_episode", latestEp,
					"error", err)
				failedCount++
				continue
			}

			logger.Info("Updated subscription latest episode",
				"id", sub.ID,
				"name", sub.Name,
				"previous", previous,
				"latest", latestEp)
			successCount++
		} else {
			logger.Debug("No update needed for subscription",
				"id", sub.ID,
				"name", sub.Name,
				"latest_episode", sub.LatestEpisode)
			skippedCount++
		}

		// 避免请求过快，休眠1秒
		time.Sleep(1 * time.Second)
	}

	logger.Info("Bangumi update completed",
		"total", totalCount,
		"success", successCount,
		"skipped", skippedCount,
		"failed", failedCount)
}

func shouldCorrectFalseCompletion(sub model.Subscription, latestEp int) bool {
	return sub.TotalEpisodes > 0 &&
		sub.LatestEpisode >= sub.TotalEpisodes &&
		latestEp > 0 &&
		latestEp < sub.TotalEpisodes
}
