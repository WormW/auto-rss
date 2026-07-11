package repository

import (
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"gorm.io/gorm"
)

const (
	MaxPageSize         = 1000
	DefaultPageSize     = 20
	bulkDeleteChunkSize = 500
)

// DownloadRepository 下载仓储接口
type DownloadRepository interface {
	Create(download *model.Download) error
	Update(download *model.Download) error
	Delete(id uint) error
	GetByID(id uint) (*model.Download, error)
	GetByHash(hash string) (*model.Download, error)
	GetBySubscriptionAndEpisode(subscriptionID uint, episode int) (*model.Download, error)
	// GetBySubscriptionAndEpisodeWithLang 根据订阅 ID 和集数获取所有下载任务（支持多语言）
	GetBySubscriptionAndEpisodeWithLang(subscriptionID uint, episode int) ([]model.Download, error)
	// GetRecentBySubscription 获取订阅的最近 N 条下载记录
	GetRecentBySubscription(subscriptionID uint, limit int) ([]model.Download, error)
	List(offset, limit int, status string) ([]model.Download, int64, error)
	ListBySubscriptionID(subscriptionID uint) ([]model.Download, error)
	UpdateStatus(id uint, status string) error
	BatchDelete(ids []uint) error
	DeleteByStatus(status string) error
	DeleteAll() error
	// GetFailedDownloadsReadyForRetry 获取准备好重试的失败下载任务
	GetFailedDownloadsReadyForRetry(limit int) ([]model.Download, error)
	// GetDownloadsByRetryCount 获取指定重试次数的下载任务
	GetDownloadsByRetryCount(minRetries, maxRetries int) ([]model.Download, error)
	// CreateInTx 在事务中创建下载任务
	CreateInTx(tx *gorm.DB, download *model.Download) error
	// UpdateInTx 在事务中更新下载任务
	UpdateInTx(tx *gorm.DB, download *model.Download) error

	// 下载历史记录
	GetDownloadHistory(filter *DownloadHistoryFilter, offset, limit int) ([]model.Download, int64, error)

	// 下载统计
	GetDownloadStatistics(days int) (*DownloadStatistics, error)
}

type downloadRepository struct {
	db *gorm.DB
}

// NewDownloadRepository 创建下载仓储实例
func NewDownloadRepository(db *gorm.DB) DownloadRepository {
	return &downloadRepository{db: db}
}

// Create 创建下载任务
func (r *downloadRepository) Create(download *model.Download) error {
	return r.db.Create(download).Error
}

// Update 更新下载任务
func (r *downloadRepository) Update(download *model.Download) error {
	return r.db.Save(download).Error
}

// Delete 删除下载任务
func (r *downloadRepository) Delete(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := detachEpisodeDownloadsInTx(tx, "id = ?", id); err != nil {
			return err
		}
		return tx.Delete(&model.Download{}, id).Error
	})
}

// GetByID 根据 ID 获取下载任务
func (r *downloadRepository) GetByID(id uint) (*model.Download, error) {
	var download model.Download
	err := r.db.Preload("Subscription").First(&download, id).Error
	if err != nil {
		return nil, err
	}
	return &download, nil
}

// GetByHash 根据种子哈希获取下载任务
func (r *downloadRepository) GetByHash(hash string) (*model.Download, error) {
	var download model.Download
	err := r.db.Where("torrent_hash = ?", hash).First(&download).Error
	if err != nil {
		return nil, err
	}
	return &download, nil
}

// List 获取下载任务列表
func (r *downloadRepository) List(offset, limit int, status string) ([]model.Download, int64, error) {
	var downloads []model.Download
	var total int64

	// Enforce pagination limits
	if limit <= 0 {
		limit = DefaultPageSize
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}
	if offset < 0 {
		offset = 0
	}

	query := r.db.Model(&model.Download{})
	if status != "" {
		query = query.Where("status = ?", status)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := query.Preload("Subscription").Offset(offset).Limit(limit).
		Order("created_at DESC").Find(&downloads).Error
	return downloads, total, err
}

// GetBySubscriptionAndEpisode 根据订阅 ID 和集数获取下载任务
func (r *downloadRepository) GetBySubscriptionAndEpisode(subscriptionID uint, episode int) (*model.Download, error) {
	var download model.Download
	err := r.db.Where("subscription_id = ? AND episode = ?", subscriptionID, episode).First(&download).Error
	if err != nil {
		return nil, err
	}
	return &download, nil
}

// GetBySubscriptionAndEpisodeWithLang 根据订阅 ID 和集数获取所有下载任务（支持多语言）
// 返回该订阅该集数的所有语言版本
func (r *downloadRepository) GetBySubscriptionAndEpisodeWithLang(subscriptionID uint, episode int) ([]model.Download, error) {
	var downloads []model.Download
	err := r.db.Where("subscription_id = ? AND episode = ?", subscriptionID, episode).
		Order("created_at DESC").Find(&downloads).Error
	return downloads, err
}

// GetRecentBySubscription 获取订阅的最近 N 条下载记录
func (r *downloadRepository) GetRecentBySubscription(subscriptionID uint, limit int) ([]model.Download, error) {
	var downloads []model.Download
	err := r.db.Where("subscription_id = ?", subscriptionID).
		Order("created_at DESC").Limit(limit).Find(&downloads).Error
	return downloads, err
}

// ListBySubscriptionID 根据订阅 ID 获取下载任务列表
func (r *downloadRepository) ListBySubscriptionID(subscriptionID uint) ([]model.Download, error) {
	var downloads []model.Download
	err := r.db.Where("subscription_id = ?", subscriptionID).
		Order("created_at DESC").Find(&downloads).Error
	return downloads, err
}

// UpdateStatus 更新下载状态
func (r *downloadRepository) UpdateStatus(id uint, status string) error {
	return r.db.Model(&model.Download{}).Where("id = ?", id).
		Update("status", status).Error
}

// BatchDelete 批量删除下载任务
func (r *downloadRepository) BatchDelete(ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		for start := 0; start < len(ids); start += bulkDeleteChunkSize {
			end := start + bulkDeleteChunkSize
			if end > len(ids) {
				end = len(ids)
			}
			chunk := ids[start:end]
			if err := detachEpisodeDownloadsInTx(tx, "id IN ?", chunk); err != nil {
				return err
			}
			if err := tx.Where("id IN ?", chunk).Delete(&model.Download{}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteByStatus 按状态删除下载任务
func (r *downloadRepository) DeleteByStatus(status string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := detachEpisodeDownloadsInTx(tx, "status = ?", status); err != nil {
			return err
		}
		return tx.Where("status = ?", status).Delete(&model.Download{}).Error
	})
}

// DeleteAll 删除所有下载任务
func (r *downloadRepository) DeleteAll() error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := detachEpisodeDownloadsInTx(tx, "1 = 1"); err != nil {
			return err
		}
		return tx.Where("1 = 1").Delete(&model.Download{}).Error
	})
}

func detachEpisodeDownloadsInTx(tx *gorm.DB, downloadWhere string, args ...any) error {
	if !tx.Migrator().HasTable(&model.SubscriptionEpisode{}) {
		return nil
	}

	downloadIDs := tx.Model(&model.Download{}).Select("id").Where(downloadWhere, args...)

	if err := tx.Model(&model.SubscriptionEpisode{}).
		Where("active_download_id IN (?) AND status = ?", downloadIDs, model.EpisodeStatusDownloading).
		Updates(map[string]any{
			"status":              model.EpisodeStatusMissing,
			"status_source":       model.EpisodeStatusSourceAutomatic,
			"active_download_id":  nil,
			"active_torrent_hash": "",
			"active_torrent_url":  "",
			"active_title":        "",
			"downloaded_at":       nil,
		}).Error; err != nil {
		return err
	}

	return tx.Model(&model.SubscriptionEpisode{}).
		Where("active_download_id IN (?)", downloadIDs).
		Update("active_download_id", nil).Error
}

// GetFailedDownloadsReadyForRetry 获取准备好重试的失败下载任务
// 条件：状态为 failed，且下次重试时间已到达或为空，且未达到最大重试次数
func (r *downloadRepository) GetFailedDownloadsReadyForRetry(limit int) ([]model.Download, error) {
	var downloads []model.Download
	now := time.Now()

	err := r.db.Where("status = ? AND (max_retries = 0 OR retry_count < max_retries) AND (next_retry_at IS NULL OR next_retry_at <= ?)",
		"failed", now).
		Order("next_retry_at ASC").
		Limit(limit).
		Find(&downloads).Error

	return downloads, err
}

// GetDownloadsByRetryCount 获取指定重试次数范围的下载任务
func (r *downloadRepository) GetDownloadsByRetryCount(minRetries, maxRetries int) ([]model.Download, error) {
	var downloads []model.Download
	err := r.db.Where("retry_count >= ? AND retry_count <= ?", minRetries, maxRetries).
		Order("retry_count DESC").Find(&downloads).Error
	return downloads, err
}

// CreateInTx 在事务中创建下载任务
func (r *downloadRepository) CreateInTx(tx *gorm.DB, download *model.Download) error {
	return tx.Create(download).Error
}

// UpdateInTx 在事务中更新下载任务
func (r *downloadRepository) UpdateInTx(tx *gorm.DB, download *model.Download) error {
	return tx.Save(download).Error
}

// ==================== 下载历史记录 ====================

// DownloadHistoryFilter 下载历史筛选条件
type DownloadHistoryFilter struct {
	SubscriptionID *uint
	Status         string
	StartDate      *time.Time
	EndDate        *time.Time
}

// GetDownloadHistory 获取下载历史记录（支持筛选和分页）
func (r *downloadRepository) GetDownloadHistory(filter *DownloadHistoryFilter, offset, limit int) ([]model.Download, int64, error) {
	var downloads []model.Download
	var total int64

	query := r.db.Model(&model.Download{}).Preload("Subscription")

	if filter != nil {
		if filter.SubscriptionID != nil {
			query = query.Where("subscription_id = ?", *filter.SubscriptionID)
		}
		if filter.Status != "" {
			query = query.Where("status = ?", filter.Status)
		}
		if filter.StartDate != nil {
			query = query.Where("created_at >= ?", *filter.StartDate)
		}
		if filter.EndDate != nil {
			query = query.Where("created_at <= ?", *filter.EndDate)
		}
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if limit <= 0 {
		limit = DefaultPageSize
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}
	if offset < 0 {
		offset = 0
	}

	err := query.Offset(offset).Limit(limit).Order("created_at DESC").Find(&downloads).Error
	return downloads, total, err
}

// ==================== 下载统计 ====================

// DownloadStatistics 下载统计信息
type DownloadStatistics struct {
	TotalCount        int64              `json:"total_count"`
	CompletedCount    int64              `json:"completed_count"`
	FailedCount       int64              `json:"failed_count"`
	DownloadingCount  int64              `json:"downloading_count"`
	PendingCount      int64              `json:"pending_count"`
	DailyStats        []DailyStat        `json:"daily_stats"`
	SubscriptionStats []SubscriptionStat `json:"subscription_stats"`
}

// DailyStat 每日统计
type DailyStat struct {
	Date      string `json:"date"`
	Count     int64  `json:"count"`
	Completed int64  `json:"completed"`
	Failed    int64  `json:"failed"`
	TotalSize int64  `json:"total_size"`
}

// SubscriptionStat 订阅下载统计
type SubscriptionStat struct {
	SubscriptionID uint   `json:"subscription_id"`
	Name           string `json:"name"`
	Count          int64  `json:"count"`
}

// GetDownloadStatistics 获取下载统计数据
func (r *downloadRepository) GetDownloadStatistics(days int) (*DownloadStatistics, error) {
	var stats DownloadStatistics

	// 获取各状态数量
	type StatusCount struct {
		Status string
		Count  int64
	}
	var statusCounts []StatusCount
	err := r.db.Model(&model.Download{}).
		Select("status, COUNT(*) as count").
		Group("status").
		Scan(&statusCounts).Error
	if err != nil {
		return nil, err
	}

	for _, sc := range statusCounts {
		switch sc.Status {
		case "completed":
			stats.CompletedCount = sc.Count
		case "failed":
			stats.FailedCount = sc.Count
		case "downloading":
			stats.DownloadingCount = sc.Count
		case "pending":
			stats.PendingCount = sc.Count
		}
		stats.TotalCount += sc.Count
	}

	// 获取每日统计
	if days > 0 {
		err = r.db.Raw(`
			SELECT
				DATE(created_at) as date,
				COUNT(*) as count,
				SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END) as completed,
				SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) as failed,
				0 as total_size
			FROM downloads
			WHERE created_at >= DATE('now', '-' || ? || ' days')
			GROUP BY DATE(created_at)
			ORDER BY date DESC
		`, days).Scan(&stats.DailyStats).Error
		if err != nil {
			return nil, err
		}
	}

	// 获取订阅下载排行 Top 10
	err = r.db.Raw(`
		SELECT
			subscription_id,
			COUNT(*) as count
		FROM downloads
		GROUP BY subscription_id
		ORDER BY count DESC
		LIMIT 10
	`).Scan(&stats.SubscriptionStats).Error
	if err != nil {
		return nil, err
	}

	// 填充订阅名称
	for i := range stats.SubscriptionStats {
		var sub model.Subscription
		if err := r.db.Select("name").First(&sub, stats.SubscriptionStats[i].SubscriptionID).Error; err == nil {
			stats.SubscriptionStats[i].Name = sub.Name
		}
	}

	return &stats, nil
}
