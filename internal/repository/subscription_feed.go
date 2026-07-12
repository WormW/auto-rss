package repository

import (
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SubscriptionFeedRepository interface {
	ListBySubscription(subscriptionID uint) ([]model.SubscriptionFeed, error)
	ListBySubscriptionIDs(subscriptionIDs []uint) ([]model.SubscriptionFeed, error)
	ListEnabledBySubscriptionIDs(subscriptionIDs []uint) ([]model.SubscriptionFeed, error)
	GetByID(id uint) (*model.SubscriptionFeed, error)
	Create(feed *model.SubscriptionFeed) error
	CreateInTx(tx *gorm.DB, feed *model.SubscriptionFeed) error
	Update(feed *model.SubscriptionFeed) error
	UpdateInTx(tx *gorm.DB, feed *model.SubscriptionFeed) error
	Delete(id uint) error
	DeleteInTx(tx *gorm.DB, id uint) error
	CountBySubscription(subscriptionID uint) (int64, error)
	HasSeenItem(feedID uint, resourceKey string) (bool, error)
	MarkSeenItem(feedID uint, resourceKey string, originalEpisode int, firstSeenAt time.Time) error
	UpdateCheckSuccess(id uint, checkedAt time.Time, maxPubTime *time.Time, baselineComplete bool) error
	UpdateCheckFailure(id uint, checkedAt time.Time, message string) error
}

type subscriptionFeedRepository struct {
	db *gorm.DB
}

func NewSubscriptionFeedRepository(db *gorm.DB) SubscriptionFeedRepository {
	return &subscriptionFeedRepository{db: db}
}

func (r *subscriptionFeedRepository) ListBySubscription(subscriptionID uint) ([]model.SubscriptionFeed, error) {
	var feeds []model.SubscriptionFeed
	err := r.db.Where("subscription_id = ?", subscriptionID).Order("created_at ASC, id ASC").Find(&feeds).Error
	return feeds, err
}

func (r *subscriptionFeedRepository) ListBySubscriptionIDs(subscriptionIDs []uint) ([]model.SubscriptionFeed, error) {
	if len(subscriptionIDs) == 0 {
		return []model.SubscriptionFeed{}, nil
	}
	var feeds []model.SubscriptionFeed
	err := r.db.Where("subscription_id IN ?", subscriptionIDs).
		Order("subscription_id ASC, id ASC").Find(&feeds).Error
	return feeds, err
}

func (r *subscriptionFeedRepository) ListEnabledBySubscriptionIDs(subscriptionIDs []uint) ([]model.SubscriptionFeed, error) {
	if len(subscriptionIDs) == 0 {
		return []model.SubscriptionFeed{}, nil
	}
	var feeds []model.SubscriptionFeed
	err := r.db.Where("enabled = ? AND subscription_id IN ?", true, subscriptionIDs).
		Order("subscription_id ASC, id ASC").Find(&feeds).Error
	return feeds, err
}

func (r *subscriptionFeedRepository) GetByID(id uint) (*model.SubscriptionFeed, error) {
	var feed model.SubscriptionFeed
	if err := r.db.First(&feed, id).Error; err != nil {
		return nil, err
	}
	return &feed, nil
}

func (r *subscriptionFeedRepository) Create(feed *model.SubscriptionFeed) error {
	return r.CreateInTx(r.db, feed)
}

func (r *subscriptionFeedRepository) CreateInTx(tx *gorm.DB, feed *model.SubscriptionFeed) error {
	enabled := feed.Enabled
	baselinePending := feed.BaselinePending
	if err := tx.Create(feed).Error; err != nil {
		return err
	}
	if err := tx.Model(feed).UpdateColumns(map[string]any{
		"enabled":          enabled,
		"baseline_pending": baselinePending,
	}).Error; err != nil {
		return err
	}
	feed.Enabled = enabled
	feed.BaselinePending = baselinePending
	return nil
}

func (r *subscriptionFeedRepository) Update(feed *model.SubscriptionFeed) error {
	return r.UpdateInTx(r.db, feed)
}

func (r *subscriptionFeedRepository) UpdateInTx(tx *gorm.DB, feed *model.SubscriptionFeed) error {
	return tx.Select("*").Save(feed).Error
}

func (r *subscriptionFeedRepository) Delete(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		return r.DeleteInTx(tx, id)
	})
}

func (r *subscriptionFeedRepository) DeleteInTx(tx *gorm.DB, id uint) error {
	if err := tx.Where("subscription_feed_id = ?", id).Delete(&model.SubscriptionFeedSeenItem{}).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.Download{}).Where("subscription_feed_id = ?", id).
		Update("subscription_feed_id", nil).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.EpisodeResourceCandidate{}).Where("subscription_feed_id = ?", id).
		Update("subscription_feed_id", nil).Error; err != nil {
		return err
	}
	return tx.Delete(&model.SubscriptionFeed{}, id).Error
}

func (r *subscriptionFeedRepository) CountBySubscription(subscriptionID uint) (int64, error) {
	var count int64
	err := r.db.Model(&model.SubscriptionFeed{}).Where("subscription_id = ?", subscriptionID).Count(&count).Error
	return count, err
}

func (r *subscriptionFeedRepository) HasSeenItem(feedID uint, resourceKey string) (bool, error) {
	var count int64
	err := r.db.Model(&model.SubscriptionFeedSeenItem{}).
		Where("subscription_feed_id = ? AND resource_key = ?", feedID, resourceKey).
		Limit(1).Count(&count).Error
	return count > 0, err
}

func (r *subscriptionFeedRepository) MarkSeenItem(feedID uint, resourceKey string, originalEpisode int, firstSeenAt time.Time) error {
	seen := model.SubscriptionFeedSeenItem{
		SubscriptionFeedID: feedID,
		ResourceKey:        resourceKey,
		OriginalEpisode:    originalEpisode,
		FirstSeenAt:        firstSeenAt,
	}
	return r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&seen).Error
}

func (r *subscriptionFeedRepository) UpdateCheckSuccess(
	id uint,
	checkedAt time.Time,
	maxPubTime *time.Time,
	baselineComplete bool,
) error {
	updates := map[string]any{
		"last_check_time": checkedAt,
		"last_success_at": checkedAt,
		"last_error":      "",
	}
	if maxPubTime != nil {
		updates["last_rss_pub_time"] = gorm.Expr(
			"CASE WHEN last_rss_pub_time IS NULL OR last_rss_pub_time < ? THEN ? ELSE last_rss_pub_time END",
			*maxPubTime,
			*maxPubTime,
		)
	}
	if baselineComplete {
		updates["baseline_pending"] = false
	}
	return r.db.Model(&model.SubscriptionFeed{}).Where("id = ?", id).Updates(updates).Error
}

func (r *subscriptionFeedRepository) UpdateCheckFailure(id uint, checkedAt time.Time, message string) error {
	return r.db.Model(&model.SubscriptionFeed{}).Where("id = ?", id).Updates(map[string]any{
		"last_check_time": checkedAt,
		"last_error":      message,
	}).Error
}
