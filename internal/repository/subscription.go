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

// SubscriptionStatistics 订阅统计信息
type SubscriptionStatistics struct {
	TotalCount        int64 `json:"total_count"`
	ActiveCount       int64 `json:"active_count"`
	DisabledCount     int64 `json:"disabled_count"`
	CompletedCount    int64 `json:"completed_count"`
	WeeklyUpdateCount int64 `json:"weekly_update_count"`
	GroupStats        []GroupStatistic `json:"group_stats"`
}

// GroupStatistic 分组统计
type GroupStatistic struct {
	GroupID   uint   `json:"group_id"`
	GroupName string `json:"group_name"`
	Count     int64  `json:"count"`
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

	// 批量操作
	BatchUpdateEnabled(ids []uint, enabled bool) error
	BatchDelete(ids []uint) error
	BatchUpdateGroup(ids []uint, groupID *uint) error

	// 分组管理
	CreateGroup(group *model.SubscriptionGroup) error
	UpdateGroup(group *model.SubscriptionGroup) error
	DeleteGroup(id uint) error
	GetGroupByID(id uint) (*model.SubscriptionGroup, error)
	ListGroups() ([]model.SubscriptionGroup, error)
	GetDefaultGroup() (*model.SubscriptionGroup, error)

	// 统计
	GetStatistics() (*SubscriptionStatistics, error)
	GetWeeklyUpdates() (int64, error)
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

// BatchUpdateEnabled 批量更新订阅启用状态
func (r *subscriptionRepository) BatchUpdateEnabled(ids []uint, enabled bool) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.Model(&model.Subscription{}).Where("id IN ?", ids).Update("enabled", enabled).Error
}

// BatchDelete 批量删除订阅
func (r *subscriptionRepository) BatchDelete(ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.Delete(&model.Subscription{}, "id IN ?", ids).Error
}

// BatchUpdateGroup 批量更新订阅分组
func (r *subscriptionRepository) BatchUpdateGroup(ids []uint, groupID *uint) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.Model(&model.Subscription{}).Where("id IN ?", ids).Update("group_id", groupID).Error
}

// CreateGroup 创建订阅分组
func (r *subscriptionRepository) CreateGroup(group *model.SubscriptionGroup) error {
	return r.db.Create(group).Error
}

// UpdateGroup 更新订阅分组
func (r *subscriptionRepository) UpdateGroup(group *model.SubscriptionGroup) error {
	return r.db.Save(group).Error
}

// DeleteGroup 删除订阅分组（会将该分组下的订阅移动到默认分组）
func (r *subscriptionRepository) DeleteGroup(id uint) error {
	// 获取默认分组
	defaultGroup, err := r.GetDefaultGroup()
	if err != nil {
		return err
	}

	return r.db.Transaction(func(tx *gorm.DB) error {
		// 将该分组下的订阅移动到默认分组
		if err := tx.Model(&model.Subscription{}).Where("group_id = ?", id).Update("group_id", defaultGroup.ID).Error; err != nil {
			return err
		}
		// 删除分组
		return tx.Delete(&model.SubscriptionGroup{}, id).Error
	})
}

// GetGroupByID 根据 ID 获取分组
func (r *subscriptionRepository) GetGroupByID(id uint) (*model.SubscriptionGroup, error) {
	var group model.SubscriptionGroup
	err := r.db.First(&group, id).Error
	if err != nil {
		return nil, err
	}
	return &group, nil
}

// ListGroups 获取所有分组
func (r *subscriptionRepository) ListGroups() ([]model.SubscriptionGroup, error) {
	var groups []model.SubscriptionGroup
	err := r.db.Order("sort_order ASC, id ASC").Find(&groups).Error
	return groups, err
}

// GetDefaultGroup 获取默认分组（如果不存在则创建一个）
func (r *subscriptionRepository) GetDefaultGroup() (*model.SubscriptionGroup, error) {
	var group model.SubscriptionGroup
	err := r.db.Where("is_default = ?", true).First(&group).Error
	if err == nil {
		return &group, nil
	}

	if err == gorm.ErrRecordNotFound {
		// 创建默认分组
		defaultGroup := &model.SubscriptionGroup{
			Name:      "默认分组",
			Color:     "#18a058",
			IsDefault: true,
			SortOrder: 0,
		}
		if err := r.db.Create(defaultGroup).Error; err != nil {
			return nil, err
		}
		return defaultGroup, nil
	}

	return nil, err
}

// GetStatistics 获取订阅统计信息
func (r *subscriptionRepository) GetStatistics() (*SubscriptionStatistics, error) {
	var stats SubscriptionStatistics

	// 总订阅数
	if err := r.db.Model(&model.Subscription{}).Count(&stats.TotalCount).Error; err != nil {
		return nil, err
	}

	// 启用/禁用统计
	if err := r.db.Model(&model.Subscription{}).Where("enabled = ?", true).Count(&stats.ActiveCount).Error; err != nil {
		return nil, err
	}
	stats.DisabledCount = stats.TotalCount - stats.ActiveCount

	// 已完结统计（TotalEpisodes > 0 且 CurrentEpisode >= TotalEpisodes）
	if err := r.db.Model(&model.Subscription{}).
		Where("total_episodes > 0 AND current_episode >= total_episodes").
		Count(&stats.CompletedCount).Error; err != nil {
		return nil, err
	}

	// 本周更新数
	weeklyUpdates, err := r.GetWeeklyUpdates()
	if err != nil {
		return nil, err
	}
	stats.WeeklyUpdateCount = weeklyUpdates

	// 分组统计
	var groupStats []GroupStatistic
	if err := r.db.Model(&model.Subscription{}).
		Select("subscription_groups.id as group_id, subscription_groups.name as group_name, COUNT(subscriptions.id) as count").
		Joins("LEFT JOIN subscription_groups ON subscriptions.group_id = subscription_groups.id").
		Group("subscription_groups.id, subscription_groups.name").
		Scan(&groupStats).Error; err != nil {
		return nil, err
	}
	stats.GroupStats = groupStats

	return &stats, nil
}

// GetWeeklyUpdates 获取本周更新的订阅数
func (r *subscriptionRepository) GetWeeklyUpdates() (int64, error) {
	var count int64
	// 获取本周内最后下载时间在这一周的订阅
	err := r.db.Model(&model.Subscription{}).
		Where("last_download_at >= datetime('now', '-7 days')").
		Count(&count).Error
	return count, err
}
