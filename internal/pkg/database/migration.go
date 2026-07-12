package database

import (
	"fmt"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// MigrationRecord gormigrate 迁移记录表结构
// 与 gormigrate 内部使用的结构对应
type MigrationRecord struct {
	ID        string `gorm:"primaryKey;size:255"`
	AppliedAt time.Time
}

// TableName 指定表名
func (MigrationRecord) TableName() string {
	return "migrations"
}

// RunMigrations 运行数据库迁移
func RunMigrations(db *gorm.DB) error {
	m := gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{
		{
			ID: "202504090001", // 初始迁移 - 创建所有表
			Migrate: func(tx *gorm.DB) error {
				return tx.AutoMigrate(
					&model.Subscription{},
					&model.Download{},
					&model.SubscriptionEpisode{},
					&model.EpisodeResourceCandidate{},
					&model.Config{},
					&model.DiskSample{},
					&model.DiskCleanupRecord{},
					&model.RSSSource{},
					&model.Log{},
					&model.RefreshToken{},
				)
			},
			Rollback: func(tx *gorm.DB) error {
				// 回滚时删除所有表
				return tx.Migrator().DropTable(
					"subscriptions",
					"downloads",
					"subscription_episodes",
					"episode_resource_candidates",
					"configs",
					"disk_samples",
					"disk_cleanup_records",
					"rss_sources",
					"logs",
					"refresh_tokens",
				)
			},
		},
		{
			ID: "202504090002", // 添加索引优化
			Migrate: func(tx *gorm.DB) error {
				// 为常用查询字段添加索引
				if err := tx.Exec("CREATE INDEX IF NOT EXISTS idx_subscriptions_status ON subscriptions(status)").Error; err != nil {
					return err
				}
				if err := tx.Exec("CREATE INDEX IF NOT EXISTS idx_downloads_status ON downloads(status)").Error; err != nil {
					return err
				}
				if err := tx.Exec("CREATE INDEX IF NOT EXISTS idx_logs_created_at ON logs(created_at)").Error; err != nil {
					return err
				}
				return nil
			},
			Rollback: func(tx *gorm.DB) error {
				tx.Exec("DROP INDEX IF EXISTS idx_subscriptions_status")
				tx.Exec("DROP INDEX IF EXISTS idx_downloads_status")
				tx.Exec("DROP INDEX IF EXISTS idx_logs_created_at")
				return nil
			},
		},
		{
			ID: "202504230001", // 回填 air_day 数据（从 update_day 复制）
			Migrate: func(tx *gorm.DB) error {
				result := tx.Exec(`UPDATE subscriptions SET air_day = update_day WHERE (air_day = '' OR air_day IS NULL) AND update_day != ''`)
				if result.Error != nil {
					return result.Error
				}
				return nil
			},
			Rollback: func(tx *gorm.DB) error {
				// 回滚无意义，无法区分原有值和回填值
				return nil
			},
		},
		{
			ID: "202606120001", // 下载记录增加媒体库刷新状态
			Migrate: func(tx *gorm.DB) error {
				if err := tx.AutoMigrate(&model.Download{}); err != nil {
					return err
				}
				return tx.Exec("CREATE INDEX IF NOT EXISTS idx_downloads_media_library_refresh_status ON downloads(media_library_refresh_status)").Error
			},
			Rollback: func(tx *gorm.DB) error {
				tx.Exec("DROP INDEX IF EXISTS idx_downloads_media_library_refresh_status")
				return nil
			},
		},
		{
			ID: "202606150001", // 智能拉取可视化与单订阅覆盖配置
			Migrate: func(tx *gorm.DB) error {
				if err := tx.AutoMigrate(&model.Subscription{}, &model.Config{}); err != nil {
					return err
				}

				defaultConfigs := []model.Config{
					{Key: "smart_fetch.enabled", Value: "true", Description: "启用智能拉取策略"},
					{Key: "smart_fetch.before_air_day", Value: "1", Description: "更新日前N天开始拉取"},
					{Key: "smart_fetch.after_air_day", Value: "2", Description: "更新日后N天继续拉取"},
					{Key: "smart_fetch.skip_completed", Value: "false", Description: "是否跳过已完结的订阅"},
					{Key: "smart_fetch.completed_stop_days", Value: "30", Description: "完结后N天停止常规检查，0表示不停止"},
					{Key: "smart_fetch.check_local_complete", Value: "true", Description: "是否检查本地完整性"},
				}
				for _, cfg := range defaultConfigs {
					if err := tx.Where("key = ?", cfg.Key).FirstOrCreate(&cfg).Error; err != nil {
						return err
					}
				}
				return nil
			},
			Rollback: func(tx *gorm.DB) error {
				// SQLite 不可靠支持删除列；保留结构，仅移除新增默认配置。
				return tx.Where("key IN ?", []string{
					"smart_fetch.completed_stop_days",
				}).Delete(&model.Config{}).Error
			},
		},
		{
			ID: "202606150002", // persist disk samples and cleanup summaries
			Migrate: func(tx *gorm.DB) error {
				return tx.AutoMigrate(&model.DiskSample{}, &model.DiskCleanupRecord{})
			},
			Rollback: func(tx *gorm.DB) error {
				return tx.Migrator().DropTable("disk_cleanup_records", "disk_samples")
			},
		},
		{
			ID: "202606180001", // add failed cleanup summary fields
			Migrate: func(tx *gorm.DB) error {
				return tx.AutoMigrate(&model.DiskCleanupRecord{})
			},
			Rollback: func(tx *gorm.DB) error {
				if tx.Migrator().HasColumn(&model.DiskCleanupRecord{}, "failed_count") {
					if err := tx.Migrator().DropColumn(&model.DiskCleanupRecord{}, "failed_count"); err != nil {
						return err
					}
				}
				if tx.Migrator().HasColumn(&model.DiskCleanupRecord{}, "failed_paths") {
					return tx.Migrator().DropColumn(&model.DiskCleanupRecord{}, "failed_paths")
				}
				return nil
			},
		},
		{
			ID: "202607110001", // episode ledger schema and historical backfill
			Migrate: func(tx *gorm.DB) error {
				if err := tx.AutoMigrate(
					&model.Subscription{},
					&model.Download{},
					&model.SubscriptionEpisode{},
					&model.EpisodeResourceCandidate{},
				); err != nil {
					return err
				}
				return backfillSubscriptionEpisodes(tx)
			},
			Rollback: func(tx *gorm.DB) error {
				return nil
			},
		},
	})

	// 设置迁移超时
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("获取底层数据库连接失败: %w", err)
	}
	sqlDB.SetConnMaxLifetime(time.Hour)

	return m.Migrate()
}

type episodeBackfillKey struct {
	subscriptionID uint
	episode        int
}

type episodeBackfillCandidate struct {
	download model.Download
	status   string
	rank     int
}

func backfillSubscriptionEpisodes(tx *gorm.DB) error {
	var subscriptions []model.Subscription
	if err := tx.Find(&subscriptions).Error; err != nil {
		return err
	}

	subscriptionByID := make(map[uint]model.Subscription, len(subscriptions))
	for _, sub := range subscriptions {
		subscriptionByID[sub.ID] = sub
	}

	var downloads []model.Download
	if err := tx.Where("subscription_id > 0 AND episode > 0").
		Order("subscription_id ASC, episode ASC, updated_at DESC, id DESC").
		Find(&downloads).Error; err != nil {
		return err
	}

	bestByEpisode := make(map[episodeBackfillKey]episodeBackfillCandidate)
	for _, download := range downloads {
		sub, ok := subscriptionByID[download.SubscriptionID]
		if !ok {
			continue
		}
		relativeEpisode := sub.RelativeEpisode(download.Episode)
		if relativeEpisode <= 0 {
			continue
		}

		status, rank := episodeStatusFromDownloadStatus(download.Status)
		candidate := episodeBackfillCandidate{download: download, status: status, rank: rank}
		key := episodeBackfillKey{subscriptionID: sub.ID, episode: relativeEpisode}
		if current, exists := bestByEpisode[key]; !exists || betterEpisodeBackfillCandidate(candidate, current) {
			bestByEpisode[key] = candidate
		}
	}

	for key, candidate := range bestByEpisode {
		download := candidate.download
		ledger := model.SubscriptionEpisode{
			SubscriptionID:    key.subscriptionID,
			Episode:           key.episode,
			Status:            candidate.status,
			ActiveDownloadID:  &download.ID,
			ActiveTorrentHash: download.TorrentHash,
			ActiveTorrentURL:  download.TorrentURL,
			ActiveTitle:       download.Title,
			StatusSource:      model.EpisodeStatusSourceMigration,
		}
		if candidate.status == model.EpisodeStatusDownloaded {
			ledger.DownloadedAt = download.DownloadedAt
			if ledger.DownloadedAt == nil && !download.UpdatedAt.IsZero() {
				downloadedAt := download.UpdatedAt
				ledger.DownloadedAt = &downloadedAt
			}
		}
		if err := tx.Where("subscription_id = ? AND episode = ?", key.subscriptionID, key.episode).
			FirstOrCreate(&ledger).Error; err != nil {
			return err
		}
	}

	for _, sub := range subscriptions {
		knownRange := sub.TotalEpisodes
		if knownRange <= 0 {
			knownRange = sub.RelativeLatestEpisode()
		}
		for episode := 1; episode <= knownRange; episode++ {
			ledger := model.SubscriptionEpisode{
				SubscriptionID: sub.ID,
				Episode:        episode,
				Status:         model.EpisodeStatusMissing,
				StatusSource:   model.EpisodeStatusSourceMigration,
			}
			if err := tx.Where("subscription_id = ? AND episode = ?", sub.ID, episode).
				FirstOrCreate(&ledger).Error; err != nil {
				return err
			}
		}
	}

	if err := tx.Model(&model.Download{}).
		Where("purpose = '' OR purpose IS NULL").
		Update("purpose", model.DownloadPurposeNormal).Error; err != nil {
		return err
	}

	return tx.Model(&model.Subscription{}).
		Where("rss_url <> '' AND source_type <> ?", "calendar").
		Update("rss_baseline_pending", true).Error
}

func episodeStatusFromDownloadStatus(status string) (string, int) {
	switch status {
	case model.DownloadStatusCompleted:
		return model.EpisodeStatusDownloaded, 3
	case model.DownloadStatusOrganizing, model.DownloadStatusDownloading, model.DownloadStatusPending, model.DownloadStatusStalled:
		return model.EpisodeStatusDownloading, 2
	case model.DownloadStatusFailed:
		return model.EpisodeStatusMissing, 1
	default:
		return model.EpisodeStatusMissing, 0
	}
}

func betterEpisodeBackfillCandidate(candidate, current episodeBackfillCandidate) bool {
	if candidate.rank != current.rank {
		return candidate.rank > current.rank
	}
	candidateHasRenamedPath := candidate.download.RenamedPath != ""
	currentHasRenamedPath := current.download.RenamedPath != ""
	if candidateHasRenamedPath != currentHasRenamedPath {
		return candidateHasRenamedPath
	}
	if !candidate.download.UpdatedAt.Equal(current.download.UpdatedAt) {
		return candidate.download.UpdatedAt.After(current.download.UpdatedAt)
	}
	return candidate.download.ID > current.download.ID
}

// GetCurrentVersion 获取当前迁移版本
func GetCurrentVersion(db *gorm.DB) (string, error) {
	var migration MigrationRecord
	err := db.Order("id DESC").First(&migration).Error
	if err == gorm.ErrRecordNotFound {
		return "none", nil
	}
	if err != nil {
		return "", err
	}
	return migration.ID, nil
}

// MigrateRSSTimeout 设置现有 RSS 源的默认超时（向后兼容函数）
// Deprecated: 此函数保留用于向后兼容，新的迁移系统不再需要
func MigrateRSSTimeout(db *gorm.DB) error {
	// 此函数现在是一个空操作，因为新的迁移系统已经处理了这个问题
	// 保留此函数是为了不破坏可能调用它的现有测试
	return nil
}

// ResetMigrations 重置所有迁移（危险操作，仅用于开发）
func ResetMigrations(db *gorm.DB) error {
	m := gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{
		{
			ID: "202504090001",
			Migrate: func(tx *gorm.DB) error {
				return tx.AutoMigrate(
					&model.Subscription{},
					&model.Download{},
					&model.SubscriptionEpisode{},
					&model.EpisodeResourceCandidate{},
					&model.Config{},
					&model.DiskSample{},
					&model.DiskCleanupRecord{},
					&model.RSSSource{},
					&model.Log{},
					&model.RefreshToken{},
				)
			},
			Rollback: func(tx *gorm.DB) error {
				return tx.Migrator().DropTable(
					"subscriptions",
					"downloads",
					"subscription_episodes",
					"episode_resource_candidates",
					"configs",
					"rss_sources",
					"logs",
					"refresh_tokens",
				)
			},
		},
	})
	return m.RollbackTo("202504090001")
}
