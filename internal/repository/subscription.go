package repository

import (
	"strings"

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
	TotalCount        int64            `json:"total_count"`
	ActiveCount       int64            `json:"active_count"`
	DisabledCount     int64            `json:"disabled_count"`
	CompletedCount    int64            `json:"completed_count"`
	WeeklyUpdateCount int64            `json:"weekly_update_count"`
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
	// CreateInTx 在调用方事务中创建订阅
	CreateInTx(tx *gorm.DB, subscription *model.Subscription) error
	Update(subscription *model.Subscription) error
	Delete(id uint) error
	GetByID(id uint) (*model.Subscription, error)
	GetByRSSURL(rssURL string) (*model.Subscription, error)
	GetByRSSURLAndSeason(rssURL string, season int) (*model.Subscription, error)
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

	// 搜索
	SearchSubscriptions(query string, groupID *uint, tagIDs []uint, enabled *bool, offset, limit int) ([]model.Subscription, int64, error)

	// 标签管理
	CreateTag(tag *model.SubscriptionTag) error
	UpdateTag(tag *model.SubscriptionTag) error
	DeleteTag(id uint) error
	GetTagByID(id uint) (*model.SubscriptionTag, error)
	GetTagByName(name string) (*model.SubscriptionTag, error)
	ListTags() ([]model.SubscriptionTag, error)
	AddTagsToSubscription(subscriptionID uint, tagIDs []uint) error
	RemoveTagsFromSubscription(subscriptionID uint, tagIDs []uint) error
	GetSubscriptionTags(subscriptionID uint) ([]model.SubscriptionTag, error)
	GetSubscriptionsByTag(tagID uint) ([]model.Subscription, error)
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

// CreateInTx 在调用方事务中创建订阅
func (r *subscriptionRepository) CreateInTx(tx *gorm.DB, subscription *model.Subscription) error {
	return tx.Create(subscription).Error
}

// Update 更新订阅
func (r *subscriptionRepository) Update(subscription *model.Subscription) error {
	return r.db.Save(subscription).Error
}

// Delete 删除订阅
func (r *subscriptionRepository) Delete(id uint) error {
	return r.deleteByIDs([]uint{id})
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

// GetByRSSURLAndSeason 根据 RSS URL 和 Season 获取订阅
func (r *subscriptionRepository) GetByRSSURLAndSeason(rssURL string, season int) (*model.Subscription, error) {
	var subscription model.Subscription
	err := r.db.Where("rss_url = ? AND season = ?", rssURL, season).First(&subscription).Error
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
	return r.deleteByIDs(ids)
}

func (r *subscriptionRepository) deleteByIDs(ids []uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if tx.Migrator().HasTable(&model.SubscriptionEpisode{}) {
			if tx.Migrator().HasTable(&model.EpisodeResourceCandidate{}) {
				if err := tx.Where(
					"subscription_episode_id IN (?)",
					tx.Model(&model.SubscriptionEpisode{}).Select("id").Where("subscription_id IN ?", ids),
				).Delete(&model.EpisodeResourceCandidate{}).Error; err != nil {
					return err
				}
			}
			if err := tx.Where("subscription_id IN ?", ids).Delete(&model.SubscriptionEpisode{}).Error; err != nil {
				return err
			}
		}
		return tx.Where("id IN ?", ids).Delete(&model.Subscription{}).Error
	})
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

	// 已完结统计使用季度内相对集数，避免偏移订阅被提前计为完结。
	if err := r.db.Model(&model.Subscription{}).
		Where("total_episodes > 0 AND CASE WHEN episode_offset > 0 AND current_episode > episode_offset THEN current_episode - episode_offset WHEN episode_offset <= 0 THEN current_episode ELSE 0 END >= total_episodes").
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

// ==================== 标签管理 ====================

// CreateTag 创建标签
func (r *subscriptionRepository) CreateTag(tag *model.SubscriptionTag) error {
	return r.db.Create(tag).Error
}

// UpdateTag 更新标签
func (r *subscriptionRepository) UpdateTag(tag *model.SubscriptionTag) error {
	return r.db.Save(tag).Error
}

// DeleteTag 删除标签
func (r *subscriptionRepository) DeleteTag(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		// 先删除关联关系
		if err := tx.Where("tag_id = ?", id).Delete(&model.SubscriptionTagRelation{}).Error; err != nil {
			return err
		}
		// 删除标签
		return tx.Delete(&model.SubscriptionTag{}, id).Error
	})
}

// GetTagByID 根据ID获取标签
func (r *subscriptionRepository) GetTagByID(id uint) (*model.SubscriptionTag, error) {
	var tag model.SubscriptionTag
	err := r.db.First(&tag, id).Error
	if err != nil {
		return nil, err
	}
	return &tag, nil
}

// GetTagByName 根据名称获取标签
func (r *subscriptionRepository) GetTagByName(name string) (*model.SubscriptionTag, error) {
	var tag model.SubscriptionTag
	err := r.db.Where("name = ?", name).First(&tag).Error
	if err != nil {
		return nil, err
	}
	return &tag, nil
}

// ListTags 获取所有标签
func (r *subscriptionRepository) ListTags() ([]model.SubscriptionTag, error) {
	var tags []model.SubscriptionTag
	err := r.db.Order("sort_order ASC, id ASC").Find(&tags).Error
	return tags, err
}

// AddTagsToSubscription 为订阅添加标签
func (r *subscriptionRepository) AddTagsToSubscription(subscriptionID uint, tagIDs []uint) error {
	if len(tagIDs) == 0 {
		return nil
	}

	return r.db.Transaction(func(tx *gorm.DB) error {
		for _, tagID := range tagIDs {
			relation := model.SubscriptionTagRelation{
				SubscriptionID: subscriptionID,
				TagID:          tagID,
			}
			// 忽略重复插入的错误
			if err := tx.Create(&relation).Error; err != nil {
				// 如果是重复键错误，忽略
				if !strings.Contains(err.Error(), "UNIQUE constraint failed") {
					return err
				}
			}
		}
		return nil
	})
}

// RemoveTagsFromSubscription 从订阅移除标签
func (r *subscriptionRepository) RemoveTagsFromSubscription(subscriptionID uint, tagIDs []uint) error {
	if len(tagIDs) == 0 {
		return nil
	}
	return r.db.Where("subscription_id = ? AND tag_id IN ?", subscriptionID, tagIDs).
		Delete(&model.SubscriptionTagRelation{}).Error
}

// GetSubscriptionTags 获取订阅的标签列表
func (r *subscriptionRepository) GetSubscriptionTags(subscriptionID uint) ([]model.SubscriptionTag, error) {
	var tags []model.SubscriptionTag
	err := r.db.Table("subscription_tags").
		Select("subscription_tags.*").
		Joins("JOIN subscription_tag_relations ON subscription_tag_relations.tag_id = subscription_tags.id").
		Where("subscription_tag_relations.subscription_id = ?", subscriptionID).
		Order("subscription_tags.sort_order ASC").
		Find(&tags).Error
	return tags, err
}

// GetSubscriptionsByTag 获取带有指定标签的所有订阅
func (r *subscriptionRepository) GetSubscriptionsByTag(tagID uint) ([]model.Subscription, error) {
	var subscriptions []model.Subscription
	err := r.db.Table("subscriptions").
		Select("subscriptions.*").
		Joins("JOIN subscription_tag_relations ON subscription_tag_relations.subscription_id = subscriptions.id").
		Where("subscription_tag_relations.tag_id = ?", tagID).
		Find(&subscriptions).Error
	return subscriptions, err
}

// SearchSubscriptions 搜索订阅（支持关键词、标签、分组筛选）
func (r *subscriptionRepository) SearchSubscriptions(query string, groupID *uint, tagIDs []uint, enabled *bool, offset, limit int) ([]model.Subscription, int64, error) {
	var subscriptions []model.Subscription
	var total int64

	db := r.db.Model(&model.Subscription{})

	// 关键词搜索
	if query != "" {
		db = db.Where("name LIKE ? OR fansub LIKE ?", "%"+query+"%", "%"+query+"%")
	}

	// 分组筛选
	if groupID != nil {
		db = db.Where("group_id = ?", *groupID)
	}

	// 标签筛选
	if len(tagIDs) > 0 {
		db = db.Table("subscriptions").
			Select("subscriptions.*").
			Joins("JOIN subscription_tag_relations ON subscription_tag_relations.subscription_id = subscriptions.id").
			Where("subscription_tag_relations.tag_id IN ?", tagIDs).
			Group("subscriptions.id")
	}

	// 启用状态筛选
	if enabled != nil {
		db = db.Where("enabled = ?", *enabled)
	}

	// 获取总数
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页
	if limit <= 0 {
		limit = DefaultPageSize
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}
	if offset < 0 {
		offset = 0
	}

	err := db.Offset(offset).Limit(limit).Find(&subscriptions).Error
	return subscriptions, total, err
}
