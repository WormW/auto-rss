package repository

import (
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
	List(offset, limit int, status string) ([]model.Download, int64, error)
	ListBySubscriptionID(subscriptionID uint) ([]model.Download, error)
	UpdateStatus(id uint, status string) error
	BatchDelete(ids []uint) error
	DeleteByStatus(status string) error
	DeleteAll() error
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
