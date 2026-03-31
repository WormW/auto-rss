package repository

import (
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"gorm.io/gorm"
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
	return r.db.Delete(&model.Download{}, id).Error
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
	return r.db.Delete(&model.Download{}, ids).Error
}

// DeleteByStatus 按状态删除下载任务
func (r *downloadRepository) DeleteByStatus(status string) error {
	return r.db.Where("status = ?", status).Delete(&model.Download{}).Error
}

// DeleteAll 删除所有下载任务
func (r *downloadRepository) DeleteAll() error {
	return r.db.Where("1 = 1").Delete(&model.Download{}).Error
}

// GetFailedDownloadsReadyForRetry 获取准备好重试的失败下载任务
// 条件：状态为 failed，且下次重试时间已到达或为空，且未达到最大重试次数
func (r *downloadRepository) GetFailedDownloadsReadyForRetry(limit int) ([]model.Download, error) {
	var downloads []model.Download
	now := time.Now()

	err := r.db.Where("status = ? AND retry_count < max_retries AND (next_retry_at IS NULL OR next_retry_at <= ?)",
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
