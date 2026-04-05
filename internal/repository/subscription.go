package repository

import (
	"github.com/WormW/auto-rss/internal/model"
	"gorm.io/gorm"
)

// SubscriptionWithStats extends Subscription with download statistics
type SubscriptionWithStats struct {
	model.Subscription
	DownloadingCount int64 `json:"downloading_count" gorm:"column:downloading_count"`
}

// SubscriptionRepository 订阅仓储接口
type SubscriptionRepository interface {
	Create(subscription *model.Subscription) error
	Update(subscription *model.Subscription) error
	Delete(id uint) error
	GetByID(id uint) (*model.Subscription, error)
	GetByRSSURL(rssURL string) (*model.Subscription, error)
	List(offset, limit int) ([]model.Subscription, int64, error)
	GetActiveSubscriptions() ([]model.Subscription, error)
	// UpdateInTx 在事务中更新订阅
	UpdateInTx(tx *gorm.DB, subscription *model.Subscription) error
	// GetSubscriptionsWithDownloadCount returns all subscriptions with downloading counts in a single query
	GetSubscriptionsWithDownloadCount() ([]SubscriptionWithStats, error)
}

type subscriptionRepository struct {
	db *gorm.DB
}

// NewSubscriptionRepository 创建订阅仓储实例
func NewSubscriptionRepository(db *gorm.DB) SubscriptionRepository {
	return &subscriptionRepository{db: db}
}

// Create 创建订阅
func (r *subscriptionRepository) Create(subscription *model.Subscription) error {
	return r.db.Create(subscription).Error
}

// Update 更新订阅
func (r *subscriptionRepository) Update(subscription *model.Subscription) error {
	return r.db.Save(subscription).Error
}

// Delete 删除订阅
func (r *subscriptionRepository) Delete(id uint) error {
	return r.db.Delete(&model.Subscription{}, id).Error
}

// GetByID 根据 ID 获取订阅
func (r *subscriptionRepository) GetByID(id uint) (*model.Subscription, error) {
	var subscription model.Subscription
	err := r.db.First(&subscription, id).Error
	if err != nil {
		return nil, err
	}
	return &subscription, nil
}

// GetByRSSURL 根据 RSS URL 获取订阅
func (r *subscriptionRepository) GetByRSSURL(rssURL string) (*model.Subscription, error) {
	var subscription model.Subscription
	err := r.db.Where("rss_url = ?", rssURL).First(&subscription).Error
	if err != nil {
		return nil, err
	}
	return &subscription, nil
}

// List 获取订阅列表
func (r *subscriptionRepository) List(offset, limit int) ([]model.Subscription, int64, error) {
	var subscriptions []model.Subscription
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

	if err := r.db.Model(&model.Subscription{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := r.db.Offset(offset).Limit(limit).Find(&subscriptions).Error
	return subscriptions, total, err
}

// GetActiveSubscriptions 获取所有激活的订阅
func (r *subscriptionRepository) GetActiveSubscriptions() ([]model.Subscription, error) {
	var subscriptions []model.Subscription
	err := r.db.Where("status = ?", "active").Find(&subscriptions).Error
	return subscriptions, err
}

// UpdateInTx 在事务中更新订阅
func (r *subscriptionRepository) UpdateInTx(tx *gorm.DB, subscription *model.Subscription) error {
	return tx.Save(subscription).Error
}

// GetSubscriptionsWithDownloadCount returns all subscriptions with downloading counts in a single query
func (r *subscriptionRepository) GetSubscriptionsWithDownloadCount() ([]SubscriptionWithStats, error) {
	var results []SubscriptionWithStats

	err := r.db.Model(&model.Subscription{}).
		Select(
			"subscriptions.*",
			"COUNT(CASE WHEN downloads.status = 'downloading' THEN 1 END) as downloading_count",
		).
		Joins("LEFT JOIN downloads ON downloads.subscription_id = subscriptions.id").
		Group("subscriptions.id").
		Find(&results).Error

	return results, err
}
